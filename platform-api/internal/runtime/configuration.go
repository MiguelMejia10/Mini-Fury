package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"mini-operador-ti/platform-api/internal/identity"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Configuration struct {
	ID         string            `json:"id"`
	Revision   int64             `json:"revision"`
	Variables  map[string]string `json:"variables"`
	SecretRefs map[string]string `json:"secretRefs"`
	CreatedAt  time.Time         `json:"createdAt"`
}

var envNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,79}$`)
var sensitiveNamePattern = regexp.MustCompile(`(?i)(password|passwd|secret|token|credential|private_key|api_key|access_key)`)

func validateConfiguration(c Configuration, b Binding) error {
	if len(c.Variables)+len(c.SecretRefs) > 50 {
		return ErrInvalid
	}
	total := 0
	for key, value := range c.Variables {
		total += len(key) + len(value)
		if !envNamePattern.MatchString(key) || sensitiveNamePattern.MatchString(key) || !utf8.ValidString(value) || len(value) > 4096 || strings.ContainsRune(value, 0) {
			return ErrInvalid
		}
		if _, both := c.SecretRefs[key]; both {
			return ErrInvalid
		}
	}
	for key, alias := range c.SecretRefs {
		if !envNamePattern.MatchString(key) {
			return ErrInvalid
		}
		if _, ok := b.Secrets[alias]; !ok {
			return ErrInvalid
		}
	}
	if total > 32*1024 {
		return ErrInvalid
	}
	return nil
}
func (h *HTTP) configuration(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("scope")) {
		return
	}
	s, e := h.scope(r)
	if e != nil {
		problem(w, e)
		return
	}
	limit, offset, e := identity.Page(r)
	if e != nil {
		problem(w, ErrInvalid)
		return
	}
	rows, e := h.DB.QueryContext(r.Context(), `SELECT id::text,revision,variables,secret_refs,created_at FROM runtime_configurations WHERE scope_id=$1 ORDER BY revision DESC LIMIT $2 OFFSET $3`, s.ID, limit+1, offset)
	if e != nil {
		problem(w, e)
		return
	}
	defer rows.Close()
	out := []Configuration{}
	for rows.Next() {
		var c Configuration
		var vars, refs []byte
		if e = rows.Scan(&c.ID, &c.Revision, &vars, &refs, &c.CreatedAt); e != nil {
			problem(w, e)
			return
		}
		if json.Unmarshal(vars, &c.Variables) != nil || json.Unmarshal(refs, &c.SecretRefs) != nil {
			problem(w, errors.New("invalid persisted configuration"))
			return
		}
		out = append(out, c)
	}
	if rows.Err() != nil {
		problem(w, rows.Err())
		return
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	output(w, 200, map[string]any{"configurations": out, "hasMore": more})
}
func (h *HTTP) saveConfiguration(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("scope")) {
		return
	}
	s, e := h.scope(r)
	if e != nil {
		problem(w, e)
		return
	}
	var in Configuration
	if !decode(w, r, &in) {
		return
	}
	if in.Variables == nil {
		in.Variables = map[string]string{}
	}
	if in.SecretRefs == nil {
		in.SecretRefs = map[string]string{}
	}
	_, binding, ok := h.Config.binding(s.ApplicationID, s.ClusterID, s.Namespace)
	if !ok || validateConfiguration(in, binding) != nil {
		problem(w, ErrInvalid)
		return
	}
	tx, e := h.DB.BeginTx(r.Context(), nil)
	if e != nil {
		problem(w, e)
		return
	}
	defer tx.Rollback()
	if e = authorize(r.Context(), tx, s.ApplicationID, s.Environment == "production"); e != nil {
		problem(w, e)
		return
	}
	var locked string
	if e = tx.QueryRowContext(r.Context(), `SELECT id::text FROM runtime_scopes WHERE id=$1 FOR UPDATE`, s.ID).Scan(&locked); e != nil {
		problem(w, e)
		return
	}
	var current int64
	if e = tx.QueryRowContext(r.Context(), `SELECT COALESCE(max(revision),0) FROM runtime_configurations WHERE scope_id=$1`, s.ID).Scan(&current); e != nil {
		problem(w, e)
		return
	}
	if current != in.Revision {
		problem(w, ErrConflict)
		return
	}
	vars, _ := json.Marshal(in.Variables)
	refs, _ := json.Marshal(in.SecretRefs)
	u, _ := identity.CurrentUser(r.Context())
	e = tx.QueryRowContext(r.Context(), `INSERT INTO runtime_configurations(scope_id,revision,variables,secret_refs,created_by)VALUES($1,$2,$3,$4,$5) RETURNING id::text,created_at`, s.ID, current+1, vars, refs, u.ID).Scan(&in.ID, &in.CreatedAt)
	if e == nil {
		e = event(r.Context(), tx, "scope.configuration_create", s.ID+":"+strconv.FormatInt(current+1, 10))
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		problem(w, e)
		return
	}
	in.Revision = current + 1
	output(w, 201, in)
}
func (h *HTTP) loadConfiguration(ctx context.Context, scope Scope, id string) (Configuration, error) {
	var c Configuration
	c.Variables = map[string]string{}
	c.SecretRefs = map[string]string{}
	if id == "" {
		return c, nil
	}
	var vars, refs []byte
	e := h.DB.QueryRowContext(ctx, `SELECT id::text,revision,variables,secret_refs,created_at FROM runtime_configurations WHERE id=$1 AND scope_id=$2`, id, scope.ID).Scan(&c.ID, &c.Revision, &vars, &refs, &c.CreatedAt)
	if e != nil {
		return c, e
	}
	if json.Unmarshal(vars, &c.Variables) != nil || json.Unmarshal(refs, &c.SecretRefs) != nil {
		return c, ErrInvalid
	}
	return c, nil
}
