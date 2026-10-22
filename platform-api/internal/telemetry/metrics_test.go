package telemetry

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWindowAndQuantile(t *testing.T) {
	m := New()
	now := time.Now()
	for range 19 {
		m.Observe(now, 200, 40*time.Millisecond)
	}
	m.Observe(now, 503, 700*time.Millisecond)
	s := m.Snapshot(now)
	if s.Requests != 20 || s.Errors != 1 || s.ErrorRate != .05 || s.P95UpperMS == nil || *s.P95UpperMS != 50 {
		t.Fatalf("bad aggregation: %+v", s)
	}
	if m.Snapshot(now.Add(301*time.Second)).Requests != 0 {
		t.Fatal("window never expired")
	}
	var out bytes.Buffer
	m.Prometheus(&out)
	if !strings.Contains(out.String(), `mini_fury_http_request_duration_seconds_bucket{le="+Inf"} 20`) {
		t.Fatal("histogram not cumulative")
	}
}
func TestConcurrentObservationAndRejections(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				m.Observe(time.Now(), 500, time.Millisecond)
				m.Snapshot(time.Now())
			}
		}()
	}
	wg.Wait()
	if m.Snapshot(time.Now()).Requests != 2000 {
		t.Fatal("lost observations")
	}
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/applications", nil))
	if m.Snapshot(time.Now()).Requests != 2001 {
		t.Fatal("rejection was not measured")
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/operations/status", nil))
	if m.Snapshot(time.Now()).Requests != 2001 {
		t.Fatal("observer poll polluted service metrics")
	}
}
