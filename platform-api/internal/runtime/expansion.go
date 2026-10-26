package runtime

import (
	"context"
	"database/sql"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (h *HTTP) expansionRoutes(m *http.ServeMux) {
	handle := func(p string, f http.HandlerFunc) { m.Handle(p, h.Auth.Protected(f)) }
	handle("GET /api/applications/{app}/incidents", h.incidents)
	handle("POST /api/applications/{app}/incidents", h.createIncident)
	handle("GET /api/incidents/{incident}", h.incidentDetail)
	handle("POST /api/incidents/{incident}/actions", h.incidentAction)
	handle("GET /api/applications/{app}/responders", h.responders)
	handle("GET /api/scopes/{scope}/slos", h.slos)
	handle("POST /api/scopes/{scope}/slos", h.createSLO)
	handle("POST /api/slos/{slo}/enabled", h.toggleSLO)
	handle("POST /api/scopes/{scope}/slos/evaluate", h.evaluateSLO)
	handle("GET /api/templates", h.templates)
	handle("POST /api/templates/{template}/applications", h.scaffold)
	handle("GET /api/applications/{app}/scaffold", h.scaffoldDownload)
	handle("GET /api/applications/{app}/template-status", h.templateStatus)
	handle("GET /api/scopes/{scope}/service-profiles", h.serviceProfiles)
	handle("GET /api/scopes/{scope}/services", h.services)
	handle("POST /api/scopes/{scope}/services", h.requestService)
	handle("POST /api/services/{service}/actions", h.serviceAction)
	handle("GET /api/services/{service}/status", h.serviceStatus)
	handle("GET /api/applications/{app}/capacity", h.capacity)
	handle("PUT /api/applications/{app}/cost-policy", h.costPolicy)
}
func cleanText(s string, max int) bool {
	return strings.TrimSpace(s) != "" && len(s) <= max && !strings.ContainsAny(s, "\x00")
}
func safeLink(s string) bool {
	if s == "" {
		return true
	}
	u, e := url.Parse(s)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && len(s) <= 1000
}
func (h *HTTP) appTx(ctx context.Context, app string, production bool) (*sql.Tx, error) {
	tx, e := h.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	if e = authorize(ctx, tx, app, production); e != nil {
		tx.Rollback()
		return nil, e
	}
	return tx, nil
}
func (h *HTTP) RunExpansion(ctx context.Context, onError func(error)) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			work, cancel := context.WithTimeout(ctx, 45*time.Second)
			if e := h.EscalateIncidents(work); e != nil && !errors.Is(e, context.Canceled) {
				onError(e)
			}
			if e := h.collectDueSLOs(work); e != nil && !errors.Is(e, context.Canceled) {
				onError(e)
			}
			h.DB.ExecContext(work, `DELETE FROM slo_measurements WHERE measured_at<now()-interval '35 days'; DELETE FROM slo_counter_state WHERE sampled_at<now()-interval '2 days'`)
			cancel()
		}
	}
}

func conflictError(e error) error {
	var p *pgconn.PgError
	if errors.As(e, &p) && p.Code == "23505" {
		return ErrConflict
	}
	return e
}
