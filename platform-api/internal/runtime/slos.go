package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"mini-operador-ti/platform-api/internal/identity"
	"net/http"
	"time"
)

type SLO struct {
	ID         string `json:"id"`
	ScopeID    string `json:"scopeId"`
	Name       string `json:"name"`
	Indicator  string `json:"indicator"`
	TargetPPM  int64  `json:"targetPpm"`
	LatencyMS  int    `json:"latencyMs"`
	WindowDays int    `json:"windowDays"`
	Version    int64  `json:"version"`
	Enabled    bool   `json:"enabled"`
	CreatedBy  string `json:"-"`
}

const sloColumns = `id::text,scope_id::text,name,indicator,target_ppm,latency_ms,window_days,version,enabled,created_by::text`

func scanSLO(row scanner) (SLO, error) {
	var s SLO
	e := row.Scan(&s.ID, &s.ScopeID, &s.Name, &s.Indicator, &s.TargetPPM, &s.LatencyMS, &s.WindowDays, &s.Version, &s.Enabled, &s.CreatedBy)
	return s, e
}

type SLOResult struct {
	SLO                    SLO        `json:"slo"`
	State                  string     `json:"state"`
	Total                  int64      `json:"total"`
	Good                   int64      `json:"good"`
	SLIPercent             *float64   `json:"sliPercent"`
	BudgetRemainingPercent *float64   `json:"budgetRemainingPercent"`
	Burn5m                 *float64   `json:"burn5m"`
	Burn1h                 *float64   `json:"burn1h"`
	Samples                int        `json:"samples"`
	CompleteSamples        int        `json:"completeSamples"`
	FirstAt                *time.Time `json:"firstAt"`
	LastAt                 *time.Time `json:"lastAt"`
	Source                 string     `json:"source"`
}

func (h *HTTP) sloResult(ctx context.Context, s SLO) (SLOResult, error) {
	out := SLOResult{SLO: s, State: "no_data", Source: "Contadores HTTP instrumentados por pod; no son probes ni puntuación documental"}
	var total5, good5, total1, good1 int64
	e := h.DB.QueryRowContext(ctx, `SELECT COALESCE(sum(total) FILTER(WHERE complete),0),COALESCE(sum(good) FILTER(WHERE complete),0),count(*),count(*) FILTER(WHERE complete),min(measured_at),max(measured_at),COALESCE(sum(total) FILTER(WHERE complete AND measured_at>now()-interval '5 minutes'),0),COALESCE(sum(good) FILTER(WHERE complete AND measured_at>now()-interval '5 minutes'),0),COALESCE(sum(total) FILTER(WHERE complete AND measured_at>now()-interval '1 hour'),0),COALESCE(sum(good) FILTER(WHERE complete AND measured_at>now()-interval '1 hour'),0) FROM slo_measurements WHERE slo_id=$1 AND measured_at>now()-$2*interval '1 day'`, s.ID, s.WindowDays).Scan(&out.Total, &out.Good, &out.Samples, &out.CompleteSamples, &out.FirstAt, &out.LastAt, &total5, &good5, &total1, &good1)
	if e != nil {
		return out, e
	}
	if !s.Enabled {
		out.State = "disabled"
		return out, nil
	}
	if out.Total == 0 {
		return out, nil
	}
	sli := 100 * float64(out.Good) / float64(out.Total)
	remaining := 100 * (1 - burn(out.Total, out.Good, s.TargetPPM))
	out.SLIPercent = &sli
	out.BudgetRemainingPercent = &remaining
	if total5 > 0 {
		v := burn(total5, good5, s.TargetPPM)
		out.Burn5m = &v
	}
	if total1 > 0 {
		v := burn(total1, good1, s.TargetPPM)
		out.Burn1h = &v
	}
	out.State = "observing"
	if out.LastAt == nil || time.Since(*out.LastAt) > 2*time.Minute {
		out.State = "stale"
	} else if out.CompleteSamples < out.Samples {
		out.State = "partial"
	} else if remaining < 0 {
		out.State = "budget_exhausted"
	}
	return out, nil
}
func burn(total, good, target int64) float64 {
	if total <= 0 {
		return 0
	}
	return (float64(total-good) / float64(total)) / (1 - float64(target)/1000000)
}
func (h *HTTP) slos(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("scope")) {
		return
	}
	scope, e := h.scope(r)
	if e != nil {
		problem(w, e)
		return
	}
	rows, e := h.DB.QueryContext(r.Context(), `SELECT `+sloColumns+` FROM scope_slos WHERE scope_id=$1 ORDER BY created_at DESC LIMIT 10`, scope.ID)
	if e != nil {
		problem(w, e)
		return
	}
	items := []SLO{}
	for rows.Next() {
		s, e := scanSLO(rows)
		if e != nil {
			rows.Close()
			problem(w, e)
			return
		}
		items = append(items, s)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		problem(w, e)
		return
	}
	out := []SLOResult{}
	for _, s := range items {
		v, e := h.sloResult(r.Context(), s)
		if e != nil {
			problem(w, e)
			return
		}
		out = append(out, v)
	}
	output(w, 200, map[string]any{"slos": out, "canEdit": scope.CanEdit, "retentionDays": 35})
}
func (h *HTTP) createSLO(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("scope")) {
		return
	}
	scope, e := h.scope(r)
	if e != nil {
		problem(w, e)
		return
	}
	var in struct {
		Name       string `json:"name"`
		Indicator  string `json:"indicator"`
		TargetPPM  int64  `json:"targetPpm"`
		LatencyMS  int    `json:"latencyMs"`
		WindowDays int    `json:"windowDays"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !cleanText(in.Name, 80) || (in.Indicator != "availability" && in.Indicator != "latency") || in.TargetPPM < 900000 || in.TargetPPM > 999990 || (in.LatencyMS != 100 && in.LatencyMS != 300 && in.LatencyMS != 1000) || (in.WindowDays != 1 && in.WindowDays != 7 && in.WindowDays != 28) {
		problem(w, ErrInvalid)
		return
	}
	tx, e := h.appTx(r.Context(), scope.ApplicationID, scope.Environment == "production")
	if e != nil {
		problem(w, e)
		return
	}
	defer tx.Rollback()
	var locked string
	if e = tx.QueryRowContext(r.Context(), `SELECT id::text FROM runtime_scopes WHERE id=$1 FOR UPDATE`, scope.ID).Scan(&locked); e != nil {
		problem(w, e)
		return
	}
	var n int
	if e = tx.QueryRowContext(r.Context(), `SELECT count(*) FROM scope_slos WHERE scope_id=$1`, scope.ID).Scan(&n); e != nil {
		problem(w, e)
		return
	}
	if n >= 10 {
		problem(w, ErrConflict)
		return
	}
	actor, _ := identity.CurrentUser(r.Context())
	s, e := scanSLO(tx.QueryRowContext(r.Context(), `INSERT INTO scope_slos(scope_id,name,indicator,target_ppm,latency_ms,window_days,created_by)VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING `+sloColumns, scope.ID, in.Name, in.Indicator, in.TargetPPM, in.LatencyMS, in.WindowDays, actor.ID))
	if e == nil {
		e = event(r.Context(), tx, "slo.create", s.ID)
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		problem(w, conflictError(e))
		return
	}
	output(w, 201, s)
}
func (h *HTTP) toggleSLO(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("slo")) {
		return
	}
	s, e := scanSLO(h.DB.QueryRowContext(r.Context(), `SELECT `+sloColumns+` FROM scope_slos WHERE id=$1`, r.PathValue("slo")))
	if e != nil {
		problem(w, ErrDenied)
		return
	}
	r.SetPathValue("scope", s.ScopeID)
	scope, e := h.scope(r)
	if e != nil {
		problem(w, e)
		return
	}
	var in struct {
		Enabled bool  `json:"enabled"`
		Version int64 `json:"version"`
	}
	if !decode(w, r, &in) {
		return
	}
	tx, e := h.appTx(r.Context(), scope.ApplicationID, scope.Environment == "production")
	if e != nil {
		problem(w, e)
		return
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(r.Context(), `UPDATE scope_slos SET enabled=$2,version=version+1 WHERE id=$1 AND version=$3`, s.ID, in.Enabled, in.Version)
	if e == nil {
		n, _ := result.RowsAffected()
		if n == 0 {
			e = ErrConflict
		}
	}
	if e == nil {
		_, e = tx.ExecContext(r.Context(), `DELETE FROM slo_counter_state WHERE slo_id=$1`, s.ID)
	}
	if e == nil {
		e = event(r.Context(), tx, "slo.toggle", s.ID)
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

// Counter payload has no user data. A pod restart starts a baseline, never a negative delta.
type RequestCounters struct {
	Epoch  string `json:"epoch"`
	Total  int64  `json:"total"`
	Errors int64  `json:"errors"`
	Le100  int64  `json:"le100"`
	Le300  int64  `json:"le300"`
	Le1000 int64  `json:"le1000"`
}

func (c RequestCounters) valid() bool {
	return cleanText(c.Epoch, 100) && c.Total >= 0 && c.Total <= 9000000000000000 && c.Errors >= 0 && c.Errors <= c.Total && c.Le100 >= 0 && c.Le100 <= c.Le300 && c.Le300 <= c.Le1000 && c.Le1000 <= c.Total
}
func (c RequestCounters) good(s SLO) int64 {
	if s.Indicator == "availability" {
		return c.Total - c.Errors
	}
	if s.LatencyMS == 100 {
		return c.Le100
	}
	if s.LatencyMS == 300 {
		return c.Le300
	}
	return c.Le1000
}
func (h *HTTP) collectSLO(ctx context.Context, s SLO) error {
	scope, e := scanScope(h.DB.QueryRowContext(ctx, `SELECT `+scopeColumns+` FROM runtime_scopes s JOIN applications a ON a.id=s.application_id WHERE s.id=$1 AND a.archived_at IS NULL`, s.ScopeID))
	if e != nil {
		return e
	}
	k, e := h.kube(scope)
	if e != nil {
		return e
	}
	// A transaction-scoped advisory lock avoids duplicate counter consumption across API instances.
	tx, e := h.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var locked bool
	if e = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,77827))`, s.ID).Scan(&locked); e != nil || !locked {
		return e
	}
	var enabled bool
	if e = tx.QueryRowContext(ctx, `SELECT enabled FROM scope_slos WHERE id=$1 FOR SHARE`, s.ID).Scan(&enabled); e != nil || !enabled {
		return e
	}
	pods, more, e := k.pods(ctx, scope)
	if e != nil {
		return e
	}
	expected := scope.Replicas
	observed := 0
	complete := !more
	var total, good int64
	var port int
	if e = tx.QueryRowContext(ctx, `SELECT port FROM applications WHERE id=$1`, scope.ApplicationID).Scan(&port); e != nil {
		return e
	}
	for i, p := range pods {
		if i >= 10 {
			complete = false
			break
		}
		ready := false
		for _, c := range p.Status.ContainerStatuses {
			if c.Ready {
				ready = true
			}
		}
		if !ready {
			continue
		}
		if _, e = k.podOwned(ctx, scope, p.Metadata.Name); e != nil {
			complete = false
			continue
		}
		var c RequestCounters
		fetch, cancel := context.WithTimeout(ctx, 2*time.Second)
		e = k.request(fetch, "GET", fmt.Sprintf("/api/v1/namespaces/%s/pods/http:%s:%d/proxy/mini-fury/metrics", scope.Namespace, p.Metadata.Name, port), nil, nil, &c)
		cancel()
		if e != nil || !c.valid() {
			complete = false
			continue
		}
		observed++
		var oldEpoch string
		var oldTotal, oldGood int64
		var sampled time.Time
		e = tx.QueryRowContext(ctx, `SELECT epoch,total,good,sampled_at FROM slo_counter_state WHERE slo_id=$1 AND pod_uid=$2 FOR UPDATE`, s.ID, p.Metadata.UID).Scan(&oldEpoch, &oldTotal, &oldGood, &sampled)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		g := c.good(s)
		if e == nil && c.Epoch == oldEpoch && c.Total >= oldTotal && g >= oldGood && g-oldGood <= c.Total-oldTotal && time.Since(sampled) <= 2*time.Minute {
			total += c.Total - oldTotal
			good += g - oldGood
		} else {
			complete = false
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO slo_counter_state(slo_id,pod_uid,epoch,total,good,sampled_at)VALUES($1,$2,$3,$4,$5,now()) ON CONFLICT(slo_id,pod_uid) DO UPDATE SET epoch=EXCLUDED.epoch,total=EXCLUDED.total,good=EXCLUDED.good,sampled_at=now()`, s.ID, p.Metadata.UID, c.Epoch, c.Total, g); e != nil {
			return e
		}
	}
	complete = complete && observed == expected
	if _, e = tx.ExecContext(ctx, `INSERT INTO slo_measurements(slo_id,total,good,observed_pods,expected_pods,complete)VALUES($1,$2,$3,$4,$5,$6)`, s.ID, total, good, observed, expected, complete); e != nil {
		return e
	}
	if complete && total >= 20 && burn(total, good, s.TargetPPM) >= 14.4 {
		var id string
		e = tx.QueryRowContext(ctx, `INSERT INTO operational_incidents(application_id,scope_id,title,severity,source_key,escalate_at,created_by)VALUES($1,$2,$3,'critical',$4,now()+interval '15 minutes',$5) ON CONFLICT(application_id,source_key) WHERE state<>'resolved' AND source_key IS NOT NULL DO NOTHING RETURNING id::text`, scope.ApplicationID, scope.ID, "Consumo rápido del presupuesto SLO: "+s.Name, "slo:"+s.ID, s.CreatedBy).Scan(&id)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e == nil {
			if _, e = tx.ExecContext(ctx, `INSERT INTO incident_events(incident_id,kind,message)VALUES($1,'slo_burn','Contadores reales: burn de intervalo >=14.4 con al menos 20 solicitudes. Revisar cobertura y ventanas antes de decidir.')`, id); e != nil {
				return e
			}
		}
	}
	return tx.Commit()
}
func (h *HTTP) evaluateSLO(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("scope")) {
		return
	}
	scope, e := h.scope(r)
	if e != nil {
		problem(w, e)
		return
	}
	tx, e := h.appTx(r.Context(), scope.ApplicationID, scope.Environment == "production")
	if e != nil {
		problem(w, e)
		return
	}
	tx.Rollback()
	release, ok := observationPermit(r.Context())
	if !ok {
		output(w, 429, map[string]string{"message": "Evaluación ocupada."})
		return
	}
	defer release()
	rows, e := h.DB.QueryContext(r.Context(), `SELECT `+sloColumns+` FROM scope_slos WHERE scope_id=$1 AND enabled ORDER BY id LIMIT 10`, scope.ID)
	if e != nil {
		problem(w, e)
		return
	}
	items := []SLO{}
	for rows.Next() {
		s, e := scanSLO(rows)
		if e != nil {
			rows.Close()
			problem(w, e)
			return
		}
		items = append(items, s)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		problem(w, e)
		return
	}
	for _, s := range items {
		if e = h.collectSLO(r.Context(), s); e != nil {
			problem(w, e)
			return
		}
	}
	output(w, 200, map[string]any{"evaluated": len(items)})
}
func (h *HTTP) collectDueSLOs(ctx context.Context) error {
	rows, e := h.DB.QueryContext(ctx, `SELECT `+sloColumns+` FROM scope_slos s WHERE enabled AND NOT EXISTS(SELECT 1 FROM slo_measurements m WHERE m.slo_id=s.id AND m.measured_at>now()-interval '55 seconds') ORDER BY COALESCE((SELECT max(measured_at) FROM slo_measurements WHERE slo_id=s.id),'epoch'::timestamptz) LIMIT 20`)
	if e != nil {
		return e
	}
	items := []SLO{}
	for rows.Next() {
		s, e := scanSLO(rows)
		if e != nil {
			rows.Close()
			return e
		}
		items = append(items, s)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, s := range items {
		work, cancel := context.WithTimeout(ctx, 6*time.Second)
		e = h.collectSLO(work, s)
		cancel()
		if e != nil {
			if ctx.Err() != nil {
				return e
			}
			// Record a missing interval, never a successful request, and rotate failed sources fairly.
			_, recordErr := h.DB.ExecContext(ctx, `INSERT INTO slo_measurements(slo_id,total,good,observed_pods,expected_pods,complete)SELECT $1,0,0,0,replicas,false FROM runtime_scopes WHERE id=$2`, s.ID, s.ScopeID)
			if recordErr != nil {
				return recordErr
			}
		}
	}
	return nil
}
