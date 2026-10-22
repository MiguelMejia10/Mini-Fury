package teams

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mini-operador-ti/platform-api/internal/identity"
	"net/http"
	"regexp"
)

type HTTP struct {
	Repo Repository
	Auth *identity.HTTP
}

var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, e error) {
	status, message := 503, "No se pudo completar la operación de equipo."
	switch {
	case errors.Is(e, ErrForbidden):
		status, message = 403, "No tienes permisos para administrar este equipo."
	case errors.Is(e, ErrNotFound):
		status, message = 404, "Recurso de equipo no encontrado."
	case errors.Is(e, ErrConflict):
		status, message = 409, "Conflicto: invitación existente, miembro existente o último administrador activo."
	}
	send(w, status, map[string]string{"message": message})
}
func body(w http.ResponseWriter, r *http.Request, v any) bool {
	kind, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || kind != "application/json" {
		send(w, 415, map[string]string{"message": "Usa JSON."})
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		send(w, 400, map[string]string{"message": "JSON inválido."})
		return false
	}
	return true
}
func ids(w http.ResponseWriter, r *http.Request) bool {
	for _, k := range []string{"team", "id"} {
		if v := r.PathValue(k); v != "" && !uuid.MatchString(v) {
			send(w, 400, map[string]string{"message": "Identificador inválido."})
			return false
		}
	}
	return true
}
func (h *HTTP) Routes(m *http.ServeMux) {
	routes := map[string]http.HandlerFunc{"GET /api/teams": h.list, "POST /api/teams": h.create, "GET /api/teams/{team}/members": h.members, "PUT /api/teams/{team}/members/{id}": h.member, "DELETE /api/teams/{team}/members/{id}": h.member, "POST /api/teams/{team}/invitations": h.invite, "GET /api/teams/{team}/invitations": h.pending, "DELETE /api/teams/{team}/invitations/{id}": h.revoke, "GET /api/team-invitations": h.invitations, "POST /api/team-invitations/{id}/accept": h.accept}
	for pattern, fn := range routes {
		handler := fn
		m.Handle(pattern, h.Auth.Protected(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ids(w, r) {
				handler(w, r)
			}
		})))
	}
}
func page(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	l, o, e := identity.Page(r)
	if e != nil {
		send(w, 400, map[string]string{"message": "Paginación inválida."})
		return 0, 0, false
	}
	return l, o, true
}
func (h *HTTP) list(w http.ResponseWriter, r *http.Request) {
	l, o, ok := page(w, r)
	if !ok {
		return
	}
	rows, e := h.Repo.List(r.Context(), l+1, o)
	if e != nil {
		fail(w, e)
		return
	}
	more := len(rows) > l
	if more {
		rows = rows[:l]
	}
	send(w, 200, map[string]any{"teams": rows, "hasMore": more})
}
func (h *HTTP) create(w http.ResponseWriter, r *http.Request) {
	var in NewTeam
	if !body(w, r, &in) {
		return
	}
	if e := in.Validate(); e != nil {
		send(w, 422, map[string]string{"message": e.Error()})
		return
	}
	t, e := h.Repo.Create(r.Context(), in)
	if e != nil {
		fail(w, e)
		return
	}
	send(w, 201, t)
}
func (h *HTTP) members(w http.ResponseWriter, r *http.Request) {
	l, o, ok := page(w, r)
	if !ok {
		return
	}
	rows, e := h.Repo.Members(r.Context(), r.PathValue("team"), l+1, o)
	if e != nil {
		fail(w, e)
		return
	}
	more := len(rows) > l
	if more {
		rows = rows[:l]
	}
	send(w, 200, map[string]any{"members": rows, "hasMore": more})
}
func (h *HTTP) member(w http.ResponseWriter, r *http.Request) {
	role := ""
	if r.Method == "PUT" {
		var in struct {
			Role string `json:"role"`
		}
		if !body(w, r, &in) {
			return
		}
		role = in.Role
		if !ValidRole(role) {
			send(w, 422, map[string]string{"message": "Rol inválido."})
			return
		}
	}
	if e := h.Repo.SetMember(r.Context(), r.PathValue("team"), r.PathValue("id"), role); e != nil {
		fail(w, e)
		return
	}
	send(w, 200, map[string]bool{"ok": true})
}
func (h *HTTP) invite(w http.ResponseWriter, r *http.Request) {
	var in Invite
	if !body(w, r, &in) {
		return
	}
	if e := in.Validate(); e != nil {
		send(w, 422, map[string]string{"message": e.Error()})
		return
	}
	i, e := h.Repo.Invite(r.Context(), r.PathValue("team"), in)
	if e != nil {
		fail(w, e)
		return
	}
	send(w, 201, i)
}
func (h *HTTP) pending(w http.ResponseWriter, r *http.Request)     { h.inviteList(w, r, true) }
func (h *HTTP) invitations(w http.ResponseWriter, r *http.Request) { h.inviteList(w, r, false) }
func (h *HTTP) inviteList(w http.ResponseWriter, r *http.Request, team bool) {
	l, o, ok := page(w, r)
	if !ok {
		return
	}
	var rows []Invitation
	var e error
	if team {
		rows, e = h.Repo.TeamInvitations(r.Context(), r.PathValue("team"), l+1, o)
	} else {
		rows, e = h.Repo.Invitations(r.Context(), l+1, o)
	}
	if e != nil {
		fail(w, e)
		return
	}
	more := len(rows) > l
	if more {
		rows = rows[:l]
	}
	send(w, 200, map[string]any{"invitations": rows, "hasMore": more})
}
func (h *HTTP) accept(w http.ResponseWriter, r *http.Request) {
	if e := h.Repo.Accept(r.Context(), r.PathValue("id")); e != nil {
		fail(w, e)
		return
	}
	send(w, 200, map[string]bool{"ok": true})
}
func (h *HTTP) revoke(w http.ResponseWriter, r *http.Request) {
	if e := h.Repo.RevokeInvitation(r.Context(), r.PathValue("team"), r.PathValue("id")); e != nil {
		fail(w, e)
		return
	}
	send(w, 200, map[string]bool{"ok": true})
}
