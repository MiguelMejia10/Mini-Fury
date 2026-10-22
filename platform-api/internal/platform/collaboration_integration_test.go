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
	"mini-operador-ti/platform-api/internal/teams"
	"mini-operador-ti/platform-api/internal/telemetry"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTeamsQualityAndOperations(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	ctx := context.Background()
	db, e := sql.Open("pgx", dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(10)
	if e = Migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	tag := fmt.Sprintf("collab-%d", time.Now().UnixNano())
	hash, e := identity.HashPassword("Collaboration-Fixture-2026!")
	if e != nil {
		t.Fatal(e)
	}
	users := []identity.User{}
	teamIDs := []string{}
	defer func() {
		for _, id := range teamIDs {
			db.Exec(`DELETE FROM team_invitations WHERE team_id=$1`, id)
			db.Exec(`DELETE FROM applications WHERE team_id=$1`, id)
			db.Exec(`DELETE FROM teams WHERE id=$1`, id)
		}
		for _, u := range users {
			db.Exec(`DELETE FROM audit_events WHERE actor_id=$1`, u.ID)
			db.Exec(`DELETE FROM applications WHERE created_by=$1`, u.ID)
			db.Exec(`DELETE FROM users WHERE id=$1`, u.ID)
			db.Exec(`DELETE FROM auth_attempts WHERE key=$1`, identity.Digest("email:"+u.Email))
		}
		db.Exec(`DELETE FROM auth_attempts WHERE key=$1`, identity.Digest("ip:198.51.100.42"))
		db.Exec(`DELETE FROM platform_alerts WHERE instance=$1`, tag)
	}()
	for _, role := range []string{"admin", "developer", "developer", "developer", "developer"} {
		var u identity.User
		if e = db.QueryRow(`INSERT INTO users(email,name,role,password_hash) VALUES($1,$2,$3,$4) RETURNING id::text,email,name,role,active`, fmt.Sprintf("%s-%d@test.local", tag, len(users)), tag, role, hash).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Active); e != nil {
			t.Fatal(e)
		}
		users = append(users, u)
	}
	repo := &identity.Postgres{DB: db}
	svc, e := identity.NewService(repo)
	if e != nil {
		t.Fatal(e)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	auth := &identity.HTTP{Service: svc, Log: log, Origins: identity.Origins("http://127.0.0.1:5173")}
	metrics := telemetry.New()
	ops := &telemetry.HTTP{DB: db, Metrics: metrics, Instance: tag, Auth: auth}
	handler := metrics.Wrap((&API{Store: &PostgresStore{DB: db}, Log: log, Auth: auth, Teams: &teams.HTTP{Repo: &teams.Postgres{DB: db}, Auth: auth}, Operations: ops}).Handler())
	type session struct {
		cookie *http.Cookie
		csrf   string
	}
	sessions := []session{}
	call := func(method, path string, value any, s session) *httptest.ResponseRecorder {
		var data []byte
		if value != nil {
			data, _ = json.Marshal(value)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		r.RemoteAddr = "198.51.100.42:1234"
		r.Header.Set("Origin", "http://127.0.0.1:5173")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", s.csrf)
		if s.cookie != nil {
			r.AddCookie(s.cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	expect := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("status=%d want=%d body=%s", w.Code, status, w.Body)
		}
	}
	for _, u := range users {
		w := call("POST", "/api/auth/login", map[string]string{"email": u.Email, "password": "Collaboration-Fixture-2026!"}, session{})
		expect(w, 200)
		var body struct {
			CSRF string `json:"csrfToken"`
		}
		json.Unmarshal(w.Body.Bytes(), &body)
		sessions = append(sessions, session{w.Result().Cookies()[0], body.CSRF})
	}
	// Ordinary developer creates and administers a team; this does not grant platform administration.
	w := call("POST", "/api/teams", teams.NewTeam{Name: tag, Description: "Collaboration fixture"}, sessions[1])
	expect(w, 201)
	var team teams.Team
	json.Unmarshal(w.Body.Bytes(), &team)
	teamIDs = append(teamIDs, team.ID)
	invite := func(index int, role string) teams.Invitation {
		w := call("POST", "/api/teams/"+team.ID+"/invitations", teams.Invite{Email: users[index].Email, Role: role}, sessions[1])
		expect(w, 201)
		var i teams.Invitation
		json.Unmarshal(w.Body.Bytes(), &i)
		return i
	}
	editor, reader := invite(2, "editor"), invite(3, "reader")
	expect(call("POST", "/api/team-invitations/"+editor.ID+"/accept", map[string]string{}, sessions[4]), 404)
	expect(call("POST", "/api/team-invitations/"+editor.ID+"/accept", map[string]string{}, sessions[2]), 200)
	expect(call("POST", "/api/team-invitations/"+editor.ID+"/accept", map[string]string{}, sessions[2]), 404)
	expect(call("POST", "/api/team-invitations/"+reader.ID+"/accept", map[string]string{}, sessions[3]), 200)
	expect(call("PUT", "/api/teams/"+team.ID+"/members/"+users[1].ID, map[string]string{"role": "reader"}, sessions[1]), 409)
	expect(call("POST", "/api/teams/"+team.ID+"/invitations", teams.Invite{Email: users[4].Email, Role: "admin"}, sessions[2]), 403)
	expired := invite(4, "reader")
	db.Exec(`UPDATE team_invitations SET expires_at=now()-interval '1 second' WHERE id=$1`, expired.ID)
	expect(call("POST", "/api/team-invitations/"+expired.ID+"/accept", map[string]string{}, sessions[4]), 404)
	revoked := invite(4, "reader")
	expect(call("DELETE", "/api/teams/"+team.ID+"/invitations/"+revoked.ID, nil, sessions[1]), 200)
	expect(call("POST", "/api/team-invitations/"+revoked.ID+"/accept", map[string]string{}, sessions[4]), 404)
	makeApp := func(name string, s session, teamID *string) Application {
		in := CreateApplication{Name: name, Repository: "https://gitlab.com/test/service", Port: 8080, Environment: "staging", Owner: "Team", Runtime: "Go", Description: "Quality fixture", Tags: []string{"quality"}, TeamID: teamID, Documentation: "# Runbook\nRestart using the future deployment flow.", HealthCheckPath: "/health", Links: []Link{{Label: "Guide", URL: "https://example.com/guide"}}}
		w := call("POST", "/api/applications", in, s)
		expect(w, 201)
		var app Application
		json.Unmarshal(w.Body.Bytes(), &app)
		return app
	}
	app := makeApp(tag+"-api", sessions[1], &team.ID)
	target := makeApp(tag+"-data", sessions[1], &team.ID)
	path := "/api/applications/" + app.ID
	if app.Readiness.Score != 100 {
		t.Fatalf("bad readiness: %+v", app.Readiness)
	}
	for index, want := range map[int]int{0: 200, 1: 200, 2: 200, 3: 200, 4: 404} {
		expect(call("GET", path, nil, sessions[index]), want)
	}
	var visible struct {
		Applications []Application `json:"applications"`
	}
	w = call("GET", "/api/applications?environment=staging", nil, sessions[3])
	expect(w, 200)
	json.Unmarshal(w.Body.Bytes(), &visible)
	if len(visible.Applications) < 2 {
		t.Fatal("reader cannot list shared apps")
	}
	update := func(a Application, s session) *httptest.ResponseRecorder {
		return call("PUT", "/api/applications/"+a.ID, map[string]any{"name": a.Name, "repository": a.Repository, "port": a.Port, "environment": a.Environment, "owner": a.Owner, "runtime": a.Runtime, "description": a.Description, "tags": a.Tags, "teamId": a.TeamID, "documentation": a.Documentation, "links": a.Links, "healthCheckPath": a.HealthCheckPath, "dependencyIds": a.DependencyIDs, "version": a.Version}, s)
	}
	expect(update(app, sessions[3]), 403)
	app.DependencyIDs = []string{target.ID}
	w = update(app, sessions[2])
	expect(w, 200)
	json.Unmarshal(w.Body.Bytes(), &app)
	if len(app.Dependencies) != 1 {
		t.Fatal("dependency not persisted")
	}
	target.DependencyIDs = []string{app.ID}
	expect(update(target, sessions[2]), 422)
	app.DependencyIDs = []string{app.ID}
	expect(update(app, sessions[2]), 422)
	app.DependencyIDs = nil
	originalTeam := app.TeamID
	app.TeamID = nil
	expect(update(app, sessions[2]), 403)
	app.TeamID = originalTeam
	app.Links = []Link{{Label: "unsafe", URL: "javascript:alert(1)"}}
	expect(update(app, sessions[2]), 422)
	app.Links = []Link{{Label: "Guide", URL: "https://example.com"}}
	app.HealthCheckPath = "https://internal/private"
	expect(update(app, sessions[2]), 422)
	app.HealthCheckPath = "/health"
	// Moving a personal application into the team removes implicit creator privileges.
	personal := makeApp(tag+"-personal", sessions[2], nil)
	personal.TeamID = &team.ID
	w = update(personal, sessions[1])
	expect(w, 404) // manager is not the personal owner
	expect(call("DELETE", "/api/teams/"+team.ID+"/members/"+users[1].ID, nil, sessions[1]), 409)
	expect(call("PATCH", "/api/admin/users/"+users[1].ID+"/status", map[string]bool{"active": false}, sessions[0]), 409)
	legacy := makeApp(tag+"-legacy", sessions[2], nil)
	expect(call("PUT", "/api/teams/"+team.ID+"/members/"+users[2].ID, map[string]string{"role": "admin"}, sessions[1]), 200)
	w = update(personal, sessions[2])
	expect(w, 200)
	expect(call("DELETE", "/api/teams/"+team.ID+"/members/"+users[2].ID, nil, sessions[1]), 200)
	expect(call("GET", path, nil, sessions[2]), 404)
	expect(call("GET", "/api/applications/"+personal.ID, nil, sessions[2]), 404)
	expect(call("GET", "/api/applications/"+legacy.ID, nil, sessions[2]), 200)
	// Concurrent edges must never create a cycle, even when requests edit different rows.
	left := makeApp(tag+"-left", sessions[1], &team.ID)
	right := makeApp(tag+"-right", sessions[1], &team.ID)
	left.DependencyIDs = []string{right.ID}
	right.DependencyIDs = []string{left.ID}
	codes := make(chan int, 2)
	start := make(chan struct{})
	for _, a := range []Application{left, right} {
		go func(a Application) { <-start; codes <- update(a, sessions[1]).Code }(a)
	}
	close(start)
	a, b := <-codes, <-codes
	if !((a == 200 && b == 422) || (a == 422 && b == 200)) {
		t.Fatalf("concurrent dependency cycle: %d %d", a, b)
	}
	// Simultaneous self-demotions must preserve one active administrator.
	w = call("POST", "/api/teams", teams.NewTeam{Name: tag + "-race"}, sessions[1])
	expect(w, 201)
	var raceTeam teams.Team
	json.Unmarshal(w.Body.Bytes(), &raceTeam)
	teamIDs = append(teamIDs, raceTeam.ID)
	w = call("POST", "/api/teams/"+raceTeam.ID+"/invitations", teams.Invite{Email: users[2].Email, Role: "admin"}, sessions[1])
	expect(w, 201)
	var raceInvite teams.Invitation
	json.Unmarshal(w.Body.Bytes(), &raceInvite)
	expect(call("POST", "/api/team-invitations/"+raceInvite.ID+"/accept", map[string]string{}, sessions[2]), 200)
	start = make(chan struct{})
	for _, index := range []int{1, 2} {
		go func(index int) {
			<-start
			codes <- call("PUT", "/api/teams/"+raceTeam.ID+"/members/"+users[index].ID, map[string]string{"role": "reader"}, sessions[index]).Code
		}(index)
	}
	close(start)
	a, b = <-codes, <-codes
	if !((a == 200 && b == 409) || (a == 409 && b == 200)) {
		t.Fatalf("concurrent last administrators: %d %d", a, b)
	}
	expect(call("GET", "/api/operations/status", nil, sessions[3]), 403)
	w = call("GET", "/api/operations/status", nil, sessions[0])
	expect(w, 200)
	if !strings.Contains(w.Body.String(), "databaseHealthy") {
		t.Fatal("missing live status")
	}
	for range 30 {
		metrics.Observe(time.Now(), 503, 2*time.Second)
	}
	if e = ops.Evaluate(ctx); e != nil {
		t.Fatal(e)
	}
	w = call("GET", "/api/operations/alerts", nil, sessions[0])
	expect(w, 200)
	var alerts struct {
		Alerts []telemetry.Alert `json:"alerts"`
	}
	json.Unmarshal(w.Body.Bytes(), &alerts)
	if len(alerts.Alerts) != 2 {
		t.Fatalf("rules did not fire: %s", w.Body)
	}
	id := alerts.Alerts[0].ID
	expect(call("POST", "/api/operations/alerts/"+id+"/ack", map[string]string{}, sessions[3]), 403)
	expect(call("POST", "/api/operations/alerts/"+id+"/ack", map[string]string{}, sessions[0]), 200)
	expect(call("POST", "/api/operations/alerts/"+id+"/ack", map[string]string{}, sessions[0]), 409)
	// Recovery retains history; a new incident clears acknowledgment and increments occurrences.
	ops.Metrics = telemetry.New()
	if e = ops.Evaluate(ctx); e != nil {
		t.Fatal(e)
	}
	var active bool
	if e = db.QueryRow(`SELECT active FROM platform_alerts WHERE id=$1`, id).Scan(&active); e != nil || active {
		t.Fatalf("alert did not resolve: %v", e)
	}
	for range 30 {
		ops.Metrics.Observe(time.Now(), 503, 2*time.Second)
	}
	if e = ops.Evaluate(ctx); e != nil {
		t.Fatal(e)
	}
	var acknowledged sql.NullTime
	var occurrences int
	if e = db.QueryRow(`SELECT active,acknowledged_at,occurrences FROM platform_alerts WHERE id=$1`, id).Scan(&active, &acknowledged, &occurrences); e != nil || !active || acknowledged.Valid || occurrences != 2 {
		t.Fatalf("alert recurrence invalid: active=%v acknowledged=%v count=%d error=%v", active, acknowledged.Valid, occurrences, e)
	}
	db.Exec(`UPDATE platform_alerts SET updated_at=now()-interval '3 minutes' WHERE id=$1`, id)
	expect(call("POST", "/api/operations/alerts/"+id+"/ack", map[string]string{}, sessions[0]), 409)
	w = call("GET", "/api/operations/metrics", nil, sessions[0])
	expect(w, 200)
	if !strings.Contains(w.Body.String(), "mini_fury_http_requests_total") {
		t.Fatal("missing Prometheus export")
	}
}
