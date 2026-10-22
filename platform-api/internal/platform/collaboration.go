package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"mini-operador-ti/platform-api/internal/identity"
	"sort"
)

var ErrPermission = errors.New("application permission denied")
var ErrDependencies = errors.New("invalid or cyclic dependencies")

func readRule(alias, role, user string) string {
	return `(` + role + `='admin' OR (` + alias + `.team_id IS NULL AND ` + alias + `.created_by=` + user + `) OR EXISTS(SELECT 1 FROM team_members m WHERE m.team_id=` + alias + `.team_id AND m.user_id=` + user + `))`
}
func teamRole(ctx context.Context, tx *sql.Tx, u identity.User, id string) (string, error) {
	var found string
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM teams WHERE id=$1 FOR SHARE`, id).Scan(&found); errors.Is(err, sql.ErrNoRows) {
		return "", ErrPermission
	} else if err != nil {
		return "", err
	}
	if u.Role == "admin" {
		return "admin", nil
	}
	var role string
	if err := tx.QueryRowContext(ctx, `SELECT role FROM team_members WHERE team_id=$1 AND user_id=$2`, id, u.ID).Scan(&role); errors.Is(err, sql.ErrNoRows) {
		return "", ErrPermission
	} else if err != nil {
		return "", err
	}
	return role, nil
}
func writeTeam(ctx context.Context, tx *sql.Tx, u identity.User, current, next *string, createdBy *string) error {
	ids := []string{}
	for _, id := range []*string{current, next} {
		if id != nil {
			found := false
			for _, v := range ids {
				if v == *id {
					found = true
				}
			}
			if !found {
				ids = append(ids, *id)
			}
		}
	}
	sort.Strings(ids)
	roles := map[string]string{}
	for _, id := range ids {
		r, e := teamRole(ctx, tx, u, id)
		if e != nil {
			return e
		}
		roles[id] = r
	}
	moving := (current == nil) != (next == nil) || (current != nil && next != nil && *current != *next)
	if current == nil {
		if u.Role != "admin" && (createdBy == nil || *createdBy != u.ID) {
			return ErrPermission
		}
	} else {
		r := roles[*current]
		if r != "admin" && r != "editor" {
			return ErrPermission
		}
		if moving && r != "admin" {
			return ErrPermission
		}
	}
	if next != nil {
		r := roles[*next]
		if r != "admin" && r != "editor" {
			return ErrPermission
		}
		if moving && r != "admin" {
			return ErrPermission
		}
	}
	return nil
}
func (s *PostgresStore) decorate(ctx context.Context, apps []Application) error {
	if len(apps) == 0 {
		return nil
	}
	u, ok := identity.CurrentUser(ctx)
	if !ok {
		return identity.ErrUnauthorized
	}
	ids := []string{}
	for _, a := range apps {
		ids = append(ids, a.ID)
	}
	data, _ := json.Marshal(ids)
	roles := map[string]string{}
	rows, e := s.DB.QueryContext(ctx, `SELECT m.team_id::text,m.role FROM team_members m WHERE m.user_id=$1 AND m.team_id IN(SELECT team_id FROM applications WHERE id::text IN(SELECT jsonb_array_elements_text($2::jsonb)))`, u.ID, data)
	if e != nil {
		return e
	}
	for rows.Next() {
		var id, r string
		if e = rows.Scan(&id, &r); e != nil {
			rows.Close()
			return e
		}
		roles[id] = r
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	lookup := map[string]int{}
	for i := range apps {
		a := &apps[i]
		lookup[a.ID] = i
		a.Dependencies = []Dependency{}
		role := ""
		if a.TeamID != nil {
			role = roles[*a.TeamID]
		}
		a.CanManageTeam = u.Role == "admin" || (a.TeamID == nil && a.CreatedBy != nil && *a.CreatedBy == u.ID) || role == "admin"
		a.CanEdit = a.CanManageTeam || role == "editor"
		a.readiness()
	}
	rows, e = s.DB.QueryContext(ctx, `SELECT d.application_id::text,a.id::text,a.name,a.environment FROM application_dependencies d JOIN applications a ON a.id=d.depends_on WHERE d.application_id::text IN(SELECT jsonb_array_elements_text($1::jsonb)) AND `+readRule("a", "$2", "$3")+` ORDER BY a.name,a.id`, data, u.Role, u.ID)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var source string
		var d Dependency
		if e = rows.Scan(&source, &d.ID, &d.Name, &d.Environment); e != nil {
			return e
		}
		if i, ok := lookup[source]; ok {
			apps[i].Dependencies = append(apps[i].Dependencies, d)
		}
	}
	return rows.Err()
}
func dependencyLock(ctx context.Context, tx *sql.Tx) error {
	_, e := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(748222)`)
	return e
}
func dependencies(ctx context.Context, tx *sql.Tx, u identity.User, id string, ids []string) error {
	if ids == nil {
		return nil
	}
	data, _ := json.Marshal(ids)
	var count int
	if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM applications a WHERE a.id::text IN(SELECT jsonb_array_elements_text($1::jsonb)) AND a.archived_at IS NULL AND `+readRule("a", "$2", "$3"), data, u.Role, u.ID).Scan(&count); e != nil {
		return e
	}
	if count != len(ids) {
		return ErrDependencies
	}
	for _, target := range ids {
		if id == target {
			return ErrDependencies
		}
		var cycle bool
		e := tx.QueryRowContext(ctx, `WITH RECURSIVE reachable(id) AS(SELECT $1::uuid UNION SELECT d.depends_on FROM application_dependencies d JOIN reachable r ON d.application_id=r.id) SELECT EXISTS(SELECT 1 FROM reachable WHERE id=$2)`, target, id).Scan(&cycle)
		if e != nil {
			return e
		}
		if cycle {
			return ErrDependencies
		}
	}
	// An editor does not silently erase edges whose destination they cannot see.
	if _, e := tx.ExecContext(ctx, `DELETE FROM application_dependencies d USING applications a WHERE d.application_id=$1 AND d.depends_on=a.id AND `+readRule("a", "$2", "$3"), id, u.Role, u.ID); e != nil {
		return e
	}
	for _, target := range ids {
		if _, e := tx.ExecContext(ctx, `INSERT INTO application_dependencies(application_id,depends_on) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, target); e != nil {
			return e
		}
	}
	if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM application_dependencies WHERE application_id=$1`, id).Scan(&count); e != nil {
		return e
	}
	if count > 20 {
		return ErrDependencies
	}
	return nil
}
