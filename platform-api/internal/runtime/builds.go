package runtime

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mini-operador-ti/platform-api/internal/identity"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var errDispatchUnknown = errors.New("external dispatch outcome unknown")

func (h *HTTP) agent(r *http.Request) (string, []byte, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || len(token) < 32 {
		return "", nil, false
	}
	for _, a := range h.Config.Agents {
		data, e := os.ReadFile(a.TokenFile)
		if e == nil && subtle.ConstantTimeCompare([]byte(strings.TrimSpace(string(data))), []byte(token)) == 1 {
			return a.ID, []byte(token), true
		}
	}
	return "", nil, false
}
func (h *HTTP) agentJob(ctx context.Context, id, agent string) (claimed, error) {
	var j claimed
	var raw []byte
	var actor string
	e := h.DB.QueryRowContext(ctx, `SELECT `+jobColumns+`,j.payload,j.owner_token,j.requested_by::text FROM runtime_jobs j WHERE j.id=$1 AND j.kind='build' AND j.state='running' AND NOT j.cancel_requested AND j.lease_until>now() AND j.started_at>now()-interval '10 minutes'`, id).Scan(&j.ID, &j.ApplicationID, &j.ScopeID, &j.Kind, &j.State, &j.Phase, &j.Message, &j.Attempt, &j.CancelRequested, &j.ParentID, &j.CreatedAt, &j.StartedAt, &j.CompletedAt, &raw, &j.Owner, &actor)
	if e != nil {
		return j, ErrDenied
	}
	repo, ok := h.Config.repository(j.ApplicationID)
	if !ok || repo.AgentID != agent || json.Unmarshal(raw, &j.Payload) != nil {
		return j, ErrDenied
	}
	e = h.DB.QueryRowContext(ctx, `SELECT id::text,email,name,role,active FROM users WHERE id=$1`, actor).Scan(&j.Actor.ID, &j.Actor.Email, &j.Actor.Name, &j.Actor.Role, &j.Actor.Active)
	return j, e
}
func (h *HTTP) agentClaim(w http.ResponseWriter, r *http.Request) {
	id, _, ok := h.agent(r)
	if !ok {
		output(w, 401, map[string]string{"message": "Identidad de build requerida."})
		return
	}
	j, worked, e := h.claim(r.Context(), id)
	if e != nil {
		problem(w, e)
		return
	}
	if !worked {
		w.WriteHeader(204)
		return
	}
	ctx := identity.WithUser(r.Context(), j.Actor)
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		problem(w, err)
		return
	}
	err = authorize(ctx, tx, j.ApplicationID, false)
	tx.Rollback()
	if err != nil {
		h.finish(r.Context(), j, "failed", "access_revoked", "El solicitante perdió acceso antes del build")
		problem(w, ErrDenied)
		return
	}
	output(w, 200, map[string]any{"job": j.Job, "payload": j.Payload, "leaseToken": j.Owner})
}
func (h *HTTP) agentGet(w http.ResponseWriter, r *http.Request) {
	id, _, ok := h.agent(r)
	if !ok {
		output(w, 401, map[string]string{"message": "Identidad de build requerida."})
		return
	}
	if !validID(w, r.PathValue("job")) {
		return
	}
	j, e := h.agentJob(r.Context(), r.PathValue("job"), id)
	if e != nil {
		problem(w, e)
		return
	}
	output(w, 200, map[string]any{"job": j.Job, "payload": j.Payload, "leaseToken": j.Owner})
}
func (h *HTTP) agentHeartbeat(w http.ResponseWriter, r *http.Request) {
	id, _, ok := h.agent(r)
	if !ok {
		output(w, 401, map[string]string{"message": "Identidad de build requerida."})
		return
	}
	if !validID(w, r.PathValue("job")) {
		return
	}
	j, e := h.agentJob(r.Context(), r.PathValue("job"), id)
	if e != nil {
		problem(w, e)
		return
	}
	var in struct {
		LeaseToken string `json:"leaseToken"`
	}
	if !decode(w, r, &in) {
		return
	}
	if subtle.ConstantTimeCompare([]byte(j.Owner), []byte(in.LeaseToken)) != 1 {
		problem(w, ErrDenied)
		return
	}
	if e = h.alive(r.Context(), j); e != nil {
		problem(w, e)
		return
	}
	output(w, 200, map[string]bool{"ok": true})
}
func (h *HTTP) agentComplete(w http.ResponseWriter, r *http.Request) {
	agent, key, ok := h.agent(r)
	if !ok {
		output(w, 401, map[string]string{"message": "Identidad de build requerida."})
		return
	}
	if !validID(w, r.PathValue("job")) {
		return
	}
	data, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
	if e != nil {
		problem(w, ErrInvalid)
		return
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(want), []byte(r.Header.Get("X-Mini-Fury-Signature"))) != 1 {
		output(w, 403, map[string]string{"message": "Firma de build inválida."})
		return
	}
	var in struct {
		LeaseToken     string `json:"leaseToken"`
		SourceRevision string `json:"sourceRevision"`
		Image          string `json:"image"`
		Success        bool   `json:"success"`
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF {
		problem(w, ErrInvalid)
		return
	}
	// A repeated successful callback is idempotent only for the exact release already attached to the job.
	var existing string
	var app string
	e = h.DB.QueryRowContext(r.Context(), `SELECT v.image,v.application_id::text FROM runtime_versions v WHERE v.build_id=$1`, r.PathValue("job")).Scan(&existing, &app)
	if e == nil {
		repo, allowed := h.Config.repository(app)
		if allowed && repo.AgentID == agent && existing == in.Image {
			output(w, 200, map[string]bool{"ok": true})
			return
		}
		problem(w, ErrConflict)
		return
	}
	j, e := h.agentJob(r.Context(), r.PathValue("job"), agent)
	if e != nil {
		problem(w, e)
		return
	}
	if subtle.ConstantTimeCompare([]byte(j.Owner), []byte(in.LeaseToken)) != 1 || in.SourceRevision != j.Payload.SourceRevision {
		problem(w, ErrDenied)
		return
	}
	repo, _ := h.Config.repository(j.ApplicationID)
	if in.Success && !allowedImage(repo, in.Image) {
		problem(w, ErrInvalid)
		return
	}
	ctx := identity.WithUser(r.Context(), j.Actor)
	tx, e := h.DB.BeginTx(ctx, nil)
	if e != nil {
		problem(w, e)
		return
	}
	defer tx.Rollback()
	if e = authorize(ctx, tx, j.ApplicationID, false); e != nil {
		problem(w, e)
		return
	}
	var state, owner string
	var cancelled bool
	if e = tx.QueryRowContext(ctx, `SELECT state,owner_token,cancel_requested FROM runtime_jobs WHERE id=$1 FOR UPDATE`, j.ID).Scan(&state, &owner, &cancelled); e != nil || state != "running" || owner != j.Owner || cancelled {
		problem(w, ErrConflict)
		return
	}
	state = "failed"
	phase, message := "build_failed", "El runner reportó un build fallido."
	if in.Success {
		_, e = tx.ExecContext(ctx, `INSERT INTO runtime_versions(application_id,name,source_revision,image,provenance,build_id,created_by) VALUES($1,$2,$3,$4,$5,$6,$7)`, j.ApplicationID, j.Payload.Name, in.SourceRevision, in.Image, repo.Provider, j.ID, j.Actor.ID)
		if e != nil {
			problem(w, ErrConflict)
			return
		}
		state, phase, message = "succeeded", "completed", "Build completado; versión inmutable registrada."
	}
	if _, e = tx.ExecContext(ctx, `UPDATE runtime_jobs SET state=$2,phase=$3,message=$4,completed_at=now(),lease_until=NULL WHERE id=$1`, j.ID, state, phase, message); e == nil {
		_, e = tx.ExecContext(ctx, `INSERT INTO runtime_job_events(job_id,state,phase,message)VALUES($1,$2,$3,$4)`, j.ID, state, phase, message)
	}
	if e == nil {
		e = event(ctx, tx, "runtime.build_completed", j.ID)
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		problem(w, e)
		return
	}
	output(w, 200, map[string]bool{"ok": true})
}
func (h *HTTP) remoteBuild(ctx context.Context, j claimed) error {
	repo, ok := h.Config.repository(j.ApplicationID)
	if !ok || repo.Provider != j.Payload.Provider || repo.Provider == "local" {
		return ErrDenied
	}
	var repository string
	if e := h.DB.QueryRowContext(ctx, `SELECT repository FROM applications WHERE id=$1`, j.ApplicationID).Scan(&repository); e != nil || repository != repo.Repository {
		return ErrDenied
	}
	if j.Phase == "starting" {
		token, e := os.ReadFile(repo.TokenFile)
		if e != nil {
			return errors.New("CI identity unavailable")
		}
		u, e := url.Parse(repo.Repository)
		if e != nil {
			return ErrInvalid
		}
		parts := strings.Trim(strings.TrimSuffix(u.Path, ".git"), "/")
		api := repo.APIURL
		if api == "" {
			if repo.Provider == "github" {
				api = "https://api.github.com"
			} else {
				api = "https://gitlab.com/api/v4"
			}
		}
		apiURL, e := url.Parse(api)
		if e != nil || apiURL.Scheme != "https" || apiURL.User != nil {
			return ErrInvalid
		}
		var endpoint string
		var body any
		if repo.Provider == "github" {
			if u.Hostname() != "github.com" || repo.Workflow == "" || strings.ContainsAny(repo.Workflow, "/\\") {
				return ErrInvalid
			}
			endpoint = api + "/repos/" + parts + "/actions/workflows/" + url.PathEscape(repo.Workflow) + "/dispatches"
			body = map[string]any{"ref": repo.Ref, "inputs": map[string]string{"mini_fury_job": j.ID, "source_revision": j.Payload.SourceRevision}}
		} else if repo.Provider == "gitlab" {
			endpoint = api + "/projects/" + url.PathEscape(parts) + "/pipeline"
			body = map[string]any{"ref": repo.Ref, "variables": []any{map[string]string{"key": "MINI_FURY_JOB", "value": j.ID}, map[string]string{"key": "SOURCE_REVISION", "value": j.Payload.SourceRevision}}}
		} else {
			return ErrInvalid
		}
		// Record uncertainty before the external call: a crash must never blindly redispatch a workflow.
		if e = h.phase(ctx, j, "dispatching", "Solicitando pipeline al proveedor configurado"); e != nil {
			return e
		}
		data, _ := json.Marshal(body)
		req, e := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(data))
		if e != nil {
			return e
		}
		req.Header.Set("Content-Type", "application/json")
		if repo.Provider == "github" {
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
			req.Header.Set("Accept", "application/vnd.github+json")
			req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		} else {
			req.Header.Set("PRIVATE-TOKEN", strings.TrimSpace(string(token)))
		}
		client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		res, e := client.Do(req)
		if e != nil {
			return errDispatchUnknown
		}
		res.Body.Close()
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return errors.New("CI rejected dispatch")
		}
		if e = h.phase(ctx, j, "waiting_ci", "Pipeline solicitado; esperando reporte firmado del runner"); e != nil {
			return e
		}
	} else if j.Phase == "dispatching" {
		return errDispatchUnknown
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		var state string
		e := h.DB.QueryRowContext(ctx, `SELECT state FROM runtime_jobs WHERE id=$1`, j.ID).Scan(&state)
		if e != nil {
			return e
		}
		if state == "succeeded" || state == "failed" {
			return nil
		}
		if e = h.alive(ctx, j); e != nil {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
