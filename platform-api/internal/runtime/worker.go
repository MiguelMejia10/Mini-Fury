package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"mini-operador-ti/platform-api/internal/identity"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type claimed struct {
	Job
	Payload JobPayload
	Actor   identity.User
	Owner   string
}

func (h *HTTP) claim(ctx context.Context, agent string) (claimed, bool, error) {
	var out claimed
	tx, e := h.DB.BeginTx(ctx, nil)
	if e != nil {
		return out, false, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `WITH expired AS (UPDATE runtime_jobs SET state='cancelled',phase='cancelled',message='Cancelación recuperada tras vencer el lease',completed_at=now(),lease_until=NULL WHERE state='running' AND cancel_requested AND lease_until<now() RETURNING id) INSERT INTO runtime_job_events(job_id,state,phase,message) SELECT id,'cancelled','cancelled','Cancelación recuperada tras vencer el lease' FROM expired`); e != nil {
		return out, false, e
	}
	// A scope has one running operation. Reclaim the same expired operation before advancing its queue.
	filter := `kind<>'build' OR payload->>'provider'<>'local'`
	if agent != "" {
		filter = `kind='build' AND payload->>'provider'='local'`
	}
	apps := []string{}
	for _, repo := range h.Config.Repositories {
		if repo.AgentID == agent && repo.Provider == "local" {
			apps = append(apps, repo.ApplicationID)
		}
	}
	appJSON, _ := json.Marshal(apps)
	rows, e := tx.QueryContext(ctx, `SELECT id::text,application_id::text,scope_id::text,requested_by::text,payload,kind FROM runtime_jobs candidate WHERE (`+filter+`) AND ($1 OR application_id::text IN(SELECT jsonb_array_elements_text($2::jsonb))) AND NOT EXISTS(SELECT 1 FROM runtime_jobs active WHERE active.state='running' AND active.id<>candidate.id AND ((candidate.scope_id IS NOT NULL AND active.scope_id=candidate.scope_id) OR (candidate.kind='build' AND active.kind='build' AND active.application_id=candidate.application_id))) AND cancel_requested=false AND (state='queued' OR (state='running' AND lease_until<now())) ORDER BY created_at,id LIMIT 50`, agent == "", appJSON)
	if e != nil {
		return out, false, e
	}
	type candidate struct {
		id, app, actor, kind string
		scope                sql.NullString
		payload              []byte
	}
	items := []candidate{}
	for rows.Next() {
		var c candidate
		if e = rows.Scan(&c.id, &c.app, &c.scope, &c.actor, &c.payload, &c.kind); e != nil {
			rows.Close()
			return out, false, e
		}
		items = append(items, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, false, e
	}
	for _, c := range items {
		if agent != "" {
			repo, ok := h.Config.repository(c.app)
			if !ok || repo.AgentID != agent || repo.Provider != "local" {
				continue
			}
		}
		if c.scope.Valid {
			var lock string
			e = tx.QueryRowContext(ctx, `SELECT id::text FROM runtime_scopes WHERE id=$1 FOR UPDATE SKIP LOCKED`, c.scope.String).Scan(&lock)
			if errors.Is(e, sql.ErrNoRows) {
				continue
			}
			if e != nil {
				return out, false, e
			}
			var busy bool
			if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runtime_jobs WHERE scope_id=$1 AND state='running' AND id<>$2)`, c.scope.String, c.id).Scan(&busy); e != nil {
				return out, false, e
			}
			if busy {
				continue
			}
		}
		if c.kind == "build" {
			var locked bool
			if e = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,748224))`, c.app).Scan(&locked); e != nil {
				return out, false, e
			}
			if !locked {
				continue
			}
			var busy bool
			if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runtime_jobs WHERE application_id=$1 AND kind='build' AND state='running' AND id<>$2)`, c.app, c.id).Scan(&busy); e != nil {
				return out, false, e
			}
			if busy {
				continue
			}
		}
		token, e := identity.Token()
		if e != nil {
			return out, false, e
		}
		row := tx.QueryRowContext(ctx, `UPDATE runtime_jobs j SET state='running',phase=CASE WHEN state='queued' THEN 'starting' ELSE phase END,attempt=attempt+1,owner_token=$2,lease_until=now()+interval '30 seconds',started_at=COALESCE(started_at,now()) WHERE id=$1 AND NOT cancel_requested AND (state='queued' OR (state='running' AND lease_until<now())) RETURNING `+jobColumns, c.id, token)
		out.Job, e = scanJob(row)
		if errors.Is(e, sql.ErrNoRows) {
			continue
		}
		if e != nil {
			return out, false, e
		}
		if json.Unmarshal(c.payload, &out.Payload) != nil {
			return out, false, ErrInvalid
		}
		out.Owner = token
		if e = tx.QueryRowContext(ctx, `SELECT id::text,email,name,role,active FROM users WHERE id=$1`, c.actor).Scan(&out.Actor.ID, &out.Actor.Email, &out.Actor.Name, &out.Actor.Role, &out.Actor.Active); e != nil {
			return out, false, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO runtime_job_events(job_id,state,phase,message)VALUES($1,'running','starting','Worker inició o recuperó la operación')`, c.id); e != nil {
			return out, false, e
		}
		return out, true, tx.Commit()
	}
	return out, false, nil
}

var errCancelled = errors.New("operation cancelled")

func (h *HTTP) alive(ctx context.Context, j claimed) error {
	var cancel bool
	e := h.DB.QueryRowContext(ctx, `UPDATE runtime_jobs SET lease_until=now()+interval '30 seconds' WHERE id=$1 AND state='running' AND owner_token=$2 RETURNING cancel_requested`, j.ID, j.Owner).Scan(&cancel)
	if errors.Is(e, sql.ErrNoRows) || cancel {
		return errCancelled
	}
	return e
}
func (h *HTTP) phase(ctx context.Context, j claimed, phase, message string) error {
	tx, e := h.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(ctx, `UPDATE runtime_jobs SET phase=$3,message=$4 WHERE id=$1 AND owner_token=$2 AND state='running'`, j.ID, j.Owner, phase, message)
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return errCancelled
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO runtime_job_events(job_id,state,phase,message) VALUES($1,'running',$2,$3)`, j.ID, phase, message)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (h *HTTP) finish(ctx context.Context, j claimed, state, phase, message string) error {
	tx, e := h.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if j.ScopeID != nil {
		var locked string
		if e = tx.QueryRowContext(ctx, `SELECT id::text FROM runtime_scopes WHERE id=$1 FOR UPDATE`, *j.ScopeID).Scan(&locked); e != nil {
			return e
		}
	}
	result, e := tx.ExecContext(ctx, `UPDATE runtime_jobs SET state=$3,phase=$4,message=$5,completed_at=now(),lease_until=NULL WHERE id=$1 AND owner_token=$2 AND state='running'`, j.ID, j.Owner, state, phase, message)
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return nil
	}
	if state == "succeeded" && j.ScopeID != nil && !strings.HasPrefix(j.Kind, "service-") {
		if _, e = tx.ExecContext(ctx, `UPDATE runtime_scopes SET replicas=$2,version=version+1,updated_at=now() WHERE id=$1`, *j.ScopeID, j.Payload.Replicas); e != nil {
			return e
		}
	}
	if strings.HasPrefix(j.Kind, "service-") {
		serviceState := "failed"
		if state == "succeeded" {
			serviceState = "ready"
			if j.Kind == "service-suspend" {
				serviceState = "suspended"
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE managed_services SET state=$2,version=version+1,updated_at=now() WHERE id=$1 AND scope_id=$3`, j.Payload.ServiceID, serviceState, j.ScopeID); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO runtime_job_events(job_id,state,phase,message) VALUES($1,$2,$3,$4)`, j.ID, state, phase, message); e != nil {
		return e
	}
	actorCtx := identity.WithUser(ctx, j.Actor)
	if e = event(actorCtx, tx, "runtime."+state, j.ID); e != nil {
		return e
	}
	return tx.Commit()
}
func (h *HTTP) Run(ctx context.Context, onError func(error)) {
	for range 2 {
		h.workers.Add(1)
		go func() {
			defer h.workers.Done()
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					j, worked, e := h.claim(ctx, "")
					if e != nil {
						onError(e)
						continue
					}
					if !worked {
						continue
					}
					run, cancel := context.WithTimeout(identity.WithUser(ctx, j.Actor), 5*time.Minute)
					e = h.execute(run, j)
					cancel()
					if ctx.Err() != nil {
						return
					}
					state, phase, message := "succeeded", "completed", "Operación completada y estado comprobado"
					if e != nil {
						state, phase, message = "failed", "failed", "La operación falló. Consulta el estado del workload y los eventos autorizados."
					}
					if errors.Is(e, errCancelled) {
						state, phase, message = "cancelled", "cancelled", "Seguimiento cancelado. Los cambios ya aplicados pueden permanecer."
					}
					if errors.Is(e, errDispatchUnknown) {
						state, phase, message = "failed", "dispatch_unknown", "No se pudo confirmar el envío al proveedor. Revisa el pipeline antes de solicitar otro build."
					}
					if j.Kind == "build" && e == nil {
						continue
					}
					finishCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
					if e = h.finish(finishCtx, j, state, phase, message); e != nil {
						onError(e)
					}
					c()
				}
			}
		}()
	}
}
func (h *HTTP) execute(ctx context.Context, j claimed) error {
	tx, e := h.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	production := false
	var s Scope
	if j.ScopeID != nil {
		s, e = scanScope(tx.QueryRowContext(ctx, `SELECT `+scopeColumns+` FROM runtime_scopes s WHERE s.id=$1`, *j.ScopeID))
		if e != nil {
			tx.Rollback()
			return e
		}
		production = s.Environment == "production"
	}
	if j.StartedAt != nil && time.Since(*j.StartedAt) > 10*time.Minute {
		tx.Rollback()
		return errors.New("job deadline exceeded")
	}
	e = authorize(ctx, tx, j.ApplicationID, production)
	tx.Rollback()
	if e != nil {
		return e
	}
	if !j.Actor.Active {
		return ErrDenied
	}
	if e = h.alive(ctx, j); e != nil {
		return e
	}
	if strings.HasPrefix(j.Kind, "service-") {
		return h.executeService(ctx, s, j)
	}
	if j.Kind == "build" {
		return h.remoteBuild(ctx, j)
	}
	k, e := h.kube(s)
	if e != nil {
		return e
	}
	repo, ok := h.Config.repository(s.ApplicationID)
	if !ok || !allowedImage(repo, j.Payload.Image) {
		return ErrDenied
	}
	configuration, e := h.loadConfiguration(ctx, s, j.Payload.ConfigurationID)
	if e != nil {
		return e
	}
	_, binding, ok := h.Config.binding(s.ApplicationID, s.ClusterID, s.Namespace)
	if !ok || validateConfiguration(configuration, binding) != nil {
		return ErrDenied
	}
	if e = h.phase(ctx, j, "applying", "Aplicando imagen inmutable, recursos y configuración del snapshot"); e != nil {
		return e
	}
	if e = h.alive(ctx, j); e != nil {
		return e
	}
	if e = h.apply(ctx, k, s, j, configuration, binding); e != nil {
		return e
	}
	if e = h.phase(ctx, j, "progressing", "Esperando réplicas listas del workload"); e != nil {
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
		d, e := k.deployment(ctx, s)
		if e != nil {
			return e
		}
		var spec struct {
			Replicas int `json:"replicas"`
			Template struct {
				Metadata ObjectMeta `json:"metadata"`
				Spec     struct {
					Containers []struct {
						Image string `json:"image"`
					} `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		}
		if json.Unmarshal(d.Spec, &spec) != nil {
			return ErrInvalid
		}
		if d.Metadata.Annotations["mini-fury.io/job"] != j.ID {
			return ErrConflict
		}
		if rolloutStatusReady(d, j.Payload.Replicas) && len(spec.Template.Spec.Containers) > 0 && spec.Template.Spec.Containers[0].Image == j.Payload.Image {
			return nil
		}
		if rolloutProgressFailed(d) {
			return errors.New("rollout did not progress")
		}
		pods, _, e := k.pods(ctx, s)
		if e == nil {
			for _, p := range pods {
				currentImage := false
				for _, container := range p.Spec.Containers {
					if container.Image == j.Payload.Image {
						currentImage = true
					}
				}
				if !currentImage {
					continue
				}
				for _, c := range p.Status.ContainerStatuses {
					var state struct {
						Waiting *struct {
							Reason string `json:"reason"`
						} `json:"waiting"`
					}
					if json.Unmarshal(c.State, &state) == nil && state.Waiting != nil && (state.Waiting.Reason == "ImagePullBackOff" || state.Waiting.Reason == "CreateContainerConfigError") {
						return errors.New("pod unavailable")
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("rollout timed out")
		case <-ticker.C:
		}
	}
}
func (h *HTTP) apply(ctx context.Context, k *Kube, s Scope, j claimed, c Configuration, b Binding) error {
	var port int
	var health string
	if e := h.DB.QueryRowContext(ctx, `SELECT port,health_check_path FROM applications WHERE id=$1`, s.ApplicationID).Scan(&port, &health); e != nil {
		return e
	}
	labels := map[string]string{"mini-fury.io/application": s.ApplicationID, "mini-fury.io/scope": s.ID}
	annotations := map[string]string{"mini-fury.io/job": j.ID, "mini-fury.io/configuration": c.ID}
	env := []any{}
	keys := []string{}
	for key := range c.Variables {
		keys = append(keys, key)
	}
	for key := range c.SecretRefs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if value, ok := c.Variables[key]; ok {
			env = append(env, map[string]any{"name": key, "value": value})
		} else {
			ref := b.Secrets[c.SecretRefs[key]]
			env = append(env, map[string]any{"name": key, "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": ref.Name, "key": ref.Key, "optional": false}}})
		}
	}
	container := map[string]any{"name": "application", "image": j.Payload.Image, "imagePullPolicy": "IfNotPresent", "ports": []any{map[string]any{"name": "http", "containerPort": port}}, "env": env, "resources": map[string]any{"requests": map[string]string{"cpu": fmt.Sprint(j.Payload.CPUMilli/2) + "m", "memory": fmt.Sprint(j.Payload.MemoryMiB/2) + "Mi"}, "limits": map[string]string{"cpu": fmt.Sprint(j.Payload.CPUMilli) + "m", "memory": fmt.Sprint(j.Payload.MemoryMiB) + "Mi"}}, "securityContext": map[string]any{"allowPrivilegeEscalation": false, "readOnlyRootFilesystem": true, "capabilities": map[string]any{"drop": []string{"ALL"}}}}
	if health != "" {
		container["readinessProbe"] = map[string]any{"httpGet": map[string]any{"path": health, "port": "http"}, "periodSeconds": 3, "timeoutSeconds": 1, "failureThreshold": 3}
	}
	desired := map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": s.Workload, "namespace": s.Namespace, "labels": labels, "annotations": annotations}, "spec": map[string]any{"replicas": j.Payload.Replicas, "revisionHistoryLimit": 5, "progressDeadlineSeconds": 120, "strategy": map[string]any{"type": "RollingUpdate", "rollingUpdate": map[string]int{"maxUnavailable": 0, "maxSurge": 1}}, "selector": map[string]any{"matchLabels": labels}, "template": map[string]any{"metadata": map[string]any{"labels": labels, "annotations": annotations}, "spec": map[string]any{"automountServiceAccountToken": false, "securityContext": map[string]any{"runAsNonRoot": true, "runAsUser": 10001, "seccompProfile": map[string]string{"type": "RuntimeDefault"}}, "containers": []any{container}, "terminationGracePeriodSeconds": 20}}}}
	current, e := k.deployment(ctx, s)
	method, path := "PUT", deploymentPath(s)
	if e != nil {
		var ke KubeError
		if !errors.As(e, &ke) || ke.Status != http.StatusNotFound {
			return e
		}
		method = "POST"
		path = "/apis/apps/v1/namespaces/" + s.Namespace + "/deployments"
	} else {
		desired["metadata"].(map[string]any)["resourceVersion"] = current.Metadata.ResourceVersion
	}
	if e = k.request(ctx, method, path, nil, desired, nil); e != nil {
		return e
	}
	// Services are also owned and reconciled; never overwrite a foreign object with the same name.
	servicePath := "/api/v1/namespaces/" + s.Namespace + "/services/" + s.Workload
	var existing struct {
		Metadata ObjectMeta     `json:"metadata"`
		Spec     map[string]any `json:"spec"`
	}
	e = k.request(ctx, "GET", servicePath, nil, nil, &existing)
	serviceSpec := map[string]any{"selector": labels, "type": "ClusterIP", "ports": []any{map[string]any{"name": "http", "port": port, "targetPort": "http"}}}
	meta := map[string]any{"name": s.Workload, "namespace": s.Namespace, "labels": labels}
	method = "PUT"
	if e != nil {
		var ke KubeError
		if !errors.As(e, &ke) || ke.Status != 404 {
			return e
		}
		method = "POST"
		servicePath = "/api/v1/namespaces/" + s.Namespace + "/services"
	} else {
		if !owned(existing.Metadata, s) {
			return ErrDenied
		}
		meta["resourceVersion"] = existing.Metadata.ResourceVersion
		for _, key := range []string{"clusterIP", "clusterIPs", "ipFamilies", "ipFamilyPolicy"} {
			if v, ok := existing.Spec[key]; ok {
				serviceSpec[key] = v
			}
		}
	}
	return k.request(ctx, method, servicePath, url.Values{}, map[string]any{"apiVersion": "v1", "kind": "Service", "metadata": meta, "spec": serviceSpec}, nil)
}

func (h *HTTP) Wait() { h.workers.Wait() }

// An available old replica must never make an unready new revision appear complete.
func rolloutStatusReady(d Deployment, replicas int) bool {
	return d.Status.ObservedGeneration >= d.Metadata.Generation && d.Status.Replicas == replicas && d.Status.UpdatedReplicas == replicas && d.Status.AvailableReplicas >= replicas
}
func rolloutProgressFailed(d Deployment) bool {
	if d.Status.ObservedGeneration < d.Metadata.Generation {
		return false
	}
	for _, c := range d.Status.Conditions {
		if c.Type == "Progressing" && c.Status == "False" {
			return true
		}
	}
	return false
}
