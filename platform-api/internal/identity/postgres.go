package identity

import (
	"context"
	"database/sql"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"time"
)

type Postgres struct{ DB *sql.DB }

func (p *Postgres) Credentials(ctx context.Context, email string) (Credential, error) {
	var c Credential
	err := p.DB.QueryRowContext(ctx, `SELECT id::text,email,name,role,password_hash,active,credential_version FROM users WHERE email=$1`, email).Scan(&c.ID, &c.Email, &c.Name, &c.Role, &c.PasswordHash, &c.Active, &c.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrCredentials
	}
	return c, err
}
func (p *Postgres) AllowLogin(ctx context.Context, ip, email string) (bool, error) {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	allowed := true
	for i, key := range []string{ip, email} {
		var count int
		err = tx.QueryRowContext(ctx, `INSERT INTO auth_attempts(key,window_start,attempts) VALUES($1,to_timestamp(floor(extract(epoch FROM now())/900)*900),1) ON CONFLICT(key) DO UPDATE SET window_start=EXCLUDED.window_start,attempts=CASE WHEN auth_attempts.window_start=EXCLUDED.window_start THEN LEAST(auth_attempts.attempts+1,1000) ELSE 1 END RETURNING attempts`, key).Scan(&count)
		if err != nil {
			return false, err
		}
		limit := 60
		if i == 1 {
			limit = 10
		}
		if count > limit {
			allowed = false
			if i == 0 {
				return false, tx.Commit()
			}
		}
	}
	return allowed, tx.Commit()
}
func writeEvent(ctx context.Context, tx *sql.Tx, u User, action, resource string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,actor_email,action,resource_id) VALUES($1,$2,$3,$4)`, u.ID, u.Email, action, resource)
	return err
}
func (p *Postgres) StartSession(ctx context.Context, u User, hash, previous string, expiry time.Time, expectedVersion int64) error {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Serialize first, then mutate only this user's sessions in a consistent lock order.
	var active bool
	var version int64
	if err = tx.QueryRowContext(ctx, `SELECT active,credential_version FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&active, &version); err != nil {
		return err
	}
	if !active || version != expectedVersion {
		return ErrCredentials
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=$1 AND user_id=$2`, previous, u.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash IN(SELECT token_hash FROM sessions WHERE user_id=$1 ORDER BY created_at DESC OFFSET 4)`, u.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,$3)`, hash, u.ID, expiry); err != nil {
		return err
	}
	if err = writeEvent(ctx, tx, u, "auth.login", u.ID); err != nil {
		return err
	}
	return tx.Commit()
}
func (p *Postgres) Session(ctx context.Context, hash string) (User, error) {
	var u User
	err := p.DB.QueryRowContext(ctx, `SELECT u.id::text,u.email,u.name,u.role,u.active FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now() AND u.active`, hash).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Active)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrUnauthorized
	}
	return u, err
}
func (p *Postgres) Logout(ctx context.Context, u User, hash string) error {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=$1 AND user_id=$2`, hash, u.ID); err != nil {
		return err
	}
	if err = writeEvent(ctx, tx, u, "auth.logout", u.ID); err != nil {
		return err
	}
	return tx.Commit()
}
func (p *Postgres) CreateUser(ctx context.Context, actor *User, in NewUser, hash string) (User, error) {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	if actor == nil {
		if in.Role != "admin" {
			return User{}, ErrForbidden
		}
		if _, err = tx.ExecContext(ctx, `LOCK TABLE users IN EXCLUSIVE MODE`); err != nil {
			return User{}, err
		}
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
			return User{}, err
		}
		if count != 0 {
			return User{}, ErrForbidden
		}
	}
	if actor != nil {
		var authorized bool
		if err = tx.QueryRowContext(ctx, `SELECT active AND role='admin' FROM users WHERE id=$1 FOR SHARE`, actor.ID).Scan(&authorized); err != nil {
			return User{}, err
		}
		if !authorized {
			return User{}, ErrForbidden
		}
	}
	var u User
	err = tx.QueryRowContext(ctx, `INSERT INTO users(email,name,role,password_hash) VALUES($1,$2,$3,$4) RETURNING id::text,email,name,role,active`, in.Email, in.Name, in.Role, hash).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Active)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return User{}, ErrConflict
	}
	if err != nil {
		return User{}, err
	}
	author := u
	action := "user.bootstrap"
	if actor != nil {
		author = *actor
		action = "user.create"
	}
	if err = writeEvent(ctx, tx, author, action, u.ID); err != nil {
		return User{}, err
	}
	return u, tx.Commit()
}
func (p *Postgres) Users(ctx context.Context, limit, offset int) ([]User, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT id::text,email,name,role,active FROM users ORDER BY created_at DESC,id LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []User{}
	for rows.Next() {
		var u User
		if err = rows.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Active); err != nil {
			return nil, err
		}
		result = append(result, u)
	}
	return result, rows.Err()
}
func (p *Postgres) Audit(ctx context.Context, limit, offset int) ([]Event, error) {
	return p.SearchAudit(ctx, AuditFilter{}, limit, offset)
}
func (p *Postgres) SearchAudit(ctx context.Context, f AuditFilter, limit, offset int) ([]Event, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	rows, err := p.DB.QueryContext(ctx, `SELECT id,actor_email,action,resource_id,created_at FROM audit_events WHERE ($1='' OR actor_email=$1) AND ($2='' OR action=$2) AND ($3='' OR resource_id=$3) AND ($4::timestamptz IS NULL OR created_at >= $4) AND ($5::timestamptz IS NULL OR created_at < $5) ORDER BY created_at DESC,id DESC LIMIT $6 OFFSET $7`, f.Actor, f.Action, f.Resource, f.From, f.Until, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Event{}
	for rows.Next() {
		var e Event
		if err = rows.Scan(&e.ID, &e.ActorEmail, &e.Action, &e.ResourceID, &e.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
func (p *Postgres) Cleanup(ctx context.Context) error {
	if _, err := p.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash IN(SELECT token_hash FROM sessions WHERE expires_at<now() LIMIT 1000)`); err != nil {
		return err
	}
	_, err := p.DB.ExecContext(ctx, `DELETE FROM auth_attempts WHERE key IN(SELECT key FROM auth_attempts WHERE window_start<now()-interval '15 minutes' LIMIT 1000)`)
	return err
}
