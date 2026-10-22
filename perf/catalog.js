import http from "k6/http";
import { check, fail, sleep } from "k6";
const base = "http://perf-api:8080";
export const options = {
  setupTimeout: "60s",
  scenarios: {
    catalog: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "10s", target: 10 },
        { duration: "20s", target: 10 },
        { duration: "10s", target: 25 },
        { duration: "20s", target: 25 },
        { duration: "5s", target: 0 },
      ],
      gracefulRampDown: "5s",
    },
  },
  thresholds: {
    "http_req_failed{phase:load}": ["rate<0.01"],
    "http_req_duration{phase:load}": ["p(95)<500", "p(99)<1000"],
    checks: ["rate>0.99"],
  },
};
export function setup() {
  let response;
  for (let i = 0; i < 20; i++) {
    response = http.get(base + "/api/health", {
      timeout: "2s",
      tags: { phase: "setup" },
    });
    if (response.status === 200) break;
    sleep(1);
  }
  if (!response || response.status !== 200) fail("API de carga no disponible");
  const login = http.post(
    base + "/api/auth/login",
    JSON.stringify({
      email: "load@mini-fury.local",
      password: "Disposable-Load-Only-2026!",
    }),
    {
      headers: {
        Origin: "http://perf.local",
        "Content-Type": "application/json",
      },
      tags: { phase: "setup" },
    },
  );
  if (login.status !== 200) fail("No se pudo iniciar la sesión de fixture");
  const cookie = login.cookies.mini_fury_session?.[0]?.value;
  if (!cookie) fail("No se recibió sesión");
  const app = http.post(
    base + "/api/applications",
    JSON.stringify({
      name: "load-api",
      repository: "https://gitlab.com/example/load-api",
      port: 8080,
      environment: "staging",
      owner: "Load fixture",
      runtime: "Go",
      description: "Disposable load fixture",
      tags: ["load"],
    }),
    {
      headers: {
        Origin: "http://perf.local",
        "Content-Type": "application/json",
        Cookie: `mini_fury_session=${cookie}`,
        "X-CSRF-Token": login.json("csrfToken"),
      },
      tags: { phase: "setup" },
    },
  );
  if (app.status !== 201 && app.status !== 409)
    fail("No se pudo preparar el catálogo de prueba");
  return { cookie };
}
export default function (data) {
  const response = http.get(
    base + "/api/applications?environment=staging&limit=20",
    {
      headers: { Cookie: `mini_fury_session=${data.cookie}` },
      tags: { phase: "load", name: "GET catalog (authenticated)" },
    },
  );
  check(response, {
    "catálogo autenticado disponible": (r) =>
      r.status === 200 && Array.isArray(r.json("applications")),
  });
  sleep(0.1);
}
export function handleSummary(data) {
  return {
    "/results/catalog-summary.json": JSON.stringify(data, null, 2),
    stdout:
      JSON.stringify(
        {
          requests: data.metrics.http_reqs?.values,
          p95: data.metrics["http_req_duration{phase:load}"]?.values,
          failures: data.metrics["http_req_failed{phase:load}"]?.values,
          thresholdsPassed: Object.values(data.metrics).every(
            (m) =>
              !m.thresholds || Object.values(m.thresholds).every((t) => t.ok),
          ),
        },
        null,
        2,
      ) + "\n",
  };
}
