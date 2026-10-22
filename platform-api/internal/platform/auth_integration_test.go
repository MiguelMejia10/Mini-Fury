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

func TestAuthenticatedLifecycleAndIsolation(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
	tag := fmt.Sprintf("auth-%d", time.Now().UnixNano())
	ids := []string{}
	defer func() {
		for _, id := range ids {
			db.Exec(`DELETE FROM audit_events WHERE actor_id=$1`, id)
			db.Exec(`DELETE FROM applications WHERE created_by=$1`, id)
			db.Exec(`DELETE FROM users WHERE id=$1`, id)
		}
	}()
	// Fixtures have real Argon2 hashes; the tests do not alter existing users.
	password := "Integration-Password-42"
	hash, err := identity.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	users := []identity.User{}
	for _, role := range []string{"admin", "developer", "developer"} {
		var u identity.User
		email := fmt.Sprintf("%s-%d@test.local", tag, len(users))
		if err = db.QueryRow(`INSERT INTO users(email,name,role,password_hash) VALUES($1,$2,$3,$4) RETURNING id::text,email,name,role`, email, tag, role, hash).Scan(&u.ID, &u.Email, &u.Name, &u.Role); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, u.ID)
		users = append(users, u)
	}
	repo := &identity.Postgres{DB: db}
	service, err := identity.NewService(repo)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auth := &identity.HTTP{Service: service, Log: logger, Origins: identity.Origins("http://127.0.0.1:5173"), Secure: false}
	handler := (&API{Store: &PostgresStore{DB: db}, Log: logger, Auth: auth}).Handler()
	call := func(h http.Handler, method, path string, body any, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		var payload []byte
		if body != nil {
			payload, _ = json.Marshal(body)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(payload))
		r.RemoteAddr = "198.51.100.18:1234"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://127.0.0.1:5173")
		r.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	unauth := call(handler, "GET", "/api/applications", nil, nil, "")
	if unauth.Code != 401 {
		t.Fatal("public catalog")
	}
	cookies := []*http.Cookie{}
	csrfTokens := []string{}
	for _, u := range users {
		w := call(handler, "POST", "/api/auth/login", map[string]string{"email": u.Email, "password": password}, nil, "")
		if w.Code != 200 {
			t.Fatalf("login status=%d %s", w.Code, w.Body)
		}
		var response struct {
			CSRF string `json:"csrfToken"`
		}
		json.Unmarshal(w.Body.Bytes(), &response)
		cookies = append(cookies, w.Result().Cookies()[0])
		csrfTokens = append(csrfTokens, response.CSRF)
	}
	var csrfError = call(handler, "POST", "/api/applications", map[string]string{}, cookies[1], "bad")
	if csrfError.Code != 403 {
		t.Fatal("CSRF bypass")
	}
	input := CreateApplication{Name: tag, Repository: "https://gitlab.com/test/service", Port: 8080, Environment: "staging", Owner: "Platform", Runtime: "Go"}
	w := call(handler, "POST", "/api/applications", input, cookies[1], csrfTokens[1])
	if w.Code != 201 {
		t.Fatalf("create: %s", w.Body)
	}
	var app Application
	json.Unmarshal(w.Body.Bytes(), &app)
	if app.CreatedBy == nil || *app.CreatedBy != users[1].ID {
		t.Fatal("client spoofed ownership")
	}
	for i, want := range []int{200, 200, 404} {
		w = call(handler, "GET", "/api/applications/"+app.ID, nil, cookies[i], "")
		if w.Code != want {
			t.Fatalf("object permission role=%d got=%d", i, w.Code)
		}
	}
	w = call(handler, "GET", "/api/applications?environment=staging", nil, cookies[2], "")
	if strings.Contains(w.Body.String(), tag) {
		t.Fatal("another developer can enumerate private app")
	}
	w = call(handler, "GET", "/api/admin/audit", nil, cookies[1], "")
	if w.Code != 403 {
		t.Fatal("developer read audit")
	}
	w = call(handler, "GET", "/api/admin/audit", nil, cookies[0], "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "application.create") {
		t.Fatal("missing audit event")
	}
	newAccount := identity.NewUser{Email: tag + "-new@test.local", Name: "New developer", Role: "developer", Password: password}
	w = call(handler, "POST", "/api/admin/users", newAccount, cookies[1], csrfTokens[1])
	if w.Code != 403 {
		t.Fatal("developer granted new access")
	}
	w = call(handler, "POST", "/api/admin/users", newAccount, cookies[0], csrfTokens[0])
	if w.Code != 201 {
		t.Fatalf("admin create user: %d %s", w.Code, w.Body)
	}
	var createdUser identity.User
	if err = json.Unmarshal(w.Body.Bytes(), &createdUser); err != nil {
		t.Fatal(err)
	}
	ids = append(ids, createdUser.ID)
	if strings.Contains(w.Body.String(), password) || strings.Contains(w.Body.String(), "passwordHash") {
		t.Fatal("user response leaked credentials")
	}
	w = call(handler, "POST", "/api/admin/users", newAccount, cookies[0], csrfTokens[0])
	if w.Code != 409 {
		t.Fatal("duplicate user accepted")
	}
	_, err = repo.CreateUser(ctx, nil, identity.NewUser{Email: tag + "-bootstrap@test.local", Name: "Another admin", Role: "admin"}, hash)
	if err != identity.ErrForbidden {
		t.Fatal("bootstrap escalated access after initialization")
	}
	// Fresh handler simulates another replica/restart; cookie remains valid through shared DB.
	otherService, err := identity.NewService(repo)
	if err != nil {
		t.Fatal(err)
	}
	otherAuth := &identity.HTTP{Service: otherService, Log: logger, Origins: auth.Origins}
	otherHandler := (&API{Store: &PostgresStore{DB: db}, Log: logger, Auth: otherAuth}).Handler()
	w = call(otherHandler, "GET", "/api/auth/me", nil, cookies[1], "")
	if w.Code != 200 {
		t.Fatal("session tied to one API replica")
	}
	// Concurrent, authenticated reads exercise bounded database connections.
	var wg sync.WaitGroup
	var failures atomic.Int32
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if call(otherHandler, "GET", "/api/applications?limit=10", nil, cookies[1], "").Code != 200 {
				failures.Add(1)
			}
		}()
	}
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatalf("concurrent read failures=%d", failures.Load())
	}
	w = call(handler, "POST", "/api/auth/logout", map[string]string{}, cookies[1], csrfTokens[1])
	if w.Code != 200 {
		t.Fatal("logout failed")
	}
	w = call(otherHandler, "GET", "/api/auth/me", nil, cookies[1], "")
	if w.Code != 401 {
		t.Fatal("revoked session survived on second replica")
	}
	var stored string
	if err = db.QueryRow(`SELECT token_hash FROM sessions WHERE user_id=$1 LIMIT 1`, users[0].ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == cookies[0].Value || len(stored) != 64 {
		t.Fatal("session token stored in plaintext")
	}
	// Rate limit is atomic and shared, independent of password hashing and replica.
	ipKey, emailKey := identity.Digest(tag+"-ip"), identity.Digest(tag+"-email")
	defer db.Exec(`DELETE FROM auth_attempts WHERE key IN($1,$2)`, ipKey, emailKey)
	var allowed atomic.Int32
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, e := repo.AllowLogin(ctx, ipKey, emailKey)
			if e != nil {
				t.Error(e)
			}
			if ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 10 {
		t.Fatalf("rate limit allowed=%d", allowed.Load())
	}
	for _, u := range users {
		db.Exec(`DELETE FROM auth_attempts WHERE key=$1`, identity.Digest("email:"+u.Email))
	}
	db.Exec(`DELETE FROM auth_attempts WHERE key=$1`, identity.Digest("ip:198.51.100.18"))
}
