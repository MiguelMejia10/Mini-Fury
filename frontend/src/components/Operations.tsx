import { useEffect, useState } from "react";
import { request } from "../services/http";
import { OperationHistory } from "./OperationHistory";
import { useAuth } from "./Auth";
interface Snapshot {
  instance: string;
  uptimeSeconds: number;
  requests: number;
  errors: number;
  requestsPerSecond: number;
  errorRate: number;
  p95UpperMs: number | null;
  inFlight: number;
  goroutines: number;
  heapBytes: number;
  databaseHealthy: boolean;
  dbOpen: number;
  dbInUse: number;
  dbMax: number;
  dbWaitCount: number;
  windowSeconds: number;
}
interface Alert {
  id: string;
  instance: string;
  rule: string;
  severity: string;
  message: string;
  active: boolean;
  stale: boolean;
  acknowledgedAt: string | null;
  updatedAt: string;
  occurrences: number;
}
export function Operations() {
  const { user } = useAuth();
  const [data, setData] = useState<Snapshot | null>(null);
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [error, setError] = useState("");
  const [received, setReceived] = useState("");
  const [revision, setRevision] = useState(0);
  const [offset, setOffset] = useState(0);
  const [more, setMore] = useState(false);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (user.role !== "admin") return;
    const controller = new AbortController();
    let timer: number | undefined;
    async function refresh() {
      if (document.hidden) {
        timer = window.setTimeout(refresh, 10000);
        return;
      }
      try {
        const signal = AbortSignal.any([
          controller.signal,
          AbortSignal.timeout(12000),
        ]);
        const [s, a] = await Promise.all([
          request<Snapshot>("/operations/status", { signal }),
          request<{ alerts: Alert[]; hasMore: boolean }>(
            `/operations/alerts?limit=20&offset=${offset}`,
            { signal },
          ),
        ]);
        if (controller.signal.aborted) return;
        setData(s);
        setAlerts(a.alerts);
        setMore(a.hasMore);
        setReceived(new Date().toLocaleTimeString("es-CO"));
        setError("");
      } catch (e) {
        if (!controller.signal.aborted)
          setError(
            e instanceof Error
              ? e.message
              : "No se pudieron consultar las señales.",
          );
      } finally {
        if (!controller.signal.aborted)
          timer = window.setTimeout(refresh, 10000);
      }
    }
    void refresh();
    return () => {
      controller.abort();
      window.clearTimeout(timer);
    };
  }, [revision, offset, user.role]);
  async function ack(id: string) {
    setBusy(true);
    try {
      await request(`/operations/alerts/${id}/ack`, {
        method: "POST",
        body: "{}",
      });
      setRevision((n) => n + 1);
    } catch (e) {
      setError(
        e instanceof Error ? e.message : "No se pudo reconocer la alerta.",
      );
    } finally {
      setBusy(false);
    }
  }
  if (user.role !== "admin")
    return (
      <div className="info-strip">
        Esta vista contiene métricas de Mini Fury y requiere un administrador de
        plataforma. Las métricas de tus aplicaciones llegarán con los
        despliegues reales.
      </div>
    );
  return (
    <>
      <OperationHistory />
      {error && (
        <div role="alert" className="form-message bad">
          {error} Los valores previos pueden estar desactualizados.
          <button
            className="secondary-button"
            onClick={() => setRevision((n) => n + 1)}
          >
            Reintentar
          </button>
        </div>
      )}
      <div className="info-strip">
        Métricas reales de la instancia de Mini Fury. No son métricas de las
        aplicaciones registradas. Ventana móvil de hasta cinco minutos;
        actualización cada diez segundos.
      </div>
      {!data && !error && (
        <p role="status" className="empty">
          Consultando métricas…
        </p>
      )}
      {data && (
        <>
          <div className="section-label">
            <span>INSTANCIA / {data.instance}</span>
            <span>Última recepción: {received}</span>
          </div>
          <div className="stats">
            {[
              ["SOLICITUDES / SEG", data.requestsPerSecond.toFixed(2)],
              ["ERRORES 5XX", `${(data.errorRate * 100).toFixed(2)}%`],
              [
                "P95 ESTIMADO",
                data.p95UpperMs === null
                  ? data.requests
                    ? ">30 s"
                    : "Sin muestras"
                  : `≤ ${data.p95UpperMs} ms`,
              ],
              [
                "POSTGRESQL",
                data.databaseHealthy ? "Disponible" : "Sin conexión",
              ],
            ].map(([label, value]) => (
              <div className="stat" key={label}>
                <div>{label}</div>
                <strong className="operation-value">{value}</strong>
              </div>
            ))}
          </div>
          <section className="panel">
            <header className="panel-head">
              <h2>RECURSOS DE MINI FURY</h2>
              <span className="muted">
                Uptime: {Math.floor(data.uptimeSeconds / 60)} min
              </span>
            </header>
            <div className="resources">
              {[
                ["Solicitudes en vuelo", data.inFlight],
                ["Goroutines", data.goroutines],
                ["Heap Go", `${(data.heapBytes / 1048576).toFixed(1)} MiB`],
                [
                  "Conexiones DB",
                  `${data.dbInUse} en uso / ${data.dbOpen} abiertas / ${data.dbMax} máx.`,
                ],
              ].map(([label, value]) => (
                <div className="meter" key={label}>
                  <span className="muted">{label}</span>
                  <p className="operation-resource">{value}</p>
                </div>
              ))}
            </div>
            <div className="panel-note">
              {data.requests} solicitudes y {data.errors} errores en{" "}
              {Math.round(data.windowSeconds)} s. Esperas acumuladas del pool:{" "}
              {data.dbWaitCount}. El p95 es un límite superior de histograma, no
              una medición exacta.
            </div>
          </section>
        </>
      )}
      <section className="panel">
        <header className="panel-head">
          <h2>ALERTAS DE PLATAFORMA</h2>
          <button
            className="text-button"
            onClick={() => setRevision((n) => n + 1)}
          >
            Actualizar
          </button>
        </header>
        <div className="admin-body section-body">
          {alerts.map((a) => (
            <div
              key={a.id}
              className={`alert-row ${a.active && !a.stale ? "warn" : "muted"}`}
            >
              <div>
                <strong>
                  {a.stale ? "SIN SEÑAL" : a.active ? "ACTIVA" : "RESUELTA"} ·{" "}
                  {a.rule}
                </strong>
                <p>{a.message}</p>
                <small>
                  {a.instance} · {new Date(a.updatedAt).toLocaleString("es-CO")}{" "}
                  · ocurrencias {a.occurrences}
                </small>
              </div>
              {a.active && !a.stale && !a.acknowledgedAt && (
                <button
                  className="secondary-button"
                  disabled={busy}
                  onClick={() => void ack(a.id)}
                >
                  Reconocer alerta
                </button>
              )}
              {a.acknowledgedAt && (
                <span className="registered-status">Reconocida</span>
              )}
            </div>
          ))}
          {!alerts.length && (
            <p className="muted">
              No hay alertas registradas en esta página. Con poco tráfico las
              reglas no se evalúan como fallos.
            </p>
          )}
          <div className="pagination">
            <button
              className="secondary-button"
              disabled={!offset}
              onClick={() => setOffset((n) => n - 20)}
            >
              Anterior
            </button>
            <button
              className="secondary-button"
              disabled={!more}
              onClick={() => setOffset((n) => n + 20)}
            >
              Siguiente
            </button>
          </div>
        </div>
      </section>
      <section className="panel">
        <header className="panel-head">
          <h2>EXPORTACIÓN Y CARGA</h2>
        </header>
        <div className="admin-body section-body">
          <p className="catalog-intro">
            Las métricas acumuladas se exponen en formato Prometheus en{" "}
            <code>/api/operations/metrics</code>, con sesión de administrador.
            Para un recolector externo habrá que configurar una identidad de
            servicio.
          </p>
          <p className="catalog-intro">
            La prueba reproducible se ejecuta con{" "}
            <code>sh scripts/load-test.sh</code>. Usa API y PostgreSQL
            desechables; no utiliza los usuarios ni datos locales. El resultado
            queda en <code>.local/perf/catalog-summary.json</code>.
          </p>
        </div>
      </section>
    </>
  );
}
