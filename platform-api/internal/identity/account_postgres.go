package identity

import (
	"context"
	"database/sql"
	"errors"
)

// All credential/session writes take the same user row lock as login.
func (p *Postgres) ChangePassword(ctx context.Context, u User, expectedHash, newHash string) error {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var hash string
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT password_hash,active FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&hash, &active); err != nil {
		return err
	}
	if !active || hash != expectedHash {
		return ErrAccountConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET password_hash=$2,credential_version=credential_version+1 WHERE id=$1`, u.ID, newHash); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=$1`, u.ID); err != nil {
		return err
	}
	if err = writeEvent(ctx, tx, u, "auth.password_change", u.ID); err != nil {
		return err
	}
	return tx.Commit()
}
func (p *Postgres) RevokeAll(ctx context.Context, u User) error {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT active FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&active); err != nil {
		return err
	}
	if !active {
		return ErrUnauthorized
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET credential_version=credential_version+1 WHERE id=$1`, u.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=$1`, u.ID); err != nil {
		return err
	}
	if err = writeEvent(ctx, tx, u, "auth.logout_all", u.ID); err != nil {
		return err
	}
	return tx.Commit()
}
func (p *Postgres) SetActive(ctx context.Context, actor User, id string, active bool) (User, error) {
	if actor.Role != "admin" {
		return User{}, ErrForbidden
	}
	if actor.ID == id && !active {
		return User{}, ErrAccountConflict
	}
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	// One lock protects cross-admin deactivations and the last active administrator.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(748220)`); err != nil {
		return User{}, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(748221)`); err != nil {
		return User{}, err
	}
	var authorized bool
	if err = tx.QueryRowContext(ctx, `SELECT active AND role='admin' FROM users WHERE id=$1 FOR SHARE`, actor.ID).Scan(&authorized); err != nil {
		return User{}, err
	}
	if !authorized {
		return User{}, ErrForbidden
	}
	var target User
	err = tx.QueryRowContext(ctx, `SELECT id::text,email,name,role,active FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&target.ID, &target.Email, &target.Name, &target.Role, &target.Active)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUnauthorized
	}
	if err != nil {
		return User{}, err
	}
	if !active {
		var orphan bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM team_members m WHERE m.user_id=$1 AND m.role='admin' AND NOT EXISTS(SELECT 1 FROM team_members other JOIN users u ON u.id=other.user_id WHERE other.team_id=m.team_id AND other.user_id<>$1 AND other.role='admin' AND u.active))`, id).Scan(&orphan)
		if err != nil {
			return User{}, err
		}
		if orphan {
			return User{}, ErrAccountConflict
		}
	}
	if target.Active == active {
		return target, nil
	}
	if !active && target.Role == "admin" {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE role='admin' AND active`).Scan(&count); err != nil {
			return User{}, err
		}
		if count <= 1 {
			return User{}, ErrAccountConflict
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET active=$2,credential_version=credential_version+1 WHERE id=$1`, id, active); err != nil {
		return User{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=$1`, id); err != nil {
		return User{}, err
	}
	action := "user.deactivate"
	if active {
		action = "user.activate"
	}
	if err = writeEvent(ctx, tx, actor, action, id); err != nil {
		return User{}, err
	}
	target.Active = active
	return target, tx.Commit()
}
