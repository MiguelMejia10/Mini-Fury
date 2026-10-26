package runtime

import (
	"context"
	"errors"
	"mini-operador-ti/platform-api/internal/identity"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// Two concurrent cluster reads per account and eight per API instance; entries expire to bound the map.
type gateEntry struct {
	n int
	t time.Time
}

var observationGate = struct {
	sync.Mutex
	users map[string]gateEntry
	total int
}{users: map[string]gateEntry{}}

func observationPermit(ctx context.Context) (func(), bool) {
	u, _ := identity.CurrentUser(ctx)
	observationGate.Lock()
	defer observationGate.Unlock()
	for id, v := range observationGate.users {
		if v.n == 0 && time.Since(v.t) > time.Minute {
			delete(observationGate.users, id)
		}
	}
	v := observationGate.users[u.ID]
	if v.n >= 2 || observationGate.total >= 8 || (v.n == 0 && len(observationGate.users) >= 1024) {
		return func() {}, false
	}
	v.n++
	v.t = time.Now()
	observationGate.users[u.ID] = v
	observationGate.total++
	return func() {
		observationGate.Lock()
		defer observationGate.Unlock()
		v := observationGate.users[u.ID]
		v.n--
		v.t = time.Now()
		observationGate.users[u.ID] = v
		observationGate.total--
	}, true
}
func (h *HTTP) kube(s Scope) (*Kube, error) {
	c, _, ok := h.Config.binding(s.ApplicationID, s.ClusterID, s.Namespace)
	if !ok {
		return nil, ErrDenied
	}
	if h.KubeFactory != nil {
		return h.KubeFactory(c)
	}
	h.clientsMu.Lock()
	defer h.clientsMu.Unlock()
	if h.clients == nil {
		h.clients = map[string]*Kube{}
	}
	if client := h.clients[c.ID]; client != nil {
		return client, nil
	}
	client, e := NewKube(c)
	if e == nil {
		h.clients[c.ID] = client
	}
	return client, e
}
func (h *HTTP) observe(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("scope")) {
		return
	}
	s, e := h.scope(r)
	if e != nil {
		problem(w, e)
		return
	}
	release, ok := observationPermit(r.Context())
	if !ok {
		w.Header().Set("Retry-After", "2")
		output(w, 429, map[string]string{"message": "Demasiadas consultas al clúster. Intenta en unos segundos."})
		return
	}
	defer release()
	k, e := h.kube(s)
	if e != nil {
		problem(w, e)
		return
	}
	d, e := k.deployment(r.Context(), s)
	if e != nil {
		var ke KubeError
		if errors.As(e, &ke) && ke.Status == 404 {
			output(w, 200, map[string]any{"deployed": false, "pods": []Pod{}, "events": []any{}, "metricsAvailable": false, "message": "El scope aún no tiene un workload desplegado."})
			return
		}
		problem(w, e)
		return
	}
	pods, more, e := k.pods(r.Context(), s)
	if e != nil {
		problem(w, e)
		return
	}
	uids := map[string]bool{d.Metadata.UID: true}
	for _, p := range pods {
		uids[p.Metadata.UID] = true
	}
	var events struct {
		Items []struct {
			InvolvedObject struct {
				UID  string `json:"uid"`
				Name string `json:"name"`
			} `json:"involvedObject"`
			Type          string `json:"type"`
			Reason        string `json:"reason"`
			Message       string `json:"message"`
			LastTimestamp string `json:"lastTimestamp"`
			Count         int    `json:"count"`
		} `json:"items"`
	}
	eventsError := k.request(r.Context(), "GET", "/api/v1/namespaces/"+s.Namespace+"/events", url.Values{"limit": {"200"}}, nil, &events)
	visible := []any{}
	if eventsError == nil {
		for _, v := range events.Items {
			if uids[v.InvolvedObject.UID] {
				visible = append(visible, v)
			}
		}
	}
	var metrics struct {
		Items []struct {
			Metadata   ObjectMeta `json:"metadata"`
			Containers []struct {
				Name  string            `json:"name"`
				Usage map[string]string `json:"usage"`
			} `json:"containers"`
		} `json:"items"`
	}
	metricErr := k.request(r.Context(), "GET", "/apis/metrics.k8s.io/v1beta1/namespaces/"+s.Namespace+"/pods", nil, nil, &metrics)
	allowed := map[string]bool{}
	for _, p := range pods {
		allowed[p.Metadata.Name] = true
	}
	usage := []any{}
	if metricErr == nil {
		for _, v := range metrics.Items {
			if allowed[v.Metadata.Name] {
				usage = append(usage, map[string]any{"pod": v.Metadata.Name, "containers": v.Containers})
			}
		}
	}
	output(w, 200, map[string]any{"deployed": true, "workload": s.Workload, "status": d.Status, "pods": pods, "hasMorePods": more, "events": visible, "eventsAvailable": eventsError == nil, "metricsAvailable": metricErr == nil, "usage": usage, "receivedAt": time.Now()})
}
func (h *HTTP) podLogs(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("scope")) {
		return
	}
	s, e := h.scope(r)
	if e != nil {
		problem(w, e)
		return
	}
	release, ok := observationPermit(r.Context())
	if !ok {
		output(w, 429, map[string]string{"message": "Espera antes de consultar más logs."})
		return
	}
	defer release()
	lines := 200
	if raw := r.URL.Query().Get("lines"); raw != "" {
		lines, e = strconv.Atoi(raw)
	}
	if e != nil || lines < 1 || lines > 500 {
		problem(w, ErrInvalid)
		return
	}
	k, e := h.kube(s)
	if e != nil {
		problem(w, e)
		return
	}
	text, truncated, e := k.logs(r.Context(), s, r.PathValue("pod"), r.URL.Query().Get("container"), lines, r.URL.Query().Get("previous") == "true")
	if e != nil {
		problem(w, e)
		return
	}
	output(w, 200, map[string]any{"text": text, "truncated": truncated, "receivedAt": time.Now()})
}
