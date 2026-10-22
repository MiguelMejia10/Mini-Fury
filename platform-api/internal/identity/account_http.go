package identity

import (
	"errors"
	"net/http"
	"regexp"
	"time"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (h *HTTP) password(w http.ResponseWriter, r *http.Request) {
	var in PasswordChange
	if !decode(w, r, &in) {
		return
	}
	u, _ := CurrentUser(r.Context())
	err := h.Service.ChangePassword(r.Context(), u, in)
	switch {
	case errors.Is(err, ErrInvalidPassword):
		problem(w, 422, err.Error())
		return
	case errors.Is(err, ErrCredentials):
		problem(w, 422, "La contraseña actual no es correcta.")
		return
	case errors.Is(err, ErrAccountConflict):
		problem(w, 409, "La cuenta cambió. Actualiza la sesión antes de continuar.")
		return
	case errors.Is(err, ErrBusy):
		problem(w, 503, "Hay demasiadas solicitudes. Intenta de nuevo.")
		return
	case errors.Is(err, ErrRate):
		w.Header().Set("Retry-After", "900")
		problem(w, 429, "Demasiados intentos. Espera 15 minutos.")
		return
	case err != nil:
		h.internal(w, err)
		return
	}
	h.setCookie(w, "")
	respond(w, 200, map[string]bool{"ok": true})
}
func (h *HTTP) logoutAll(w http.ResponseWriter, r *http.Request) {
	u, _ := CurrentUser(r.Context())
	if err := h.Service.Repo.RevokeAll(r.Context(), u); err != nil {
		h.internal(w, err)
		return
	}
	h.setCookie(w, "")
	respond(w, 200, map[string]bool{"ok": true})
}
func (h *HTTP) setActive(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		problem(w, 400, "Identificador inválido.")
		return
	}
	var in struct {
		Active *bool `json:"active"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Active == nil {
		problem(w, 422, "El estado es obligatorio.")
		return
	}
	actor, _ := CurrentUser(r.Context())
	user, err := h.Service.Repo.SetActive(r.Context(), actor, id, *in.Active)
	switch {
	case errors.Is(err, ErrAccountConflict):
		problem(w, 409, "No puedes desactivar tu cuenta ni al último administrador activo de la plataforma o un equipo.")
		return
	case errors.Is(err, ErrForbidden):
		problem(w, 403, "Operación no permitida.")
		return
	case errors.Is(err, ErrUnauthorized):
		problem(w, 404, "Usuario no encontrado.")
		return
	case err != nil:
		h.internal(w, err)
		return
	}
	respond(w, 200, user)
}
func auditFilter(r *http.Request) (AuditFilter, error) {
	q := r.URL.Query()
	f := AuditFilter{Actor: q.Get("actor"), Action: q.Get("action"), Resource: q.Get("resource")}
	for _, field := range []struct {
		key    string
		target **time.Time
	}{{"from", &f.From}, {"until", &f.Until}} {
		if raw := q.Get(field.key); raw != "" {
			value, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return f, err
			}
			*field.target = &value
		}
	}
	return f, f.Validate()
}
