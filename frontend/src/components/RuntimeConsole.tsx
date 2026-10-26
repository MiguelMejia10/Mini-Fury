import { useEffect, useState, type FormEvent } from "react";
import { request } from "../services/http";
import { useAuth } from "./Auth";
import type { RegisteredApplication } from "../types/catalog";
interface Scope {
  id: string;
  applicationId: string;
  name: string;
  environment: string;
  criticality: string;
  region: string;
  domain: string;
  clusterId: string;
  namespace: string;
  workload: string;
  replicas: number;
  cpuMilli: number;
  memoryMiB: number;
  version: number;
  canEdit: boolean;
}
interface Connection {
  clusterId: string;
  clusterName: string;
  namespace: string;
  secretAliases: string[];
}
interface Version {
  id: string;
  name: string;
  image: string;
  sourceRevision: string;
  provenance: string;
  createdAt: string;
}
interface Job {
  id: string;
  applicationId: string;
  scopeId: string | null;
  kind: string;
  state: string;
  phase: string;
  message: string;
  attempt: number;
  cancelRequested: boolean;
  createdAt: string;
}
interface Configuration {
  id: string;
  revision: number;
  variables: Record<string, string>;
  secretRefs: Record<string, string>;
}
interface Pod {
  metadata: { name: string };
  spec: { containers: Array<{ name: string; image: string }> };
  status: {
    phase: string;
    containerStatuses?: Array<{
      name: string;
      ready: boolean;
      restartCount: number;
    }>;
  };
}
interface KubernetesState {
  deployed: boolean;
  workload: string;
  pods: Pod[];
  hasMorePods: boolean;
  status?: {
    readyReplicas: number;
    updatedReplicas: number;
    availableReplicas: number;
  };
  metricsAvailable: boolean;
  usage: Array<{
    pod: string;
    containers: Array<{ name: string; usage: Record<string, string> }>;
  }>;
  eventsAvailable: boolean;
  events: Array<{
    type: string;
    reason: string;
    message: string;
    count: number;
    lastTimestamp: string;
  }>;
  receivedAt?: string;
  message?: string;
}
const stateLabel: Record<string, string> = {
  queued: "En cola",
  running: "En ejecución",
  succeeded: "Completado",
  failed: "Fallido",
  cancelled: "Cancelado",
};
export function RuntimeConsole({
  initialApp,
  mode = "all",
}: {
  initialApp?: string;
  mode?: "all" | "jobs" | "configuration";
}) {
  const { user } = useAuth();
  const [apps, setApps] = useState<RegisteredApplication[]>([]),
    [appId, setAppId] = useState(initialApp || ""),
    [scopeId, setScopeId] = useState(""),
    [scopes, setScopes] = useState<Scope[]>([]),
    [scope, setScope] = useState<Scope | null>(null),
    [connections, setConnections] = useState<Connection[]>([]),
    [versions, setVersions] = useState<Version[]>([]),
    [configurations, setConfigurations] = useState<Configuration[]>([]),
    [jobs, setJobs] = useState<Job[]>([]),
    [kube, setKube] = useState<KubernetesState | null>(null),
    [pod, setPod] = useState(""),
    [container, setContainer] = useState(""),
    [logs, setLogs] = useState(""),
    [previous, setPrevious] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [revision, setRevision] = useState(0),
    [busy, setBusy] = useState(false),
    [loading, setLoading] = useState(false),
    [tab, setTab] = useState(
      mode === "jobs" ? 5 : mode === "configuration" ? 4 : 1,
    ),
    [buildProvider, setBuildProvider] = useState(""),
    [page, setPage] = useState(0),
    [hasMore, setHasMore] = useState(false),
    [state, setState] = useState(""),
    [kind, setKind] = useState(""),
    [detail, setDetail] = useState<{
      job: Job;
      events: Array<{
        state: string;
        phase: string;
        message: string;
        createdAt: string;
      }>;
    } | null>(null),
    [confirm, setConfirm] = useState<{
      title: string;
      path: string;
      body: unknown;
      method?: string;
    } | null>(null),
    [connectionIndex, setConnectionIndex] = useState(0),
    [appsMore, setAppsMore] = useState(false),
    [appsOffset, setAppsOffset] = useState(0),
    [scopeMore, setScopeMore] = useState(false),
    [scopeOffset, setScopeOffset] = useState(0),
    [versionsMore, setVersionsMore] = useState(false),
    [versionOffset, setVersionOffset] = useState(0),
    [configurationMore, setConfigurationMore] = useState(false),
    [configurationOffset, setConfigurationOffset] = useState(0);
  const app = apps.find((a) => a.id === appId);
  const edit = !!app?.canEdit;
  const connection = connections[connectionIndex];
  useEffect(() => {
    const c = new AbortController();
    request<{ applications: RegisteredApplication[]; hasMore: boolean }>(
      `/applications?limit=100&offset=${appsOffset}`,
      { signal: c.signal },
    )
      .then((v) => {
        if (!c.signal.aborted) {
          setApps((old) =>
            appsOffset ? [...old, ...v.applications] : v.applications,
          );
          setAppsMore(v.hasMore);
        }
      })
      .catch((e) => {
        if (!c.signal.aborted) setError(e.message);
      });
    return () => c.abort();
  }, [appsOffset]);
  useEffect(() => {
    setScopeId("");
    setScope(null);
    setKube(null);
    setLogs("");
    setScopeOffset(0);
    setVersionOffset(0);
    setConfigurationOffset(0);
    setPage(0);
    setConfirm(null);
    setDetail(null);
  }, [appId]);
  useEffect(() => {
    if (!appId) {
      setScopes([]);
      setVersions([]);
      setConnections([]);
      return;
    }
    const c = new AbortController();
    setLoading(true);
    setError("");
    Promise.all([
      request<{ scopes: Scope[]; hasMore: boolean }>(
        `/applications/${appId}/scopes?limit=20&offset=${scopeOffset}`,
        { signal: c.signal },
      ),
      request<{
        connections: Connection[];
        buildConfigured: boolean;
        buildProvider: string;
      }>(`/applications/${appId}/runtime-connections`, { signal: c.signal }),
      request<{ versions: Version[]; hasMore: boolean }>(
        `/applications/${appId}/versions?limit=20&offset=${versionOffset}`,
        { signal: c.signal },
      ),
    ])
      .then(([s, cfg, v]) => {
        if (c.signal.aborted) return;
        setScopes(s.scopes);
        setScopeMore(s.hasMore);
        setVersions(v.versions);
        setVersionsMore(v.hasMore);
        setConnections(cfg.connections);
        setBuildProvider(cfg.buildConfigured ? cfg.buildProvider : "");
        setConnectionIndex(0);
      })
      .catch((e) => {
        if (!c.signal.aborted) setError(e.message);
      })
      .finally(() => {
        if (!c.signal.aborted) setLoading(false);
      });
    return () => c.abort();
  }, [appId, revision, scopeOffset, versionOffset]);
  useEffect(() => {
    setKube(null);
    setLogs("");
    setPod("");
    setContainer("");
    setConfigurationOffset(0);
    setPage(0);
    setConfirm(null);
    if (!scopeId) {
      setScope(null);
      setConfigurations([]);
      return;
    }
    const c = new AbortController();
    request<Scope>(`/scopes/${scopeId}`, { signal: c.signal })
      .then((s) => {
        if (!c.signal.aborted) setScope(s);
      })
      .catch((e) => {
        if (!c.signal.aborted) setError(e.message);
      });
    return () => c.abort();
  }, [scopeId, revision]);
  useEffect(() => {
    if (!scopeId) return;
    const c = new AbortController();
    request<{ configurations: Configuration[]; hasMore: boolean }>(
      `/scopes/${scopeId}/configuration?limit=20&offset=${configurationOffset}`,
      { signal: c.signal },
    )
      .then((v) => {
        if (!c.signal.aborted) {
          setConfigurations(v.configurations);
          setConfigurationMore(v.hasMore);
        }
      })
      .catch((e) => {
        if (!c.signal.aborted) setError(e.message);
      });
    return () => c.abort();
  }, [scopeId, revision, configurationOffset]);
  useEffect(() => {
    const c = new AbortController();
    let timer: number | undefined;
    async function refresh() {
      if (document.hidden) {
        timer = window.setTimeout(refresh, 5000);
        return;
      }
      try {
        const query = new URLSearchParams({
          limit: "20",
          offset: String(page * 20),
          state,
          kind,
          ...(appId ? { applicationId: appId } : {}),
          ...(scopeId ? { scopeId } : {}),
        });
        const v = await request<{ jobs: Job[]; hasMore: boolean }>(
          "/runtime/jobs?" + query,
          { signal: c.signal },
        );
        if (c.signal.aborted) return;
        setJobs(v.jobs);
        setHasMore(v.hasMore);
      } catch (e) {
        if (!c.signal.aborted)
          setError(
            e instanceof Error
              ? e.message
              : "No se pudieron consultar los trabajos.",
          );
      } finally {
        if (!c.signal.aborted)
          timer = window.setTimeout(() => {
            void refresh();
          }, 5000);
      }
    }
    void refresh();
    return () => {
      c.abort();
      window.clearTimeout(timer);
    };
  }, [appId, scopeId, page, state, kind, revision]);
  function selectScope(id: string) {
    setScope(null);
    setConfigurations([]);
    setKube(null);
    setLogs("");
    setPod("");
    setContainer("");
    setJobs([]);
    setDetail(null);
    setConfirm(null);
    setError("");
    setNotice("");
    setScopeId(id);
  }
  async function mutate(path: string, body: unknown, method = "POST") {
    if (busy) return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const v = await request<{ id?: string; state?: string }>(path, {
        method,
        headers: { "Idempotency-Key": crypto.randomUUID() },
        body: JSON.stringify(body),
      });
      setNotice(
        v.state
          ? "Operación registrada. Consulta su progreso en Trabajos."
          : "Cambio guardado.",
      );
      setConfirm(null);
      setRevision((n) => n + 1);
      return true;
    } catch (e) {
      setError(e instanceof Error ? e.message : "No se pudo completar.");
      return false;
    } finally {
      setBusy(false);
    }
  }
  async function observe() {
    if (!scopeId || busy) return;
    setBusy(true);
    setError("");
    try {
      const v = await request<KubernetesState>(
        `/scopes/${scopeId}/kubernetes`,
        { signal: AbortSignal.timeout(12000) },
      );
      setKube(v);
      setPod("");
      setContainer("");
      setLogs("");
    } catch (e) {
      setKube(null);
      setError(e instanceof Error ? e.message : "Clúster no disponible.");
    } finally {
      setBusy(false);
    }
  }
  async function readLogs() {
    if (!scopeId || !pod || !container || busy) return;
    setBusy(true);
    setError("");
    try {
      const v = await request<{ text: string; truncated: boolean }>(
        `/scopes/${scopeId}/pods/${encodeURIComponent(pod)}/logs?` +
          new URLSearchParams({
            container,
            lines: "200",
            previous: String(previous),
          }),
        { signal: AbortSignal.timeout(10000) },
      );
      setLogs(v.text || "Sin líneas disponibles.");
      if (v.truncated) setNotice("Respuesta truncada a 128 KiB.");
    } catch (e) {
      setLogs("");
      setError(e instanceof Error ? e.message : "Logs no disponibles.");
    } finally {
      setBusy(false);
    }
  }
  async function openJob(j: Job) {
    setError("");
    try {
      setDetail(await request(`/runtime/jobs/${j.id}`));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Detalle no disponible.");
    }
  }
  function createScope(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!connection) return;
    const d = new FormData(e.currentTarget);
    setConfirm({
      title:
        "¿Crear este scope? El dominio se registra como metadato; no publica tráfico.",
      path: `/applications/${appId}/scopes`,
      body: {
        name: String(d.get("name")),
        environment: String(d.get("environment")),
        criticality: String(d.get("criticality")),
        region: String(d.get("region")),
        domain: String(d.get("domain")),
        clusterId: connection.clusterId,
        namespace: connection.namespace,
        replicas: Number(d.get("replicas")),
        cpuMilli: Number(d.get("cpuMilli")),
        memoryMiB: Number(d.get("memoryMiB")),
      },
    });
  }
  function saveConfiguration(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const d = new FormData(e.currentTarget);
    try {
      const variables = JSON.parse(String(d.get("variables"))),
        secretRefs = JSON.parse(String(d.get("secretRefs")));
      setConfirm({
        title:
          "¿Guardar una nueva revisión? Se aplicará al desplegar o ejecutar Aplicar configuración.",
        path: `/scopes/${scopeId}/configuration`,
        body: {
          revision: configurations[0]?.revision || 0,
          variables,
          secretRefs,
        },
      });
    } catch {
      setError("Usa objetos JSON válidos para variables y referencias.");
    }
  }
  return (
    <>
      <div className="info-strip">
        Orden de implementación: 1 Scopes → 2 Kubernetes → 3 Versiones y
        despliegues → 4 Configuración → 5 Trabajos. Las operaciones usan
        conexiones autorizadas; no ejecutan shell arbitrario.
      </div>
      <div className="panel section-body runtime-selectors">
        <label>
          Aplicación
          <select
            value={appId}
            disabled={busy}
            onChange={(e) => {
              selectScope("");
              setScopes([]);
              setVersions([]);
              setConnections([]);
              setBuildProvider("");
              setAppId(e.target.value);
            }}
          >
            <option value="">Selecciona una aplicación</option>
            {apps.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </select>
        </label>
        {appsMore && (
          <button
            className="secondary-button"
            onClick={() => setAppsOffset((n) => n + 100)}
          >
            Más aplicaciones
          </button>
        )}
        <label>
          Scope
          <select
            value={scopeId}
            disabled={busy}
            onChange={(e) => selectScope(e.target.value)}
          >
            <option value="">Selecciona un scope</option>
            {scopes.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name} / {s.environment}
              </option>
            ))}
          </select>
        </label>
        <button
          className="secondary-button"
          disabled={loading || busy}
          onClick={() => setRevision((n) => n + 1)}
        >
          Actualizar
        </button>
      </div>
      <div className="segmented runtime-tabs">
        {[
          [1, "Scopes"],
          [2, "Kubernetes"],
          [3, "Versiones / despliegues"],
          [4, "Configuración"],
          [5, "Trabajos"],
        ].map(([n, label]) => (
          <button
            key={n}
            className={tab === n ? "selected" : ""}
            onClick={() => setTab(Number(n))}
          >
            {n}. {label}
          </button>
        ))}
      </div>
      {error && (
        <div role="alert" className="form-message bad">
          {error}
        </div>
      )}
      {notice && (
        <div role="status" className="form-message good">
          {notice}
        </div>
      )}
      {loading && <p role="status">Consultando recursos…</p>}
      {confirm && (
        <section className="confirm-box">
          <p>{confirm.title}</p>
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
              onClick={() =>
                void mutate(confirm.path, confirm.body, confirm.method)
              }
            >
              Confirmar
            </button>
          </div>
        </section>
      )}
      {tab === 1 && (
        <section className="panel">
          <header className="panel-head">
            <h2>1 / SCOPES</h2>
          </header>
          <div className="section-body">
            {!appId ? (
              <p>Selecciona una aplicación del catálogo.</p>
            ) : (
              <>
                <p className="catalog-intro">
                  Cada scope conserva ambiente, criticidad, recursos y ejecución
                  independientes. Producción requiere administrador de equipo o
                  plataforma.
                </p>
                {scopes.map((s) => (
                  <button
                    key={s.id}
                    className="team-choice"
                    onClick={() => selectScope(s.id)}
                  >
                    <strong>{s.name}</strong>
                    <span>
                      {s.environment} · {s.criticality}
                    </span>
                    <small>
                      {s.clusterId} / {s.namespace} · {s.replicas} réplicas ·{" "}
                      {s.cpuMilli}m / {s.memoryMiB} MiB
                    </small>
                  </button>
                ))}
                <div className="pagination">
                  <button
                    className="secondary-button"
                    disabled={!scopeOffset}
                    onClick={() => setScopeOffset((n) => n - 20)}
                  >
                    Anterior
                  </button>
                  <button
                    className="secondary-button"
                    disabled={!scopeMore}
                    onClick={() => setScopeOffset((n) => n + 20)}
                  >
                    Más scopes
                  </button>
                </div>
                {!connections.length && (
                  <p className="muted">
                    Un operador debe vincular esta aplicación con un namespace
                    autorizado antes de crear scopes.
                  </p>
                )}
                {scope?.canEdit && (
                  <form
                    key={scope.id + ":" + scope.version}
                    className="application-form"
                    onSubmit={(e) => {
                      e.preventDefault();
                      const d = new FormData(e.currentTarget);
                      setConfirm({
                        title:
                          "¿Guardar los recursos deseados? Se aplicarán en la próxima operación de despliegue o aplicación de configuración.",
                        path: `/scopes/${scope.id}`,
                        method: "PUT",
                        body: {
                          name: scope.name,
                          environment: scope.environment,
                          clusterId: scope.clusterId,
                          namespace: scope.namespace,
                          version: scope.version,
                          criticality: String(d.get("criticality")),
                          region: String(d.get("region")),
                          domain: String(d.get("domain")),
                          replicas: Number(d.get("replicas")),
                          cpuMilli: Number(d.get("cpuMilli")),
                          memoryMiB: Number(d.get("memoryMiB")),
                        },
                      });
                    }}
                  >
                    <h3>Editar scope: {scope.name}</h3>
                    <fieldset disabled={busy}>
                      <div className="form-row">
                        <label>
                          Criticidad
                          <select
                            name="criticality"
                            defaultValue={scope.criticality}
                          >
                            <option>low</option>
                            <option>medium</option>
                            <option>high</option>
                          </select>
                        </label>
                        <label>
                          Región
                          <input
                            name="region"
                            maxLength={80}
                            defaultValue={scope.region}
                          />
                        </label>
                        <label>
                          Dominio (metadato)
                          <input
                            name="domain"
                            maxLength={253}
                            defaultValue={scope.domain}
                          />
                        </label>
                      </div>
                      <div className="form-row">
                        <label>
                          Réplicas
                          <input
                            name="replicas"
                            type="number"
                            min={1}
                            max={10}
                            defaultValue={scope.replicas}
                            required
                          />
                        </label>
                        <label>
                          CPU (m)
                          <input
                            name="cpuMilli"
                            type="number"
                            min={50}
                            max={2000}
                            defaultValue={scope.cpuMilli}
                            required
                          />
                        </label>
                        <label>
                          Memoria (MiB)
                          <input
                            name="memoryMiB"
                            type="number"
                            min={64}
                            max={2048}
                            defaultValue={scope.memoryMiB}
                            required
                          />
                        </label>
                      </div>
                    </fieldset>
                    <button className="primary-button" disabled={busy}>
                      Guardar recursos deseados
                    </button>
                  </form>
                )}
                {edit && connections.length > 0 && (
                  <form className="application-form" onSubmit={createScope}>
                    <h3>Crear scope</h3>
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
                          Ambiente
                          <select name="environment">
                            <option value="staging">Staging</option>
                            <option value="production">
                              Producción (requiere administrador)
                            </option>
                          </select>
                        </label>
                        <label>
                          Criticidad
                          <select name="criticality">
                            <option>low</option>
                            <option>medium</option>
                            <option>high</option>
                          </select>
                        </label>
                      </div>
                      <label>
                        Conexión
                        <select
                          value={connectionIndex}
                          onChange={(e) =>
                            setConnectionIndex(Number(e.target.value))
                          }
                        >
                          {connections.map((c, i) => (
                            <option key={c.clusterId + c.namespace} value={i}>
                              {c.clusterName} / {c.namespace}
                            </option>
                          ))}
                        </select>
                      </label>
                      <div className="form-row">
                        <label>
                          Réplicas
                          <input
                            name="replicas"
                            type="number"
                            defaultValue={1}
                            min={1}
                            max={10}
                            required
                          />
                        </label>
                        <label>
                          Límite CPU (m)
                          <input
                            name="cpuMilli"
                            type="number"
                            defaultValue={250}
                            min={50}
                            max={2000}
                            required
                          />
                        </label>
                        <label>
                          Límite memoria (MiB)
                          <input
                            name="memoryMiB"
                            type="number"
                            defaultValue={128}
                            min={64}
                            max={2048}
                            required
                          />
                        </label>
                      </div>
                      <label>
                        Región
                        <input name="region" maxLength={80} />
                      </label>
                      <label>
                        Dominio declarado
                        <input
                          name="domain"
                          maxLength={253}
                          placeholder="api.example.com"
                        />
                      </label>
                    </fieldset>
                    <button className="primary-button" disabled={busy}>
                      Crear scope
                    </button>
                  </form>
                )}
                {scope && (
                  <p className="muted">
                    Workload administrado: {scope.workload}. Nombre, aplicación,
                    ambiente y conexión quedan fijos para conservar el
                    aislamiento.
                  </p>
                )}
              </>
            )}
          </div>
        </section>
      )}
      {tab === 2 && (
        <section className="panel">
          <header className="panel-head">
            <h2>2 / OPERACIÓN KUBERNETES</h2>
            <button
              className="secondary-button"
              disabled={!scope || busy}
              onClick={() => void observe()}
            >
              Consultar estado real
            </button>
          </header>
          <div className="section-body">
            {!scope ? (
              <p>Selecciona un scope.</p>
            ) : (
              <>
                <p>
                  {scope.clusterId} / {scope.namespace} / {scope.workload}
                </p>
                {kube && (
                  <>
                    {!kube.deployed ? (
                      <p>{kube.message}</p>
                    ) : (
                      <>
                        <p>
                          Réplicas listas: {kube.status?.readyReplicas || 0} ·
                          actualizadas: {kube.status?.updatedReplicas || 0}.
                          Recibido:{" "}
                          {kube.receivedAt
                            ? new Date(kube.receivedAt).toLocaleTimeString(
                                "es-CO",
                              )
                            : ""}
                        </p>
                        <div className="table-wrap">
                          <table>
                            <thead>
                              <tr>
                                <th>Pod</th>
                                <th>Estado</th>
                                <th>Contenedores / reinicios</th>
                              </tr>
                            </thead>
                            <tbody>
                              {kube.pods.map((p) => (
                                <tr key={p.metadata.name}>
                                  <td>{p.metadata.name}</td>
                                  <td>{p.status.phase}</td>
                                  <td>
                                    {p.status.containerStatuses
                                      ?.map(
                                        (c) =>
                                          `${c.name}: ${c.ready ? "listo" : "no listo"} / ${c.restartCount}`,
                                      )
                                      .join(" · ")}
                                  </td>
                                </tr>
                              ))}
                            </tbody>
                          </table>
                        </div>
                        {kube.hasMorePods && <p>Se muestran hasta 100 pods.</p>}
                        <h3>Consumo medido</h3>
                        {!kube.metricsAvailable ? (
                          <p className="muted">
                            Metrics Server no está disponible o no está
                            autorizado. No se muestran valores simulados.
                          </p>
                        ) : (
                          kube.usage?.map((p) => (
                            <p key={p.pod}>
                              {p.pod}:{" "}
                              {p.containers
                                .map(
                                  (c) =>
                                    `${c.name} CPU ${c.usage.cpu} / memoria ${c.usage.memory}`,
                                )
                                .join(" · ")}
                            </p>
                          ))
                        )}
                        <h3>Eventos autorizados</h3>
                        {!kube.eventsAvailable && (
                          <p>Eventos no disponibles.</p>
                        )}
                        {kube.events.map((e, i) => (
                          <p key={i}>
                            {e.type} / {e.reason}: {e.message}
                          </p>
                        ))}
                        <h3>Logs por contenedor</h3>
                        <div className="form-row">
                          <label>
                            Pod
                            <select
                              value={pod}
                              disabled={busy}
                              onChange={(e) => {
                                setPod(e.target.value);
                                setContainer("");
                                setLogs("");
                              }}
                            >
                              <option value="">Seleccionar</option>
                              {kube.pods.map((p) => (
                                <option key={p.metadata.name}>
                                  {p.metadata.name}
                                </option>
                              ))}
                            </select>
                          </label>
                          <label>
                            Contenedor
                            <select
                              value={container}
                              disabled={busy}
                              onChange={(e) => {
                                setContainer(e.target.value);
                                setLogs("");
                              }}
                            >
                              <option value="">Seleccionar</option>
                              {kube.pods
                                .find((p) => p.metadata.name === pod)
                                ?.spec.containers.map((c) => (
                                  <option key={c.name}>{c.name}</option>
                                ))}
                            </select>
                          </label>
                          <label>
                            <input
                              type="checkbox"
                              checked={previous}
                              onChange={(e) => setPrevious(e.target.checked)}
                            />{" "}
                            Instancia anterior
                          </label>
                          <button
                            className="primary-button"
                            disabled={busy || !container}
                            onClick={() => void readLogs()}
                          >
                            Últimas 200 líneas
                          </button>
                        </div>
                        {logs && (
                          <pre className="historical-logs runtime-log">
                            {logs}
                          </pre>
                        )}
                      </>
                    )}
                  </>
                )}
                {scope.canEdit && (
                  <div className="form-actions">
                    <button
                      className="secondary-button"
                      disabled={busy}
                      onClick={() =>
                        setConfirm({
                          title:
                            "¿Reiniciar este workload mediante un rollout auditado?",
                          path: `/scopes/${scopeId}/actions/restart`,
                          body: {},
                        })
                      }
                    >
                      Reiniciar
                    </button>
                    <form
                      onSubmit={(e) => {
                        e.preventDefault();
                        setConfirm({
                          title: "¿Cambiar las réplicas de este scope?",
                          path: `/scopes/${scopeId}/actions/scale`,
                          body: {
                            replicas: Number(
                              new FormData(e.currentTarget).get("replicas"),
                            ),
                          },
                        });
                      }}
                    >
                      <input
                        aria-label="Réplicas deseadas"
                        name="replicas"
                        type="number"
                        min={1}
                        max={10}
                        defaultValue={scope.replicas}
                      />
                      <button className="secondary-button" disabled={busy}>
                        Escalar
                      </button>
                    </form>
                  </div>
                )}
              </>
            )}
          </div>
        </section>
      )}
      {tab === 3 && (
        <section className="panel">
          <header className="panel-head">
            <h2>3 / VERSIONES Y DESPLIEGUES</h2>
          </header>
          <div className="section-body">
            {!appId ? (
              <p>Selecciona una aplicación.</p>
            ) : (
              <>
                <p className="catalog-intro">
                  Las versiones se registran desde runners autorizados por
                  digest. Build configurado:{" "}
                  {buildProvider || "pendiente de conexión"}.
                </p>
                {buildProvider && edit && (
                  <form
                    className="application-form"
                    onSubmit={(e) => {
                      e.preventDefault();
                      const d = new FormData(e.currentTarget);
                      setConfirm({
                        title: `¿Solicitar build mediante ${buildProvider}?`,
                        path: `/applications/${appId}/builds`,
                        body: {
                          name: String(d.get("name")),
                          sourceRevision: String(d.get("revision")),
                        },
                      });
                    }}
                  >
                    <label>
                      Nombre de versión
                      <input
                        name="name"
                        maxLength={40}
                        required
                        pattern="[a-z][a-z0-9-]{0,39}"
                      />
                    </label>
                    <label>
                      Revisión completa de origen
                      <input
                        name="revision"
                        maxLength={64}
                        required
                        pattern="[a-f0-9]{40}([a-f0-9]{24})?"
                        placeholder="SHA completo del commit; hash de contenido en el laboratorio"
                      />
                    </label>
                    <button className="primary-button" disabled={busy}>
                      Solicitar build
                    </button>
                  </form>
                )}
                {versions.map((v) => (
                  <div className="runtime-version" key={v.id}>
                    <strong>
                      {v.name} · {v.provenance}
                    </strong>
                    <small>{v.sourceRevision}</small>
                    <code>{v.image}</code>
                    {scope?.canEdit && (
                      <button
                        className="secondary-button"
                        disabled={busy}
                        onClick={() =>
                          setConfirm({
                            title: `¿Desplegar ${v.name} en ${scope.name}? Usará la configuración guardada más reciente.`,
                            path: `/scopes/${scopeId}/actions/deploy`,
                            body: { versionId: v.id },
                          })
                        }
                      >
                        Desplegar en scope seleccionado
                      </button>
                    )}
                  </div>
                ))}
                {!versions.length && (
                  <p className="muted">No hay versiones registradas.</p>
                )}
                <div className="pagination">
                  <button
                    className="secondary-button"
                    disabled={!versionOffset}
                    onClick={() => setVersionOffset((n) => n - 20)}
                  >
                    Anterior
                  </button>
                  <button
                    className="secondary-button"
                    disabled={!versionsMore}
                    onClick={() => setVersionOffset((n) => n + 20)}
                  >
                    Más versiones
                  </button>
                </div>
                <p className="muted">
                  La recuperación se solicita desde un despliegue anterior
                  completado en Trabajos. Conserva imagen, configuración y
                  recursos de ese snapshot.
                </p>
              </>
            )}
          </div>
        </section>
      )}
      {tab === 4 && (
        <section className="panel">
          <header className="panel-head">
            <h2>4 / CONFIGURACIÓN Y REFERENCIAS A SECRETOS</h2>
          </header>
          <div className="section-body">
            {!scope ? (
              <p>Selecciona un scope.</p>
            ) : (
              <>
                <p className="catalog-intro">
                  Solo variables no sensibles. Las referencias usan aliases
                  autorizados por el operador; Mini Fury nunca consulta ni
                  devuelve valores de Secrets.
                </p>
                <p>
                  Aliases disponibles:{" "}
                  {connections
                    .find(
                      (c) =>
                        c.clusterId === scope.clusterId &&
                        c.namespace === scope.namespace,
                    )
                    ?.secretAliases.join(", ") || "ninguno configurado"}
                </p>
                {scope.canEdit && configurationOffset === 0 && (
                  <form
                    key={scope.id + revision + configurations[0]?.revision}
                    className="application-form"
                    onSubmit={saveConfiguration}
                  >
                    <fieldset disabled={busy}>
                      <label>
                        Variables no sensibles (JSON)
                        <textarea
                          name="variables"
                          rows={7}
                          defaultValue={JSON.stringify(
                            configurations[0]?.variables || {},
                            null,
                            2,
                          )}
                        />
                      </label>
                      <label>
                        Referencias ENV → alias (JSON)
                        <textarea
                          name="secretRefs"
                          rows={5}
                          defaultValue={JSON.stringify(
                            configurations[0]?.secretRefs || {},
                            null,
                            2,
                          )}
                        />
                      </label>
                    </fieldset>
                    <button className="primary-button" disabled={busy}>
                      Guardar nueva revisión
                    </button>
                    {scope.canEdit && (
                      <button
                        className="secondary-button"
                        type="button"
                        disabled={busy}
                        onClick={() =>
                          setConfirm({
                            title:
                              "¿Aplicar la configuración guardada más reciente? Provocará un rollout del workload.",
                            path: `/scopes/${scopeId}/actions/configure`,
                            body: {},
                          })
                        }
                      >
                        Aplicar configuración
                      </button>
                    )}
                  </form>
                )}
                <h3>Historial de revisiones</h3>
                {configurations.map((c) => (
                  <details key={c.id}>
                    <summary>Revisión {c.revision}</summary>
                    <pre className="runtime-log">
                      {JSON.stringify(
                        { variables: c.variables, secretRefs: c.secretRefs },
                        null,
                        2,
                      )}
                    </pre>
                  </details>
                ))}
                <div className="pagination">
                  <button
                    className="secondary-button"
                    disabled={!configurationOffset}
                    onClick={() => setConfigurationOffset((n) => n - 20)}
                  >
                    Anterior
                  </button>
                  <button
                    className="secondary-button"
                    disabled={!configurationMore}
                    onClick={() => setConfigurationOffset((n) => n + 20)}
                  >
                    Más revisiones
                  </button>
                </div>
              </>
            )}
          </div>
        </section>
      )}
      {tab === 5 && (
        <section className="panel">
          <header className="panel-head">
            <h2>5 / CENTRO DE TRABAJOS</h2>
            <span className="muted">Actualización cada 5 s</span>
          </header>
          <div className="section-body">
            <div className="form-row">
              <label>
                Estado
                <select
                  value={state}
                  onChange={(e) => {
                    setState(e.target.value);
                    setPage(0);
                  }}
                >
                  <option value="">Todos</option>
                  {Object.entries(stateLabel).map(([v, label]) => (
                    <option value={v} key={v}>
                      {label}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Operación
                <select
                  value={kind}
                  onChange={(e) => {
                    setKind(e.target.value);
                    setPage(0);
                  }}
                >
                  <option value="">Todas</option>
                  {[
                    "build",
                    "deploy",
                    "rollback",
                    "restart",
                    "scale",
                    "configure",
 "service-provision", "service-suspend", "service-resume",
                  ].map((v) => (
                    <option key={v}>{v}</option>
                  ))}
                </select>
              </label>
            </div>
            {!jobs.length && (
              <p className="muted">No hay trabajos con estos filtros.</p>
            )}
            {jobs.map((j) => (
              <div className="runtime-job" key={j.id}>
                <button className="table-link" onClick={() => void openJob(j)}>
                  {j.kind} · {stateLabel[j.state]} · {j.id.slice(0, 8)}
                </button>
                <small>
                  {j.phase} · intento {j.attempt} ·{" "}
                  {new Date(j.createdAt).toLocaleString("es-CO")}
                </small>
                <p>{j.message}</p>
                {(edit || user.role === "admin") && (
                  <div className="form-actions">
                    {["queued", "running"].includes(j.state) &&
                      !j.cancelRequested && (
                        <button
                          className="text-button"
                          disabled={busy}
                          onClick={() =>
                            setConfirm({
                              title:
                                "¿Solicitar cancelación? No revierte cambios que ya se hayan aplicado.",
                              path: `/runtime/jobs/${j.id}/cancel`,
                              body: {},
                            })
                          }
                        >
                          Cancelar
                        </button>
                      )}
                    {["failed", "cancelled"].includes(j.state) && (
                      <button
                        className="secondary-button"
                        disabled={busy}
                        onClick={() =>
                          setConfirm({
                            title: "¿Crear un nuevo intento de esta operación?",
                            path: `/runtime/jobs/${j.id}/retry`,
                            body: {},
                          })
                        }
                      >
                        Reintentar
                      </button>
                    )}
                    {j.scopeId &&
                      j.state === "succeeded" &&
                      ["deploy", "rollback", "configure"].includes(j.kind) && (
                        <button
                          className="secondary-button"
                          disabled={busy}
                          onClick={() =>
                            setConfirm({
                              title:
                                "¿Recuperar el snapshot de este despliegue? Incluye imagen y configuración anteriores.",
                              path: `/scopes/${j.scopeId}/actions/rollback`,
                              body: { previousJobId: j.id },
                            })
                          }
                        >
                          Recuperar esta versión
                        </button>
                      )}
                  </div>
                )}
              </div>
            ))}
            <div className="pagination">
              <button
                className="secondary-button"
                disabled={!page}
                onClick={() => setPage((n) => n - 1)}
              >
                Anterior
              </button>
              <button
                className="secondary-button"
                disabled={!hasMore}
                onClick={() => setPage((n) => n + 1)}
              >
                Siguiente
              </button>
            </div>
            {detail && (
              <div className="confirm-box">
                <h3>
                  {detail.job.kind} / {detail.job.id}
                </h3>
                <button
                  className="text-button"
                  onClick={() => void openJob(detail.job)}
                >
                  Actualizar detalle
                </button>
                <button className="text-button" onClick={() => setDetail(null)}>
                  Cerrar
                </button>
                {detail.events.map((e, i) => (
                  <p key={i}>
                    {new Date(e.createdAt).toLocaleTimeString("es-CO")} ·{" "}
                    {e.state} / {e.phase}: {e.message}
                  </p>
                ))}
              </div>
            )}
          </div>
        </section>
      )}
    </>
  );
}
