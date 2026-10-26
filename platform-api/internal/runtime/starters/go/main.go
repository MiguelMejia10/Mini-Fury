package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

type counters struct {
	sync.Mutex
	Epoch  string `json:"epoch"`
	Total  int64  `json:"total"`
	Errors int64  `json:"errors"`
	Le100  int64  `json:"le100"`
	Le300  int64  `json:"le300"`
	Le1000 int64  `json:"le1000"`
}

func handler() http.Handler {
	c := &counters{Epoch: time.Now().UTC().Format(time.RFC3339Nano)}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			if os.Getenv("FAIL_READINESS") == "true" {
				http.Error(w, "fixture not ready", 503)
				return
			}
			w.Write([]byte("ok"))
			return
		}
		if r.URL.Path == "/mini-fury/metrics" {
			c.Lock()
			defer c.Unlock()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(c)
			return
		}
		start := time.Now()
		status := 200
		if r.URL.Path == "/error" {
			status = 503
		}
		if r.URL.Path != "/" && r.URL.Path != "/error" {
			status = 404
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]string{"service": "__APP_NAME__", "message": os.Getenv("MESSAGE"), "version": os.Getenv("LAB_VERSION")})
		elapsed := time.Since(start)
		c.Lock()
		c.Total++
		if status >= 500 {
			c.Errors++
		}
		if elapsed <= 100*time.Millisecond {
			c.Le100++
		}
		if elapsed <= 300*time.Millisecond {
			c.Le300++
		}
		if elapsed <= time.Second {
			c.Le1000++
		}
		c.Unlock()
	})
}
func main() {
	log.Print("mini_fury_lab_started")
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for range t.C {
			log.Print("mini_fury_lab_heartbeat")
		}
	}()
	server := &http.Server{Addr: ":8080", Handler: handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	log.Fatal(server.ListenAndServe())
}
