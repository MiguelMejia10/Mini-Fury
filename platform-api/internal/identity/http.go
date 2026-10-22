package identity

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type HTTP struct {
	Service *Service
	Log     *slog.Logger
	Origins map[string]bool
	Secure  bool
}

func (h *HTTP) cookieName() string {
	if h.Secure {
		return "__Host-mini_fury_session"
	}
	return "mini_fury_session"
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"message": message})
}
func (h *HTTP) originOK(r *http.Request) bool {
	return h.Origins[r.Header.Get("Origin")] && r.Header.Get("Sec-Fetch-Site") != "cross-site"
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		problem(w, 415, "Usa Content-Type: application/json.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err = d.Decode(v); err != nil {
		problem(w, 400, "JSON inválido.")
		return false
	}
	if d.Decode(new(any)) != io.EOF {
		problem(w, 400, "Envía un único objeto JSON.")
		return false
	}
	return true
}
func (h *HTTP) token(r *http.Request) string {
	c, err := r.Cookie(h.cookieName())
	if err != nil || len(c.Value) != 43 {
		return ""
	}
	return c.Value
}
func (h *HTTP) setCookie(w http.ResponseWriter, token string) {
	maxAge := 8 * 60 * 60
	expiry := time.Now().Add(8 * time.Hour)
	if token == "" {
		maxAge = -1
		expiry = time.Unix(1, 0)
	}
	http.SetCookie(w, &http.Cookie{Name: h.cookieName(), Value: token, Path: "/", HttpOnly: true, Secure: h.Secure, SameSite: http.SameSiteStrictMode, MaxAge: maxAge, Expires: expiry})
}
func (h *HTTP) Protected(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := h.token(r)
		if token == "" {
			problem(w, 401, "Inicia sesión para continuar.")
			return
		}
		user, err := h.Service.Repo.Session(r.Context(), Digest(token))
		if errors.Is(err, ErrUnauthorized) {
			h.setCookie(w, "")
			problem(w, 401, "Tu sesión terminó. Inicia sesión de nuevo.")
			return
		}
		if err != nil {
			h.internal(w, err)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if !h.originOK(r) || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(CSRF(token))) != 1 {
				problem(w, 403, "Solicitud no autorizada. Actualiza la sesión e intenta de nuevo.")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
	})
}
func (h *HTTP) admin(next http.HandlerFunc) http.Handler {
	return h.Protected(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := CurrentUser(r.Context())
		if u.Role != "admin" {
			problem(w, 403, "Se requiere el rol administrador.")
			return
		}
		next(w, r)
	}))
}
func (h *HTTP) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/login", h.login)
	mux.Handle("GET /api/auth/me", h.Protected(http.HandlerFunc(h.me)))
	mux.Handle("POST /api/auth/logout", h.Protected(http.HandlerFunc(h.logout)))
	mux.Handle("POST /api/auth/password", h.Protected(http.HandlerFunc(h.password)))
	mux.Handle("POST /api/auth/logout-all", h.Protected(http.HandlerFunc(h.logoutAll)))
	mux.Handle("PATCH /api/admin/users/{id}/status", h.admin(h.setActive))
	mux.Handle("GET /api/admin/users", h.admin(h.users))
	mux.Handle("POST /api/admin/users", h.admin(h.createUser))
	mux.Handle("GET /api/admin/audit", h.admin(h.audit))
}
func (h *HTTP) internal(w http.ResponseWriter, err error) {
	h.Log.Error("identity_operation_failed", "error", err)
	problem(w, 503, "El servicio de identidad no está disponible. Intenta de nuevo.")
}
func (h *HTTP) login(w http.ResponseWriter, r *http.Request) {
	if !h.originOK(r) {
		problem(w, 403, "Origen no autorizado.")
		return
	}
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	user, token, err := h.Service.Login(r.Context(), input.Email, input.Password, ip, h.token(r))
	switch {
	case errors.Is(err, ErrCredentials):
		h.Log.Warn("login_rejected")
		problem(w, 401, "Correo o contraseña incorrectos.")
		return
	case errors.Is(err, ErrRate):
		w.Header().Set("Retry-After", "900")
		problem(w, 429, "Demasiados intentos. Espera 15 minutos antes de volver a intentar.")
		return
	case errors.Is(err, ErrBusy):
		w.Header().Set("Retry-After", "2")
		problem(w, 503, "Hay demasiados accesos simultáneos. Intenta en unos segundos.")
		return
	case err != nil:
		h.internal(w, err)
		return
	}
	h.setCookie(w, token)
	respond(w, 200, map[string]any{"user": user, "csrfToken": CSRF(token)})
}
func (h *HTTP) me(w http.ResponseWriter, r *http.Request) {
	u, _ := CurrentUser(r.Context())
	respond(w, 200, map[string]any{"user": u, "csrfToken": CSRF(h.token(r))})
}
func (h *HTTP) logout(w http.ResponseWriter, r *http.Request) {
	u, _ := CurrentUser(r.Context())
	if err := h.Service.Repo.Logout(r.Context(), u, Digest(h.token(r))); err != nil {
		h.internal(w, err)
		return
	}
	h.setCookie(w, "")
	respond(w, 200, map[string]bool{"ok": true})
}
func Page(r *http.Request) (int, int, error) {
	limit, offset := 100, 0
	var err error
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
		if err != nil {
			return 0, 0, err
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		offset, err = strconv.Atoi(v)
		if err != nil {
			return 0, 0, err
		}
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 10000 {
		return 0, 0, errors.New("invalid pagination")
	}
	return limit, offset, nil
}
func (h *HTTP) users(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := Page(r)
	if err != nil {
		problem(w, 400, "Paginación inválida.")
		return
	}
	users, err := h.Service.Repo.Users(r.Context(), limit+1, offset)
	if err != nil {
		h.internal(w, err)
		return
	}
	more := len(users) > limit
	if more {
		users = users[:limit]
	}
	respond(w, 200, map[string]any{"users": users, "hasMore": more})
}
func (h *HTTP) createUser(w http.ResponseWriter, r *http.Request) {
	var in NewUser
	if !decode(w, r, &in) {
		return
	}
	if err := in.Validate(); err != nil {
		problem(w, 422, err.Error())
		return
	}
	u, _ := CurrentUser(r.Context())
	created, err := h.Service.CreateUser(r.Context(), &u, in)
	if errors.Is(err, ErrForbidden) {
		problem(w, 403, "La cuenta ya no tiene permisos de administrador.")
		return
	}
	if errors.Is(err, ErrConflict) {
		problem(w, 409, "El correo ya está registrado.")
		return
	}
	if errors.Is(err, ErrBusy) {
		problem(w, 503, "Intenta de nuevo en unos segundos.")
		return
	}
	if err != nil {
		h.internal(w, err)
		return
	}
	respond(w, 201, created)
}
func (h *HTTP) audit(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := Page(r)
	if err != nil {
		problem(w, 400, "Paginación inválida.")
		return
	}
	f, err := auditFilter(r)
	if err != nil {
		problem(w, 400, "Filtros de auditoría inválidos. Usa fechas RFC3339 y un rango válido.")
		return
	}
	events, err := h.Service.Repo.SearchAudit(r.Context(), f, limit+1, offset)
	if err != nil {
		h.internal(w, err)
		return
	}
	more := len(events) > limit
	if more {
		events = events[:limit]
	}
	respond(w, 200, map[string]any{"events": events, "hasMore": more})
}
func Origins(raw string) map[string]bool {
	m := map[string]bool{}
	for _, origin := range strings.Split(raw, ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			m[origin] = true
		}
	}
	return m
}
