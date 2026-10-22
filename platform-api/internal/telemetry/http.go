package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"mini-operador-ti/platform-api/internal/identity"
	"net/http"
	"regexp"
	"time"
)

type HTTP struct {
	DB       *sql.DB
	Metrics  *Metrics
	Instance string
	Auth     *identity.HTTP
}
type Alert struct {
	Stale          bool       `json:"stale"`
	ID             string     `json:"id"`
	Instance       string     `json:"instance"`
	Rule           string     `json:"rule"`
	Severity       string     `json:"severity"`
	Message        string     `json:"message"`
	Active         bool       `json:"active"`
	AcknowledgedAt *time.Time `json:"acknowledgedAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	Occurrences    int        `json:"occurrences"`
}

func output(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (h *HTTP) admin(fn http.HandlerFunc) http.Handler {
	return h.Auth.Protected(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := identity.CurrentUser(r.Context())
		if u.Role != "admin" {
			output(w, 403, map[string]string{"message": "Esta vista requiere administrador de plataforma."})
			return
		}
		fn(w, r)
	}))
}
func (h *HTTP) Routes(m *http.ServeMux) {
	m.Handle("GET /api/operations/status", h.admin(h.status))
	m.Handle("GET /api/operations/alerts", h.admin(h.alerts))
	m.Handle("POST /api/operations/alerts/{id}/ack", h.admin(h.ack))
	m.Handle("GET /api/operations/metrics", h.admin(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		h.Metrics.Prometheus(w)
	}))
}
func (h *HTTP) snapshot(ctx context.Context) Snapshot {
	s := h.Metrics.Snapshot(time.Now())
	s.Instance = h.Instance
	ping, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	s.DatabaseHealthy = h.DB.PingContext(ping) == nil
	stats := h.DB.Stats()
	s.DBOpen = stats.OpenConnections
	s.DBInUse = stats.InUse
	s.DBMax = stats.MaxOpenConnections
	s.DBWaitCount = stats.WaitCount
	return s
}
func (h *HTTP) status(w http.ResponseWriter, r *http.Request) {
	output(w, 200, h.snapshot(r.Context()))
}
func (h *HTTP) alerts(w http.ResponseWriter, r *http.Request) {
	limit, offset, e := identity.Page(r)
	if e != nil {
		output(w, 400, map[string]string{"message": "Paginación inválida."})
		return
	}
	rows, e := h.DB.QueryContext(r.Context(), `SELECT id::text,instance,rule,severity,message,active,(updated_at<=now()-interval '2 minutes'),acknowledged_at,updated_at,occurrences FROM platform_alerts ORDER BY updated_at DESC,id LIMIT $1 OFFSET $2`, limit+1, offset)
	if e != nil {
		output(w, 503, map[string]string{"message": "Alertas no disponibles."})
		return
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		var a Alert
		if e = rows.Scan(&a.ID, &a.Instance, &a.Rule, &a.Severity, &a.Message, &a.Active, &a.Stale, &a.AcknowledgedAt, &a.UpdatedAt, &a.Occurrences); e != nil {
			output(w, 503, map[string]string{"message": "No se pudieron cargar las alertas."})
			return
		}
		out = append(out, a)
	}
	if rows.Err() != nil {
		output(w, 503, map[string]string{"message": "No se pudieron cargar las alertas."})
		return
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	output(w, 200, map[string]any{"alerts": out, "hasMore": more})
}

var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (h *HTTP) ack(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uuid.MatchString(id) {
		output(w, 400, map[string]string{"message": "ID inválido."})
		return
	}
	u, _ := identity.CurrentUser(r.Context())
	tx, e := h.DB.BeginTx(r.Context(), nil)
	if e != nil {
		output(w, 503, map[string]string{"message": "No disponible."})
		return
	}
	defer tx.Rollback()
	var authorized bool
	if e = tx.QueryRowContext(r.Context(), `SELECT active AND role='admin' FROM users WHERE id=$1 FOR SHARE`, u.ID).Scan(&authorized); e != nil || !authorized {
		output(w, 403, map[string]string{"message": "El acceso administrativo cambió. Inicia sesión nuevamente."})
		return
	}
	result, e := tx.ExecContext(r.Context(), `UPDATE platform_alerts SET acknowledged_by=$2,acknowledged_at=now() WHERE id=$1 AND active AND updated_at>now()-interval '2 minutes' AND acknowledged_at IS NULL`, id, u.ID)
	if e != nil {
		output(w, 503, map[string]string{"message": "No disponible."})
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		output(w, 409, map[string]string{"message": "La alerta cambió, ya fue reconocida o dejó de estar activa."})
		return
	}
	if _, e = tx.ExecContext(r.Context(), `INSERT INTO audit_events(actor_id,actor_email,action,resource_id) VALUES($1,$2,'operations.alert_ack',$3)`, u.ID, u.Email, id); e != nil {
		output(w, 503, map[string]string{"message": "No disponible."})
		return
	}
	if tx.Commit() != nil {
		output(w, 503, map[string]string{"message": "No disponible."})
		return
	}
	output(w, 200, map[string]bool{"ok": true})
}
func (h *HTTP) Evaluate(ctx context.Context) error {
	s := h.snapshot(ctx)
	rules := []struct {
		name, severity, message string
		active                  bool
	}{{"http_errors", "critical", "La API supera 5% de respuestas 5xx con al menos 20 solicitudes en la ventana.", s.Requests >= 20 && s.ErrorRate >= .05}, {"http_latency", "warning", "El límite superior estimado de p95 supera 1000 ms con al menos 20 solicitudes.", s.Requests >= 20 && (s.P95UpperMS == nil || *s.P95UpperMS > 1000)}}
	for _, rule := range rules {
		_, e := h.DB.ExecContext(ctx, `INSERT INTO platform_alerts(instance,rule,severity,message,active) SELECT $1,$2,$3,$4,$5 WHERE $5 OR EXISTS(SELECT 1 FROM platform_alerts WHERE instance=$1 AND rule=$2) ON CONFLICT(instance,rule) DO UPDATE SET active=EXCLUDED.active,updated_at=now(),acknowledged_by=CASE WHEN NOT platform_alerts.active AND EXCLUDED.active THEN NULL ELSE platform_alerts.acknowledged_by END,acknowledged_at=CASE WHEN NOT platform_alerts.active AND EXCLUDED.active THEN NULL ELSE platform_alerts.acknowledged_at END,first_seen=CASE WHEN NOT platform_alerts.active AND EXCLUDED.active THEN now() ELSE platform_alerts.first_seen END,occurrences=platform_alerts.occurrences+CASE WHEN NOT platform_alerts.active AND EXCLUDED.active THEN 1 ELSE 0 END`, h.Instance, rule.name, rule.severity, rule.message, rule.active)
		if e != nil {
			return e
		}
	}
	return nil
}
func (h *HTTP) Run(ctx context.Context, onError func(error)) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		evaluation, cancel := context.WithTimeout(ctx, 5*time.Second)
		e := h.Evaluate(evaluation)
		cancel()
		if e != nil && ctx.Err() == nil {
			onError(e)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
