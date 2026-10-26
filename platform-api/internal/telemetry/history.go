package telemetry

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type History struct {
	LokiURL, PrometheusURL, GrafanaURL string
	Client                             *http.Client
}

func (h *History) get(ctx context.Context, base, path string, query url.Values, out any) error {
	if base == "" {
		return errors.New("disabled")
	}
	u, e := url.Parse(base)
	if e != nil {
		return e
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = query.Encode()
	req, e := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if e != nil {
		return e
	}
	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: 6 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	res, e := client.Do(req)
	if e != nil {
		return errors.New("history unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return errors.New("history rejected")
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, 2*1024*1024+1))
	if e != nil || len(raw) > 2*1024*1024 {
		return errors.New("history too large")
	}
	return json.Unmarshal(raw, out)
}
func historyRange(r *http.Request) (time.Time, time.Time, error) {
	until := time.Now()
	from := until.Add(-time.Hour)
	var e error
	if raw := r.URL.Query().Get("from"); raw != "" {
		from, e = time.Parse(time.RFC3339, raw)
		if e != nil {
			return from, until, e
		}
	}
	if raw := r.URL.Query().Get("until"); raw != "" {
		until, e = time.Parse(time.RFC3339, raw)
		if e != nil {
			return from, until, e
		}
	}
	if !from.Before(until) || until.Sub(from) > 24*time.Hour || from.Before(time.Now().Add(-7*24*time.Hour)) || until.After(time.Now().Add(time.Minute)) {
		return from, until, errors.New("range invalid")
	}
	return from, until, nil
}
func (h *HTTP) logs(w http.ResponseWriter, r *http.Request) {
	if h.History == nil || h.History.LokiURL == "" {
		output(w, 503, map[string]string{"message": "El histórico Loki aún no está configurado."})
		return
	}
	from, until, e := historyRange(r)
	if e != nil {
		output(w, 400, map[string]string{"message": "Usa un rango de hasta 24 horas dentro de los últimos siete días."})
		return
	}
	limit := 200
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, e = strconv.Atoi(raw)
	}
	contains := r.URL.Query().Get("contains")
	level := r.URL.Query().Get("level")
	if e != nil || limit < 1 || limit > 500 || len(contains) > 200 || (level != "" && level != "info" && level != "warn" && level != "error") {
		output(w, 400, map[string]string{"message": "Filtros inválidos."})
		return
	}
	query := `{app="mini-fury"}`
	if contains != "" {
		query += " |= " + strconv.Quote(contains)
	}
	if level != "" {
		query += " | json | level=" + strconv.Quote(level)
	}
	var result struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Values [][]string `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	e = h.History.get(r.Context(), h.History.LokiURL, "/loki/api/v1/query_range", url.Values{"query": {query}, "start": {strconv.FormatInt(from.UnixNano(), 10)}, "end": {strconv.FormatInt(until.UnixNano(), 10)}, "direction": {"backward"}, "limit": {strconv.Itoa(limit)}}, &result)
	if e != nil || result.Status != "success" {
		output(w, 503, map[string]string{"message": "No se pudo consultar Loki. Intenta nuevamente."})
		return
	}
	type line struct {
		Timestamp string `json:"timestamp"`
		Text      string `json:"text"`
	}
	lines := []line{}
	for _, stream := range result.Data.Result {
		for _, v := range stream.Values {
			if len(v) == 2 {
				lines = append(lines, line{v[0], v[1]})
			}
		}
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].Timestamp > lines[j].Timestamp })
	truncated := len(lines) >= limit
	if len(lines) > limit {
		lines = lines[:limit]
	}
	output(w, 200, map[string]any{"lines": lines, "truncated": truncated, "from": from, "until": until})
}

var historyQueries = map[string]string{"requests": `sum(rate(mini_fury_http_requests_total[2m]))`, "errors": `sum(rate(mini_fury_http_errors_total[2m]))`, "latency": `histogram_quantile(0.95,sum by(le)(rate(mini_fury_http_request_duration_seconds_bucket[2m])))`}

func (h *HTTP) history(w http.ResponseWriter, r *http.Request) {
	if h.History == nil || h.History.PrometheusURL == "" {
		output(w, 503, map[string]string{"message": "El histórico Prometheus aún no está configurado."})
		return
	}
	from, until, e := historyRange(r)
	expression, ok := historyQueries[r.URL.Query().Get("metric")]
	if e != nil || !ok {
		output(w, 400, map[string]string{"message": "Rango o métrica inválidos."})
		return
	}
	step := int(until.Sub(from).Seconds() / 240)
	if step < 15 {
		step = 15
	}
	var result struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	e = h.History.get(r.Context(), h.History.PrometheusURL, "/api/v1/query_range", url.Values{"query": {expression}, "start": {strconv.FormatInt(from.Unix(), 10)}, "end": {strconv.FormatInt(until.Unix(), 10)}, "step": {strconv.Itoa(step)}}, &result)
	if e != nil || result.Status != "success" {
		output(w, 503, map[string]string{"message": "No se pudo consultar el histórico de métricas."})
		return
	}
	output(w, 200, result.Data)
}
func (h *HTTP) integrations(w http.ResponseWriter, r *http.Request) {
	var pending, failed int
	if e := h.DB.QueryRowContext(r.Context(), `SELECT count(*) FILTER(WHERE state IN ('pending','processing') AND expires_at>now()),count(*) FILTER(WHERE state='failed') FROM delivery_jobs`).Scan(&pending, &failed); e != nil {
		output(w, 503, map[string]string{"message": "Estado de entregas no disponible."})
		return
	}
	grafana := ""
	loki, prom := false, false
	if h.History != nil {
		grafana = h.History.GrafanaURL
		loki = h.History.LokiURL != ""
		prom = h.History.PrometheusURL != ""
	}
	output(w, 200, map[string]any{"logsConfigured": loki, "metricsConfigured": prom, "grafanaUrl": grafana, "webhookConfigured": h.WebhookEnabled, "deliveryPending": pending, "deliveryFailed": failed})
}
func (h *HTTP) internalMetrics(w http.ResponseWriter, r *http.Request) {
	token, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !bearer || h.MetricsToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(h.MetricsToken)) != 1 {
		output(w, 401, map[string]string{"message": "Identidad de servicio requerida."})
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	h.Metrics.Prometheus(w)
}
