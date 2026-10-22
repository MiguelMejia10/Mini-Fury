// Package telemetry uses bounded in-memory windows; no request bodies, users or URLs are labels.
package telemetry

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

var bounds = [...]float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000, math.Inf(1)}

type bucket struct {
	Second           int64
	Requests, Errors uint64
	Latency          [13]uint64
}
type Metrics struct {
	mu            sync.Mutex
	start         time.Time
	window        [300]bucket
	total, errors uint64
	latency       [13]uint64
	sum           float64
	active        atomic.Int64
}
type Snapshot struct {
	Instance          string    `json:"instance"`
	StartedAt         time.Time `json:"startedAt"`
	UptimeSeconds     float64   `json:"uptimeSeconds"`
	WindowSeconds     float64   `json:"windowSeconds"`
	Requests          uint64    `json:"requests"`
	Errors            uint64    `json:"errors"`
	RequestsPerSecond float64   `json:"requestsPerSecond"`
	ErrorRate         float64   `json:"errorRate"`
	P95UpperMS        *float64  `json:"p95UpperMs"`
	InFlight          int64     `json:"inFlight"`
	Goroutines        int       `json:"goroutines"`
	HeapBytes         uint64    `json:"heapBytes"`
	DatabaseHealthy   bool      `json:"databaseHealthy"`
	DBOpen            int       `json:"dbOpen"`
	DBInUse           int       `json:"dbInUse"`
	DBMax             int       `json:"dbMax"`
	DBWaitCount       int64     `json:"dbWaitCount"`
}

func New() *Metrics { return &Metrics{start: time.Now()} }
func (m *Metrics) Observe(now time.Time, status int, duration time.Duration) {
	ms := float64(duration) / float64(time.Millisecond)
	index := 0
	for ms > bounds[index] {
		index++
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.total++
	m.latency[index]++
	m.sum += ms / 1000
	slot := &m.window[now.Unix()%300]
	if slot.Second != now.Unix() {
		*slot = bucket{Second: now.Unix()}
	}
	slot.Requests++
	slot.Latency[index]++
	if status >= 500 {
		m.errors++
		slot.Errors++
	}
}
func (m *Metrics) Snapshot(now time.Time) Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Snapshot{StartedAt: m.start, UptimeSeconds: now.Sub(m.start).Seconds(), WindowSeconds: math.Min(300, math.Max(1, now.Sub(m.start).Seconds())), InFlight: m.active.Load(), Goroutines: runtime.NumGoroutine()}
	var histogram [13]uint64
	for _, slot := range m.window {
		if slot.Second > now.Unix()-300 && slot.Second <= now.Unix() {
			s.Requests += slot.Requests
			s.Errors += slot.Errors
			for i, n := range slot.Latency {
				histogram[i] += n
			}
		}
	}
	s.RequestsPerSecond = float64(s.Requests) / s.WindowSeconds
	if s.Requests > 0 {
		s.ErrorRate = float64(s.Errors) / float64(s.Requests)
		target := uint64(math.Ceil(float64(s.Requests) * .95))
		var count uint64
		for i, n := range histogram {
			count += n
			if count >= target {
				v := bounds[i]
				if !math.IsInf(v, 1) {
					s.P95UpperMS = &v
				}
				break
			}
		}
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	s.HeapBytes = mem.HeapAlloc
	return s
}
func (m *Metrics) Prometheus(w io.Writer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fmt.Fprintln(w, "# TYPE mini_fury_http_requests_total counter")
	fmt.Fprintf(w, "mini_fury_http_requests_total %d\n", m.total)
	fmt.Fprintln(w, "# TYPE mini_fury_http_errors_total counter")
	fmt.Fprintf(w, "mini_fury_http_errors_total %d\n", m.errors)
	fmt.Fprintln(w, "# TYPE mini_fury_http_request_duration_seconds histogram")
	var cumulative uint64
	for i, n := range m.latency {
		cumulative += n
		upper := fmt.Sprintf("%g", bounds[i]/1000)
		if math.IsInf(bounds[i], 1) {
			upper = "+Inf"
		}
		fmt.Fprintf(w, "mini_fury_http_request_duration_seconds_bucket{le=%q} %d\n", upper, cumulative)
	}
	fmt.Fprintf(w, "mini_fury_http_request_duration_seconds_sum %g\nmini_fury_http_request_duration_seconds_count %d\n", m.sum, m.total)
	fmt.Fprintf(w, "# TYPE mini_fury_http_in_flight gauge\nmini_fury_http_in_flight %d\n", m.active.Load())
	fmt.Fprintf(w, "# TYPE mini_fury_process_uptime_seconds gauge\nmini_fury_process_uptime_seconds %g\n", time.Since(m.start).Seconds())
}

type response struct {
	http.ResponseWriter
	status int
}

func (w *response) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
		w.ResponseWriter.WriteHeader(code)
	}
}
func (w *response) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
func (m *Metrics) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) >= 15 && r.URL.Path[:15] == "/api/operations" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		record := &response{ResponseWriter: w}
		m.active.Add(1)
		defer func() {
			m.active.Add(-1)
			status := record.status
			if status == 0 {
				status = 200
			}
			if p := recover(); p != nil {
				m.Observe(time.Now(), 500, time.Since(start))
				panic(p)
			}
			m.Observe(time.Now(), status, time.Since(start))
		}()
		next.ServeHTTP(record, r)
	})
}
