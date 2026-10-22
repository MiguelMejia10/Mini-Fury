package platform

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}
type Dependency struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Environment string `json:"environment"`
}
type ReadinessCheck struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Passed bool   `json:"passed"`
}
type Readiness struct {
	Score  int              `json:"score"`
	Checks []ReadinessCheck `json:"checks"`
}

func secureURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && len(raw) <= 2048 && !hasControl(raw)
}
func (a *CreateApplication) qualityValidate(fields map[string]string) {
	if a.TeamID != nil && *a.TeamID == "" {
		a.TeamID = nil
	}
	if a.TeamID != nil && !idPattern.MatchString(*a.TeamID) {
		fields["teamId"] = "Equipo inválido."
	}
	if utf8.RuneCountInString(a.Documentation) > 20000 || strings.ContainsRune(a.Documentation, 0) {
		fields["documentation"] = "La documentación admite hasta 20000 caracteres."
	}
	if len(a.Links) > 10 {
		fields["links"] = "Máximo diez enlaces."
	}
	if a.Links == nil {
		a.Links = []Link{}
	}
	for i := range a.Links {
		a.Links[i].Label = strings.TrimSpace(a.Links[i].Label)
		a.Links[i].URL = strings.TrimSpace(a.Links[i].URL)
		if a.Links[i].Label == "" || utf8.RuneCountInString(a.Links[i].Label) > 80 || hasControl(a.Links[i].Label) || !secureURL(a.Links[i].URL) {
			fields["links"] = "Los enlaces requieren un título y URL HTTPS sin credenciales."
		}
	}
	path := a.HealthCheckPath
	if path != "" {
		u, e := url.Parse(path)
		if e != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || len(path) > 200 || hasControl(path) || u.IsAbs() || u.RawQuery != "" || u.Fragment != "" || strings.Contains(path, "..") {
			fields["healthCheckPath"] = "Usa una ruta local como /health, sin parámetros."
		}
	}
	if len(a.DependencyIDs) > 20 {
		fields["dependencyIds"] = "Máximo veinte dependencias."
	}
	seen := map[string]bool{}
	for _, id := range a.DependencyIDs {
		if !idPattern.MatchString(id) || seen[id] {
			fields["dependencyIds"] = "Dependencias inválidas o duplicadas."
		}
		seen[id] = true
	}
}
func (a *Application) readiness() {
	checks := []ReadinessCheck{{"owner", "Responsable definido", a.Owner != ""}, {"repository", "Repositorio configurado", a.Repository != ""}, {"description", "Descripción disponible", a.Description != ""}, {"documentation", "Documentación disponible", strings.TrimSpace(a.Documentation) != ""}, {"health", "Ruta de health check configurada", a.HealthCheckPath != ""}, {"team", "Equipo responsable asignado", a.TeamID != nil}}
	score := 0
	for _, c := range checks {
		if c.Passed {
			score++
		}
	}
	a.Readiness = Readiness{Score: score * 100 / len(checks), Checks: checks}
}
