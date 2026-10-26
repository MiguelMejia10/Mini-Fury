package runtime

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"io"
	"mime"
	"mini-operador-ti/platform-api/internal/delivery"
	"mini-operador-ti/platform-api/internal/identity"
	"net/http"
	"sync"
)

type HTTP struct {
	workers        sync.WaitGroup
	DB             *sql.DB
	Queue          *delivery.Queue
	WebhookEnabled bool
	Auth           *identity.HTTP
	Config         Config
	KubeFactory    func(ClusterConfig) (*Kube, error)
	clientsMu      sync.Mutex
	clients        map[string]*Kube
}

func (h *HTTP) Routes(m *http.ServeMux) {
	h.expansionRoutes(m)
	handle := func(pattern string, fn http.HandlerFunc) { m.Handle(pattern, h.Auth.Protected(http.HandlerFunc(fn))) }
	m.HandleFunc("POST /api/runtime/build-agent/claim", h.agentClaim)
	m.HandleFunc("GET /api/runtime/build-agent/jobs/{job}", h.agentGet)
	m.HandleFunc("POST /api/runtime/build-agent/jobs/{job}/heartbeat", h.agentHeartbeat)
	m.HandleFunc("POST /api/runtime/build-agent/jobs/{job}/complete", h.agentComplete)
	handle("GET /api/applications/{app}/scopes", h.scopes)
	handle("POST /api/applications/{app}/scopes", h.createScope)
	handle("GET /api/applications/{app}/runtime-connections", h.connections)
	handle("PUT /api/scopes/{scope}", h.updateScope)
	handle("GET /api/scopes/{scope}", h.getScope)
	handle("GET /api/scopes/{scope}/kubernetes", h.observe)
	handle("GET /api/applications/{app}/versions", h.versions)
	handle("POST /api/applications/{app}/builds", h.requestBuild)
	handle("POST /api/scopes/{scope}/actions/{action}", h.requestAction)
	handle("GET /api/scopes/{scope}/configuration", h.configuration)
	handle("POST /api/scopes/{scope}/configuration", h.saveConfiguration)
	handle("GET /api/runtime/jobs", h.jobs)
	handle("GET /api/runtime/jobs/{job}", h.jobDetail)
	handle("POST /api/runtime/jobs/{job}/cancel", h.cancelJob)
	handle("POST /api/runtime/jobs/{job}/retry", h.retryJob)
	handle("GET /api/scopes/{scope}/pods/{pod}/logs", h.podLogs)
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	kind, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || kind != "application/json" {
		output(w, 415, map[string]string{"message": "Usa JSON."})
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		output(w, 400, map[string]string{"message": "JSON inválido."})
		return false
	}
	return true
}
func validID(w http.ResponseWriter, id string) bool {
	if !uuidPattern.MatchString(id) {
		output(w, 400, map[string]string{"message": "Identificador inválido."})
		return false
	}
	return true
}
func (h *HTTP) scope(r *http.Request) (Scope, error) {
	s, e := scanScope(h.DB.QueryRowContext(r.Context(), `SELECT `+scopeColumns+` FROM runtime_scopes s WHERE s.id=$1`, r.PathValue("scope")))
	if errors.Is(e, sql.ErrNoRows) {
		return s, ErrDenied
	}
	if e != nil {
		return s, e
	}
	edit, e := appAccess(r.Context(), h.DB, s.ApplicationID)
	s.CanEdit = edit
	if s.Environment == "production" && edit {
		tx, e2 := h.DB.BeginTx(r.Context(), nil)
		if e2 != nil {
			return s, e2
		}
		defer tx.Rollback()
		s.CanEdit = authorize(r.Context(), tx, s.ApplicationID, true) == nil
	}
	return s, e
}
func (h *HTTP) getScope(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("scope")) {
		return
	}
	s, e := h.scope(r)
	if e != nil {
		problem(w, e)
		return
	}
	output(w, 200, s)
}
func (h *HTTP) scopes(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	if !validID(w, app) {
		return
	}
	edit, e := appAccess(r.Context(), h.DB, app)
	if e != nil {
		problem(w, e)
		return
	}
	limit, offset, e := identity.Page(r)
	if e != nil {
		problem(w, ErrInvalid)
		return
	}
	rows, e := h.DB.QueryContext(r.Context(), `SELECT `+scopeColumns+` FROM runtime_scopes s WHERE s.application_id=$1 ORDER BY s.created_at DESC,s.id LIMIT $2 OFFSET $3`, app, limit+1, offset)
	if e != nil {
		problem(w, e)
		return
	}
	out := []Scope{}
	for rows.Next() {
		s, e := scanScope(rows)
		if e != nil {
			rows.Close()
			problem(w, e)
			return
		}
		s.CanEdit = edit && s.Environment != "production"
		out = append(out, s)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		problem(w, e)
		return
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	output(w, 200, map[string]any{"scopes": out, "hasMore": more})
}
func (h *HTTP) connections(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	if !validID(w, app) {
		return
	}
	if _, e := appAccess(r.Context(), h.DB, app); e != nil {
		problem(w, e)
		return
	}
	out := []map[string]any{}
	for _, c := range h.Config.Clusters {
		for _, b := range c.Bindings {
			if b.ApplicationID == app {
				aliases := []string{}
				for alias := range b.Secrets {
					aliases = append(aliases, alias)
				}
				out = append(out, map[string]any{"clusterId": c.ID, "clusterName": c.Name, "namespace": b.Namespace, "secretAliases": aliases})
			}
		}
	}
	repo, configured := h.Config.repository(app)
	output(w, 200, map[string]any{"connections": out, "buildConfigured": configured, "buildProvider": repo.Provider})
}
func (h *HTTP) createScope(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	if !validID(w, app) {
		return
	}
	var in ScopeInput
	if !decode(w, r, &in) {
		return
	}
	if e := in.Validate(); e != nil {
		problem(w, e)
		return
	}
	if _, _, ok := h.Config.binding(app, in.ClusterID, in.Namespace); !ok {
		problem(w, ErrInvalid)
		return
	}
	tx, e := h.DB.BeginTx(r.Context(), nil)
	if e != nil {
		problem(w, e)
		return
	}
	defer tx.Rollback()
	if e = authorize(r.Context(), tx, app, in.Environment == "production"); e != nil {
		problem(w, e)
		return
	}
	u, _ := identity.CurrentUser(r.Context())
	var id string
	if e = tx.QueryRowContext(r.Context(), `SELECT gen_random_uuid()::text`).Scan(&id); e != nil {
		problem(w, e)
		return
	}
	_, e = tx.ExecContext(r.Context(), `INSERT INTO runtime_scopes(id,application_id,name,environment,criticality,region,domain,cluster_id,namespace,workload,replicas,cpu_milli,memory_mib,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, id, app, in.Name, in.Environment, in.Criticality, in.Region, in.Domain, in.ClusterID, in.Namespace, "mf-"+id, in.Replicas, in.CPUMilli, in.MemoryMiB, u.ID)
	var pg *pgconn.PgError
	if errors.As(e, &pg) && pg.Code == "23505" {
		e = ErrConflict
	}
	if e != nil {
		problem(w, e)
		return
	}
	if e = event(r.Context(), tx, "scope.create", id); e == nil {
		e = tx.Commit()
	}
	if e != nil {
		problem(w, e)
		return
	}
	output(w, 201, map[string]string{"id": id})
}
func (h *HTTP) updateScope(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("scope")) {
		return
	}
	s, e := h.scope(r)
	if e != nil {
		problem(w, e)
		return
	}
	var in ScopeInput
	if !decode(w, r, &in) {
		return
	}
	if e = in.Validate(); e != nil {
		problem(w, e)
		return
	}
	if in.ClusterID != s.ClusterID || in.Namespace != s.Namespace || in.Name != s.Name || in.Environment != s.Environment {
		problem(w, ErrInvalid)
		return
	}
	tx, e := h.DB.BeginTx(r.Context(), nil)
	if e != nil {
		problem(w, e)
		return
	}
	defer tx.Rollback()
	if e = authorize(r.Context(), tx, s.ApplicationID, s.Environment == "production"); e != nil {
		problem(w, e)
		return
	}
	var locked string
	if e = tx.QueryRowContext(r.Context(), `SELECT id::text FROM runtime_scopes WHERE id=$1 FOR UPDATE`, s.ID).Scan(&locked); e != nil {
		problem(w, e)
		return
	}
	var busy bool
	if e = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM runtime_jobs WHERE scope_id=$1 AND state IN ('queued','running'))`, s.ID).Scan(&busy); e != nil || busy {
		problem(w, ErrConflict)
		return
	}
	result, e := tx.ExecContext(r.Context(), `UPDATE runtime_scopes SET region=$2,domain=$3,criticality=$4,replicas=$5,cpu_milli=$6,memory_mib=$7,version=version+1,updated_at=now() WHERE id=$1 AND version=$8`, s.ID, in.Region, in.Domain, in.Criticality, in.Replicas, in.CPUMilli, in.MemoryMiB, in.Version)
	if e == nil {
		n, _ := result.RowsAffected()
		if n == 0 {
			e = ErrConflict
		}
	}
	if e == nil {
		e = event(r.Context(), tx, "scope.update", s.ID)
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		problem(w, e)
		return
	}
	output(w, 200, map[string]bool{"ok": true})
}
