package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"mini-operador-ti/platform-api/internal/identity"
	"net/http"
	"time"
)

type ManagedService struct {
	ID           string    `json:"id"`
	ScopeID      string    `json:"scopeId"`
	Name         string    `json:"name"`
	Profile      string    `json:"profile"`
	State        string    `json:"state"`
	StorageGiB   int       `json:"storageGiB"`
	BackupPolicy string    `json:"backupPolicy"`
	Version      int64     `json:"version"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

const serviceColumns = `id::text,scope_id::text,name,profile,state,storage_gib,backup_policy,version,created_at,updated_at`

func scanService(row scanner) (ManagedService, error) {
	var s ManagedService
	e := row.Scan(&s.ID, &s.ScopeID, &s.Name, &s.Profile, &s.State, &s.StorageGiB, &s.BackupPolicy, &s.Version, &s.CreatedAt, &s.UpdatedAt)
	return s, e
}
func (h *HTTP) profile(id string) (ServiceProfile, bool) {
	for _, p := range h.Config.ServiceProfiles {
		if p.ID == id {
			return p, true
		}
	}
	return ServiceProfile{}, false
}
func (h *HTTP) serviceProfiles(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("scope")) {
		return
	}
	s, e := h.scope(r)
	if e != nil {
		problem(w, e)
		return
	}
	items := []map[string]any{}
	_, b, ok := h.Config.binding(s.ApplicationID, s.ClusterID, s.Namespace)
	if ok {
		for _, p := range h.Config.ServiceProfiles {
			if _, ok := b.Secrets[p.SecretAlias]; ok {
				items = append(items, map[string]any{"id": p.ID, "name": p.Name, "engine": p.Engine, "cpuMilli": p.CPUMilli, "memoryMiB": p.MemoryMiB, "maxStorageGiB": 10, "backupPolicy": "development-only", "productionEnabled": false})
			}
		}
	}
	output(w, 200, map[string]any{"profiles": items, "canProvision": s.CanEdit && s.Environment != "production", "maxPerScope": 2})
}
func (h *HTTP) services(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("scope")) {
		return
	}
	s, e := h.scope(r)
	if e != nil {
		problem(w, e)
		return
	}
	rows, e := h.DB.QueryContext(r.Context(), `SELECT `+serviceColumns+` FROM managed_services WHERE scope_id=$1 ORDER BY created_at DESC LIMIT 20`, s.ID)
	if e != nil {
		problem(w, e)
		return
	}
	defer rows.Close()
	items := []ManagedService{}
	for rows.Next() {
		v, e := scanService(rows)
		if e != nil {
			problem(w, e)
			return
		}
		items = append(items, v)
	}
	if e = rows.Err(); e != nil {
		problem(w, e)
		return
	}
	output(w, 200, map[string]any{"services": items, "canEdit": s.CanEdit})
}
func (h *HTTP) requestService(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("scope")) {
		return
	}
	scope, e := h.scope(r)
	if e != nil {
		problem(w, e)
		return
	}
	var in struct {
		Name         string `json:"name"`
		Profile      string `json:"profile"`
		StorageGiB   int    `json:"storageGiB"`
		BackupPolicy string `json:"backupPolicy"`
	}
	if !decode(w, r, &in) {
		return
	}
	p, ok := h.profile(in.Profile)
	_, binding, bound := h.Config.binding(scope.ApplicationID, scope.ClusterID, scope.Namespace)
	_, hasSecret := binding.Secrets[p.SecretAlias]
	if !ok || !bound || !hasSecret || scope.Environment == "production" || !namePattern.MatchString(in.Name) || in.StorageGiB < 1 || in.StorageGiB > 10 || in.BackupPolicy != "development-only" {
		problem(w, ErrInvalid)
		return
	}
	tx, e := h.appTx(r.Context(), scope.ApplicationID, false)
	if e != nil {
		problem(w, e)
		return
	}
	defer tx.Rollback()
	var locked string
	if e = tx.QueryRowContext(r.Context(), `SELECT id::text FROM runtime_scopes WHERE id=$1 FOR UPDATE`, scope.ID).Scan(&locked); e != nil {
		problem(w, e)
		return
	}
	var existing ManagedService
	existing, e = scanService(tx.QueryRowContext(r.Context(), `SELECT `+serviceColumns+` FROM managed_services WHERE scope_id=$1 AND name=$2`, scope.ID, in.Name))
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		problem(w, e)
		return
	}
	if e == nil {
		if existing.Profile != in.Profile || existing.StorageGiB != in.StorageGiB || existing.BackupPolicy != in.BackupPolicy {
			problem(w, ErrConflict)
			return
		}
	} else {
		var count int
		if e = tx.QueryRowContext(r.Context(), `SELECT count(*) FROM managed_services WHERE scope_id=$1`, scope.ID).Scan(&count); e != nil {
			problem(w, e)
			return
		}
		if count >= 2 {
			problem(w, ErrConflict)
			return
		}
		actor, _ := identity.CurrentUser(r.Context())
		existing, e = scanService(tx.QueryRowContext(r.Context(), `INSERT INTO managed_services(scope_id,name,profile,storage_gib,backup_policy,created_by)VALUES($1,$2,$3,$4,$5,$6)RETURNING `+serviceColumns, scope.ID, in.Name, in.Profile, in.StorageGiB, in.BackupPolicy, actor.ID))
		if e != nil {
			problem(w, conflictError(e))
			return
		}
	}
	j, e := h.enqueueTx(r.Context(), tx, scope.ApplicationID, &scope, "service-provision", r.Header.Get("Idempotency-Key"), JobPayload{ServiceID: existing.ID}, nil)
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		problem(w, e)
		return
	}
	output(w, 202, map[string]any{"service": existing, "job": j})
}
func (h *HTTP) service(r *http.Request) (ManagedService, Scope, error) {
	v, e := scanService(h.DB.QueryRowContext(r.Context(), `SELECT `+serviceColumns+` FROM managed_services WHERE id=$1`, r.PathValue("service")))
	if e != nil {
		return v, Scope{}, ErrDenied
	}
	r.SetPathValue("scope", v.ScopeID)
	s, e := h.scope(r)
	return v, s, e
}
func (h *HTTP) serviceAction(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("service")) {
		return
	}
	v, s, e := h.service(r)
	if e != nil {
		problem(w, e)
		return
	}
	var in struct {
		Action  string `json:"action"`
		Version int64  `json:"version"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Action != "suspend" && in.Action != "resume" {
		problem(w, ErrInvalid)
		return
	}
	if v.Version != in.Version {
		problem(w, ErrConflict)
		return
	}
	if s.Environment == "production" {
		problem(w, ErrInvalid)
		return
	}
	tx, e := h.appTx(r.Context(), s.ApplicationID, false)
	if e != nil {
		problem(w, e)
		return
	}
	defer tx.Rollback()
	var scopeVersion int64
	if e = tx.QueryRowContext(r.Context(), `SELECT version FROM runtime_scopes WHERE id=$1 FOR UPDATE`, s.ID).Scan(&scopeVersion); e != nil {
		problem(w, e)
		return
	}
	if scopeVersion != s.Version {
		problem(w, ErrConflict)
		return
	}
	var serviceVersion int64
	if e = tx.QueryRowContext(r.Context(), `SELECT version FROM managed_services WHERE id=$1 FOR UPDATE`, v.ID).Scan(&serviceVersion); e != nil {
		problem(w, e)
		return
	}
	if serviceVersion != in.Version {
		problem(w, ErrConflict)
		return
	}
	var pending bool
	if e = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM runtime_jobs WHERE scope_id=$1 AND payload->>'serviceId'=$2 AND state IN ('queued','running'))`, s.ID, v.ID).Scan(&pending); e != nil || pending {
		problem(w, ErrConflict)
		return
	}
	j, e := h.enqueueTx(r.Context(), tx, s.ApplicationID, &s, "service-"+in.Action, r.Header.Get("Idempotency-Key"), JobPayload{ServiceID: v.ID}, nil)
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		problem(w, e)
		return
	}
	output(w, 202, j)
}

type StatefulSet struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		Replicas int `json:"replicas"`
	} `json:"spec"`
	Status struct {
		ObservedGeneration int64  `json:"observedGeneration"`
		Replicas           int    `json:"replicas"`
		ReadyReplicas      int    `json:"readyReplicas"`
		CurrentRevision    string `json:"currentRevision"`
		UpdateRevision     string `json:"updateRevision"`
	} `json:"status"`
}

func servicePath(s Scope, v ManagedService) string {
	return "/apis/apps/v1/namespaces/" + s.Namespace + "/statefulsets/mfs-" + v.ID
}
func managedOwned(meta ObjectMeta, s Scope, v ManagedService) bool {
	return owned(meta, s) && meta.Labels["mini-fury.io/service"] == v.ID
}
func (h *HTTP) serviceStatus(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r.PathValue("service")) {
		return
	}
	v, s, e := h.service(r)
	if e != nil {
		problem(w, e)
		return
	}
	release, ok := observationPermit(r.Context())
	if !ok {
		output(w, 429, map[string]string{"message": "Consulta ocupada."})
		return
	}
	defer release()
	k, e := h.kube(s)
	if e != nil {
		problem(w, e)
		return
	}
	var state StatefulSet
	e = k.request(r.Context(), "GET", servicePath(s, v), nil, nil, &state)
	if e != nil {
		problem(w, e)
		return
	}
	if !managedOwned(state.Metadata, s, v) {
		problem(w, ErrDenied)
		return
	}
	port := 5432
	if p, _ := h.profile(v.Profile); p.Engine == "redis" {
		port = 6379
	}
	output(w, 200, map[string]any{"service": v, "desiredReplicas": state.Spec.Replicas, "readyReplicas": state.Status.ReadyReplicas, "endpoint": fmt.Sprintf("mfs-%s.%s.svc.cluster.local:%d", v.ID, s.Namespace, port), "dataRetained": true, "backupPolicy": v.BackupPolicy, "receivedAt": time.Now()})
}
func (h *HTTP) executeService(ctx context.Context, s Scope, j claimed) error {
	if s.Environment == "production" {
		return ErrDenied
	}
	v, e := scanService(h.DB.QueryRowContext(ctx, `SELECT `+serviceColumns+` FROM managed_services WHERE id=$1 AND scope_id=$2`, j.Payload.ServiceID, s.ID))
	if e != nil {
		return e
	}
	p, ok := h.profile(v.Profile)
	_, binding, bound := h.Config.binding(s.ApplicationID, s.ClusterID, s.Namespace)
	secret, has := binding.Secrets[p.SecretAlias]
	if !ok || !bound || !has || !imagePattern.MatchString(p.Image) {
		return ErrDenied
	}
	k, e := h.kube(s)
	if e != nil {
		return e
	}
	desired := 1
	if j.Kind == "service-suspend" {
		desired = 0
	}
	if e = h.phase(ctx, j, "applying", "Reconciliando servicio aprobado; los volúmenes se conservan"); e != nil {
		return e
	}
	if e = h.alive(ctx, j); e != nil {
		return e
	}
	labels := map[string]string{"mini-fury.io/application": s.ApplicationID, "mini-fury.io/scope": s.ID, "mini-fury.io/service": v.ID}
	name := "mfs-" + v.ID
	port := 5432
	uid := int64(70)
	mount := "/var/lib/postgresql/data"
	args := []string{}
	env := []any{}
	volumes := []any{}
	volumeMounts := []any{}
	if p.Engine == "postgres" {
		env = []any{map[string]any{"name": "POSTGRES_DB", "value": "mini_fury"}, map[string]any{"name": "POSTGRES_USER", "value": "mini_fury"}, map[string]any{"name": "PGDATA", "value": "/var/lib/postgresql/data/pgdata"}, map[string]any{"name": "POSTGRES_PASSWORD", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": secret.Name, "key": secret.Key}}}}
	} else {
		port = 6379
		uid = 999
		mount = "/data"
		args = []string{"redis-server", "/etc/redis/redis.conf", "--appendonly", "yes"}
		volumes = append(volumes, map[string]any{"name": "configuration", "secret": map[string]any{"secretName": secret.Name, "defaultMode": 0444, "items": []any{map[string]any{"key": secret.Key, "path": "redis.conf"}}}})
		volumeMounts = append(volumeMounts, map[string]any{"name": "configuration", "mountPath": "/etc/redis", "readOnly": true})
	}
	volumeMounts = append(volumeMounts, map[string]any{"name": "data", "mountPath": mount}, map[string]any{"name": "tmp", "mountPath": "/tmp"})
	volumes = append(volumes, map[string]any{"name": "tmp", "emptyDir": map[string]any{"sizeLimit": "64Mi"}})
	container := map[string]any{"name": "service", "image": p.Image, "env": env, "ports": []any{map[string]any{"containerPort": port}}, "volumeMounts": volumeMounts, "resources": map[string]any{"requests": map[string]string{"cpu": fmt.Sprint(p.CPUMilli/2) + "m", "memory": fmt.Sprint(p.MemoryMiB/2) + "Mi"}, "limits": map[string]string{"cpu": fmt.Sprint(p.CPUMilli) + "m", "memory": fmt.Sprint(p.MemoryMiB) + "Mi"}}, "readinessProbe": map[string]any{"tcpSocket": map[string]int{"port": port}, "periodSeconds": 3}, "securityContext": map[string]any{"allowPrivilegeEscalation": false, "readOnlyRootFilesystem": true, "capabilities": map[string]any{"drop": []string{"ALL"}}}}
	if len(args) > 0 {
		container["command"] = args
	}
	if p.Engine == "postgres" {
		volumeMounts = append(volumeMounts, map[string]any{"name": "socket", "mountPath": "/var/run/postgresql"})
		container["volumeMounts"] = volumeMounts
		volumes = append(volumes, map[string]any{"name": "socket", "emptyDir": map[string]any{"sizeLimit": "16Mi"}})
	}
	pvcSpec := map[string]any{"accessModes": []string{"ReadWriteOnce"}, "resources": map[string]any{"requests": map[string]string{"storage": fmt.Sprint(v.StorageGiB) + "Gi"}}}
	if p.StorageClass != "" {
		pvcSpec["storageClassName"] = p.StorageClass
	}
	body := map[string]any{"apiVersion": "apps/v1", "kind": "StatefulSet", "metadata": map[string]any{"name": name, "namespace": s.Namespace, "labels": labels, "annotations": map[string]string{"mini-fury.io/job": j.ID}}, "spec": map[string]any{"serviceName": name, "replicas": desired, "selector": map[string]any{"matchLabels": labels}, "persistentVolumeClaimRetentionPolicy": map[string]string{"whenDeleted": "Retain", "whenScaled": "Retain"}, "template": map[string]any{"metadata": map[string]any{"labels": labels}, "spec": map[string]any{"automountServiceAccountToken": false, "securityContext": map[string]any{"runAsNonRoot": true, "runAsUser": uid, "fsGroup": uid, "seccompProfile": map[string]string{"type": "RuntimeDefault"}}, "containers": []any{container}, "volumes": volumes}}, "volumeClaimTemplates": []any{map[string]any{"metadata": map[string]any{"name": "data", "labels": labels}, "spec": pvcSpec}}}}
	var current StatefulSet
	e = k.request(ctx, "GET", servicePath(s, v), nil, nil, &current)
	method, path := "PUT", servicePath(s, v)
	if e != nil {
		var ke KubeError
		if !errors.As(e, &ke) || ke.Status != 404 {
			return e
		}
		if desired == 0 {
			return ErrConflict
		}
		method = "POST"
		path = "/apis/apps/v1/namespaces/" + s.Namespace + "/statefulsets"
	} else {
		if !managedOwned(current.Metadata, s, v) {
			return ErrDenied
		}
		body["metadata"].(map[string]any)["resourceVersion"] = current.Metadata.ResourceVersion
	}
	if e = k.request(ctx, method, path, nil, body, nil); e != nil {
		return e
	}
	// Service ownership is checked independently. No public ports or LoadBalancer are created.
	sp := "/api/v1/namespaces/" + s.Namespace + "/services/" + name
	var existing struct {
		Metadata ObjectMeta     `json:"metadata"`
		Spec     map[string]any `json:"spec"`
	}
	e = k.request(ctx, "GET", sp, nil, nil, &existing)
	meta := map[string]any{"name": name, "namespace": s.Namespace, "labels": labels}
	spec := map[string]any{"clusterIP": "None", "type": "ClusterIP", "selector": labels, "ports": []any{map[string]any{"name": "service", "port": port, "targetPort": port}}}
	method = "PUT"
	if e != nil {
		var ke KubeError
		if !errors.As(e, &ke) || ke.Status != 404 {
			return e
		}
		method = "POST"
		sp = "/api/v1/namespaces/" + s.Namespace + "/services"
	} else {
		if !managedOwned(existing.Metadata, s, v) {
			return ErrDenied
		}
		meta["resourceVersion"] = existing.Metadata.ResourceVersion
	}
	if e = k.request(ctx, method, sp, nil, map[string]any{"apiVersion": "v1", "kind": "Service", "metadata": meta, "spec": spec}, nil); e != nil {
		return e
	}
	if e = h.phase(ctx, j, "progressing", "Esperando estado del StatefulSet y volumen persistente"); e != nil {
		return e
	}
	deadline := time.NewTimer(150 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if e = h.alive(ctx, j); e != nil {
			return e
		}
		var observed StatefulSet
		if e = k.request(ctx, "GET", servicePath(s, v), nil, nil, &observed); e != nil {
			return e
		}
		if !managedOwned(observed.Metadata, s, v) {
			return ErrDenied
		}
		if observed.Metadata.Annotations["mini-fury.io/job"] != j.ID {
			return ErrConflict
		}
		if observed.Status.ObservedGeneration >= observed.Metadata.Generation && observed.Status.Replicas == desired && observed.Status.ReadyReplicas == desired {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("service readiness timeout")
		case <-ticker.C:
		}
	}
}
