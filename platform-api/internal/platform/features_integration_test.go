package platform

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mini-operador-ti/platform-api/internal/identity"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCatalogAndAccountFeatures(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(10)
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("features-%d", time.Now().UnixNano())
	password := "Fixture-Password-2026"
	newPassword := "Changed-Fixture-Password-2026"
	hash, err := identity.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	users := []identity.User{}
	defer func() {
		for _, u := range users {
			db.Exec(`DELETE FROM audit_events WHERE actor_id=$1`, u.ID)
			db.Exec(`DELETE FROM applications WHERE created_by=$1`, u.ID)
			db.Exec(`DELETE FROM users WHERE id=$1`, u.ID)
			db.Exec(`DELETE FROM auth_attempts WHERE key IN($1,$2,$3)`, identity.Digest("email:"+u.Email), identity.Digest("password-ip:"+u.ID), identity.Digest("password-account:"+u.ID))
		}
		db.Exec(`DELETE FROM auth_attempts WHERE key=$1`, identity.Digest("ip:198.51.100.31"))
	}()
	for _, role := range []string{"admin", "developer", "developer", "admin"} {
		var u identity.User
		err = db.QueryRow(`INSERT INTO users(email,name,role,password_hash) VALUES($1,$2,$3,$4) RETURNING id::text,email,name,role,active`, fmt.Sprintf("%s-%d@test.local", prefix, len(users)), prefix, role, hash).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Active)
		if err != nil {
			t.Fatal(err)
		}
		users = append(users, u)
	}
	repo := &identity.Postgres{DB: db}
	svc, err := identity.NewService(repo)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auth := &identity.HTTP{Service: svc, Log: logger, Origins: identity.Origins("http://127.0.0.1:5173")}
	handler := (&API{Store: &PostgresStore{DB: db}, Log: logger, Auth: auth}).Handler()
	type loginSession struct {
		cookie *http.Cookie
		csrf   string
	}
	call := func(method, path string, value any, session loginSession) *httptest.ResponseRecorder {
		var payload []byte
		if value != nil {
			payload, _ = json.Marshal(value)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(payload))
		r.RemoteAddr = "198.51.100.31:8000"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://127.0.0.1:5173")
		r.Header.Set("X-CSRF-Token", session.csrf)
		if session.cookie != nil {
			r.AddCookie(session.cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	expect := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("status=%d expected=%d body=%s", w.Code, status, w.Body)
		}
	}
	login := func(index int, password string) loginSession {
		w := call("POST", "/api/auth/login", map[string]string{"email": users[index].Email, "password": password}, loginSession{})
		expect(w, 200)
		var body struct {
			CSRF string `json:"csrfToken"`
		}
		json.Unmarshal(w.Body.Bytes(), &body)
		return loginSession{w.Result().Cookies()[0], body.CSRF}
	}
	admin, developer, other := login(0, password), login(1, password), login(2, password)
	input := map[string]any{"name": prefix, "repository": "https://gitlab.com/test/api", "port": 8080, "environment": "staging", "owner": "Features Team", "runtime": "Go", "description": "Facturacion conciliacion financiera", "tags": []string{"billing", "critical", "billing"}}
	w := call("POST", "/api/applications", input, developer)
	expect(w, 201)
	var app Application
	json.Unmarshal(w.Body.Bytes(), &app)
	if len(app.Tags) != 2 || app.Version != 1 {
		t.Fatal("metadata normalization failed")
	}
	path := "/api/applications/" + app.ID
	for _, query := range []string{"?q=conciliacion&tag=billing&runtime=Go&owner=Features%20Team&environment=staging", "?q=" + prefix} {
		w = call("GET", "/api/applications"+query, nil, developer)
		expect(w, 200)
		if !strings.Contains(w.Body.String(), app.ID) {
			t.Fatalf("search did not find app: %s", w.Body)
		}
	}
	w = call("GET", "/api/applications?tag=missing", nil, developer)
	if strings.Contains(w.Body.String(), app.ID) {
		t.Fatal("tag filter ignored")
	}
	w = call("GET", "/api/applications?q=conciliacion", nil, other)
	if strings.Contains(w.Body.String(), app.ID) {
		t.Fatal("search bypasses permissions")
	}
	input["version"] = app.Version
	input["description"] = "Actualizada con datos de facturacion"
	expect(call("PUT", path, input, other), 404)
	w = call("PUT", path, input, admin)
	expect(w, 200)
	json.Unmarshal(w.Body.Bytes(), &app)
	if app.CreatedBy == nil || *app.CreatedBy != users[1].ID || app.Version != 2 {
		t.Fatal("edit reassigned ownership or ignored version")
	}
	expect(call("PUT", path, input, developer), 409)
	input["version"] = app.Version
	var successes, conflicts atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := call("PUT", path, input, developer)
			switch result.Code {
			case 200:
				successes.Add(1)
			case 409:
				conflicts.Add(1)
			default:
				t.Errorf("concurrent edit: %d %s", result.Code, result.Body)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 || conflicts.Load() != 7 {
		t.Fatalf("lost update: success=%d conflict=%d", successes.Load(), conflicts.Load())
	}
	w = call("GET", path, nil, developer)
	expect(w, 200)
	json.Unmarshal(w.Body.Bytes(), &app)
	w = call("POST", path+"/archive", map[string]int64{"version": app.Version}, developer)
	expect(w, 200)
	json.Unmarshal(w.Body.Bytes(), &app)
	if app.ArchivedAt == nil {
		t.Fatal("not archived")
	}
	w = call("GET", "/api/applications?environment=staging", nil, developer)
	if strings.Contains(w.Body.String(), app.ID) {
		t.Fatal("archived app in active catalog")
	}
	w = call("GET", "/api/applications?state=archived&tag=billing", nil, developer)
	if !strings.Contains(w.Body.String(), app.ID) {
		t.Fatal("archive filter ignored")
	}
	input["version"] = app.Version
	expect(call("PUT", path, input, developer), 409)
	createDuplicate := map[string]any{}
	for k, v := range input {
		if k != "version" {
			createDuplicate[k] = v
		}
	}
	expect(call("POST", "/api/applications", createDuplicate, developer), 409)
	expect(call("POST", path+"/restore", map[string]int64{"version": app.Version}, other), 404)
	w = call("POST", path+"/restore", map[string]int64{"version": app.Version}, developer)
	expect(w, 200)
	json.Unmarshal(w.Body.Bytes(), &app)
	if app.ArchivedAt != nil {
		t.Fatal("restore failed")
	}
	w = call("GET", "/api/admin/audit?actor="+users[1].Email+"&action=application.archive&resource="+app.ID+"&from=2020-01-01T00:00:00Z&until=2099-01-01T00:00:00Z", nil, admin)
	expect(w, 200)
	var history struct {
		Events []identity.Event `json:"events"`
	}
	json.Unmarshal(w.Body.Bytes(), &history)
	if len(history.Events) != 1 || history.Events[0].ResourceID != app.ID {
		t.Fatalf("audit filters ignored: %s", w.Body)
	}
	expect(call("GET", "/api/admin/audit?from=invalid", nil, admin), 400)
	expect(call("GET", "/api/admin/audit?from=2026-12-01T00:00:00Z&until=2026-01-01T00:00:00Z", nil, admin), 400)
	var edits int
	if err = db.QueryRow(`SELECT count(*) FROM audit_events WHERE resource_id=$1 AND action='application.update'`, app.ID).Scan(&edits); err != nil || edits != 2 {
		t.Fatalf("failed edit wrote audit or successful edit missing: %d %v", edits, err)
	}
	// Closing all sessions and credential changes cannot be undone by a stale in-flight login.
	second := login(1, password)
	credential, err := repo.Credentials(ctx, users[1].Email)
	if err != nil {
		t.Fatal(err)
	}
	expect(call("POST", "/api/auth/logout-all", map[string]string{}, developer), 200)
	expect(call("GET", "/api/auth/me", nil, developer), 401)
	expect(call("GET", "/api/auth/me", nil, second), 401)
	if err = repo.StartSession(ctx, users[1], identity.Digest(prefix+"-stale"), "", time.Now().Add(time.Hour), credential.Version); err != identity.ErrCredentials {
		t.Fatalf("revocation raced with login: %v", err)
	}
	developer = login(1, password)
	second = login(1, password)
	expect(call("POST", "/api/auth/password", identity.PasswordChange{CurrentPassword: "wrong", NewPassword: newPassword}, developer), 422)
	expect(call("GET", "/api/auth/me", nil, developer), 200)
	expect(call("POST", "/api/auth/password", identity.PasswordChange{CurrentPassword: password, NewPassword: newPassword}, developer), 200)
	expect(call("GET", "/api/auth/me", nil, second), 401)
	expect(call("POST", "/api/auth/login", map[string]string{"email": users[1].Email, "password": password}, loginSession{}), 401)
	developer = login(1, newPassword)
	expect(call("PATCH", "/api/admin/users/"+users[2].ID+"/status", map[string]bool{"active": false}, developer), 403)
	expect(call("PATCH", "/api/admin/users/"+users[0].ID+"/status", map[string]bool{"active": false}, admin), 409)
	expect(call("PATCH", "/api/admin/users/"+users[1].ID+"/status", map[string]bool{"active": false}, admin), 200)
	expect(call("GET", "/api/auth/me", nil, developer), 401)
	expect(call("POST", "/api/auth/login", map[string]string{"email": users[1].Email, "password": newPassword}, loginSession{}), 401)
	expect(call("PATCH", "/api/admin/users/"+users[1].ID+"/status", map[string]bool{"active": true}, admin), 200)
	developer = login(1, newPassword)
	expect(call("GET", path, nil, developer), 200)
	// Concurrent mutual deactivation must leave one of these administrators active.
	admin2 := login(3, password)
	var deactivated atomic.Int32
	for _, test := range []struct {
		s  loginSession
		id string
	}{{admin, users[3].ID}, {admin2, users[0].ID}} {
		wg.Add(1)
		go func(s loginSession, id string) {
			defer wg.Done()
			w := call("PATCH", "/api/admin/users/"+id+"/status", map[string]bool{"active": false}, s)
			if w.Code == 200 {
				deactivated.Add(1)
			} else if w.Code != 403 && w.Code != 401 {
				t.Errorf("unexpected admin race response=%d", w.Code)
			}
		}(test.s, test.id)
	}
	wg.Wait()
	if deactivated.Load() != 1 {
		t.Fatalf("administrator race was not serialized: %d", deactivated.Load())
	}
}
