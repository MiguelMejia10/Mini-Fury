package runtime

import (
	"archive/zip"
	"bytes"
	"embed"
	"errors"
	"io/fs"
	"mini-operador-ti/platform-api/internal/identity"
	"mini-operador-ti/platform-api/internal/platform"
	"net/http"
	"strings"
)

//go:embed starters
var starters embed.FS

const templateVersion = "1.0.0"

func templateRuntime(id string) string {
	if id == "go-http" {
		return "Go"
	}
	if id == "node-http" {
		return "Node.js"
	}
	return ""
}
func (h *HTTP) templates(w http.ResponseWriter, r *http.Request) {
	output(w, 200, map[string]any{"templates": []map[string]string{{"id": "go-http", "name": "Go HTTP instrumentado", "version": templateVersion, "runtime": "Go"}, {"id": "node-http", "name": "Node HTTP instrumentado", "version": templateVersion, "runtime": "Node.js"}}})
}
func (h *HTTP) scaffold(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("template")
	runtime := templateRuntime(id)
	if runtime == "" {
		problem(w, ErrInvalid)
		return
	}
	var in struct {
		Name       string  `json:"name"`
		Repository string  `json:"repository"`
		Owner      string  `json:"owner"`
		TeamID     *string `json:"teamId"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !namePattern.MatchString(in.Name) {
		problem(w, ErrInvalid)
		return
	}
	app := platform.CreateApplication{Name: in.Name, Repository: in.Repository, Owner: in.Owner, TeamID: in.TeamID, Runtime: runtime, Environment: "staging", Port: 8080, HealthCheckPath: "/health", Documentation: "Aplicación creada con plantilla " + id + " v" + templateVersion + ". Endpoints: /health y /mini-fury/metrics. Descarga el código, ejecútalo y conecta su repositorio antes de desplegar."}
	if len(app.Validate()) > 0 {
		problem(w, ErrInvalid)
		return
	}
	store := &platform.PostgresStore{DB: h.DB}
	created, e := store.CreateFromTemplate(r.Context(), app, id, templateVersion)
	if e != nil {
		if errors.Is(e, platform.ErrConflict) {
			e = ErrConflict
		}
		if errors.Is(e, platform.ErrPermission) || errors.Is(e, identity.ErrUnauthorized) {
			e = ErrDenied
		}
		problem(w, e)
		return
	}
	output(w, 201, map[string]any{"application": created, "templateId": id, "templateVersion": templateVersion, "downloadUrl": "/api/applications/" + created.ID + "/scaffold", "repositoryCreated": false})
}
func templateArchive(id, name string) ([]byte, error) {
	runtime := templateRuntime(id)
	if runtime == "" || !namePattern.MatchString(name) {
		return nil, ErrInvalid
	}
	directory := "starters/go"
	if id == "node-http" {
		directory = "starters/node"
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	add := func(path string, data []byte) error {
		file, e := writer.Create(name + "/" + path)
		if e != nil {
			return e
		}
		_, e = file.Write(bytes.ReplaceAll(data, []byte("__APP_NAME__"), []byte(name)))
		return e
	}
	e := fs.WalkDir(starters, directory, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		b, e := starters.ReadFile(path)
		if e != nil {
			return e
		}
		return add(strings.TrimSuffix(strings.TrimPrefix(path, directory+"/"), ".tmpl"), b)
	})
	if e != nil {
		return nil, e
	}
	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: __APP_NAME__
spec:
  replicas: 1
  selector:
    matchLabels: {app: __APP_NAME__}
  template:
    metadata:
      labels: {app: __APP_NAME__}
    spec:
      automountServiceAccountToken: false
      securityContext: {runAsNonRoot: true, runAsUser: 10001, seccompProfile: {type: RuntimeDefault}}
      containers:
        - name: application
          image: REPLACE_WITH_APPROVED_IMAGE_AT_SHA256_DIGEST
          ports: [{containerPort: 8080}]
          readinessProbe:
            httpGet: {path: /health, port: 8080}
          resources:
            requests: {cpu: 100m, memory: 64Mi}
            limits: {cpu: 250m, memory: 128Mi}
          securityContext: {allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: {drop: [ALL]}}
`
	workflow := `name: Verify starter
on: [push, pull_request]
jobs:
  verify:
    runs-on: ubuntu-24.04
    permissions: {contents: read}
    steps:
      - uses: actions/checkout@v4
      - run: docker build -t starter-verification .
`
	gitlab := "stages: [verify]\nverify:\n  stage: verify\n  tags: [docker]\n  script:\n    - docker build -t starter-verification .\n"
	readme := "# __APP_NAME__\n\nPlantilla " + id + " v" + templateVersion + ".\n\nPrueba: docker build -t __APP_NAME__ .\nEjecución local: docker run --rm -p 127.0.0.1:18081:8080 __APP_NAME__\n\n/health comprueba salud. /mini-fury/metrics expone contadores HTTP acumulativos reales (total, errores 5xx, latencia <=100/300/1000ms); excluye probes y consultas de métricas. /error devuelve 503 para ensayos locales. Protege este endpoint en tu red antes de publicar.\n\nCI GitHub incluida; GitLab requiere runner con etiqueta docker y acceso de build. No contiene credenciales ni publica imágenes. El manifiesto requiere sustituir la imagen por un digest autorizado. Mini Fury administra sus propios manifiestos al desplegar por scope.\n\nSube estos archivos al repositorio indicado y configura el agente/proveedor. Descargar la plantilla no crea un repositorio remoto ni despliega recursos.\n"
	for path, data := range map[string]string{"README.md": readme, ".dockerignore": ".git\nnode_modules\n", ".github/workflows/verify.yml": workflow, ".gitlab-ci.yml": gitlab, "kubernetes/deployment.yml": manifest, "mini-fury-template.json": "{\"templateId\":\"" + id + "\",\"version\":\"" + templateVersion + "\"}\n"} {
		if e = add(path, []byte(data)); e != nil {
			return nil, e
		}
	}
	if e = writer.Close(); e != nil {
		return nil, e
	}
	return buffer.Bytes(), nil
}
func (h *HTTP) scaffoldDownload(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	if !validID(w, app) {
		return
	}
	if _, e := appAccess(r.Context(), h.DB, app); e != nil {
		problem(w, e)
		return
	}
	var id, name string
	e := h.DB.QueryRowContext(r.Context(), `SELECT s.template_id,a.name FROM application_scaffolds s JOIN applications a ON a.id=s.application_id WHERE a.id=$1`, app).Scan(&id, &name)
	if e != nil {
		problem(w, ErrDenied)
		return
	}
	b, e := templateArchive(id, name)
	if e != nil {
		problem(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`-starter.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}
func (h *HTTP) templateStatus(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	if !validID(w, app) {
		return
	}
	if _, e := appAccess(r.Context(), h.DB, app); e != nil {
		problem(w, e)
		return
	}
	var id, version string
	e := h.DB.QueryRowContext(r.Context(), `SELECT template_id,template_version FROM application_scaffolds WHERE application_id=$1`, app).Scan(&id, &version)
	if e != nil {
		output(w, 200, map[string]any{"managed": false})
		return
	}
	output(w, 200, map[string]any{"managed": true, "templateId": id, "installedVersion": version, "latestVersion": templateVersion, "updateAvailable": version != templateVersion})
}
