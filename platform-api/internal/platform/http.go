package platform

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mini-operador-ti/platform-api/internal/identity"
	"mini-operador-ti/platform-api/internal/teams"
	"mini-operador-ti/platform-api/internal/telemetry"
	"net/http"
	"time"
)

type API struct {
	Store      Store
	Log        *slog.Logger
	Auth       *identity.HTTP
	Teams      *teams.HTTP
	Operations *telemetry.HTTP
}
type apiError struct {
	Code    string            `json:"code,omitempty"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func send(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, message string) {
	send(w, status, apiError{Message: message})
}
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", a.health)
	protected := func(fn http.HandlerFunc) http.Handler {
		if a.Auth == nil {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fail(w, 503, "Identidad no configurada.") })
		}
		return a.Auth.Protected(fn)
	}
	mux.Handle("GET /api/applications", protected(a.list))
	mux.Handle("POST /api/applications", protected(a.create))
	mux.Handle("GET /api/applications/{id}", protected(a.get))
	mux.Handle("PUT /api/applications/{id}", protected(a.update))
	mux.Handle("POST /api/applications/{id}/archive", protected(a.archive))
	mux.Handle("POST /api/applications/{id}/restore", protected(a.restore))
	if a.Auth != nil {
		a.Auth.Routes(mux)
	}
	if a.Teams != nil {
		a.Teams.Routes(mux)
	}
	if a.Operations != nil {
		a.Operations.Routes(mux)
	}
	slots := make(chan struct{}, 64)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fail(w, http.StatusNotFound, "Ruta no encontrada.") })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			w.Header().Set("Retry-After", "2")
			fail(w, 503, "Capacidad temporalmente ocupada. Intenta de nuevo.")
			return
		}
		start := time.Now()
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
		a.Log.Info("http_request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(start).Milliseconds())
	})
}
func (a *API) internal(w http.ResponseWriter, err error) {
	a.Log.Error("database_operation_failed", "error", err)
	fail(w, http.StatusServiceUnavailable, "No pudimos acceder al catálogo. Intenta de nuevo.")
}
func (a *API) health(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.Ping(r.Context()); err != nil {
		fail(w, 503, "Base de datos no disponible.")
		return
	}
	send(w, 200, map[string]string{"status": "ok", "service": "platform-api"})
}
func (a *API) list(w http.ResponseWriter, r *http.Request) {
	env := r.URL.Query().Get("environment")
	if env != "" && env != "production" && env != "staging" {
		fail(w, 400, "Ambiente inválido.")
		return
	}
	limit, offset, err := identity.Page(r)
	if err != nil {
		fail(w, 400, "Paginación inválida.")
		return
	}
	f := Filter{Environment: env, Query: r.URL.Query().Get("q"), State: r.URL.Query().Get("state"), Runtime: r.URL.Query().Get("runtime"), Tag: r.URL.Query().Get("tag"), Owner: r.URL.Query().Get("owner")}
	if err = f.Validate(); err != nil {
		fail(w, 400, err.Error())
		return
	}
	apps, err := a.Store.Search(r.Context(), f, limit+1, offset)
	if err != nil {
		a.internal(w, err)
		return
	}
	more := len(apps) > limit
	if more {
		apps = apps[:limit]
	}
	send(w, 200, map[string]any{"applications": apps, "hasMore": more, "nextOffset": offset + len(apps)})
}
func (a *API) create(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		fail(w, http.StatusUnsupportedMediaType, "Usa Content-Type: application/json.")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 128*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var in CreateApplication
	if err := dec.Decode(&in); err != nil {
		fail(w, 400, "Envía un objeto JSON válido con los campos de la aplicación.")
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		fail(w, 400, "El cuerpo debe contener un único objeto JSON.")
		return
	}
	if fields := in.Validate(); len(fields) > 0 {
		send(w, 422, apiError{Message: "Revisa los campos indicados.", Fields: fields})
		return
	}
	app, err := a.Store.Create(r.Context(), in)
	if errors.Is(err, ErrConflict) {
		send(w, 409, apiError{Message: "Ya existe una aplicación con ese nombre en este ambiente.", Fields: map[string]string{"name": "Este nombre ya está registrado en el ambiente seleccionado."}})
		return
	}
	if err != nil {
		a.operationError(w, err)
		return
	}
	w.Header().Set("Location", "/api/applications/"+app.ID)
	send(w, 201, app)
}
func (a *API) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !idPattern.MatchString(id) {
		fail(w, 400, "Identificador de aplicación inválido.")
		return
	}
	app, err := a.Store.Get(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		fail(w, 404, "Aplicación no encontrada.")
		return
	}
	if err != nil {
		a.internal(w, err)
		return
	}
	send(w, 200, app)
}

func (a *API) operationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrPermission):
		fail(w, 403, "Tu rol no permite modificar esta aplicación o su equipo.")
	case errors.Is(err, ErrDependencies):
		fail(w, 422, "Dependencias inválidas, inaccesibles o con ciclos.")
	case errors.Is(err, ErrNotFound):
		fail(w, 404, "Aplicación no encontrada.")
	case errors.Is(err, ErrStale):
		send(w, 409, apiError{Code: "stale_version", Message: "La aplicación cambió en otra sesión. Actualiza el detalle antes de guardar."})
	case errors.Is(err, ErrArchived):
		send(w, 409, apiError{Code: "application_archived", Message: "Restaura la aplicación antes de editarla."})
	case errors.Is(err, ErrConflict):
		send(w, 409, apiError{Code: "name_conflict", Message: "Ya existe ese nombre en el ambiente seleccionado.", Fields: map[string]string{"name": "El nombre ya está reservado en ese ambiente."}})
	case errors.Is(err, identity.ErrUnauthorized):
		fail(w, 401, "La cuenta ya no puede realizar esta operación.")
	default:
		a.internal(w, err)
	}
}
func applicationJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		fail(w, 415, "Usa Content-Type: application/json.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if dec.Decode(target) != nil || dec.Decode(new(any)) != io.EOF {
		fail(w, 400, "JSON inválido.")
		return false
	}
	return true
}
func (a *API) update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !idPattern.MatchString(id) {
		fail(w, 400, "Identificador inválido.")
		return
	}
	var in struct {
		CreateApplication
		Version int64 `json:"version"`
	}
	if !applicationJSON(w, r, &in) {
		return
	}
	if fields := in.Validate(); len(fields) > 0 {
		send(w, 422, apiError{Message: "Revisa los campos indicados.", Fields: fields})
		return
	}
	if in.Version < 1 {
		fail(w, 422, "La versión es obligatoria.")
		return
	}
	app, err := a.Store.Update(r.Context(), id, in.CreateApplication, in.Version)
	if err != nil {
		a.operationError(w, err)
		return
	}
	send(w, 200, app)
}
func (a *API) archive(w http.ResponseWriter, r *http.Request) { a.setArchived(w, r, true) }
func (a *API) restore(w http.ResponseWriter, r *http.Request) { a.setArchived(w, r, false) }
func (a *API) setArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	id := r.PathValue("id")
	if !idPattern.MatchString(id) {
		fail(w, 400, "Identificador inválido.")
		return
	}
	var in struct {
		Version int64 `json:"version"`
	}
	if !applicationJSON(w, r, &in) {
		return
	}
	if in.Version < 1 {
		fail(w, 422, "La versión es obligatoria.")
		return
	}
	app, err := a.Store.Archive(r.Context(), id, archived, in.Version)
	if err != nil {
		a.operationError(w, err)
		return
	}
	send(w, 200, app)
}
