package recovery_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"mini-operador-ti/platform-api/internal/delivery"
	"mini-operador-ti/platform-api/internal/identity"
	"mini-operador-ti/platform-api/internal/platform"
	"mini-operador-ti/platform-api/internal/recovery"
	"mini-operador-ti/platform-api/internal/telemetry"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type sink struct {
	mu       sync.Mutex
	messages []delivery.Message
	failNext bool
}

func (s *sink) Send(ctx context.Context, kind string, m delivery.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext {
		s.failNext = false
		return fmt.Errorf("fixture provider down")
	}
	s.messages = append(s.messages, m)
	return nil
}
func TestRecoveryRoundTrip(t *testing.T) {
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
	db.SetMaxOpenConns(8)
	if e = platform.Migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	email := fmt.Sprintf("recovery-%d@test.local", time.Now().UnixNano())
	hash, _ := identity.HashPassword("Previous-Password-2026!")
	var id string
	if e = db.QueryRow(`INSERT INTO users(email,name,role,password_hash)VALUES($1,'Recovery test','developer',$2) RETURNING id::text`, email, hash).Scan(&id); e != nil {
		t.Fatal(e)
	}
	keys := []string{}
	defer func() {
		for _, key := range keys {
			db.Exec(`DELETE FROM delivery_jobs WHERE dedupe_key=$1`, key)
		}
		db.Exec(`DELETE FROM audit_events WHERE actor_id=$1`, id)
		db.Exec(`DELETE FROM users WHERE id=$1`, id)
		db.Exec(`DELETE FROM auth_attempts WHERE key=$1 OR key=$2`, identity.Digest("recovery:"+email), identity.Digest("recovery-ip:192.0.2.90"))
	}()
	send := &sink{}
	q, e := delivery.New(db, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)), send)
	if e != nil {
		t.Fatal(e)
	}
	h, e := recovery.New(db, q, "http://127.0.0.1:5173", identity.Origins("http://127.0.0.1:5173"), true)
	if e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	call := func(path string, body any, origin string) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", path, bytes.NewReader(data))
		r.RemoteAddr = "192.0.2.90:123"
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	origin := "http://127.0.0.1:5173"
	w := call("/api/auth/forgot-password", map[string]string{"email": email}, "https://evil.example")
	if w.Code != 403 {
		t.Fatal("cross-origin recovery accepted")
	}
	w = call("/api/auth/forgot-password", map[string]string{"email": email}, origin)
	if w.Code != 202 {
		t.Fatalf("forgot %d %s", w.Code, w.Body)
	}
	known := w.Body.String()
	w = call("/api/auth/forgot-password", map[string]string{"email": "absent-" + email}, origin)
	if w.Code != 202 || w.Body.String() != known {
		t.Fatal("account enumeration")
	}
	if worked, e := q.ProcessOne(ctx); e != nil || !worked {
		t.Fatalf("queue %v %v", worked, e)
	}
	if len(send.messages) != 1 {
		t.Fatalf("sent %d", len(send.messages))
	}
	text := send.messages[0].Text
	start := strings.Index(text, "http://")
	end := strings.Index(text[start:], "\n")
	link, _ := url.Parse(text[start : start+end])
	token := strings.TrimPrefix(link.Fragment, "reset-password?token=")
	tokenHash := identity.Digest(token)
	keys = append(keys, "reset:"+tokenHash)
	var payload []byte
	db.QueryRow(`SELECT payload FROM delivery_jobs WHERE dedupe_key=$1`, keys[0]).Scan(&payload)
	if len(payload) != 0 {
		t.Fatal("sent sensitive payload retained")
	}
	oldSession, _ := identity.Token()
	db.Exec(`INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,now()+interval '1 hour')`, identity.Digest(oldSession), id)
	if w = call("/api/auth/reset-password", map[string]string{"token": token, "password": "密密密密密密"}, origin); w.Code != 422 {
		t.Fatal("short unicode password accepted")
	}
	// A token can only complete once, including concurrent submissions.
	results := make(chan int, 2)
	for range 2 {
		go func() {
			results <- call("/api/auth/reset-password", map[string]string{"token": token, "password": "Recovered-Password-2026!"}, origin).Code
		}()
	}
	a, b := <-results, <-results
	if !((a == 200 && b == 400) || (a == 400 && b == 200)) {
		t.Fatalf("single use: %d %d", a, b)
	}
	var saved string
	var sessions int
	db.QueryRow(`SELECT password_hash FROM users WHERE id=$1`, id).Scan(&saved)
	db.QueryRow(`SELECT count(*) FROM sessions WHERE user_id=$1`, id).Scan(&sessions)
	if !identity.VerifyPassword("Recovered-Password-2026!", saved) || sessions != 0 {
		t.Fatal("password or session revocation failed")
	}
	// Duplicate enqueue is idempotent, provider errors retry, and concurrent workers claim once.
	retryKey := "fixture-retry:" + id
	keys = append(keys, retryKey)
	tx, _ := db.BeginTx(ctx, nil)
	for range 2 {
		if e = q.Enqueue(ctx, tx, retryKey, "webhook", delivery.Message{ID: id, Text: "fixture"}, time.Now().Add(time.Hour)); e != nil {
			t.Fatal(e)
		}
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	send.failNext = true
	if worked, e := q.ProcessOne(ctx); e != nil || !worked {
		t.Fatalf("first retry %v %v", worked, e)
	}
	var state string
	var attempts int
	if e = db.QueryRow(`SELECT state,attempts FROM delivery_jobs WHERE dedupe_key=$1`, retryKey).Scan(&state, &attempts); e != nil || state != "pending" || attempts != 1 {
		t.Fatalf("retry state %s %d %v", state, attempts, e)
	}
	db.Exec(`UPDATE delivery_jobs SET available_at=now() WHERE dedupe_key=$1`, retryKey)
	claims := make(chan bool, 2)
	for range 2 {
		go func() {
			worked, e := q.ProcessOne(ctx)
			if e != nil {
				claims <- false
				return
			}
			claims <- worked
		}()
	}
	aClaim, bClaim := <-claims, <-claims
	if aClaim == bClaim {
		t.Fatal("outbox claim was duplicated or lost")
	}
	if len(send.messages) != 2 {
		t.Fatal("outbox sent an unexpected number of messages")
	}
	// Alert transitions enqueue once and persist both firing and recovery notifications.
	instance := "notify-" + id
	defer func() {
		db.Exec(`DELETE FROM delivery_jobs WHERE dedupe_key IN(SELECT 'alert:'||t.id::text FROM alert_transitions t JOIN platform_alerts a ON a.id=t.alert_id WHERE a.instance=$1)`, instance)
		db.Exec(`DELETE FROM platform_alerts WHERE instance=$1`, instance)
	}()
	ops := &telemetry.HTTP{DB: db, Metrics: telemetry.New(), Instance: instance, Queue: q, WebhookEnabled: true}
	for range 30 {
		ops.Metrics.Observe(time.Now(), 503, 2*time.Second)
	}
	if e = ops.Evaluate(ctx); e != nil {
		t.Fatal(e)
	}
	countJobs := func() int {
		var count int
		if e = db.QueryRow(`SELECT count(*) FROM delivery_jobs j JOIN alert_transitions t ON j.dedupe_key='alert:'||t.id::text JOIN platform_alerts a ON a.id=t.alert_id WHERE a.instance=$1`, instance).Scan(&count); e != nil {
			t.Fatal(e)
		}
		return count
	}
	if countJobs() != 2 {
		t.Fatal("missing firing notifications")
	}
	if e = ops.Evaluate(ctx); e != nil {
		t.Fatal(e)
	}
	if countJobs() != 2 {
		t.Fatal("duplicate alert jobs")
	}
	ops.Metrics = telemetry.New()
	for range 30 {
		ops.Metrics.Observe(time.Now(), 200, time.Millisecond)
	}
	if e = ops.Evaluate(ctx); e != nil {
		t.Fatal(e)
	}
	if countJobs() != 4 {
		t.Fatal("missing recovery notifications")
	}
	for range 4 {
		if worked, e := q.ProcessOne(ctx); e != nil || !worked {
			t.Fatalf("notification delivery: %v %v", worked, e)
		}
	}
	if len(send.messages) != 6 {
		t.Fatal("transition notifications were not delivered")
	}
	// Expired/version-invalid tokens never change a password.
	token2, _ := identity.Token()
	db.Exec(`INSERT INTO password_resets(token_hash,user_id,credential_version,expires_at) SELECT $1,id,credential_version,now()-interval '1 second' FROM users WHERE id=$2`, identity.Digest(token2), id)
	if w = call("/api/auth/reset-password", map[string]string{"token": token2, "password": "Another-Password-2026!"}, origin); w.Code != 400 {
		t.Fatal("expired token accepted")
	}
	db.Exec(`UPDATE password_resets SET expires_at=now()+interval '1 hour',credential_version=credential_version-1 WHERE token_hash=$1`, identity.Digest(token2))
	if w = call("/api/auth/reset-password", map[string]string{"token": token2, "password": "Another-Password-2026!"}, origin); w.Code != 400 {
		t.Fatal("stale credential token accepted")
	}
}
