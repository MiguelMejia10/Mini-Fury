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

type Version struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	SourceRevision string    `json:"sourceRevision"`
	Image          string    `json:"image"`
	Provenance     string    `json:"provenance"`
	CreatedAt      time.Time `json:"createdAt"`
}
type Job struct {
	ID              string     `json:"id"`
	ApplicationID   string     `json:"applicationId"`
	ScopeID         *string    `json:"scopeId"`
	Kind            string     `json:"kind"`
	State           string     `json:"state"`
	Phase           string     `json:"phase"`
	Message         string     `json:"message"`
	Attempt         int        `json:"attempt"`
	CancelRequested bool       `json:"cancelRequested"`
	ParentID        *string    `json:"parentId"`
	CreatedAt       time.Time  `json:"createdAt"`
	StartedAt       *time.Time `json:"startedAt"`
	CompletedAt     *time.Time `json:"completedAt"`
}

const jobColumns = `j.id::text,j.application_id::text,j.scope_id::text,j.kind,j.state,j.phase,j.message,j.attempt,j.cancel_requested,j.parent_id::text,j.created_at,j.started_at,j.completed_at`

func scanJob(row scanner) (Job, error) {
	var j Job
	e := row.Scan(&j.ID, &j.ApplicationID, &j.ScopeID, &j.Kind, &j.State, &j.Phase, &j.Message, &j.Attempt, &j.CancelRequested, &j.ParentID, &j.CreatedAt, &j.StartedAt, &j.CompletedAt)
	return j, e
}

type JobPayload struct {
	ServiceID       string `json:"serviceId,omitempty"`
	VersionID       string `json:"versionId,omitempty"`
	Image           string `json:"image,omitempty"`
	SourceRevision  string `json:"sourceRevision,omitempty"`
	Name            string `json:"name,omitempty"`
	Provider        string `json:"provider,omitempty"`
	Ref             string `json:"ref,omitempty"`
	Replicas        int    `json:"replicas,omitempty"`
	CPUMilli        int    `json:"cpuMilli,omitempty"`
	MemoryMiB       int    `json:"memoryMiB,omitempty"`
	ConfigurationID string `json:"configurationId,omitempty"`
}

var revisionPattern = regexp.MustCompile(`^[a-f0-9]{40}([a-f0-9]{24})?$`)
var imagePattern = regexp.MustCompile(`^[a-z0-9.-]+(:[0-9]+)?/[a-z0-9/._-]+@sha256:[a-f0-9]{64}$`)
var idemPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,80}$`)

func allowedImage(c RepositoryConfig, image string) bool {
	if !imagePattern.MatchString(image) {
		return false
	}
	for _, prefix := range c.ImagePrefixes {
		if strings.HasPrefix(image, prefix) && strings.HasSuffix(prefix, "/") {
			return true
		}
	}
	return false
}
func (h *HTTP) versions(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	if !validID(w, app) {
		return
	}
	if _, e := appAccess(r.Context(), h.DB, app); e != nil {
		problem(w, e)
		return
	}
	limit, offset, e := identity.Page(r)
	if e != nil {
		problem(w, ErrInvalid)
		return
	}
	rows, e := h.DB.QueryContext(r.Context(), `SELECT id::text,name,source_revision,image,provenance,created_at FROM runtime_versions WHERE application_id=$1 ORDER BY created_at DESC,id LIMIT $2 OFFSET $3`, app, limit+1, offset)
	if e != nil {
		problem(w, e)
		return
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		var v Version
		if e = rows.Scan(&v.ID, &v.Name, &v.SourceRevision, &v.Image, &v.Provenance, &v.CreatedAt); e != nil {
			problem(w, e)
			return
		}
		out = append(out, v)
	}
	if rows.Err() != nil {
		problem(w, rows.Err())
		return
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	output(w, 200, map[string]any{"versions": out, "hasMore": more})
}
func (h *HTTP) enqueue(ctx context.Context, app string, scope *Scope, kind, key string, payload JobPayload, parent *string) (Job, error) {
	var empty Job
	if !idemPattern.MatchString(key) {
		return empty, ErrInvalid
	}
	tx, e := h.DB.BeginTx(ctx, nil)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback()
	j, e := h.enqueueTx(ctx, tx, app, scope, kind, key, payload, parent)
	if e != nil {
		return empty, e
	}
	return j, tx.Commit()
}
func (h *HTTP) enqueueTx(ctx context.Context, tx *sql.Tx, app string, scope *Scope, kind, key string, payload JobPayload, parent *string) (Job, error) {
	var empty Job
	if !idemPattern.MatchString(key) {
		return empty, ErrInvalid
	}
	var e error
	production := scope != nil && scope.Environment == "production"
	if e = authorize(ctx, tx, app, production); e != nil {
		return empty, e
	}
	if scope != nil {
		var version int64
		if e = tx.QueryRowContext(ctx, `SELECT version FROM runtime_scopes WHERE id=$1 FOR UPDATE`, scope.ID).Scan(&version); e != nil {
			return empty, e
		}
		if version != scope.Version {
			return empty, ErrConflict
		}
	}
	raw, _ := json.Marshal(payload)
	var scopeID *string
	if scope != nil {
		scopeID = &scope.ID
	}
	u, _ := identity.CurrentUser(ctx)
	var id string
	e = tx.QueryRowContext(ctx, `INSERT INTO runtime_jobs(application_id,scope_id,kind,payload,idempotency_key,requested_by,parent_id) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(application_id,idempotency_key) DO NOTHING RETURNING id::text`, app, scopeID, kind, raw, key, u.ID, parent).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		var existingPayload []byte
		var existingKind, existingScope string
		var existingParent sql.NullString
		if e = tx.QueryRowContext(ctx, `SELECT id::text,kind,COALESCE(scope_id::text,''),payload,parent_id::text FROM runtime_jobs WHERE application_id=$1 AND idempotency_key=$2`, app, key).Scan(&id, &existingKind, &existingScope, &existingPayload, &existingParent); e != nil {
			return empty, e
		}
		var old JobPayload
		json.Unmarshal(existingPayload, &old)
		sameParent := parent == nil && !existingParent.Valid || parent != nil && existingParent.String == *parent
		expectedScope := ""
		if scope != nil {
			expectedScope = scope.ID
		}
		if old != payload || existingKind != kind || existingScope != expectedScope || !sameParent {
			return empty, ErrConflict
		}
		return scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM runtime_jobs j WHERE j.id=$1`, id))
	}
	if e != nil {
		return empty, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO runtime_job_events(job_id,state,phase,message) VALUES($1,'queued','queued','Operación en cola')`, id); e != nil {
		return empty, e
	}
	if e = event(ctx, tx, "runtime."+kind+"_requested", id); e != nil {
		return empty, e
	}
	j, e := scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM runtime_jobs j WHERE j.id=$1`, id))
	if e != nil {
		return empty, e
	}
	return j, nil
}
func (h *HTTP) requestBuild(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	if !validID(w, app) {
		return
	}
	var in struct {
		Name           string `json:"name"`
		SourceRevision string `json:"sourceRevision"`
	}
	if !decode(w, r, &in) {
		return
	}
	repo, ok := h.Config.repository(app)
	if !ok || !namePattern.MatchString(in.Name) || !revisionPattern.MatchString(in.SourceRevision) {
		problem(w, ErrInvalid)
		return
	}
	var currentRepo string
	if e := h.DB.QueryRowContext(r.Context(), `SELECT repository FROM applications WHERE id=$1`, app).Scan(&currentRepo); e != nil || currentRepo != repo.Repository {
		problem(w, ErrDenied)
		return
	}
	j, e := h.enqueue(r.Context(), app, nil, "build", r.Header.Get("Idempotency-Key"), JobPayload{Name: in.Name, SourceRevision: in.SourceRevision, Provider: repo.Provider, Ref: repo.Ref}, nil)
	if e != nil {
		problem(w, e)
		return
	}
	output(w, 202, j)
}
func (h *HTTP) requestAction(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("scope")) {
		return
	}
	s, e := h.scope(r)
	if e != nil {
		problem(w, e)
		return
	}
	kind := r.PathValue("action")
	if kind != "deploy" && kind != "rollback" && kind != "restart" && kind != "scale" && kind != "configure" {
		problem(w, ErrInvalid)
		return
	}
	var in struct {
		VersionID     string `json:"versionId"`
		Replicas      int    `json:"replicas"`
		PreviousJobID string `json:"previousJobId"`
	}
	if !decode(w, r, &in) {
		return
	}
	p := JobPayload{Replicas: s.Replicas, CPUMilli: s.CPUMilli, MemoryMiB: s.MemoryMiB}
	if kind == "deploy" {
		if !uuidPattern.MatchString(in.VersionID) {
			problem(w, ErrInvalid)
			return
		}
		if e = h.DB.QueryRowContext(r.Context(), `SELECT id::text,image FROM runtime_versions WHERE id=$1 AND application_id=$2`, in.VersionID, s.ApplicationID).Scan(&p.VersionID, &p.Image); e != nil {
			problem(w, ErrInvalid)
			return
		}
	}
	if kind == "rollback" {
		if !uuidPattern.MatchString(in.PreviousJobID) {
			problem(w, ErrInvalid)
			return
		}
		var raw []byte
		e = h.DB.QueryRowContext(r.Context(), `SELECT payload FROM runtime_jobs WHERE id=$1 AND scope_id=$2 AND state='succeeded' AND kind IN ('deploy','rollback','configure')`, in.PreviousJobID, s.ID).Scan(&raw)
		if e != nil || json.Unmarshal(raw, &p) != nil || p.Image == "" {
			problem(w, ErrInvalid)
			return
		}
	}
	if kind == "scale" {
		if in.Replicas < 1 || in.Replicas > 10 {
			problem(w, ErrInvalid)
			return
		}
		p.Replicas = in.Replicas
	}
	if kind != "rollback" {
		var configID string
		e = h.DB.QueryRowContext(r.Context(), `SELECT id::text FROM runtime_configurations WHERE scope_id=$1 ORDER BY revision DESC LIMIT 1`, s.ID).Scan(&configID)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			problem(w, e)
			return
		}
		p.ConfigurationID = configID
	}
	if p.Image == "" {
		var raw []byte
		e = h.DB.QueryRowContext(r.Context(), `SELECT payload FROM runtime_jobs WHERE scope_id=$1 AND state='succeeded' AND kind IN ('deploy','rollback','configure') ORDER BY completed_at DESC,id LIMIT 1`, s.ID).Scan(&raw)
		var previous JobPayload
		if e != nil || json.Unmarshal(raw, &previous) != nil || previous.Image == "" {
			problem(w, ErrInvalid)
			return
		}
		p.VersionID = previous.VersionID
		p.Image = previous.Image
		if kind == "restart" || kind == "scale" {
			desiredReplicas := p.Replicas
			p = previous
			p.Replicas = desiredReplicas
		}
	}
	j, e := h.enqueue(r.Context(), s.ApplicationID, &s, kind, r.Header.Get("Idempotency-Key"), p, nil)
	if e != nil {
		problem(w, e)
		return
	}
	output(w, 202, j)
}
func (h *HTTP) jobs(w http.ResponseWriter, r *http.Request) {
	limit, offset, e := identity.Page(r)
	if e != nil {
		problem(w, ErrInvalid)
		return
	}
	state := r.URL.Query().Get("state")
	kind := r.URL.Query().Get("kind")
	app := r.URL.Query().Get("applicationId")
	scope := r.URL.Query().Get("scopeId")
	if app != "" && !uuidPattern.MatchString(app) || scope != "" && !uuidPattern.MatchString(scope) || state != "" && !strings.Contains("|queued|running|succeeded|failed|cancelled|", "|"+state+"|") || kind != "" && !strings.Contains("|build|deploy|rollback|restart|scale|configure|service-provision|service-suspend|service-resume|", "|"+kind+"|") {
		problem(w, ErrInvalid)
		return
	}
	u, _ := identity.CurrentUser(r.Context())
	rows, e := h.DB.QueryContext(r.Context(), `SELECT `+jobColumns+` FROM runtime_jobs j JOIN applications a ON a.id=j.application_id WHERE `+readRule("a")+` AND ($3='' OR j.state=$3) AND ($4='' OR j.kind=$4) AND ($5='' OR j.application_id::text=$5) AND ($6='' OR j.scope_id::text=$6) ORDER BY j.created_at DESC,j.id LIMIT $7 OFFSET $8`, u.Role, u.ID, state, kind, app, scope, limit+1, offset)
	if e != nil {
		problem(w, e)
		return
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, e := scanJob(rows)
		if e != nil {
			problem(w, e)
			return
		}
		out = append(out, j)
	}
	if rows.Err() != nil {
		problem(w, rows.Err())
		return
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	output(w, 200, map[string]any{"jobs": out, "hasMore": more})
}
func (h *HTTP) job(r *http.Request) (Job, error) {
	j, e := scanJob(h.DB.QueryRowContext(r.Context(), `SELECT `+jobColumns+` FROM runtime_jobs j WHERE j.id=$1`, r.PathValue("job")))
	if errors.Is(e, sql.ErrNoRows) {
		return j, ErrDenied
	}
	if e != nil {
		return j, e
	}
	_, e = appAccess(r.Context(), h.DB, j.ApplicationID)
	return j, e
}
func (h *HTTP) jobDetail(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("job")) {
		return
	}
	j, e := h.job(r)
	if e != nil {
		problem(w, e)
		return
	}
	rows, e := h.DB.QueryContext(r.Context(), `SELECT state,phase,message,created_at FROM runtime_job_events WHERE job_id=$1 ORDER BY id DESC LIMIT 100`, j.ID)
	if e != nil {
		problem(w, e)
		return
	}
	defer rows.Close()
	events := []map[string]any{}
	for rows.Next() {
		var state, phase, message string
		var at time.Time
		if e = rows.Scan(&state, &phase, &message, &at); e != nil {
			problem(w, e)
			return
		}
		events = append(events, map[string]any{"state": state, "phase": phase, "message": message, "createdAt": at})
	}
	if rows.Err() != nil {
		problem(w, rows.Err())
		return
	}
	output(w, 200, map[string]any{"job": j, "events": events})
}
func (h *HTTP) cancelJob(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("job")) {
		return
	}
	j, e := h.job(r)
	if e != nil {
		problem(w, e)
		return
	}
	tx, e := h.DB.BeginTx(r.Context(), nil)
	if e != nil {
		problem(w, e)
		return
	}
	defer tx.Rollback()
	production := false
	if j.ScopeID != nil {
		var env string
		if e = tx.QueryRowContext(r.Context(), `SELECT environment FROM runtime_scopes WHERE id=$1`, *j.ScopeID).Scan(&env); e != nil {
			problem(w, e)
			return
		}
		production = env == "production"
	}
	if e = authorize(r.Context(), tx, j.ApplicationID, production); e != nil {
		problem(w, e)
		return
	}
	result, e := tx.ExecContext(r.Context(), `UPDATE runtime_jobs SET cancel_requested=true,state=CASE WHEN state='queued' THEN 'cancelled' ELSE state END,completed_at=CASE WHEN state='queued' THEN now() ELSE completed_at END,message='Cancelación solicitada; cambios ya aplicados pueden permanecer.' WHERE id=$1 AND state IN ('queued','running') AND NOT cancel_requested`, j.ID)
	if e == nil {
		n, _ := result.RowsAffected()
		if n == 0 {
			e = ErrConflict
		}
	}
	if e == nil {
		e = event(r.Context(), tx, "runtime.cancel_requested", j.ID)
	}
	if e == nil {
		_, e = tx.ExecContext(r.Context(), `INSERT INTO runtime_job_events(job_id,state,phase,message) SELECT id,state,phase,message FROM runtime_jobs WHERE id=$1`, j.ID)
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		problem(w, e)
		return
	}
	output(w, 202, map[string]bool{"ok": true})
}
func (h *HTTP) retryJob(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("job")) {
		return
	}
	j, e := h.job(r)
	if e != nil {
		problem(w, e)
		return
	}
	if j.State != "failed" && j.State != "cancelled" {
		problem(w, ErrConflict)
		return
	}
	if j.Kind == "build" && j.Phase == "dispatch_unknown" {
		problem(w, ErrConflict)
		return
	}
	var scope *Scope
	if j.ScopeID != nil {
		s, e := scanScope(h.DB.QueryRowContext(r.Context(), `SELECT `+scopeColumns+` FROM runtime_scopes s WHERE s.id=$1`, *j.ScopeID))
		if e != nil {
			problem(w, e)
			return
		}
		scope = &s
	}
	var raw []byte
	if e = h.DB.QueryRowContext(r.Context(), `SELECT payload FROM runtime_jobs WHERE id=$1`, j.ID).Scan(&raw); e != nil {
		problem(w, e)
		return
	}
	var p JobPayload
	if json.Unmarshal(raw, &p) != nil {
		problem(w, ErrInvalid)
		return
	}
	next, e := h.enqueue(r.Context(), j.ApplicationID, scope, j.Kind, r.Header.Get("Idempotency-Key"), p, &j.ID)
	if e != nil {
		problem(w, e)
		return
	}
	output(w, 202, next)
}
