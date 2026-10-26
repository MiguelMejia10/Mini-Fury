// Package recovery provides single-use, version-bound recovery tokens delivered through an encrypted outbox.
package recovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mini-operador-ti/platform-api/internal/delivery"
	"mini-operador-ti/platform-api/internal/identity"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

type HTTP struct {
	DB        *sql.DB
	Queue     *delivery.Queue
	PublicURL string
	Origins   map[string]bool
	Enabled   bool
	slots     chan struct{}
}

func New(db *sql.DB, q *delivery.Queue, publicURL string, origins map[string]bool, enabled bool) (*HTTP, error) {
	if enabled {
		u, e := url.Parse(publicURL)
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" || u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost")) {
			return nil, errors.New("APP_PUBLIC_URL must be an HTTPS origin (loopback HTTP allowed for development)")
		}
		if q == nil {
			return nil, errors.New("recovery requires encrypted delivery queue")
		}
	}
	return &HTTP{DB: db, Queue: q, PublicURL: strings.TrimRight(publicURL, "/"), Origins: origins, Enabled: enabled, slots: make(chan struct{}, 2)}, nil
}
func output(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func reply(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"message": message})
}
func (h *HTTP) Routes(m *http.ServeMux) {
	m.HandleFunc("GET /api/auth/recovery", func(w http.ResponseWriter, r *http.Request) { output(w, 200, map[string]bool{"enabled": h.Enabled}) })
	m.HandleFunc("POST /api/auth/forgot-password", h.forgot)
	m.HandleFunc("POST /api/auth/reset-password", h.reset)
}
func (h *HTTP) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if !h.Origins[r.Header.Get("Origin")] || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		reply(w, 403, "Origen no autorizado.")
		return false
	}
	kind, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || kind != "application/json" {
		reply(w, 415, "Usa JSON.")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		reply(w, 400, "Solicitud inválida.")
		return false
	}
	return true
}
func (h *HTTP) rate(ctx context.Context, r *http.Request, key string) bool {
	ip, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		ip = r.RemoteAddr
	}
	allowed, e := (&identity.Postgres{DB: h.DB}).AllowLogin(ctx, identity.Digest("recovery-ip:"+ip), identity.Digest("recovery:"+key))
	return e == nil && allowed
}
func (h *HTTP) forgot(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if !h.decode(w, r, &in) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	address, e := mail.ParseAddress(email)
	if e != nil || address.Address != email || len(email) > 254 {
		reply(w, 422, "Correo inválido.")
		return
	}
	if !h.rate(r.Context(), r, email) {
		w.Header().Set("Retry-After", "900")
		reply(w, 429, "Espera antes de volver a solicitar recuperación.")
		return
	}
	if !h.Enabled {
		reply(w, 503, "La recuperación por correo aún no está configurada.")
		return
	}
	// A minimum response duration reduces account-existence timing differences; no SMTP runs on the request.
	start := time.Now()
	e = h.request(r.Context(), email)
	remaining := 100*time.Millisecond - time.Since(start)
	if remaining > 0 {
		timer := time.NewTimer(remaining)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-r.Context().Done():
			return
		}
	}
	if e != nil {
		reply(w, 503, "No se pudo procesar la solicitud. Intenta más tarde.")
		return
	}
	reply(w, 202, "Si la cuenta está activa, recibirás instrucciones para recuperar el acceso.")
}
func (h *HTTP) request(ctx context.Context, email string) error {
	tx, e := h.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var id string
	var version int64
	e = tx.QueryRowContext(ctx, `SELECT id::text,credential_version FROM users WHERE email=$1 AND active FOR UPDATE`, email).Scan(&id, &version)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	token, e := identity.Token()
	if e != nil {
		return e
	}
	hash := identity.Digest(token)
	expiry := time.Now().Add(30 * time.Minute)
	if _, e = tx.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id=$1`, id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO password_resets(token_hash,user_id,credential_version,expires_at) VALUES($1,$2,$3,$4)`, hash, id, version, expiry); e != nil {
		return e
	}
	// Fragment tokens do not reach HTTP access logs or Referer headers.
	link := h.PublicURL + "/#reset-password?token=" + url.QueryEscape(token)
	e = h.Queue.Enqueue(ctx, tx, "reset:"+hash, "email", delivery.Message{To: email, Subject: "Recupera tu acceso a Mini Fury", Text: "Abre este enlace para elegir una nueva contraseña. Vence en 30 minutos y solo puede usarse una vez:\n\n" + link + "\n\nSi no solicitaste este cambio, ignora este mensaje."}, expiry)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (h *HTTP) reset(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !h.decode(w, r, &in) {
		return
	}
	if !h.Enabled {
		reply(w, 503, "Recuperación no configurada.")
		return
	}
	if len(in.Token) != 43 || utf8.RuneCountInString(in.Password) < 12 || len(in.Password) > 128 {
		reply(w, 422, "Usa un enlace válido y una contraseña de al menos 12 caracteres y hasta 128 bytes.")
		return
	}
	if !h.rate(r.Context(), r, "reset:"+identity.Digest(in.Token)) {
		w.Header().Set("Retry-After", "900")
		reply(w, 429, "Espera antes de volver a intentar.")
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		reply(w, 503, "Intenta de nuevo en unos segundos.")
		return
	}
	hash, e := identity.HashPassword(in.Password)
	if e != nil {
		reply(w, 422, "Contraseña inválida.")
		return
	}
	e = h.consume(r.Context(), identity.Digest(in.Token), hash)
	if errors.Is(e, identity.ErrCredentials) {
		reply(w, 400, "El enlace venció o ya no es válido. Solicita otro.")
		return
	}
	if e != nil {
		reply(w, 503, "No se pudo recuperar el acceso.")
		return
	}
	reply(w, 200, "Contraseña actualizada. Inicia sesión de nuevo; las sesiones anteriores fueron cerradas.")
}
func (h *HTTP) consume(ctx context.Context, token, hash string) error {
	tx, e := h.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var id string
	if e = tx.QueryRowContext(ctx, `SELECT user_id::text FROM password_resets WHERE token_hash=$1`, token).Scan(&id); errors.Is(e, sql.ErrNoRows) {
		return identity.ErrCredentials
	} else if e != nil {
		return e
	}
	var active bool
	var version int64
	var email string
	if e = tx.QueryRowContext(ctx, `SELECT active,credential_version,email FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&active, &version, &email); e != nil {
		return e
	}
	var valid bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM password_resets WHERE token_hash=$1 AND expires_at>now() AND credential_version=$2)`, token, version).Scan(&valid); e != nil {
		return e
	}
	if !active || !valid {
		return identity.ErrCredentials
	}
	if _, e = tx.ExecContext(ctx, `UPDATE users SET password_hash=$2,credential_version=credential_version+1 WHERE id=$1`, id, hash); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=$1;`, id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id=$1`, id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,actor_email,action,resource_id) VALUES($1,$2,'auth.password_reset',$3)`, id, email, id); e != nil {
		return e
	}
	return tx.Commit()
}
