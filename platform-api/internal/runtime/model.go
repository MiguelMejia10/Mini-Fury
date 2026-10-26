// Package runtime owns application scopes and their operational lifecycle.
package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"mini-operador-ti/platform-api/internal/identity"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var ErrDenied = errors.New("runtime access denied")
var ErrConflict = errors.New("runtime version conflict")
var ErrInvalid = errors.New("runtime input invalid")
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
var uuidPattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var domainPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$`)

type ScopeInput struct {
	Name        string `json:"name"`
	Environment string `json:"environment"`
	Criticality string `json:"criticality"`
	Region      string `json:"region"`
	Domain      string `json:"domain"`
	ClusterID   string `json:"clusterId"`
	Namespace   string `json:"namespace"`
	Replicas    int    `json:"replicas"`
	CPUMilli    int    `json:"cpuMilli"`
	MemoryMiB   int    `json:"memoryMiB"`
	Version     int64  `json:"version"`
}
type Scope struct {
	ID            string `json:"id"`
	ApplicationID string `json:"applicationId"`
	Workload      string `json:"workload"`
	ScopeInput
	CanEdit   bool      `json:"canEdit"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (in *ScopeInput) Validate() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Region = strings.TrimSpace(in.Region)
	in.Domain = strings.ToLower(strings.TrimSpace(in.Domain))
	if !namePattern.MatchString(in.Name) || (in.Environment != "staging" && in.Environment != "production") || (in.Criticality != "low" && in.Criticality != "medium" && in.Criticality != "high") || len(in.Region) > 80 || strings.ContainsAny(in.Region, "\r\n\x00") || (in.Domain != "" && (!domainPattern.MatchString(in.Domain) || strings.Contains(in.Domain, ".."))) || in.Replicas < 1 || in.Replicas > 10 || in.CPUMilli < 50 || in.CPUMilli > 2000 || in.MemoryMiB < 64 || in.MemoryMiB > 2048 {
		return ErrInvalid
	}
	return nil
}

const scopeColumns = `s.id::text,s.application_id::text,s.name,s.environment,s.criticality,s.region,s.domain,s.cluster_id,s.namespace,s.workload,s.replicas,s.cpu_milli,s.memory_mib,s.version,s.created_at,s.updated_at`

type scanner interface{ Scan(...any) error }

func scanScope(row scanner) (Scope, error) {
	var s Scope
	e := row.Scan(&s.ID, &s.ApplicationID, &s.Name, &s.Environment, &s.Criticality, &s.Region, &s.Domain, &s.ClusterID, &s.Namespace, &s.Workload, &s.Replicas, &s.CPUMilli, &s.MemoryMiB, &s.Version, &s.CreatedAt, &s.UpdatedAt)
	return s, e
}
func readRule(alias string) string {
	return `($1='admin' OR (` + alias + `.team_id IS NULL AND ` + alias + `.created_by=$2) OR EXISTS(SELECT 1 FROM team_members m WHERE m.team_id=` + alias + `.team_id AND m.user_id=$2))`
}
func appAccess(ctx context.Context, db *sql.DB, app string) (bool, error) {
	u, ok := identity.CurrentUser(ctx)
	if !ok {
		return false, ErrDenied
	}
	var allowed bool
	e := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM applications a WHERE a.id=$3 AND `+readRule("a")+`)`, u.Role, u.ID, app).Scan(&allowed)
	if e != nil {
		return false, e
	}
	if !allowed {
		return false, ErrDenied
	}
	var edit bool
	e = db.QueryRowContext(ctx, `SELECT $1='admin' OR (a.team_id IS NULL AND a.created_by=$2) OR EXISTS(SELECT 1 FROM team_members m WHERE m.team_id=a.team_id AND m.user_id=$2 AND m.role IN ('admin','editor')) FROM applications a WHERE a.id=$3 AND a.archived_at IS NULL`, u.Role, u.ID, app).Scan(&edit)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	return edit, e
}

// Match existing catalog lock order: actor -> team -> application -> scope. Revalidate bindings after locking.
func authorize(ctx context.Context, tx *sql.Tx, app string, production bool) error {
	u, ok := identity.CurrentUser(ctx)
	if !ok {
		return ErrDenied
	}
	var active bool
	var role string
	if e := tx.QueryRowContext(ctx, `SELECT active,role FROM users WHERE id=$1 FOR SHARE`, u.ID).Scan(&active, &role); e != nil {
		return e
	}
	if !active || role != u.Role {
		return ErrDenied
	}
	var initial sql.NullString
	if e := tx.QueryRowContext(ctx, `SELECT team_id::text FROM applications WHERE id=$1`, app).Scan(&initial); e != nil {
		return ErrDenied
	}
	if initial.Valid {
		var id string
		if e := tx.QueryRowContext(ctx, `SELECT id::text FROM teams WHERE id=$1 FOR SHARE`, initial.String).Scan(&id); e != nil {
			return e
		}
	}
	var team, creator sql.NullString
	var archived sql.NullTime
	if e := tx.QueryRowContext(ctx, `SELECT team_id::text,created_by::text,archived_at FROM applications WHERE id=$1 FOR SHARE`, app).Scan(&team, &creator, &archived); e != nil {
		return e
	}
	if team != initial {
		return ErrConflict
	}
	if archived.Valid {
		return ErrDenied
	}
	if u.Role == "admin" {
		return nil
	}
	if !team.Valid && creator.String == u.ID && !production {
		return nil
	}
	if !team.Valid {
		return ErrDenied
	}
	if e := tx.QueryRowContext(ctx, `SELECT role FROM team_members WHERE team_id=$1 AND user_id=$2`, team.String, u.ID).Scan(&role); e != nil {
		return ErrDenied
	}
	if role == "admin" || role == "editor" && !production {
		return nil
	}
	return ErrDenied
}
func event(ctx context.Context, tx *sql.Tx, action, resource string) error {
	u, _ := identity.CurrentUser(ctx)
	_, e := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,actor_email,action,resource_id) VALUES($1,$2,$3,$4)`, u.ID, u.Email, action, resource)
	return e
}
func output(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func problem(w http.ResponseWriter, e error) {
	status, message := 503, "Operación no disponible. Intenta nuevamente."
	if errors.Is(e, ErrDenied) {
		status, message = 404, "Recurso no disponible para esta cuenta."
	}
	if errors.Is(e, ErrConflict) {
		status, message = 409, "El recurso cambió o ya existe una operación en curso. Actualiza y reintenta."
	}
	if errors.Is(e, ErrInvalid) {
		status, message = 422, "Datos inválidos. Revisa límites, permisos del scope y conexiones configuradas."
	}
	output(w, status, map[string]string{"message": message})
}
