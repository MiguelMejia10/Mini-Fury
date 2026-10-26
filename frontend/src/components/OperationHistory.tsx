import { useEffect, useState } from "react";
import { request } from "../services/http";
interface Integration {
  logsConfigured: boolean;
  metricsConfigured: boolean;
  grafanaUrl: string;
  webhookConfigured: boolean;
  deliveryPending: number;
  deliveryFailed: number;
}
interface Line {
  timestamp: string;
  text: string;
}
interface Series {
  result: Array<{ values: Array<[number, string]> }>;
}
export function OperationHistory() {
  const [config, setConfig] = useState<Integration | null>(null),
    [minutes, setMinutes] = useState(60),
    [metric, setMetric] = useState("requests"),
    [level, setLevel] = useState(""),
    [contains, setContains] = useState(""),
    [lines, setLines] = useState<Line[]>([]),
    [points, setPoints] = useState<Array<[number, number]>>([]),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [truncated, setTruncated] = useState(false),
    [revision, setRevision] = useState(0),
    [loaded, setLoaded] = useState(false);
  useEffect(() => {
    const c = new AbortController();
    request<Integration>("/operations/integrations", { signal: c.signal })
      .then(setConfig)
      .catch((e) => {
        if (!c.signal.aborted) setError(e.message);
      });
    return () => c.abort();
  }, [revision]);
  useEffect(() => {
    if (!config?.metricsConfigured) {
      setPoints([]);
      return;
    }
    const c = new AbortController();
    const now = new Date();
    const query = new URLSearchParams({
      metric,
      from: new Date(now.getTime() - minutes * 60000).toISOString(),
      until: now.toISOString(),
    });
    request<Series>("/operations/history?" + query, {
      signal: AbortSignal.any([c.signal, AbortSignal.timeout(10000)]),
    })
      .then((s) => {
        if (!c.signal.aborted)
          setPoints(
            (s.result[0]?.values || [])
              .map(([t, v]) => [t, Number(v)] as [number, number])
              .filter(([, v]) => Number.isFinite(v)),
          );
      })
      .catch((e) => {
        if (!c.signal.aborted) {
          setPoints([]);
          setError(e.message);
        }
      });
    return () => c.abort();
  }, [config?.metricsConfigured, minutes, metric, revision]);
  async function load() {
    setBusy(true);
    setError("");
    setLoaded(false);
    const now = new Date();
    try {
      const q = new URLSearchParams({
        from: new Date(now.getTime() - minutes * 60000).toISOString(),
        until: now.toISOString(),
        limit: "200",
        level,
        contains,
      });
      const data = await request<{ lines: Line[]; truncated: boolean }>(
        "/operations/logs?" + q,
        { signal: AbortSignal.timeout(10000) },
      );
      setLines(data.lines);
      setTruncated(data.truncated);
      setLoaded(true);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Histórico no disponible.");
    } finally {
      setBusy(false);
    }
  }
  const max = Math.max(1e-6, ...points.map(([, v]) => v));
  const coordinates = points
    .map(
      ([, v], i) =>
        `${10 + (i / Math.max(1, points.length - 1)) * 780},${150 - (v / max) * 130}`,
    )
    .join(" ");
  const safeGrafana =
    config?.grafanaUrl && /^https?:\/\//.test(config.grafanaUrl)
      ? config.grafanaUrl
      : "";
  return (
    <section className="panel">
      <header className="panel-head">
        <h2>HISTÓRICO / LOGS Y MÉTRICAS</h2>
        {safeGrafana && (
          <a
            className="secondary-button"
            href={safeGrafana}
            target="_blank"
            rel="noopener noreferrer"
          >
            Abrir Grafana
          </a>
        )}
      </header>
      <div className="admin-body section-body">
        {error && (
          <div className="form-message bad" role="alert">
            {error}
            <button
              className="secondary-button"
              onClick={() => setRevision((n) => n + 1)}
            >
              Reintentar
            </button>
          </div>
        )}
        {!config && !error && <p role="status">Consultando integraciones…</p>}
        {config && (
          <>
            <p className="catalog-intro">
              Loki: {config.logsConfigured ? "configurado" : "pendiente"} ·
              Prometheus:{" "}
              {config.metricsConfigured ? "configurado" : "pendiente"} · Alertas
              externas:{" "}
              {config.webhookConfigured ? "configuradas" : "sin destino"}.
              Entregas pendientes: {config.deliveryPending}; fallidas:{" "}
              {config.deliveryFailed}.
            </p>
            <div className="form-row">
              <label>
                Periodo
                <select
                  value={minutes}
                disabled={busy}
                  onChange={(e) => {
                    setMinutes(Number(e.target.value));
                    setLoaded(false);
                    setLines([]);
                  }}
                >
                  <option value={15}>15 minutos</option>
                  <option value={60}>1 hora</option>
                  <option value={360}>6 horas</option>
                  <option value={1440}>24 horas</option>
                </select>
              </label>
              <label>
                Métrica
                <select
                  value={metric}
                  onChange={(e) => setMetric(e.target.value)}
                >
                  <option value="requests">Solicitudes / segundo</option>
                  <option value="errors">Errores / segundo</option>
                  <option value="latency">Latencia p95 / segundos</option>
                </select>
              </label>
              <button
                className="secondary-button"
                onClick={() => setRevision((n) => n + 1)}
              >
                Actualizar métricas
              </button>
            </div>
            {config.metricsConfigured &&
              (points.length > 1 ? (
                <figure className="history-chart">
                  <figcaption>
                    {metric === "latency"
                      ? "Latencia p95 estimada (s)"
                      : metric === "errors"
                        ? "Errores 5xx por segundo"
                        : "Solicitudes por segundo"}{" "}
                    · últimos {minutes} minutos
                  </figcaption>
                  <svg
                    viewBox="0 0 800 180"
                    role="img"
                    aria-label="Serie temporal de la métrica seleccionada"
                  >
                    <line
                      x1="10"
                      y1="150"
                      x2="790"
                      y2="150"
                      stroke="currentColor"
                      opacity="0.3"
                    />
                    <polyline
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                      points={coordinates}
                    />
                    <text x="10" y="175" fill="currentColor" fontSize="12">
                      {new Date(points[0][0] * 1000).toLocaleTimeString(
                        "es-CO",
                      )}
                    </text>
                    <text
                      x="790"
                      y="175"
                      textAnchor="end"
                      fill="currentColor"
                      fontSize="12"
                    >
                      {new Date(
                        points[points.length - 1][0] * 1000,
                      ).toLocaleTimeString("es-CO")}
                    </text>
                  </svg>
                  <small>
                    Máximo observado: {max.toFixed(3)} · Fuente: Prometheus. Sin
                    datos para periodos anteriores a su instalación.
                  </small>
                </figure>
              ) : (
                <p className="muted">
                  Aún no hay suficientes muestras históricas en este periodo.
                </p>
              ))}
            <p className="catalog-intro">
              Logs operativos de Mini Fury, con retención configurada de siete
              días. Los pods de aplicaciones requieren conectar Kubernetes; no
              están incluidos en este flujo.
            </p>
            {config.logsConfigured && (
              <>
                <div className="form-row">
                  <label>
                    Nivel
                    <select
                      value={level}
                disabled={busy}
                      onChange={(e) => setLevel(e.target.value)}
                    >
                      <option value="">Todos</option>
                      <option value="info">Info</option>
                      <option value="warn">Advertencia</option>
                      <option value="error">Error</option>
                    </select>
                  </label>
                  <label>
                    Contiene
                    <input
                      maxLength={200}
                disabled={busy}
                      value={contains}
                      onChange={(e) => setContains(e.target.value)}
                      placeholder="Texto literal"
                    />
                  </label>
                  <button
                    className="primary-button"
                    disabled={busy}
                    onClick={() => void load()}
                  >
                    {busy ? "Consultando…" : "Consultar logs"}
                  </button>
                </div>
                {loaded && !lines.length && (
                  <p className="muted">No hay registros para estos filtros.</p>
                )}
                {truncated && (
                  <p className="muted">
                    Se muestran las 200 líneas más recientes. Reduce el periodo
                    o filtra el texto.
                  </p>
                )}
                {lines.length > 0 && (
                  <div
                    className="historical-logs"
                    role="log"
                    aria-label="Logs históricos"
                  >
                    {lines.map((line, i) => (
                      <pre key={line.timestamp + "-" + i}>{line.text}</pre>
                    ))}
                  </div>
                )}
              </>
            )}
          </>
        )}
      </div>
    </section>
  );
}
