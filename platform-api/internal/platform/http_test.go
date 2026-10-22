package platform

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeStore struct {
	app   Application
	err   error
	calls int
}

func (s *fakeStore) Ping(context.Context) error { return s.err }
func (s *fakeStore) List(context.Context, string, int, int) ([]Application, error) {
	return []Application{s.app}, s.err
}
func (s *fakeStore) Get(context.Context, string) (Application, error) { return s.app, s.err }
func (s *fakeStore) Create(_ context.Context, in CreateApplication) (Application, error) {
	s.calls++
	s.app.CreateApplication = in
	return s.app, s.err
}

const validBody = `{"name":"payments-api","repository":"https://gitlab.com/team/payments","port":8080,"environment":"staging","owner":"Platform Team","runtime":"Go"}`

func request(store *fakeStore, method, path, body string) *httptest.ResponseRecorder {
	api := &API{Store: store, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", api.health)
	mux.HandleFunc("POST /api/applications", api.create)
	mux.HandleFunc("GET /api/applications", api.list)
	mux.HandleFunc("GET /api/applications/{id}", api.get)
	mux.ServeHTTP(rec, req)
	return rec
}
func TestCreateValidationAndConflicts(t *testing.T) {
	cases := []struct {
		name, body    string
		storeErr      error
		status, calls int
	}{
		{"valid", validBody, nil, 201, 1},
		{"duplicate", validBody, ErrConflict, 409, 1},
		{"database unavailable", validBody, errors.New("private dsn must not leak"), 503, 1},
		{"invalid name", strings.Replace(validBody, "payments-api", "Bad Name", 1), nil, 422, 0},
		{"zero port", strings.Replace(validBody, "8080", "0", 1), nil, 422, 0},
		{"fractional port", strings.Replace(validBody, "8080", "1.5", 1), nil, 400, 0},
		{"unknown environment", strings.Replace(validBody, "staging", "qa", 1), nil, 422, 0},
		{"repository credentials", strings.Replace(validBody, "gitlab.com", "token@gitlab.com", 1), nil, 422, 0},
		{"query credentials", strings.Replace(validBody, "team/payments", "team/payments?token=secret", 1), nil, 422, 0},
		{"missing owner", strings.Replace(validBody, "Platform Team", "", 1), nil, 422, 0},
		{"multiple JSON", validBody + ` {}`, nil, 400, 0},
		{"unknown field", strings.TrimSuffix(validBody, "}") + `,"admin":true}`, nil, 400, 0},
		{"malformed", "{", nil, 400, 0},
		{"oversized", strings.Replace(validBody, "Platform Team", strings.Repeat("x", 140000), 1), nil, 400, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &fakeStore{err: tc.storeErr}
			res := request(s, "POST", "/api/applications", tc.body)
			if res.Code != tc.status {
				t.Fatalf("status=%d body=%s", res.Code, res.Body)
			}
			if s.calls != tc.calls {
				t.Fatalf("writes=%d", s.calls)
			}
			if strings.Contains(res.Body.String(), "private dsn") {
				t.Fatal("internal error leaked")
			}
		})
	}
}
func TestGetAndHealth(t *testing.T) {
	for _, tc := range []struct {
		path   string
		err    error
		status int
	}{
		{"/api/applications/not-a-uuid", nil, 400},
		{"/api/applications/11111111-1111-1111-1111-111111111111", ErrNotFound, 404},
		{"/api/applications/11111111-1111-1111-1111-111111111111", nil, 200},
		{"/api/applications?environment=qa", nil, 400},
		{"/api/health", nil, 200}, {"/api/health", errors.New("offline"), 503},
	} {
		res := request(&fakeStore{err: tc.err}, http.MethodGet, tc.path, "")
		if res.Code != tc.status {
			t.Errorf("%s status=%d want=%d", tc.path, res.Code, tc.status)
		}
	}
}
func TestCreationNormalizesAndDefaults(t *testing.T) {
	body := strings.Replace(validBody, `"payments-api"`, `" payments-api "`, 1)
	body = strings.Replace(body, `"runtime":"Go"`, `"runtime":""`, 1)
	res := request(&fakeStore{}, "POST", "/api/applications", body)
	var app Application
	if err := json.Unmarshal(res.Body.Bytes(), &app); err != nil {
		t.Fatal(err)
	}
	if res.Code != 201 || app.Name != "payments-api" || app.Runtime != "Contenedor HTTP" {
		t.Fatalf("invalid normalization: %s", res.Body)
	}
}

func TestIdentityConfigurationFailsClosed(t *testing.T) {
	api := &API{Store: &fakeStore{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/applications", nil))
	if rec.Code != 503 {
		t.Fatalf("unprotected API: %d", rec.Code)
	}
}

type blockingStore struct {
	fakeStore
	entered chan struct{}
	release chan struct{}
}

func (s *blockingStore) Ping(ctx context.Context) error {
	select {
	case s.entered <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestCapacityRejectsExcessWithoutQueue(t *testing.T) {
	store := &blockingStore{entered: make(chan struct{}, 64), release: make(chan struct{})}
	handler := (&API{Store: store, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}).Handler()
	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/health", nil))
		}()
	}
	defer func() { close(store.release); wg.Wait() }()
	for range 64 {
		select {
		case <-store.entered:
		case <-time.After(2 * time.Second):
			t.Fatal("requests never reached capacity")
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/health", nil))
	if rec.Code != 503 || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("excess queued instead of rejected: %d", rec.Code)
	}
}

func (s *fakeStore) Search(ctx context.Context, f Filter, limit, offset int) ([]Application, error) {
	return s.List(ctx, f.Environment, limit, offset)
}
func (s *fakeStore) Update(context.Context, string, CreateApplication, int64) (Application, error) {
	return s.app, s.err
}
func (s *fakeStore) Archive(context.Context, string, bool, int64) (Application, error) {
	return s.app, s.err
}
