import { useEffect, useRef, useState, type FormEvent } from "react";
import { request, ApiError } from "../services/http";
import type { RegisteredApplication } from "../types/catalog";
import type { Team } from "./Teams";
interface Scope {
  id: string;
  name: string;
  environment: string;
  canEdit: boolean;
}
interface Incident {
  id: string;
  title: string;
  severity: string;
  state: string;
  version: number;
  assigneeId: string | null;
  mutedUntil: string | null;
  escalatedAt: string | null;
  notificationState: string;
  runbook: string;
}
interface IncidentDetail {
  incident: Incident;
  events: { kind: string; message: string; actor: string; createdAt: string }[];
}
interface Objective {
  id: string;
  name: string;
  indicator: string;
  targetPpm: number;
  latencyMs: number;
  windowDays: number;
  version: number;
  enabled: boolean;
}
interface SLOResult {
  slo: Objective;
  state: string;
  total: number;
  good: number;
  sliPercent: number | null;
  budgetRemainingPercent: number | null;
  burn5m: number | null;
  burn1h: number | null;
  samples: number;
  completeSamples: number;
  firstAt: string | null;
  lastAt: string | null;
}
interface Profile {
  id: string;
  name: string;
  engine: string;
  cpuMilli: number;
  memoryMiB: number;
  backupPolicy: string;
}
interface Service {
  id: string;
  name: string;
  profile: string;
  state: string;
  storageGiB: number;
  backupPolicy: string;
  version: number;
  updatedAt: string;
}
interface Policy {
  currency: string;
  cpuCoreHour: number;
  memoryGiBHour: number;
  storageGiBMonth: number;
  monthlyBudget: number;
  version: number;
}
interface Capacity {
  scopes: {
    scopeId: string;
    name: string;
    environment: string;
    replicas: number;
    requestedCPUCores: number;
    requestedMemoryGiB: number;
    estimatedMonthly?: number;
  }[];
  hasMoreScopes: boolean;
  canManagePolicy: boolean;
  requestedCPUCores: number;
  requestedMemoryGiB: number;
  retainedStorageGiB: number;
  estimateBasis: string;
  policy: Policy | null;
  estimatedMonthly: number | null;
  overBudget: boolean;
  budgetUsedPercent?: number;
}
function useData<T>(path: string | null, revision: number) {
  const loadedPath = useRef<string | null>(null);
  const [data, setData] = useState<T | null>(null),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(false);
  useEffect(() => {
    setData(null);
    setError("");
    if (!path) return;
    const c = new AbortController();
    setLoading(true);
    request<T>(path, { signal: c.signal })
      .then((v) => {
        if (!c.signal.aborted) {
          loadedPath.current = path;
          setData(v);
        }
      })
      .catch((e) => {
        if (!c.signal.aborted) setError(e.message);
      })
      .finally(() => {
        if (!c.signal.aborted) setLoading(false);
      });
    return () => c.abort();
  }, [path, revision]);
  return { data: loadedPath.current === path ? data : null, error, loading };
}
const states: Record<string, string> = {
  open: "Abierto",
  acknowledged: "Reconocido",
  resolved: "Resuelto",
  no_data: "Sin datos suficientes",
  observing: "Ventana en observación",
  stale: "Medición desactualizada",
  partial: "Cobertura parcial",
  budget_exhausted: "Presupuesto agotado",
  disabled: "Deshabilitado",
  requested: "Solicitado",
  ready: "Última operación completada",
  failed: "Operación fallida",
  suspended: "Suspendido",
};
const number = (n: number | null | undefined, suffix = "") =>
  n == null
    ? "Sin datos"
    : n.toLocaleString("es-CO", { maximumFractionDigits: 3 }) + suffix;
export function DevOpsHub() {
  const [tab, setTab] = useState(6),
    [appId, setAppId] = useState(""),
    [scopeId, setScopeId] = useState(""),
    [revision, setRevision] = useState(0),
    [appsPage, setAppsPage] = useState(0),
    [scopePage, setScopePage] = useState(0),
    [incidentPage, setIncidentPage] = useState(0),
    [incidentState, setIncidentState] = useState(""),
    [selectedIncident, setSelectedIncident] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [createdApp, setCreatedApp] = useState(""),
    [serviceStatus, setServiceStatus] = useState<{
      endpoint: string;
      readyReplicas: number;
      desiredReplicas: number;
      receivedAt: string;
    } | null>(null),
    [measured, setMeasured] = useState<{
      metricsAvailable: boolean;
      usage: {
        pod: string;
        containers: { name: string; usage: Record<string, string> }[];
      }[];
      receivedAt: string;
    } | null>(null),
    [confirm, setConfirm] = useState<{
      path: string;
      body: unknown;
      message: string;
      method?: string;
      key: string;
    } | null>(null);
  const flight = useRef(false);
  const apps = useData<{
    applications: RegisteredApplication[];
    hasMore: boolean;
  }>(`/applications?limit=100&offset=${appsPage * 100}`, revision);
  const scopes = useData<{ scopes: Scope[]; hasMore: boolean }>(
    appId
      ? `/applications/${appId}/scopes?limit=20&offset=${scopePage * 20}`
      : null,
    revision,
  );
  const scope = useData<Scope>(scopeId ? `/scopes/${scopeId}` : null, revision);
  const incidents = useData<{
    incidents: Incident[];
    hasMore: boolean;
    canEdit: boolean;
  }>(
    tab === 6 && appId
      ? `/applications/${appId}/incidents?limit=20&offset=${incidentPage * 20}&state=${incidentState}`
      : null,
    revision,
  );
  const responders = useData<{ responders: { id: string; name: string }[] }>(
    tab === 6 && appId ? `/applications/${appId}/responders` : null,
    revision,
  );
  const detail = useData<IncidentDetail>(
    tab === 6 && selectedIncident ? `/incidents/${selectedIncident}` : null,
    revision,
  );
  const slos = useData<{ slos: SLOResult[]; canEdit: boolean }>(
    tab === 7 && scopeId ? `/scopes/${scopeId}/slos` : null,
    revision,
  );
  const templates = useData<{
    templates: { id: string; name: string; version: string; runtime: string }[];
  }>(tab === 8 ? "/templates" : null, revision);
  const teams = useData<{ teams: Team[]; hasMore: boolean }>(
    tab === 8 ? "/teams?limit=100" : null,
    revision,
  );
  const templateStatus = useData<{
    managed: boolean;
    templateId: string;
    installedVersion: string;
    latestVersion: string;
    updateAvailable: boolean;
  }>(
    tab === 8 && appId ? `/applications/${appId}/template-status` : null,
    revision,
  );
  const profiles = useData<{
    profiles: Profile[];
    canProvision: boolean;
    maxPerScope: number;
  }>(
    tab === 9 && scopeId ? `/scopes/${scopeId}/service-profiles` : null,
    revision,
  );
  const services = useData<{ services: Service[]; canEdit: boolean }>(
    tab === 9 && scopeId ? `/scopes/${scopeId}/services` : null,
    revision,
  );
  const capacity = useData<Capacity>(
    tab === 10 && appId ? `/applications/${appId}/capacity` : null,
    revision,
  );
  const dataError = [
    apps,
    scopes,
    scope,
    incidents,
    responders,
    detail,
    slos,
    templates,
    teams,
    templateStatus,
    profiles,
    services,
    capacity,
  ].find((v) => v.error)?.error;
  const loading = [
    apps,
    scopes,
    scope,
    incidents,
    detail,
    slos,
    templates,
    profiles,
    services,
    capacity,
  ].some((v) => v.loading);
  function selectScope(id: string) {
    setScopeId(id);
    setServiceStatus(null);
    setMeasured(null);
    setConfirm(null);
    setError("");
    setNotice("");
  }
  function selectApp(id: string) {
    selectScope("");
    setAppId(id);
    setScopePage(0);
    setSelectedIncident("");
    setIncidentPage(0);
  }
  function propose(
    path: string,
    body: unknown,
    message: string,
    method = "POST",
  ) {
    setConfirm({ path, body, message, method, key: crypto.randomUUID() });
    setError("");
  }
  async function mutate() {
    if (!confirm || flight.current) return;
    flight.current = true;
    setBusy(true);
    setError("");
    try {
      const result = await request<{ application?: RegisteredApplication }>(
        confirm.path,
        {
          method: confirm.method,
          headers: { "Idempotency-Key": confirm.key },
          body: JSON.stringify(confirm.body),
        },
      );
      if (result.application) setCreatedApp(result.application.id);
      setConfirm(null);
      setRevision((n) => n + 1);
      setNotice(
        "Solicitud registrada. Actualiza el estado o consulta Operación → Trabajos.",
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : "No se pudo guardar");
      if (e instanceof ApiError && e.status >= 400 && e.status < 500)
        setConfirm(null);
    } finally {
      setBusy(false);
      flight.current = false;
    }
  }
  function createIncident(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const d = new FormData(e.currentTarget);
    propose(
      `/applications/${appId}/incidents`,
      {
        title: d.get("title"),
        description: d.get("description"),
        severity: d.get("severity"),
        scopeId,
        assigneeId: d.get("assigneeId"),
        runbook: d.get("runbook"),
        escalationMinutes: Number(d.get("escalation")),
      },
      "¿Abrir este incidente y comenzar su plazo de atención?",
    );
  }
  async function readService(id: string) {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      setServiceStatus(await request(`/services/${id}/status`));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Estado no disponible");
    } finally {
      setBusy(false);
    }
  }
  async function readUsage() {
    if (busy || !scopeId) return;
    setBusy(true);
    setError("");
    try {
      setMeasured(await request(`/scopes/${scopeId}/kubernetes`));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Medición no disponible");
    } finally {
      setBusy(false);
    }
  }
  const p = capacity.data?.policy;
  return (
    <div className="devops-hub">
      <div className="info-strip">
        Gestión DevOps · puntos 6–10. Las operaciones respetan los equipos y
        permisos de cada aplicación.
      </div>
      <div className="segmented runtime-tabs">
        {[
          [6, "Incidentes"],
          [7, "SLO"],
          [8, "Plantillas"],
          [9, "Servicios"],
          [10, "Capacidad y costos"],
        ].map(([id, label]) => (
          <button
            key={id}
            className={tab === id ? "selected" : ""}
            disabled={busy}
            onClick={() => {
              setTab(Number(id));
              setConfirm(null);
              setError("");
              setNotice("");
            }}
          >
            {id}. {label}
          </button>
        ))}
      </div>
      <section className="panel section-body">
        <div className="form-row">
          <label>
            Aplicación
            <select
              value={appId}
              disabled={busy || loading}
              onChange={(e) => selectApp(e.target.value)}
            >
              <option value="">Selecciona una aplicación</option>
              {apps.data?.applications.map((a) => (
                <option value={a.id} key={a.id}>
                  {a.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            Scope
            <select
              value={scopeId}
              disabled={busy || loading}
              onChange={(e) => selectScope(e.target.value)}
            >
              <option value="">Selecciona un scope</option>
              {scopes.data?.scopes.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name} / {s.environment}
                </option>
              ))}
            </select>
          </label>
          <button
            className="secondary-button"
            disabled={busy || loading}
            onClick={() => setRevision((n) => n + 1)}
          >
            Actualizar
          </button>
        </div>
        <div className="pagination">
          <button
            className="text-button"
            disabled={busy || appsPage === 0}
            onClick={() => {
              selectApp("");
              setAppsPage((n) => n - 1);
            }}
          >
            Aplicaciones anteriores
          </button>
          <button
            className="text-button"
            disabled={busy || !apps.data?.hasMore}
            onClick={() => {
              selectApp("");
              setAppsPage((n) => n + 1);
            }}
          >
            Más aplicaciones
          </button>
          <button
            className="text-button"
            disabled={busy || scopePage === 0}
            onClick={() => {
              selectScope("");
              setScopePage((n) => n - 1);
            }}
          >
            Scopes anteriores
          </button>
          <button
            className="text-button"
            disabled={busy || !scopes.data?.hasMore}
            onClick={() => {
              selectScope("");
              setScopePage((n) => n + 1);
            }}
          >
            Más scopes
          </button>
        </div>
      </section>
      {(error || dataError) && (
        <p role="alert" className="form-message bad">
          {error || dataError}
        </p>
      )}
      {notice && (
        <p role="status" className="form-message good">
          {notice}
        </p>
      )}
      {loading && <p role="status">Consultando…</p>}
      {confirm && (
        <section className="confirm-box">
          <p>{confirm.message}</p>
          <div className="form-actions">
            <button
              className="secondary-button"
              disabled={busy}
              onClick={() => setConfirm(null)}
            >
              Cancelar
            </button>
            <button
              className="primary-button"
              disabled={busy}
              onClick={() => void mutate()}
            >
              {busy ? "Procesando…" : "Confirmar"}
            </button>
          </div>
        </section>
      )}
      {tab === 6 && (
        <section className="panel">
          <header className="panel-head">
            <h2>6 / INCIDENTES</h2>
          </header>
          <div className="section-body">
            {!appId ? (
              <p>Selecciona una aplicación para gestionar sus incidentes.</p>
            ) : (
              <>
                <label>
                  Estado
                  <select
                    value={incidentState}
                    onChange={(e) => {
                      setIncidentState(e.target.value);
                      setIncidentPage(0);
                    }}
                  >
                    <option value="">Todos</option>
                    {["open", "acknowledged", "resolved"].map((s) => (
                      <option value={s} key={s}>
                        {states[s]}
                      </option>
                    ))}
                  </select>
                </label>
                {incidents.data?.incidents.map((i) => (
                  <button
                    className="team-choice"
                    key={i.id}
                    onClick={() => setSelectedIncident(i.id)}
                  >
                    <strong>{i.title}</strong>
                    <span>
                      {i.severity} · {states[i.state]}
                    </span>
                    <small>
                      {i.mutedUntil
                        ? "Silencio hasta " +
                          new Date(i.mutedUntil).toLocaleString("es-CO")
                        : i.escalatedAt
                          ? "Escalado"
                          : "Plazo de atención activo"}{" "}
                      · notificación: {i.notificationState}
                    </small>
                  </button>
                ))}
                {incidents.data?.incidents.length === 0 && (
                  <p>No hay incidentes con este filtro.</p>
                )}
                <div className="pagination">
                  <button
                    className="secondary-button"
                    disabled={!incidentPage}
                    onClick={() => setIncidentPage((n) => n - 1)}
                  >
                    Anterior
                  </button>
                  <button
                    className="secondary-button"
                    disabled={!incidents.data?.hasMore}
                    onClick={() => setIncidentPage((n) => n + 1)}
                  >
                    Siguiente
                  </button>
                </div>
                {detail.data && (
                  <section className="confirm-box">
                    <h3>{detail.data.incident.title}</h3>
                    <p>
                      {states[detail.data.incident.state]} · revisión{" "}
                      {detail.data.incident.version} · notificación:{" "}
                      {detail.data.incident.notificationState}
                    </p>
                    {detail.data.incident.runbook && (
                      <a
                        href={detail.data.incident.runbook}
                        target="_blank"
                        rel="noreferrer"
                      >
                        Abrir runbook
                      </a>
                    )}
                    {incidents.data?.canEdit && (
                      <form
                        onSubmit={(e) => {
                          e.preventDefault();
                          const d = new FormData(e.currentTarget);
                          propose(
                            `/incidents/${selectedIncident}/actions`,
                            {
                              action: d.get("action"),
                              version: detail.data!.incident.version,
                              message: d.get("message"),
                              assigneeId: d.get("assigneeId"),
                              minutes: Number(d.get("minutes")),
                            },
                            "¿Registrar esta acción en la cronología del incidente?",
                          );
                        }}
                      >
                        <fieldset disabled={busy}>
                          <div className="form-row">
                            <label>
                              Acción
                              <select name="action">
                                <option value="comment">Agregar nota</option>
                                <option value="acknowledge">Reconocer</option>
                                <option value="resolve">Resolver</option>
                                <option value="reopen">Reabrir</option>
                                <option value="assign">
                                  Asignar responsable
                                </option>
                                <option value="mute">
                                  Silenciar escalamiento
                                </option>
                                <option value="unmute">Quitar silencio</option>
                              </select>
                            </label>
                            <label>
                              Responsable
                              <select name="assigneeId">
                                <option value="">Seleccionar</option>
                                {responders.data?.responders.map((u) => (
                                  <option key={u.id} value={u.id}>
                                    {u.name}
                                  </option>
                                ))}
                              </select>
                            </label>
                            <label>
                              Minutos de silencio
                              <input
                                name="minutes"
                                type="number"
                                min={1}
                                max={1440}
                                defaultValue={30}
                              />
                            </label>
                          </div>
                          <label>
                            Nota
                            <textarea
                              name="message"
                              required
                              maxLength={2000}
                            />
                          </label>
                        </fieldset>
                        <button className="primary-button" disabled={busy}>
                          Registrar acción
                        </button>
                      </form>
                    )}
                    <h4>Cronología (últimos 200 eventos)</h4>
                    {detail.data.events.map((v, i) => (
                      <div className="runtime-job" key={i}>
                        <small>
                          {new Date(v.createdAt).toLocaleString("es-CO")} ·{" "}
                          {v.actor} · {v.kind}
                        </small>
                        <p>{v.message}</p>
                      </div>
                    ))}
                  </section>
                )}
                {incidents.data?.canEdit && (
                  <form className="application-form" onSubmit={createIncident}>
                    <h3>Abrir incidente</h3>
                    <fieldset disabled={busy}>
                      <label>
                        Título
                        <input name="title" maxLength={160} required />
                      </label>
                      <label>
                        Descripción
                        <textarea
                          name="description"
                          maxLength={2000}
                          required
                        />
                      </label>
                      <div className="form-row">
                        <label>
                          Severidad
                          <select name="severity">
                            <option value="critical">Crítico</option>
                            <option value="warning">Advertencia</option>
                            <option value="info">Informativo</option>
                          </select>
                        </label>
                        <label>
                          Responsable
                          <select name="assigneeId">
                            <option value="">Sin asignar</option>
                            {responders.data?.responders.map((u) => (
                              <option key={u.id} value={u.id}>
                                {u.name}
                              </option>
                            ))}
                          </select>
                        </label>
                        <label>
                          Escalar después de (min)
                          <input
                            type="number"
                            name="escalation"
                            min={1}
                            max={1440}
                            defaultValue={15}
                            required
                          />
                        </label>
                      </div>
                      <label>
                        Runbook HTTPS
                        <input
                          name="runbook"
                          type="url"
                          pattern="https://.*"
                          maxLength={1000}
                        />
                      </label>
                      <p className="muted">
                        Scope asociado:{" "}
                        {scope.data?.name || "incidente de aplicación"}.
                        Reconocer no resuelve y silenciar solo pospone el
                        escalamiento.
                      </p>
                    </fieldset>
                    <button className="primary-button" disabled={busy}>
                      Abrir incidente
                    </button>
                  </form>
                )}
              </>
            )}
          </div>
        </section>
      )}
      {tab === 7 && (
        <section className="panel">
          <header className="panel-head">
            <h2>7 / SLO Y PRESUPUESTO DE ERROR</h2>
          </header>
          <div className="section-body">
            {!scopeId ? (
              <p>Selecciona un scope.</p>
            ) : (
              <>
                <p className="catalog-intro">
                  Disponibilidad: solicitudes sin error 5xx / solicitudes
                  medidas. Latencia: solicitudes dentro del umbral / solicitudes
                  medidas. Requiere instrumentación; los datos ausentes se
                  muestran como tales. La ventana se acumula desde la primera
                  medición.
                </p>
                {slos.data?.slos.map((v) => (
                  <article className="runtime-version" key={v.slo.id}>
                    <strong>
                      {v.slo.name} · {number(v.slo.targetPpm / 10000, "%")} en{" "}
                      {v.slo.windowDays} días
                    </strong>
                    <p>
                      {v.slo.indicator === "latency"
                        ? `Latencia ≤ ${v.slo.latencyMs} ms`
                        : "Disponibilidad HTTP"}{" "}
                      · {states[v.state]}
                    </p>
                    <div className="form-row">
                      <span>
                        SLI: <b>{number(v.sliPercent, "%")}</b>
                      </span>
                      <span>
                        Presupuesto restante:{" "}
                        <b
                          className={
                            (v.budgetRemainingPercent ?? 1) < 0 ? "bad" : ""
                          }
                        >
                          {number(v.budgetRemainingPercent, "%")}
                        </b>
                      </span>
                      <span>
                        Burn 5 min / 1 h: {number(v.burn5m, "×")} /{" "}
                        {number(v.burn1h, "×")}
                      </span>
                    </div>
                    <small>
                      {v.good} buenas / {v.total} medidas · intervalos completos{" "}
                      {v.completeSamples}/{v.samples} · última muestra:{" "}
                      {v.lastAt
                        ? new Date(v.lastAt).toLocaleString("es-CO")
                        : "pendiente"}
                    </small>
                    {slos.data?.canEdit && (
                      <button
                        className="text-button"
                        disabled={busy}
                        onClick={() =>
                          propose(
                            `/slos/${v.slo.id}/enabled`,
                            { version: v.slo.version, enabled: !v.slo.enabled },
                            v.slo.enabled
                              ? "¿Deshabilitar este objetivo? Se conserva el historial."
                              : "¿Habilitar este objetivo con una nueva línea base?",
                          )
                        }
                      >
                        {v.slo.enabled ? "Deshabilitar" : "Habilitar"}
                      </button>
                    )}
                  </article>
                ))}
                {slos.data?.canEdit && (
                  <>
                    <button
                      className="secondary-button"
                      disabled={busy}
                      onClick={() =>
                        propose(
                          `/scopes/${scopeId}/slos/evaluate`,
                          {},
                          "¿Leer los contadores actuales? La primera evaluación establece la línea base.",
                        )
                      }
                    >
                      Evaluar ahora
                    </button>
                    <form
                      className="application-form"
                      onSubmit={(e) => {
                        e.preventDefault();
                        const d = new FormData(e.currentTarget);
                        propose(
                          `/scopes/${scopeId}/slos`,
                          {
                            name: d.get("name"),
                            indicator: d.get("indicator"),
                            targetPpm: Math.round(
                              Number(d.get("target")) * 10000,
                            ),
                            latencyMs: Number(d.get("latency")),
                            windowDays: Number(d.get("window")),
                          },
                          "¿Crear este SLO? El objetivo será inmutable; puedes deshabilitarlo para conservar su historial.",
                        );
                      }}
                    >
                      <h3>Nuevo objetivo (máximo 10 por scope)</h3>
                      <fieldset disabled={busy}>
                        <label>
                          Nombre
                          <input name="name" maxLength={80} required />
                        </label>
                        <div className="form-row">
                          <label>
                            Indicador
                            <select name="indicator">
                              <option value="availability">
                                Disponibilidad
                              </option>
                              <option value="latency">Latencia</option>
                            </select>
                          </label>
                          <label>
                            Objetivo (%)
                            <input
                              name="target"
                              type="number"
                              min={90}
                              max={99.999}
                              step={0.001}
                              defaultValue={99.9}
                              required
                            />
                          </label>
                          <label>
                            Umbral de latencia
                            <select name="latency">
                              <option value={100}>100 ms</option>
                              <option value={300}>300 ms</option>
                              <option value={1000}>1000 ms</option>
                            </select>
                          </label>
                          <label>
                            Ventana
                            <select name="window">
                              <option value={1}>1 día</option>
                              <option value={7}>7 días</option>
                              <option value={28}>28 días</option>
                            </select>
                          </label>
                        </div>
                      </fieldset>
                      <button className="primary-button" disabled={busy}>
                        Crear SLO
                      </button>
                    </form>
                  </>
                )}
              </>
            )}
          </div>
        </section>
      )}
      {tab === 8 && (
        <section className="panel">
          <header className="panel-head">
            <h2>8 / PLANTILLAS DE AUTOSERVICIO</h2>
          </header>
          <div className="section-body">
            <p className="catalog-intro">
              Crea una aplicación del catálogo y descarga su código inicial con
              pruebas, contadores HTTP, Dockerfile, CI y manifiesto. Después
              puedes conectar el repositorio y desplegarlo desde Operación.
            </p>
            {templates.data?.templates.map((t) => (
              <article className="runtime-version" key={t.id}>
                <strong>
                  {t.name} · v{t.version}
                </strong>
                <form
                  className="application-form"
                  onSubmit={(e) => {
                    e.preventDefault();
                    const d = new FormData(e.currentTarget);
                    propose(
                      `/templates/${t.id}/applications`,
                      {
                        name: d.get("name"),
                        repository: d.get("repository"),
                        owner: d.get("owner"),
                        teamId: d.get("teamId") || null,
                      },
                      `¿Registrar la aplicación desde ${t.name}? La descarga no crea repositorios remotos ni publica recursos.`,
                    );
                  }}
                >
                  <fieldset disabled={busy}>
                    <div className="form-row">
                      <label>
                        Nombre
                        <input
                          name="name"
                          pattern="[a-z][a-z0-9-]{0,39}"
                          maxLength={40}
                          required
                        />
                      </label>
                      <label>
                        Responsable
                        <input name="owner" maxLength={120} required />
                      </label>
                      <label>
                        Equipo
                        <select name="teamId">
                          <option value="">Aplicación personal</option>
                          {teams.data?.teams
                            .filter((t) => t.role !== "reader")
                            .map((t) => (
                              <option key={t.id} value={t.id}>
                                {t.name}
                              </option>
                            ))}
                        </select>
                      </label>
                    </div>
                    <label>
                      Repositorio HTTPS
                      <input
                        name="repository"
                        type="url"
                        pattern="https://.*"
                        required
                        placeholder="https://github.com/organizacion/aplicacion"
                      />
                    </label>
                  </fieldset>
                  <button className="primary-button" disabled={busy}>
                    Crear aplicación y preparar descarga
                  </button>
                </form>
              </article>
            ))}
            {createdApp && (
              <p className="form-message good">
                <a href={`/api/applications/${createdApp}/scaffold`}>
                  Descargar plantilla de la aplicación creada
                </a>
              </p>
            )}
            {templateStatus.data?.managed && (
              <div className="confirm-box">
                <p>
                  Aplicación seleccionada: {templateStatus.data.templateId} ·
                  instalada {templateStatus.data.installedVersion} · disponible{" "}
                  {templateStatus.data.latestVersion}
                </p>
                <p>
                  {templateStatus.data.updateAvailable
                    ? "Hay una actualización de plantilla para revisar."
                    : "Versión de plantilla actual."}
                </p>
                <a href={`/api/applications/${appId}/scaffold`}>
                  Descargar código inicial
                </a>
              </div>
            )}
            {teams.data?.hasMore && (
              <p>
                La selección muestra los primeros 100 equipos. Puedes asignar el
                equipo desde el catálogo.
              </p>
            )}
          </div>
        </section>
      )}
      {tab === 9 && (
        <section className="panel">
          <header className="panel-head">
            <h2>9 / SERVICIOS ADMINISTRADOS</h2>
          </header>
          <div className="section-body">
            {!scopeId ? (
              <p>Selecciona un scope.</p>
            ) : (
              <>
                <p className="catalog-intro">
                  Perfiles aprobados para desarrollo: hasta dos servicios por
                  scope, con volumen persistente y acceso interno. Suspender
                  detiene el servicio y conserva su volumen. Producción requiere
                  implementar y verificar la política de respaldo externa.
                </p>
                {services.data?.services.map((s) => (
                  <article className="runtime-version" key={s.id}>
                    <strong>
                      {s.name} · {s.profile}
                    </strong>
                    <p>
                      {states[s.state]} · {s.storageGiB} GiB · {s.backupPolicy}
                    </p>
                    <small>
                      Última operación:{" "}
                      {new Date(s.updatedAt).toLocaleString("es-CO")}
                    </small>
                    <div className="form-actions">
                      <button
                        className="secondary-button"
                        disabled={busy}
                        onClick={() => void readService(s.id)}
                      >
                        Consultar estado real
                      </button>
                      {services.data?.canEdit && (
                        <button
                          className="secondary-button"
                          disabled={busy}
                          onClick={() =>
                            propose(
                              `/services/${s.id}/actions`,
                              {
                                action:
                                  s.state === "suspended"
                                    ? "resume"
                                    : "suspend",
                                version: s.version,
                              },
                              s.state === "suspended"
                                ? "¿Reanudar este servicio con el volumen existente?"
                                : "¿Suspender este servicio? Se detienen sus conexiones; los datos se conservan.",
                            )
                          }
                        >
                          {s.state === "suspended" ? "Reanudar" : "Suspender"}
                        </button>
                      )}
                    </div>
                  </article>
                ))}
                {serviceStatus && (
                  <div className="confirm-box">
                    <strong>
                      {serviceStatus.readyReplicas}/
                      {serviceStatus.desiredReplicas} réplicas listas
                    </strong>
                    <p>
                      <code>{serviceStatus.endpoint}</code>
                    </p>
                    <small>
                      Consulta:{" "}
                      {new Date(serviceStatus.receivedAt).toLocaleString(
                        "es-CO",
                      )}
                    </small>
                  </div>
                )}
                {!profiles.data?.profiles.length && (
                  <p>
                    El operador debe habilitar perfiles y referencias a
                    credenciales en este namespace.
                  </p>
                )}
                {profiles.data?.canProvision &&
                  profiles.data.profiles.map((p) => (
                    <form
                      className="application-form"
                      key={p.id}
                      onSubmit={(e) => {
                        e.preventDefault();
                        const d = new FormData(e.currentTarget);
                        propose(
                          `/scopes/${scopeId}/services`,
                          {
                            name: d.get("name"),
                            profile: p.id,
                            storageGiB: Number(d.get("storage")),
                            backupPolicy: "development-only",
                          },
                          `Plan: ${p.name}, límite ${p.cpuMilli}m CPU / ${p.memoryMiB} MiB, ${d.get("storage")} GiB persistentes, red interna y política de desarrollo. ¿Aprobar y aprovisionar?`,
                        );
                      }}
                    >
                      <h3>{p.name}</h3>
                      <p>
                        {p.cpuMilli}m CPU / {p.memoryMiB} MiB · volumen
                        persistente · perfil de desarrollo
                      </p>
                      <fieldset disabled={busy}>
                        <div className="form-row">
                          <label>
                            Nombre
                            <input
                              name="name"
                              pattern="[a-z][a-z0-9-]{0,39}"
                              maxLength={40}
                              required
                            />
                          </label>
                          <label>
                            Almacenamiento (GiB)
                            <input
                              name="storage"
                              type="number"
                              min={1}
                              max={10}
                              defaultValue={1}
                              required
                            />
                          </label>
                        </div>
                      </fieldset>
                      <button className="primary-button" disabled={busy}>
                        Revisar plan
                      </button>
                    </form>
                  ))}
                <p>
                  <a href="#deployments">
                    Ver progreso y reintentar en Trabajos
                  </a>
                </p>
              </>
            )}
          </div>
        </section>
      )}
      {tab === 10 && (
        <section className="panel">
          <header className="panel-head">
            <h2>10 / CAPACIDAD Y COSTOS</h2>
          </header>
          <div className="section-body">
            {!appId ? (
              <p>Selecciona una aplicación.</p>
            ) : (
              capacity.data && (
                <>
                  <p>
                    {capacity.data.estimateBasis}. No incluye impuestos,
                    tráfico, control plane, recursos compartidos ni facturas de
                    proveedores.
                  </p>
                  <div className="stats">
                    <div className="stat">
                      <div>CPU SOLICITADA</div>
                      <strong>{number(capacity.data.requestedCPUCores)}</strong>
                      <span>cores declarados</span>
                    </div>
                    <div className="stat">
                      <div>MEMORIA SOLICITADA</div>
                      <strong>
                        {number(capacity.data.requestedMemoryGiB)}
                      </strong>
                      <span>GiB declarados</span>
                    </div>
                    <div className="stat">
                      <div>ALMACENAMIENTO</div>
                      <strong>
                        {number(capacity.data.retainedStorageGiB)}
                      </strong>
                      <span>GiB, incluidos volúmenes suspendidos</span>
                    </div>
                    <div className="stat">
                      <div>ESTIMACIÓN / MES</div>
                      <strong>{number(capacity.data.estimatedMonthly)}</strong>
                      <span>
                        {p
                          ? "USD según tarifas configuradas"
                          : "Configura tarifas para estimar"}
                      </span>
                    </div>
                  </div>
                  {p && (
                    <p
                      className={
                        capacity.data.overBudget
                          ? "form-message bad"
                          : "form-message good"
                      }
                    >
                      Presupuesto: {number(p.monthlyBudget, " USD")} · uso
                      estimado {number(capacity.data.budgetUsedPercent, "%")}
                      {capacity.data.overBudget
                        ? " · supera el presupuesto"
                        : ""}
                    </p>
                  )}
                  {capacity.data.scopes.map((s) => (
                    <div className="runtime-job" key={s.scopeId}>
                      <strong>
                        {s.name} / {s.environment}
                      </strong>
                      <p>
                        {s.replicas} réplicas ·{" "}
                        {number(s.requestedCPUCores, " cores solicitados")} ·{" "}
                        {number(s.requestedMemoryGiB, " GiB solicitados")} ·{" "}
                        {number(s.estimatedMonthly, " USD/mes estimados")}
                      </p>
                    </div>
                  ))}
                  {capacity.data.hasMoreScopes && (
                    <p>
                      Se muestran los primeros 50 scopes; el total de la
                      aplicación incluye todos.
                    </p>
                  )}
                  <button
                    className="secondary-button"
                    disabled={busy || !scopeId}
                    onClick={() => void readUsage()}
                  >
                    Medir consumo del scope seleccionado
                  </button>
                  {measured && (
                    <div className="confirm-box">
                      <p>
                        {measured.metricsAvailable
                          ? "Consumo medido de pods:"
                          : "Metrics Server sin datos para este scope."}
                      </p>
                      {measured.usage?.map((u) => (
                        <div key={u.pod}>
                          <strong>{u.pod}</strong>
                          {u.containers.map((c) => (
                            <p key={c.name}>
                              {c.name}: CPU {c.usage.cpu}, memoria{" "}
                              {c.usage.memory}
                            </p>
                          ))}
                        </div>
                      ))}
                      <small>
                        {measured.receivedAt &&
                          new Date(measured.receivedAt).toLocaleString("es-CO")}
                      </small>
                    </div>
                  )}
                  {capacity.data.canManagePolicy && (
                    <form
                      key={p?.version || 0}
                      className="application-form"
                      onSubmit={(e) => {
                        e.preventDefault();
                        const d = new FormData(e.currentTarget);
                        propose(
                          `/applications/${appId}/cost-policy`,
                          {
                            currency: "USD",
                            cpuCoreHour: Number(d.get("cpu")),
                            memoryGiBHour: Number(d.get("memory")),
                            storageGiBMonth: Number(d.get("storage")),
                            monthlyBudget: Number(d.get("budget")),
                            version: p?.version || 0,
                          },
                          "¿Guardar estas tarifas de referencia y el presupuesto de la aplicación?",
                          "PUT",
                        );
                      }}
                    >
                      <h3>Tarifas y presupuesto (administración)</h3>
                      <fieldset disabled={busy}>
                        <div className="form-row">
                          <label>
                            USD / core-hora
                            <input
                              name="cpu"
                              type="number"
                              min={0}
                              max={100}
                              step={0.000001}
                              defaultValue={p?.cpuCoreHour}
                              required
                            />
                          </label>
                          <label>
                            USD / GiB-hora memoria
                            <input
                              name="memory"
                              type="number"
                              min={0}
                              max={100}
                              step={0.000001}
                              defaultValue={p?.memoryGiBHour}
                              required
                            />
                          </label>
                          <label>
                            USD / GiB-mes almacenamiento
                            <input
                              name="storage"
                              type="number"
                              min={0}
                              max={100}
                              step={0.000001}
                              defaultValue={p?.storageGiBMonth}
                              required
                            />
                          </label>
                          <label>
                            Presupuesto mensual USD
                            <input
                              name="budget"
                              type="number"
                              min={1}
                              max={1000000}
                              step={0.01}
                              defaultValue={p?.monthlyBudget}
                              required
                            />
                          </label>
                        </div>
                      </fieldset>
                      <button className="primary-button" disabled={busy}>
                        Guardar política
                      </button>
                    </form>
                  )}
                </>
              )
            )}
          </div>
        </section>
      )}
    </div>
  );
}
