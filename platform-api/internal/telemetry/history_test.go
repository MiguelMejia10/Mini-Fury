package telemetry

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHistoryBoundsAndSafeLogs(t *testing.T) {
	for _, query := range []string{"from=garbage", "from=" + time.Now().Add(-48*time.Hour).Format(time.RFC3339), "until=" + time.Now().Add(time.Hour).Format(time.RFC3339)} {
		if _, _, e := historyRange(httptest.NewRequest("GET", "/?"+query, nil)); e == nil {
			t.Fatal("invalid range accepted")
		}
	}
	dir := t.TempDir()
	l, e := NewLogFile(dir, "fixture")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	if e = l.Record("POST", "/api/applications/private-name?token=secret", 200, time.Millisecond); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "api.log"))
	if strings.Contains(string(data), "private-name") || strings.Contains(string(data), "secret") {
		t.Fatal("sensitive path logged")
	}
	h := &HTTP{Metrics: New(), MetricsToken: "fixture-token"}
	w := httptest.NewRecorder()
	h.internalMetrics(w, httptest.NewRequest("GET", "/internal/metrics", nil))
	if w.Code != 401 {
		t.Fatal("anonymous scrape accepted")
	}
	r := httptest.NewRequest("GET", "/internal/metrics", nil)
	r.Header.Set("Authorization", "Bearer fixture-token")
	w = httptest.NewRecorder()
	h.internalMetrics(w, r)
	if w.Code != http.StatusOK {
		t.Fatal("service scrape rejected")
	}
}
