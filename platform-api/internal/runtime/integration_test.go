package runtime_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"io"
	"log/slog"
	"mini-operador-ti/platform-api/internal/identity"
	"mini-operador-ti/platform-api/internal/platform"
	operating "mini-operador-ti/platform-api/internal/runtime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestScopesBuildsConfigurationAndJobs(t *testing.T) {
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
	if e = platform.Migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	tag := fmt.Sprintf("runtime-%d", time.Now().UnixNano())
	users := []identity.User{}
	tokens := []string{}
	var app, team string
	defer func() {
		if app != "" {
			db.Exec(`DELETE FROM application_cost_policies WHERE application_id=$1`, app)
			db.Exec(`DELETE FROM managed_services WHERE scope_id IN(SELECT id FROM runtime_scopes WHERE application_id=$1)`, app)
			db.Exec(`DELETE FROM scope_slos WHERE scope_id IN(SELECT id FROM runtime_scopes WHERE application_id=$1)`, app)
			db.Exec(`DELETE FROM operational_incidents WHERE application_id=$1`, app)
			db.Exec(`DELETE FROM runtime_versions WHERE application_id=$1`, app)
			db.Exec(`DELETE FROM runtime_job_events WHERE job_id IN(SELECT id FROM runtime_jobs WHERE application_id=$1)`, app)
			db.Exec(`DELETE FROM runtime_jobs WHERE application_id=$1`, app)
			db.Exec(`DELETE FROM runtime_configurations WHERE scope_id IN(SELECT id FROM runtime_scopes WHERE application_id=$1)`, app)
			db.Exec(`DELETE FROM runtime_scopes WHERE application_id=$1`, app)
			db.Exec(`DELETE FROM applications WHERE id=$1`, app)
		}
		if team != "" {
			db.Exec(`DELETE FROM teams WHERE id=$1`, team)
		}
		for _, u := range users {
			db.Exec(`DELETE FROM audit_events WHERE actor_id=$1`, u.ID)
			db.Exec(`DELETE FROM users WHERE id=$1`, u.ID)
		}
	}()
	for i, role := range []string{"admin", "developer", "developer", "developer", "developer"} {
		var u identity.User
		e = db.QueryRow(`INSERT INTO users(email,name,role,password_hash) VALUES($1,$2,$3,'fixture') RETURNING id::text,email,name,role,active`, fmt.Sprintf("%s-%d@test.local", tag, i), tag, role).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Active)
		if e != nil {
			t.Fatal(e)
		}
		users = append(users, u)
		token, _ := identity.Token()
		tokens = append(tokens, token)
		if _, e = db.Exec(`INSERT INTO sessions(token_hash,user_id,expires_at)VALUES($1,$2,now()+interval '1 hour')`, identity.Digest(token), u.ID); e != nil {
			t.Fatal(e)
		}
	}
	if e = db.QueryRow(`INSERT INTO teams(name,created_by)VALUES($1,$2)RETURNING id::text`, tag, users[1].ID).Scan(&team); e != nil {
		t.Fatal(e)
	}
	for i, role := range []string{"admin", "editor", "reader"} {
		if _, e = db.Exec(`INSERT INTO team_members(team_id,user_id,role)VALUES($1,$2,$3)`, team, users[i+1].ID, role); e != nil {
			t.Fatal(e)
		}
	}
	repository := "https://github.com/example/runtime-fixture"
	e = db.QueryRow(`INSERT INTO applications(name,repository,port,environment,owner,runtime,created_by,team_id)VALUES($1,$2,8080,'staging','fixture','Go',$3,$4)RETURNING id::text`, tag, repository, users[1].ID, team).Scan(&app)
	if e != nil {
		t.Fatal(e)
	}
	key := strings.Repeat("b", 43)
	path := filepath.Join(t.TempDir(), "agent")
	if e = os.WriteFile(path, []byte(key), 0600); e != nil {
		t.Fatal(e)
	}
	svc, e := identity.NewService(&identity.Postgres{DB: db})
	if e != nil {
		t.Fatal(e)
	}
	auth := &identity.HTTP{Service: svc, Origins: identity.Origins("http://127.0.0.1:5173"), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	h := &operating.HTTP{DB: db, Auth: auth, Config: operating.Config{Clusters: []operating.ClusterConfig{{ID: "lab", Name: "Lab", Server: "https://cluster.invalid", CAFile: "unused", TokenFile: "unused", Bindings: []operating.Binding{{Namespace: "fixture", ApplicationID: app, Secrets: map[string]operating.SecretReference{"database": {Name: "private-secret", Key: "password"}}}}}}, Repositories: []operating.RepositoryConfig{{ApplicationID: app, Provider: "local", Repository: repository, AgentID: "fixture", ImagePrefixes: []string{"registry.example.com/team/"}}}, Agents: []operating.AgentConfig{{ID: "fixture", TokenFile: path}}}}
	mux := http.NewServeMux()
	h.Routes(mux)
	call := func(method, path string, body any, index int, key string) *httptest.ResponseRecorder {
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		r.Header.Set("Origin", "http://127.0.0.1:5173")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		if index >= 0 {
			r.AddCookie(&http.Cookie{Name: "mini_fury_session", Value: tokens[index]})
			r.Header.Set("X-CSRF-Token", identity.CSRF(tokens[index]))
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	expect := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("status %d want %d: %s", w.Code, status, w.Body)
		}
	}
	in := operating.ScopeInput{Name: "test-api", Environment: "staging", Criticality: "low", ClusterID: "lab", Namespace: "fixture", Replicas: 1, CPUMilli: 100, MemoryMiB: 128}
	expect(call("POST", "/api/applications/"+app+"/scopes", in, 3, ""), 404)
	expect(call("GET", "/api/applications/"+app+"/scopes", nil, 4, ""), 404)
	w := call("POST", "/api/applications/"+app+"/scopes", in, 2, "")
	expect(w, 201)
	var created struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &created)
	scope := created.ID
	expect(call("POST", "/api/applications/"+app+"/scopes", in, 2, ""), 409)
	in.Name = "prod-api"
	in.Environment = "production"
	expect(call("POST", "/api/applications/"+app+"/scopes", in, 2, ""), 404)
	expect(call("POST", "/api/applications/"+app+"/scopes", in, 1, ""), 201)
	in.Name = "foreign"
	in.Namespace = "other"
	expect(call("POST", "/api/applications/"+app+"/scopes", in, 0, ""), 422)
	expect(call("POST", "/api/scopes/"+scope+"/configuration", map[string]any{"revision": 0, "variables": map[string]string{"PASSWORD": "secret"}}, 2, ""), 422)
	config := map[string]any{"revision": 0, "variables": map[string]string{"MESSAGE": "ready"}, "secretRefs": map[string]string{"DB_PASSWORD": "database"}}
	expect(call("POST", "/api/scopes/"+scope+"/configuration", config, 3, ""), 404)
	expect(call("POST", "/api/scopes/"+scope+"/configuration", config, 2, ""), 201)
	expect(call("POST", "/api/scopes/"+scope+"/configuration", config, 2, ""), 409)
	w = call("GET", "/api/scopes/"+scope+"/configuration", nil, 3, "")
	expect(w, 200)
	if strings.Contains(w.Body.String(), "private-secret") {
		t.Fatal("physical secret metadata exposed")
	}
	rev := strings.Repeat("a", 64)
	build := map[string]string{"name": "vone", "sourceRevision": rev}
	idem := "integration-build-key-0001"
	w = call("POST", "/api/applications/"+app+"/builds", build, 2, idem)
	expect(w, 202)
	var job operating.Job
	json.Unmarshal(w.Body.Bytes(), &job)
	w2 := call("POST", "/api/applications/"+app+"/builds", build, 2, idem)
	expect(w2, 202)
	var duplicate operating.Job
	json.Unmarshal(w2.Body.Bytes(), &duplicate)
	if duplicate.ID != job.ID {
		t.Fatal("duplicate operation")
	}
	expect(call("POST", "/api/applications/"+app+"/builds", map[string]string{"name": "different", "sourceRevision": rev}, 2, idem), 409)
	agentCall := func(path string, body any, signed bool) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", path, bytes.NewReader(data))
		r.Header.Set("Authorization", "Bearer "+key)
		r.Header.Set("Content-Type", "application/json")
		if signed {
			mac := hmac.New(sha256.New, []byte(key))
			mac.Write(data)
			r.Header.Set("X-Mini-Fury-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	expect(call("POST", "/api/runtime/build-agent/claim", map[string]string{}, -1, ""), 401)
	w = agentCall("/api/runtime/build-agent/claim", map[string]string{}, false)
	expect(w, 200)
	var lease struct {
		LeaseToken string `json:"leaseToken"`
	}
	json.Unmarshal(w.Body.Bytes(), &lease)
	// Concurrent agents cannot claim the same build twice.
	var claims sync.WaitGroup
	codes := make(chan int, 8)
	for i := 0; i < 8; i++ {
		claims.Add(1)
		go func() {
			defer claims.Done()
			codes <- agentCall("/api/runtime/build-agent/claim", map[string]string{}, false).Code
		}()
	}
	claims.Wait()
	close(codes)
	for code := range codes {
		if code != 204 {
			t.Fatalf("concurrent duplicate claim: %d", code)
		}
	}
	oldLease := lease.LeaseToken
	if _, err := db.Exec(`UPDATE runtime_jobs SET lease_until=now()-interval '1 second' WHERE id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	w = agentCall("/api/runtime/build-agent/claim", map[string]string{}, false)
	expect(w, 200)
	json.Unmarshal(w.Body.Bytes(), &lease)
	if oldLease == lease.LeaseToken {
		t.Fatal("expired lease was not fenced")
	}
	expect(agentCall("/api/runtime/build-agent/jobs/"+job.ID+"/heartbeat", map[string]string{"leaseToken": oldLease}, false), 404)
	report := map[string]any{"leaseToken": lease.LeaseToken, "sourceRevision": rev, "image": "registry.example.com/team/api@sha256:" + strings.Repeat("c", 64), "success": true}
	expect(agentCall("/api/runtime/build-agent/jobs/"+job.ID+"/complete", report, false), 403)
	expect(agentCall("/api/runtime/build-agent/jobs/"+job.ID+"/complete", report, true), 200)
	expect(agentCall("/api/runtime/build-agent/jobs/"+job.ID+"/complete", report, true), 200)
	w = call("GET", "/api/applications/"+app+"/versions", nil, 3, "")
	expect(w, 200)
	var versions struct {
		Versions []operating.Version `json:"versions"`
	}
	json.Unmarshal(w.Body.Bytes(), &versions)
	if len(versions.Versions) != 1 {
		t.Fatal("missing immutable version")
	}
	expect(call("POST", "/api/scopes/"+scope+"/actions/deploy", map[string]string{"versionId": versions.Versions[0].ID}, 3, "integration-deploy-reader"), 404)
	w = call("POST", "/api/scopes/"+scope+"/actions/deploy", map[string]string{"versionId": versions.Versions[0].ID}, 2, "integration-deploy-key-0001")
	expect(w, 202)
	var deploy operating.Job
	json.Unmarshal(w.Body.Bytes(), &deploy)
	expect(call("GET", "/api/runtime/jobs/"+deploy.ID, nil, 4, ""), 404)
	expect(call("POST", "/api/runtime/jobs/"+deploy.ID+"/cancel", map[string]string{}, 3, ""), 404)
	expect(call("POST", "/api/runtime/jobs/"+deploy.ID+"/cancel", map[string]string{}, 2, ""), 202)
	w = call("GET", "/api/runtime/jobs/"+deploy.ID, nil, 2, "")
	expect(w, 200)
	if !strings.Contains(w.Body.String(), `"cancelled"`) {
		t.Fatal("cancelled job did not transition")
	}
	w = call("POST", "/api/runtime/jobs/"+deploy.ID+"/retry", map[string]string{}, 2, "integration-retry-key-0001")
	expect(w, 202)

	// 6: Incident lifecycle, optimistic changes, mutes, and concurrent escalation.
	incidentInput := map[string]any{"title": "Service unavailable", "description": "Measured outage in isolated test", "severity": "critical", "scopeId": scope, "assigneeId": users[2].ID, "runbook": "https://example.com/runbook", "escalationMinutes": 1}
	expect(call("POST", "/api/applications/"+app+"/incidents", incidentInput, 3, ""), 404)
	w = call("POST", "/api/applications/"+app+"/incidents", incidentInput, 2, "")
	expect(w, 201)
	var incident operating.Incident
	json.Unmarshal(w.Body.Bytes(), &incident)
	expect(call("GET", "/api/incidents/"+incident.ID, nil, 4, ""), 404)
	action := func(kind string, version int64, minutes int) *httptest.ResponseRecorder {
		return call("POST", "/api/incidents/"+incident.ID+"/actions", map[string]any{"action": kind, "version": version, "message": "Explicit test action", "minutes": minutes}, 2, "")
	}
	expect(action("acknowledge", 1, 0), 200)
	expect(action("resolve", 1, 0), 409)
	expect(action("mute", 2, 30), 200)
	db.Exec(`UPDATE operational_incidents SET escalate_at=now()-interval '1 minute' WHERE id=$1`, incident.ID)
	if err := h.EscalateIncidents(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	db.QueryRow(`SELECT count(*) FROM incident_events WHERE incident_id=$1 AND kind='escalated'`, incident.ID).Scan(&count)
	if count != 0 {
		t.Fatal("muted incident escalated")
	}
	expect(action("unmute", 3, 0), 200)
	var escalation sync.WaitGroup
	failures := make(chan error, 6)
	for i := 0; i < 6; i++ {
		escalation.Add(1)
		go func() { defer escalation.Done(); failures <- h.EscalateIncidents(ctx) }()
	}
	escalation.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	db.QueryRow(`SELECT count(*) FROM incident_events WHERE incident_id=$1 AND kind='escalated'`, incident.ID).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate escalation", count)
	}
	expect(action("resolve", 5, 0), 200)
	// 7: Real measurements are absent until instrumentation is sampled.
	sloInput := map[string]any{"name": "Availability", "indicator": "availability", "targetPpm": 999000, "latencyMs": 300, "windowDays": 7}
	expect(call("POST", "/api/scopes/"+scope+"/slos", sloInput, 3, ""), 404)
	expect(call("POST", "/api/scopes/"+scope+"/slos", sloInput, 2, ""), 201)
	w = call("GET", "/api/scopes/"+scope+"/slos", nil, 3, "")
	expect(w, 200)
	if !strings.Contains(w.Body.String(), `"no_data"`) {
		t.Fatal("fabricated SLO", w.Body.String())
	}
	// 8: Scaffold and catalogue creation commit together; reader cannot write to a team.
	templateInput := map[string]any{"name": "starter-" + fmt.Sprint(time.Now().UnixNano()), "repository": "https://github.com/example/starter", "owner": "Fixture", "teamId": team}
	expect(call("POST", "/api/templates/go-http/applications", templateInput, 3, ""), 404)
	w = call("POST", "/api/templates/go-http/applications", templateInput, 2, "")
	expect(w, 201)
	var scaffold struct {
		Application struct {
			ID string `json:"id"`
		} `json:"application"`
	}
	json.Unmarshal(w.Body.Bytes(), &scaffold)
	defer func() {
		db.Exec(`DELETE FROM application_scaffolds WHERE application_id=$1`, scaffold.Application.ID)
		db.Exec(`DELETE FROM applications WHERE id=$1`, scaffold.Application.ID)
	}()
	w = call("GET", "/api/applications/"+scaffold.Application.ID+"/scaffold", nil, 3, "")
	expect(w, 200)
	if w.Header().Get("Content-Type") != "application/zip" {
		t.Fatal("missing starter archive")
	}
	expect(call("GET", "/api/applications/"+scaffold.Application.ID+"/scaffold", nil, 4, ""), 404)
	// 9: No arbitrary image/command input; reservation and durable operation are atomic.
	h.Config.ServiceProfiles = []operating.ServiceProfile{{ID: "postgres-dev", Name: "PostgreSQL", Engine: "postgres", Image: "registry.example.com/postgres@sha256:" + strings.Repeat("d", 64), SecretAlias: "database", CPUMilli: 250, MemoryMiB: 256}}
	serviceInput := map[string]any{"name": "database", "profile": "postgres-dev", "storageGiB": 1, "backupPolicy": "development-only"}
	expect(call("POST", "/api/scopes/"+scope+"/services", serviceInput, 3, "service-request-key-0001"), 404)
	w = call("POST", "/api/scopes/"+scope+"/services", serviceInput, 2, "service-request-key-0001")
	expect(w, 202)
	expect(call("POST", "/api/scopes/"+scope+"/services", serviceInput, 2, "service-request-key-0001"), 202)
	db.QueryRow(`SELECT count(*) FROM managed_services WHERE scope_id=$1`, scope).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate managed service")
	}
	serviceInput["name"] = "database-two"
	expect(call("POST", "/api/scopes/"+scope+"/services", serviceInput, 2, "service-request-key-0002"), 202)
	serviceInput["name"] = "database-three"
	expect(call("POST", "/api/scopes/"+scope+"/services", serviceInput, 2, "service-request-key-0003"), 409)
	serviceInput["profile"] = "unapproved"
	expect(call("POST", "/api/scopes/"+scope+"/services", serviceInput, 2, "service-request-key-0004"), 422)
	serviceInput["profile"] = "postgres-dev"
	serviceInput["command"] = "arbitrary-shell"
	expect(call("POST", "/api/scopes/"+scope+"/services", serviceInput, 2, "service-request-key-0005"), 400)
	// 10: Governance roles, tariff bounds and optimistic policy updates.
	policy := map[string]any{"currency": "USD", "cpuCoreHour": 0.02, "memoryGiBHour": 0.01, "storageGiBMonth": 0.1, "monthlyBudget": 100, "version": 0}
	expect(call("PUT", "/api/applications/"+app+"/cost-policy", policy, 2, ""), 404)
	expect(call("PUT", "/api/applications/"+app+"/cost-policy", policy, 1, ""), 200)
	expect(call("PUT", "/api/applications/"+app+"/cost-policy", policy, 1, ""), 409)
	policy["version"] = 1
	policy["monthlyBudget"] = 1
	expect(call("PUT", "/api/applications/"+app+"/cost-policy", policy, 1, ""), 200)
	w = call("GET", "/api/applications/"+app+"/capacity", nil, 3, "")
	expect(w, 200)
	if !strings.Contains(w.Body.String(), `"invoicedCost":null`) {
		t.Fatal("estimate presented as invoice")
	}
	// Revocation takes effect on every read, including job history and configuration.
	db.Exec(`DELETE FROM team_members WHERE team_id=$1 AND user_id=$2`, team, users[2].ID)
	expect(call("GET", "/api/scopes/"+scope, nil, 2, ""), 404)
	expect(call("GET", "/api/runtime/jobs/"+deploy.ID, nil, 2, ""), 404)
	expect(call("GET", "/api/incidents/"+incident.ID, nil, 2, ""), 404)
	expect(call("GET", "/api/scopes/"+scope+"/slos", nil, 2, ""), 404)
	expect(call("GET", "/api/scopes/"+scope+"/services", nil, 2, ""), 404)
	expect(call("GET", "/api/applications/"+app+"/capacity", nil, 2, ""), 404)

}
