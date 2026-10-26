package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestHealthAndCounters(t *testing.T) {
	h := handler()
	for _, path := range []string{"/health", "/", "/error"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if path == "/health" && w.Code != 200 {
			t.Fatal("health unavailable")
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/mini-fury/metrics", nil))
	var c counters
	if json.Unmarshal(w.Body.Bytes(), &c) != nil || c.Total != 2 || c.Errors != 1 {
		t.Fatal("request counters must exclude probes and scrape")
	}
}
