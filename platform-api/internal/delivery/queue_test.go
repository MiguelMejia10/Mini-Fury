package delivery

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestEncryptionAndDestinations(t *testing.T) {
	q, e := New(nil, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)), nil)
	if e != nil {
		t.Fatal(e)
	}
	nonce := bytes.Repeat([]byte{2}, q.box.NonceSize())
	sealed := q.box.Seal(nonce, nonce, []byte("secret-token"), []byte("email"))
	if bytes.Contains(sealed, []byte("secret-token")) {
		t.Fatal("plaintext leaked")
	}
	if _, e = q.box.Open(nil, sealed[:q.box.NonceSize()], sealed[q.box.NonceSize():], []byte("webhook")); e == nil {
		t.Fatal("kind substitution accepted")
	}
	if _, e = New(nil, "bad", nil); e == nil {
		t.Fatal("invalid encryption key accepted")
	}
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("Idempotency-Key") != "transition-id" || r.Header.Get("X-Mini-Fury-Signature") == "" {
			t.Error("missing delivery identity")
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()
	if _, e = NewSender(Config{WebhookURL: srv.URL}); e == nil {
		t.Fatal("cleartext production webhook accepted")
	}
	sender, e := NewSender(Config{WebhookURL: srv.URL, LocalWebhook: true, WebhookSecret: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	if e = sender.Send(context.Background(), "webhook", Message{ID: "transition-id", Text: "firing"}); e != nil {
		t.Fatal(e)
	}
	if hits != 1 {
		t.Fatal("missing webhook")
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, srv.URL, 302) }))
	defer redirect.Close()
	sender, _ = NewSender(Config{WebhookURL: redirect.URL, LocalWebhook: true})
	if e = sender.Send(context.Background(), "webhook", Message{}); e == nil || hits != 1 {
		t.Fatal("webhook redirects followed")
	}
}

func TestLocalSMTPFixture(t *testing.T) {
	address := os.Getenv("SMTP_TEST_ADDRESS")
	if address == "" {
		t.Skip("local mail sink required")
	}
	sender, e := NewSender(Config{SMTPAddress: address, From: "no-reply@mini-fury.local", LocalSMTP: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = sender.Send(context.Background(), "email", Message{To: "smtp-fixture@mini-fury.local", Subject: "Mini Fury delivery fixture", Text: "Local-only test delivery. No password reset tokens or real recipients."}); e != nil {
		t.Fatal(e)
	}
}
