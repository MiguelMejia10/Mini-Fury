package platform

import (
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrNotFound = errors.New("application not found")
var ErrConflict = errors.New("application already registered")
var namePattern = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)
var idPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type CreateApplication struct {
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Name        string   `json:"name"`
	Repository  string   `json:"repository"`
	Port        int      `json:"port"`
	Environment string   `json:"environment"`
	Owner       string   `json:"owner"`
	Runtime     string   `json:"runtime"`
}
type Application struct {
	Version    int64      `json:"version"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	ArchivedAt *time.Time `json:"archivedAt"`
	ID         string     `json:"id"`
	CreateApplication
	Status    string    `json:"status"`
	CreatedBy *string   `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
}

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
func (a *CreateApplication) Validate() map[string]string {
	a.Name = strings.TrimSpace(a.Name)
	a.Repository = strings.TrimSpace(a.Repository)
	a.Owner = strings.TrimSpace(a.Owner)
	a.Runtime = strings.TrimSpace(a.Runtime)
	if a.Runtime == "" {
		a.Runtime = "Contenedor HTTP"
	}
	fields := map[string]string{}
	a.Description = strings.TrimSpace(a.Description)
	if utf8.RuneCountInString(a.Description) > 2000 || strings.ContainsRune(a.Description, 0) {
		fields["description"] = "Máximo 2000 caracteres."
	}
	if len(a.Tags) > 10 {
		fields["tags"] = "Máximo diez etiquetas."
	}
	unique := map[string]bool{}
	tags := []string{}
	for _, tag := range a.Tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if !tagPattern.MatchString(tag) {
			fields["tags"] = "Usa etiquetas de 1 a 32 caracteres: minúsculas, números y guiones."
		}
		if !unique[tag] {
			tags = append(tags, tag)
			unique[tag] = true
		}
	}
	sort.Strings(tags)
	a.Tags = tags
	if !namePattern.MatchString(a.Name) {
		fields["name"] = "Usa entre 1 y 63 caracteres: minúsculas, números y guiones; empieza con letra y termina con letra o número."
	}
	u, err := url.Parse(a.Repository)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.Path == "" || u.Path == "/" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(a.Repository) > 2048 || hasControl(a.Repository) {
		fields["repository"] = "Ingresa la URL HTTPS del repositorio, sin credenciales, parámetros ni fragmentos."
	}
	if a.Port < 1 || a.Port > 65535 {
		fields["port"] = "El puerto debe estar entre 1 y 65535."
	}
	if a.Environment != "production" && a.Environment != "staging" {
		fields["environment"] = "Selecciona production o staging."
	}
	if a.Owner == "" || utf8.RuneCountInString(a.Owner) > 120 || hasControl(a.Owner) {
		fields["owner"] = "Ingresa un responsable de hasta 120 caracteres, sin caracteres de control."
	}
	if utf8.RuneCountInString(a.Runtime) > 80 || hasControl(a.Runtime) {
		fields["runtime"] = "El runtime debe tener hasta 80 caracteres, sin caracteres de control."
	}
	return fields
}

var tagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
var ErrStale = errors.New("stale application version")
var ErrArchived = errors.New("application archived")

type Filter struct{ Environment, Query, State, Runtime, Tag, Owner string }

func (f *Filter) Validate() error {
	f.Query = strings.TrimSpace(f.Query)
	f.Owner = strings.TrimSpace(f.Owner)
	f.Tag = strings.ToLower(strings.TrimSpace(f.Tag))
	f.Runtime = strings.TrimSpace(f.Runtime)
	if f.State == "" {
		f.State = "active"
	}
	if (f.Environment != "" && f.Environment != "production" && f.Environment != "staging") || (f.State != "active" && f.State != "archived" && f.State != "all") || hasControl(f.Query) || hasControl(f.Owner) || hasControl(f.Runtime) || len(f.Query) > 200 || len(f.Owner) > 120 || len(f.Runtime) > 80 || (f.Tag != "" && !tagPattern.MatchString(f.Tag)) {
		return errors.New("Filtros inválidos.")
	}
	return nil
}
