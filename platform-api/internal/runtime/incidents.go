package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"mini-operador-ti/platform-api/internal/delivery"
	"mini-operador-ti/platform-api/internal/identity"
	"net/http"
	"strings"
	"time"
)

type Incident struct {
	ID                string     `json:"id"`
	ApplicationID     string     `json:"applicationId"`
	ScopeID           *string    `json:"scopeId"`
	Title             string     `json:"title"`
	Severity          string     `json:"severity"`
	State             string     `json:"state"`
	AssigneeID        *string    `json:"assigneeId"`
	Runbook           string     `json:"runbook"`
	Version           int64      `json:"version"`
	MutedUntil        *time.Time `json:"mutedUntil"`
	EscalateAt        *time.Time `json:"escalateAt"`
	EscalatedAt       *time.Time `json:"escalatedAt"`
	NotificationState string     `json:"notificationState"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	ResolvedAt        *time.Time `json:"resolvedAt"`
}

const incidentColumns = `i.id::text,i.application_id::text,i.scope_id::text,i.title,i.severity,i.state,i.assignee_id::text,i.runbook,i.version,i.muted_until,i.escalate_at,i.escalated_at,i.notification_state,i.created_at,i.updated_at,i.resolved_at`

func scanIncident(row scanner) (Incident, error) {
	var i Incident
	e := row.Scan(&i.ID, &i.ApplicationID, &i.ScopeID, &i.Title, &i.Severity, &i.State, &i.AssigneeID, &i.Runbook, &i.Version, &i.MutedUntil, &i.EscalateAt, &i.EscalatedAt, &i.NotificationState, &i.CreatedAt, &i.UpdatedAt, &i.ResolvedAt)
	return i, e
}
func (h *HTTP) incidents(w http.ResponseWriter, r *http.Request) {
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
	state := r.URL.Query().Get("state")
	if state != "" && state != "open" && state != "acknowledged" && state != "resolved" {
		problem(w, ErrInvalid)
		return
	}
	rows, e := h.DB.QueryContext(r.Context(), `SELECT `+incidentColumns+` FROM operational_incidents i WHERE application_id=$1 AND ($2='' OR state=$2) ORDER BY updated_at DESC,id LIMIT $3 OFFSET $4`, app, state, limit+1, offset)
	if e != nil {
		problem(w, e)
		return
	}
	defer rows.Close()
	items := []Incident{}
	for rows.Next() {
		v, e := scanIncident(rows)
		if e != nil {
			problem(w, e)
			return
		}
		items = append(items, v)
	}
	if e = rows.Err(); e != nil {
		problem(w, e)
		return
	}
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	output(w, 200, map[string]any{"incidents": items, "hasMore": more, "canEdit": edit})
}
func validResponder(ctx context.Context, tx *sql.Tx, app, id string) bool {
	if !uuidPattern.MatchString(id) {
		return false
	}
	var ok bool
	e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN applications a ON a.id=$1 WHERE u.id=$2 AND u.active AND (u.role='admin' OR a.created_by=u.id OR EXISTS(SELECT 1 FROM team_members m WHERE m.team_id=a.team_id AND m.user_id=u.id)))`, app, id).Scan(&ok)
	return e == nil && ok
}
func (h *HTTP) responders(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	if !validID(w, app) {
		return
	}
	if _, e := appAccess(r.Context(), h.DB, app); e != nil {
		problem(w, e)
		return
	}
	rows, e := h.DB.QueryContext(r.Context(), `SELECT DISTINCT u.id::text,u.name FROM users u JOIN applications a ON a.id=$1 WHERE u.active AND (a.created_by=u.id OR EXISTS(SELECT 1 FROM team_members m WHERE m.team_id=a.team_id AND m.user_id=u.id)) ORDER BY u.name LIMIT 100`, app)
	if e != nil {
		problem(w, e)
		return
	}
	defer rows.Close()
	items := []map[string]string{}
	for rows.Next() {
		var id, name string
		if e = rows.Scan(&id, &name); e != nil {
			problem(w, e)
			return
		}
		items = append(items, map[string]string{"id": id, "name": name})
	}
	if e = rows.Err(); e != nil {
		problem(w, e)
		return
	}
	output(w, 200, map[string]any{"responders": items})
}
func (h *HTTP) createIncident(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	if !validID(w, app) {
		return
	}
	var in struct {
		Title             string `json:"title"`
		Severity          string `json:"severity"`
		ScopeID           string `json:"scopeId"`
		AssigneeID        string `json:"assigneeId"`
		Runbook           string `json:"runbook"`
		EscalationMinutes int    `json:"escalationMinutes"`
		Description       string `json:"description"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !cleanText(in.Title, 160) || !cleanText(in.Description, 2000) || !safeLink(in.Runbook) || (in.Severity != "critical" && in.Severity != "warning" && in.Severity != "info") || in.EscalationMinutes < 1 || in.EscalationMinutes > 1440 || (in.ScopeID != "" && !uuidPattern.MatchString(in.ScopeID)) {
		problem(w, ErrInvalid)
		return
	}
	tx, e := h.appTx(r.Context(), app, false)
	if e != nil {
		problem(w, e)
		return
	}
	defer tx.Rollback()
	var scope any
	if in.ScopeID != "" {
		var ok bool
		if e = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM runtime_scopes WHERE id=$1 AND application_id=$2)`, in.ScopeID, app).Scan(&ok); e != nil || !ok {
			problem(w, ErrInvalid)
			return
		}
		scope = in.ScopeID
	}
	var assignee any
	if in.AssigneeID != "" {
		if !validResponder(r.Context(), tx, app, in.AssigneeID) {
			problem(w, ErrInvalid)
			return
		}
		assignee = in.AssigneeID
	}
	actor, _ := identity.CurrentUser(r.Context())
	i, e := scanIncident(tx.QueryRowContext(r.Context(), `INSERT INTO operational_incidents AS i(application_id,scope_id,title,severity,assignee_id,runbook,escalate_at,created_by)VALUES($1,$2,$3,$4,$5,$6,now()+$7*interval '1 minute',$8) RETURNING `+incidentColumns, app, scope, strings.TrimSpace(in.Title), in.Severity, assignee, in.Runbook, in.EscalationMinutes, actor.ID))
	if e == nil {
		_, e = tx.ExecContext(r.Context(), `INSERT INTO incident_events(incident_id,actor_id,kind,message) VALUES($1,$2,'created',$3)`, i.ID, actor.ID, in.Description)
	}
	if e == nil {
		e = event(r.Context(), tx, "incident.create", i.ID)
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		problem(w, e)
		return
	}
	output(w, 201, i)
}
func (h *HTTP) incident(r *http.Request) (Incident, error) {
	i, e := scanIncident(h.DB.QueryRowContext(r.Context(), `SELECT `+incidentColumns+` FROM operational_incidents i WHERE id=$1`, r.PathValue("incident")))
	if errors.Is(e, sql.ErrNoRows) {
		return i, ErrDenied
	}
	if e == nil {
		_, e = appAccess(r.Context(), h.DB, i.ApplicationID)
	}
	return i, e
}
func (h *HTTP) incidentDetail(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("incident")) {
		return
	}
	i, e := h.incident(r)
	if e != nil {
		problem(w, e)
		return
	}
	rows, e := h.DB.QueryContext(r.Context(), `SELECT e.kind,e.message,e.created_at,COALESCE(u.name,'Sistema') FROM incident_events e LEFT JOIN users u ON u.id=e.actor_id WHERE incident_id=$1 ORDER BY e.id DESC LIMIT 200`, i.ID)
	if e != nil {
		problem(w, e)
		return
	}
	defer rows.Close()
	events := []map[string]any{}
	for rows.Next() {
		var kind, message, name string
		var created time.Time
		if e = rows.Scan(&kind, &message, &created, &name); e != nil {
			problem(w, e)
			return
		}
		events = append(events, map[string]any{"kind": kind, "message": message, "actor": name, "createdAt": created})
	}
	if e = rows.Err(); e != nil {
		problem(w, e)
		return
	}
	var deliveryState string
	e = h.DB.QueryRowContext(r.Context(), `SELECT state FROM delivery_jobs WHERE dedupe_key LIKE $1 ORDER BY created_at DESC LIMIT 1`, "incident:"+i.ID+":escalation:%").Scan(&deliveryState)
	if e == nil {
		i.NotificationState = deliveryState
	}
	output(w, 200, map[string]any{"incident": i, "events": events, "historyLimit": 200})
}
func (h *HTTP) incidentAction(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("incident")) {
		return
	}
	i, e := h.incident(r)
	if e != nil {
		problem(w, e)
		return
	}
	var in struct {
		Action     string `json:"action"`
		Version    int64  `json:"version"`
		Message    string `json:"message"`
		AssigneeID string `json:"assigneeId"`
		Minutes    int    `json:"minutes"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !cleanText(in.Message, 2000) {
		problem(w, ErrInvalid)
		return
	}
	tx, e := h.appTx(r.Context(), i.ApplicationID, false)
	if e != nil {
		problem(w, e)
		return
	}
	defer tx.Rollback()
	i, e = scanIncident(tx.QueryRowContext(r.Context(), `SELECT `+incidentColumns+` FROM operational_incidents i WHERE id=$1 FOR UPDATE`, i.ID))
	if e != nil {
		problem(w, e)
		return
	}
	if i.Version != in.Version {
		problem(w, ErrConflict)
		return
	}
	var query string
	var extra any
	switch in.Action {
	case "acknowledge":
		if i.State != "open" {
			problem(w, ErrConflict)
			return
		}
		query = `state='acknowledged'`
	case "resolve":
		if i.State == "resolved" {
			problem(w, ErrConflict)
			return
		}
		query = `state='resolved',resolved_at=now(),muted_until=NULL`
	case "reopen":
		if i.State != "resolved" {
			problem(w, ErrConflict)
			return
		}
		query = `state='open',resolved_at=NULL,escalated_at=NULL,escalate_at=now()+interval '15 minutes',notification_state='not_configured'`
	case "assign":
		if !validResponder(r.Context(), tx, i.ApplicationID, in.AssigneeID) {
			problem(w, ErrInvalid)
			return
		}
		query = `assignee_id=$3`
		extra = in.AssigneeID
	case "mute":
		if i.State == "resolved" || in.Minutes < 1 || in.Minutes > 1440 {
			problem(w, ErrInvalid)
			return
		}
		query = `muted_until=now()+$3*interval '1 minute'`
		extra = in.Minutes
	case "unmute":
		query = `muted_until=NULL`
	case "comment":
		query = `title=title`
	default:
		problem(w, ErrInvalid)
		return
	}
	args := []any{i.ID, in.Version}
	if extra != nil {
		args = append(args, extra)
	}
	_, e = tx.ExecContext(r.Context(), `UPDATE operational_incidents SET `+query+`,version=version+1,updated_at=now() WHERE id=$1 AND version=$2`, args...)
	actor, _ := identity.CurrentUser(r.Context())
	if e == nil {
		_, e = tx.ExecContext(r.Context(), `INSERT INTO incident_events(incident_id,actor_id,kind,message)VALUES($1,$2,$3,$4)`, i.ID, actor.ID, in.Action, in.Message)
	}
	if e == nil {
		e = event(r.Context(), tx, "incident."+in.Action, i.ID)
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

// Escalations are atomically marked and deduplicated; mute never resolves an incident.
func (h *HTTP) EscalateIncidents(ctx context.Context) error {
	tx, e := h.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	rows, e := tx.QueryContext(ctx, `SELECT i.id::text,i.title,i.version FROM operational_incidents i JOIN applications a ON a.id=i.application_id WHERE i.state<>'resolved' AND a.archived_at IS NULL AND i.escalated_at IS NULL AND i.escalate_at<=now() AND (i.muted_until IS NULL OR i.muted_until<=now()) ORDER BY i.escalate_at FOR UPDATE OF i SKIP LOCKED LIMIT 50`)
	if e != nil {
		return e
	}
	type due struct {
		id, title string
		version   int64
	}
	items := []due{}
	for rows.Next() {
		var d due
		if e = rows.Scan(&d.id, &d.title, &d.version); e != nil {
			rows.Close()
			return e
		}
		items = append(items, d)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, d := range items {
		state := "not_configured"
		if h.Queue != nil && h.WebhookEnabled {
			state = "queued"
			e = h.Queue.Enqueue(ctx, tx, fmt.Sprintf("incident:%s:escalation:%d", d.id, d.version), "webhook", delivery.Message{Event: "incident.escalated", ID: d.id, Text: fmt.Sprintf("Incidente requiere atención: %s (%s)", d.title, d.id)}, time.Now().Add(24*time.Hour))
			if e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE operational_incidents SET escalated_at=now(),notification_state=$2,version=version+1,updated_at=now() WHERE id=$1`, d.id, state); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO incident_events(incident_id,kind,message)VALUES($1,'escalated',$2)`, d.id, "Plazo de atención vencido; notificación: "+state); e != nil {
			return e
		}
	}
	return tx.Commit()
}
