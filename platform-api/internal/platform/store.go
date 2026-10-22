package platform

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"mini-operador-ti/platform-api/internal/identity"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store interface {
	List(context.Context, string, int, int) ([]Application, error)
	Create(context.Context, CreateApplication) (Application, error)
	Get(context.Context, string) (Application, error)
	Ping(context.Context) error
	Search(context.Context, Filter, int, int) ([]Application, error)
	Update(context.Context, string, CreateApplication, int64) (Application, error)
	Archive(context.Context, string, bool, int64) (Application, error)
}
type PostgresStore struct{ DB *sql.DB }

// A transaction and advisory lock keep migrations safe across concurrent starts.
func Migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(748219)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, entry.Name()).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		contents, readErr := migrations.ReadFile("migrations/" + entry.Name())
		if readErr != nil {
			return readErr
		}
		if _, err = tx.ExecContext(ctx, string(contents)); err != nil {
			return fmt.Errorf("migration %s: %w", entry.Name(), err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, entry.Name()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const columns = `id::text, name, repository, port, environment, owner, runtime, created_at, created_by::text, description, tags, archived_at, updated_at, version`

type scanner interface{ Scan(...any) error }

func scanApplication(row scanner) (Application, error) {
	var a Application
	var tags []byte
	err := row.Scan(&a.ID, &a.Name, &a.Repository, &a.Port, &a.Environment, &a.Owner, &a.Runtime, &a.CreatedAt, &a.CreatedBy, &a.Description, &tags, &a.ArchivedAt, &a.UpdatedAt, &a.Version)
	if err == nil {
		err = json.Unmarshal(tags, &a.Tags)
	}
	a.Status = "registered"
	if a.ArchivedAt != nil {
		a.Status = "archived"
	}
	return a, err
}
func (s *PostgresStore) Ping(ctx context.Context) error { return s.DB.PingContext(ctx) }
func (s *PostgresStore) List(ctx context.Context, env string, limit, offset int) ([]Application, error) {
	return s.Search(ctx, Filter{Environment: env, State: "active"}, limit, offset)
}
func (s *PostgresStore) Search(ctx context.Context, f Filter, limit, offset int) ([]Application, error) {
	u, ok := identity.CurrentUser(ctx)
	if !ok {
		return nil, identity.ErrUnauthorized
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	// Text search uses a GIN-backed document; the name prefix is an additional convenience.
	prefix := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.ToLower(f.Query)) + "%"
	rows, err := s.DB.QueryContext(ctx, `SELECT `+columns+` FROM applications WHERE ($1='' OR environment=$1) AND ($2='admin' OR created_by=$3) AND ($4='all' OR ($4='active' AND archived_at IS NULL) OR ($4='archived' AND archived_at IS NOT NULL)) AND ($5='' OR search_document @@ websearch_to_tsquery('simple',$5) OR lower(name) LIKE $6) AND ($7='' OR runtime=$7) AND ($8='' OR tags @> jsonb_build_array($8::text)) AND ($9='' OR lower(owner)=lower($9)) ORDER BY created_at DESC,id LIMIT $10 OFFSET $11`, f.Environment, u.Role, u.ID, f.State, f.Query, prefix, f.Runtime, f.Tag, f.Owner, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Application{}
	for rows.Next() {
		a, e := scanApplication(rows)
		if e != nil {
			return nil, e
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
func (s *PostgresStore) Create(ctx context.Context, in CreateApplication) (Application, error) {
	u, ok := identity.CurrentUser(ctx)
	if !ok {
		return Application{}, identity.ErrUnauthorized
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Application{}, err
	}
	defer tx.Rollback()
	if err = authorizeActor(ctx, tx, u); err != nil {
		return Application{}, err
	}
	tags, _ := json.Marshal(in.Tags)
	if in.Tags == nil {
		tags = []byte("[]")
	}
	a, err := scanApplication(tx.QueryRowContext(ctx, `INSERT INTO applications(name,repository,port,environment,owner,runtime,created_by,description,tags) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+columns, in.Name, in.Repository, in.Port, in.Environment, in.Owner, in.Runtime, u.ID, in.Description, tags))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return Application{}, ErrConflict
	}
	if err != nil {
		return Application{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,actor_email,action,resource_id) VALUES($1,$2,'application.create',$3)`, u.ID, u.Email, a.ID); err != nil {
		return Application{}, err
	}
	return a, tx.Commit()
}
func (s *PostgresStore) Get(ctx context.Context, id string) (Application, error) {
	u, ok := identity.CurrentUser(ctx)
	if !ok {
		return Application{}, identity.ErrUnauthorized
	}
	a, err := scanApplication(s.DB.QueryRowContext(ctx, `SELECT `+columns+` FROM applications WHERE id=$1 AND ($2='admin' OR created_by=$3)`, id, u.Role, u.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return Application{}, ErrNotFound
	}
	return a, err
}

// Shared user lock serializes writes against password changes and account deactivation.
func authorizeActor(ctx context.Context, tx *sql.Tx, u identity.User) error {
	var active bool
	var role string
	if err := tx.QueryRowContext(ctx, `SELECT active,role FROM users WHERE id=$1 FOR SHARE`, u.ID).Scan(&active, &role); err != nil {
		return err
	}
	if !active || role != u.Role {
		return identity.ErrUnauthorized
	}
	return nil
}
func (s *PostgresStore) Update(ctx context.Context, id string, in CreateApplication, version int64) (Application, error) {
	return s.mutate(ctx, id, &in, nil, version)
}
func (s *PostgresStore) Archive(ctx context.Context, id string, archived bool, version int64) (Application, error) {
	return s.mutate(ctx, id, nil, &archived, version)
}
func (s *PostgresStore) mutate(ctx context.Context, id string, in *CreateApplication, archived *bool, version int64) (Application, error) {
	u, ok := identity.CurrentUser(ctx)
	if !ok {
		return Application{}, identity.ErrUnauthorized
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Application{}, err
	}
	defer tx.Rollback()
	if err = authorizeActor(ctx, tx, u); err != nil {
		return Application{}, err
	}
	existing, err := scanApplication(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM applications WHERE id=$1 AND ($2='admin' OR created_by=$3) FOR UPDATE`, id, u.Role, u.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return Application{}, ErrNotFound
	}
	if err != nil {
		return Application{}, err
	}
	if existing.Version != version {
		return Application{}, ErrStale
	}
	action := "application.update"
	var result Application
	if in != nil {
		if existing.ArchivedAt != nil {
			return Application{}, ErrArchived
		}
		tags, _ := json.Marshal(in.Tags)
		result, err = scanApplication(tx.QueryRowContext(ctx, `UPDATE applications SET name=$2,repository=$3,port=$4,environment=$5,owner=$6,runtime=$7,description=$8,tags=$9,updated_at=now(),version=version+1 WHERE id=$1 RETURNING `+columns, id, in.Name, in.Repository, in.Port, in.Environment, in.Owner, in.Runtime, in.Description, tags))
	} else {
		if *archived {
			action = "application.archive"
		} else {
			action = "application.restore"
		}
		if (existing.ArchivedAt != nil) == *archived {
			return existing, nil
		}
		result, err = scanApplication(tx.QueryRowContext(ctx, `UPDATE applications SET archived_at=CASE WHEN $2 THEN now() ELSE NULL END,updated_at=now(),version=version+1 WHERE id=$1 RETURNING `+columns, id, *archived))
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return Application{}, ErrConflict
	}
	if err != nil {
		return Application{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,actor_email,action,resource_id) VALUES($1,$2,$3,$4)`, u.ID, u.Email, action, id); err != nil {
		return Application{}, err
	}
	return result, tx.Commit()
}
