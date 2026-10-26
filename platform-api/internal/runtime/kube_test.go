package runtime

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestClusterTLSRotationAndOwnership(t *testing.T) {
	expected := "first-private-token"
	response := `{"metadata":{"labels":{"mini-fury.io/application":"app","mini-fury.io/scope":"scope"}}}`
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+expected {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "https://example.invalid", 302)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(response))
	}))
	defer server.Close()
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca")
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(token, []byte(expected), 0600); err != nil {
		t.Fatal(err)
	}
	config := ClusterConfig{Server: server.URL, CAFile: ca, TokenFile: token}
	client, err := NewKube(config)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{ID: "scope", ApplicationID: "app", ScopeInput: ScopeInput{Namespace: "fixture"}, Workload: "workload"}
	if _, err = client.deployment(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	expected = "rotated-private-token"
	os.WriteFile(token, []byte(expected), 0600)
	if _, err = client.deployment(context.Background(), scope); err != nil {
		t.Fatal("rotated identity not read", err)
	}
	response = `{"metadata":{"labels":{"mini-fury.io/application":"other","mini-fury.io/scope":"scope"}}}`
	if _, err = client.deployment(context.Background(), scope); !errors.Is(err, ErrDenied) {
		t.Fatal("foreign workload accepted", err)
	}
	var out any
	if err = client.request(context.Background(), "GET", "/redirect", nil, nil, &out); err == nil {
		t.Fatal("redirect accepted")
	}
	config.Server = "http://cluster.invalid"
	if _, err = NewKube(config); err == nil {
		t.Fatal("unencrypted cluster accepted")
	}
}

func TestRolloutIgnoresOldAvailabilityAndFailure(t *testing.T) {
	var d Deployment
	json.Unmarshal([]byte(`{"metadata":{"generation":3},"status":{"observedGeneration":2,"replicas":2,"updatedReplicas":1,"availableReplicas":1,"conditions":[{"type":"Progressing","status":"False"}]}}`), &d)
	if rolloutStatusReady(d, 1) || rolloutProgressFailed(d) {
		t.Fatal("stale status determined new rollout outcome")
	}
	d.Status.ObservedGeneration = 3
	if rolloutStatusReady(d, 1) {
		t.Fatal("available old replica hid unready new replica")
	}
	if !rolloutProgressFailed(d) {
		t.Fatal("observed failure ignored")
	}
	d.Status.Replicas = 1
	d.Status.Conditions = nil
	if !rolloutStatusReady(d, 1) {
		t.Fatal("completed rollout not recognized")
	}
}
