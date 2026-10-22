package teams

import (
	"context"
	"database/sql"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"mini-operador-ti/platform-api/internal/identity"
)

type Postgres struct{ DB *sql.DB }

func event(ctx context.Context, tx *sql.Tx, u identity.User, action, id string) error {
	_, e := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,actor_email,action,resource_id) VALUES($1,$2,$3,$4)`, u.ID, u.Email, action, id)
	return e
}
func actorLock(ctx context.Context, tx *sql.Tx, u identity.User) error {
	var active bool
	var role string
	e := tx.QueryRowContext(ctx, `SELECT active,role FROM users WHERE id=$1 FOR SHARE`, u.ID).Scan(&active, &role)
	if e != nil {
		return e
	}
	if !active || role != u.Role {
		return ErrForbidden
	}
	return nil
}

// Lock the team before memberships; catalog writes use the same order.
func admin(ctx context.Context, tx *sql.Tx, u identity.User, id string) error {
	var exists string
	e := tx.QueryRowContext(ctx, `SELECT id::text FROM teams WHERE id=$1 FOR UPDATE`, id).Scan(&exists)
	if errors.Is(e, sql.ErrNoRows) {
		return ErrNotFound
	}
	if e != nil {
		return e
	}
	if u.Role == "admin" {
		return nil
	}
	var role string
	e = tx.QueryRowContext(ctx, `SELECT role FROM team_members WHERE team_id=$1 AND user_id=$2`, id, u.ID).Scan(&role)
	if e != nil || role != "admin" {
		return ErrForbidden
	}
	return nil
}
func (p *Postgres) List(ctx context.Context, limit, offset int) ([]Team, error) {
	u, e := Actor(ctx)
	if e != nil {
		return nil, e
	}
	rows, e := p.DB.QueryContext(ctx, `SELECT t.id::text,t.name,t.description,CASE WHEN $1='admin' THEN 'admin' ELSE m.role END,t.created_at FROM teams t LEFT JOIN team_members m ON m.team_id=t.id AND m.user_id=$2 WHERE $1='admin' OR m.user_id IS NOT NULL ORDER BY t.created_at DESC,t.id LIMIT $3 OFFSET $4`, u.Role, u.ID, limit, offset)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Team{}
	for rows.Next() {
		var t Team
		if e = rows.Scan(&t.ID, &t.Name, &t.Description, &t.Role, &t.CreatedAt); e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (p *Postgres) Create(ctx context.Context, in NewTeam) (Team, error) {
	u, e := Actor(ctx)
	if e != nil {
		return Team{}, e
	}
	tx, e := p.DB.BeginTx(ctx, nil)
	if e != nil {
		return Team{}, e
	}
	defer tx.Rollback()
	if e = actorLock(ctx, tx, u); e != nil {
		return Team{}, e
	}
	var t Team
	e = tx.QueryRowContext(ctx, `INSERT INTO teams(name,description,created_by) VALUES($1,$2,$3) RETURNING id::text,name,description,created_at`, in.Name, in.Description, u.ID).Scan(&t.ID, &t.Name, &t.Description, &t.CreatedAt)
	if e != nil {
		return t, e
	}
	t.Role = "admin"
	if _, e = tx.ExecContext(ctx, `INSERT INTO team_members(team_id,user_id,role) VALUES($1,$2,'admin')`, t.ID, u.ID); e != nil {
		return t, e
	}
	if e = event(ctx, tx, u, "team.create", t.ID); e != nil {
		return t, e
	}
	return t, tx.Commit()
}
func (p *Postgres) Members(ctx context.Context, id string, limit, offset int) ([]Member, error) {
	u, e := Actor(ctx)
	if e != nil {
		return nil, e
	}
	var allowed bool
	if e = p.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM teams t WHERE t.id=$1 AND ($2='admin' OR EXISTS(SELECT 1 FROM team_members m WHERE m.team_id=t.id AND m.user_id=$3)))`, id, u.Role, u.ID).Scan(&allowed); e != nil {
		return nil, e
	}
	if !allowed {
		return nil, ErrNotFound
	}
	rows, e := p.DB.QueryContext(ctx, `SELECT u.id::text,u.name,u.email,m.role,u.active FROM team_members m JOIN users u ON u.id=m.user_id WHERE m.team_id=$1 ORDER BY u.name,u.id LIMIT $2 OFFSET $3`, id, limit, offset)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if e = rows.Scan(&m.ID, &m.Name, &m.Email, &m.Role, &m.Active); e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (p *Postgres) Invite(ctx context.Context, id string, in Invite) (Invitation, error) {
	u, e := Actor(ctx)
	if e != nil {
		return Invitation{}, e
	}
	tx, e := p.DB.BeginTx(ctx, nil)
	if e != nil {
		return Invitation{}, e
	}
	defer tx.Rollback()
	if e = actorLock(ctx, tx, u); e != nil {
		return Invitation{}, e
	}
	if e = admin(ctx, tx, u, id); e != nil {
		return Invitation{}, e
	}
	var exists bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM team_members m JOIN users u ON u.id=m.user_id WHERE m.team_id=$1 AND u.email=$2)`, id, in.Email).Scan(&exists); e != nil {
		return Invitation{}, e
	}
	if exists {
		return Invitation{}, ErrConflict
	}
	if _, e = tx.ExecContext(ctx, `UPDATE team_invitations SET revoked_at=now() WHERE team_id=$1 AND email=$2 AND expires_at<=now() AND accepted_at IS NULL AND revoked_at IS NULL`, id, in.Email); e != nil {
		return Invitation{}, e
	}
	var i Invitation
	i.TeamID = id
	e = tx.QueryRowContext(ctx, `INSERT INTO team_invitations(team_id,email,role,invited_by) VALUES($1,$2,$3,$4) RETURNING id::text,email,role,expires_at`, id, in.Email, in.Role, u.ID).Scan(&i.ID, &i.Email, &i.Role, &i.ExpiresAt)
	var pg *pgconn.PgError
	if errors.As(e, &pg) && pg.Code == "23505" {
		return i, ErrConflict
	}
	if e != nil {
		return i, e
	}
	if e = event(ctx, tx, u, "team.invite", i.ID); e != nil {
		return i, e
	}
	return i, tx.Commit()
}
func (p *Postgres) Invitations(ctx context.Context, limit, offset int) ([]Invitation, error) {
	u, e := Actor(ctx)
	if e != nil {
		return nil, e
	}
	return p.invites(ctx, p.DB, `WHERE i.email=$1 AND i.accepted_at IS NULL AND i.revoked_at IS NULL AND i.expires_at>now()`, u.Email, limit, offset)
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (p *Postgres) invites(ctx context.Context, q queryer, where string, arg any, limit, offset int) ([]Invitation, error) {
	rows, e := q.QueryContext(ctx, `SELECT i.id::text,i.team_id::text,t.name,i.email,i.role,i.expires_at FROM team_invitations i JOIN teams t ON t.id=i.team_id `+where+` ORDER BY i.created_at DESC,i.id LIMIT $2 OFFSET $3`, arg, limit, offset)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Invitation{}
	for rows.Next() {
		var i Invitation
		if e = rows.Scan(&i.ID, &i.TeamID, &i.TeamName, &i.Email, &i.Role, &i.ExpiresAt); e != nil {
			return nil, e
		}
		out = append(out, i)
	}
	return out, rows.Err()
}
func (p *Postgres) TeamInvitations(ctx context.Context, id string, limit, offset int) ([]Invitation, error) {
	u, e := Actor(ctx)
	if e != nil {
		return nil, e
	}
	tx, e := p.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = admin(ctx, tx, u, id); e != nil {
		return nil, e
	}
	return p.invites(ctx, tx, `WHERE i.team_id=$1 AND i.accepted_at IS NULL AND i.revoked_at IS NULL AND i.expires_at>now()`, id, limit, offset)
}
func (p *Postgres) Accept(ctx context.Context, id string) error {
	u, e := Actor(ctx)
	if e != nil {
		return e
	}
	tx, e := p.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(748221)`); e != nil {
		return e
	}
	if e = actorLock(ctx, tx, u); e != nil {
		return e
	}
	var team string
	// Read the team first, then lock it and revalidate the invitation to maintain lock order.
	if e = tx.QueryRowContext(ctx, `SELECT team_id::text FROM team_invitations WHERE id=$1 AND email=$2`, id, u.Email).Scan(&team); errors.Is(e, sql.ErrNoRows) {
		return ErrNotFound
	} else if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `SELECT id FROM teams WHERE id=$1 FOR UPDATE`, team); e != nil {
		return e
	}
	var role string
	if e = tx.QueryRowContext(ctx, `SELECT role FROM team_invitations WHERE id=$1 AND email=$2 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at>now() FOR UPDATE`, id, u.Email).Scan(&role); errors.Is(e, sql.ErrNoRows) {
		return ErrNotFound
	} else if e != nil {
		return e
	}
	result, e := tx.ExecContext(ctx, `INSERT INTO team_members(team_id,user_id,role) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, team, u.ID, role)
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	if _, e = tx.ExecContext(ctx, `UPDATE team_invitations SET accepted_at=now() WHERE id=$1`, id); e != nil {
		return e
	}
	if e = event(ctx, tx, u, "team.accept", id); e != nil {
		return e
	}
	return tx.Commit()
}
func (p *Postgres) RevokeInvitation(ctx context.Context, team, id string) error {
	u, e := Actor(ctx)
	if e != nil {
		return e
	}
	tx, e := p.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = actorLock(ctx, tx, u); e != nil {
		return e
	}
	if e = admin(ctx, tx, u, team); e != nil {
		return e
	}
	r, e := tx.ExecContext(ctx, `UPDATE team_invitations SET revoked_at=now() WHERE id=$1 AND team_id=$2 AND accepted_at IS NULL AND revoked_at IS NULL`, id, team)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if e = event(ctx, tx, u, "team.invite_revoke", id); e != nil {
		return e
	}
	return tx.Commit()
}
func (p *Postgres) SetMember(ctx context.Context, team, id, role string) error {
	u, e := Actor(ctx)
	if e != nil {
		return e
	}
	tx, e := p.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(748221)`); e != nil {
		return e
	}
	if e = actorLock(ctx, tx, u); e != nil {
		return e
	}
	if e = admin(ctx, tx, u, team); e != nil {
		return e
	}
	var old string
	var active bool
	e = tx.QueryRowContext(ctx, `SELECT m.role,u.active FROM team_members m JOIN users u ON u.id=m.user_id WHERE m.team_id=$1 AND m.user_id=$2`, team, id).Scan(&old, &active)
	if errors.Is(e, sql.ErrNoRows) {
		return ErrNotFound
	}
	if e != nil {
		return e
	}
	if old == role {
		return nil
	}
	if old == "admin" && active {
		var count int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM team_members m JOIN users u ON u.id=m.user_id WHERE m.team_id=$1 AND m.role='admin' AND u.active`, team).Scan(&count); e != nil {
			return e
		}
		if count <= 1 {
			return ErrConflict
		}
	}
	action := "team.member_role"
	if role == "" {
		action = "team.member_remove"
		_, e = tx.ExecContext(ctx, `DELETE FROM team_members WHERE team_id=$1 AND user_id=$2`, team, id)
	} else {
		_, e = tx.ExecContext(ctx, `UPDATE team_members SET role=$3 WHERE team_id=$1 AND user_id=$2`, team, id, role)
	}
	if e != nil {
		return e
	}
	if e = event(ctx, tx, u, action, team+":"+id); e != nil {
		return e
	}
	return tx.Commit()
}
