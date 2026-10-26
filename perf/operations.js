import http from "k6/http";
import { check, fail, sleep } from "k6";
import exec from "k6/execution";
import { Counter } from "k6/metrics";
const base = "http://perf-api:8080",
  origin = "http://perf.local";
const writes = new Counter("catalog_writes"),
  conflicts = new Counter("version_conflicts");
export const options = {
  setupTimeout: "180s",
  summaryTrendStats: ["avg", "min", "med", "max", "p(90)", "p(95)", "p(99)"],
  scenarios: {
    operations: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "10s", target: 10 },
        { duration: "15s", target: 10 },
        { duration: "10s", target: 25 },
        { duration: "20s", target: 25 },
        { duration: "10s", target: 50 },
        { duration: "30s", target: 50 },
        { duration: "5s", target: 0 },
      ],
      gracefulRampDown: "10s",
    },
  },
  thresholds: {
    "http_req_failed{phase:load}": ["rate<0.01"],
    "http_req_duration{phase:load}": ["p(95)<500", "p(99)<1000"],
    checks: ["rate>0.99"],
  },
};
function request(
  method,
  path,
  body,
  session,
  phase = "setup",
  allowed = [200, 201],
) {
  return http.request(
    method,
    base + path,
    body === undefined ? null : JSON.stringify(body),
    {
      headers: {
        Origin: origin,
        "Content-Type": "application/json",
        ...(session
          ? {
              Cookie: `mini_fury_session=${session.cookie}`,
              "X-CSRF-Token": session.csrf,
            }
          : {}),
      },
      tags: {
        phase,
        name:
          method + " " + path.split("?")[0].replace(/[a-f0-9-]{36}/g, "{id}"),
      },
      responseCallback: http.expectedStatuses(...allowed),
    },
  );
}
function login(email, password) {
  const r = request("POST", "/api/auth/login", { email, password });
  if (r.status !== 200) fail("fixture login failed");
  return {
    cookie: r.cookies.mini_fury_session[0].value,
    csrf: r.json("csrfToken"),
  };
}
export function setup() {
  let healthy = false;
  for (let i = 0; i < 20; i++) {
    if (
      http.get(base + "/api/health", { tags: { phase: "setup" } }).status ===
      200
    ) {
      healthy = true;
      break;
    }
    sleep(1);
  }
  if (!healthy) fail("load API not ready");
  const admin = login("load@mini-fury.local", "Disposable-Load-Only-2026!");
  const run = Date.now().toString(36);
  const users = [];
  for (let i = 0; i < 12; i++) {
    const email = `load-${run}-${i}@mini-fury.local`,
      password = "Disposable-Mixed-Only-2026!";
    const created = request(
      "POST",
      "/api/admin/users",
      { email, name: `Mixed fixture ${i}`, password, role: "developer" },
      admin,
    );
    if (created.status !== 201) fail("fixture user creation failed");
    const s = login(email, password);
    const apps = [];
    for (let j = 0; j < 10; j++) {
      const a = request(
        "POST",
        "/api/applications",
        {
          name: `load-${run}-${i}-${j}`,
          repository: "https://github.com/example/load-fixture",
          port: 8080,
          environment: "staging",
          owner: `Load team ${i % 3}`,
          runtime: "Go",
          description: "Synthetic mixed operations fixture",
          tags: ["load", `group-${i % 3}`],
          documentation:
            "Disposable runbook used to measure catalog operations.",
          healthCheckPath: "/health",
        },
        s,
      );
      if (a.status !== 201) fail("fixture catalog creation failed");
      apps.push(a.json());
    }
    users.push({ ...s, apps });
  }
  return { users, run };
}
export default function (data) {
  const u = data.users[(exec.vu.idInTest - 1) % data.users.length];
  const n = exec.vu.iterationInScenario % 100;
  const app = u.apps[exec.vu.iterationInScenario % u.apps.length];
  if (n < 70) {
    const r = request(
      "GET",
      "/api/applications?environment=staging&limit=20&q=load",
      undefined,
      u,
      "load",
    );
    check(r, {
      "authorized paginated search": (r) =>
        r.status === 200 && Array.isArray(r.json("applications")),
    });
  } else if (n < 90) {
    const r = request(
      "GET",
      "/api/applications/" + app.id,
      undefined,
      u,
      "load",
    );
    check(r, { "authorized detail": (r) => r.status === 200 });
  } else if (n < 98) {
    const current = request(
      "GET",
      "/api/applications/" + app.id,
      undefined,
      u,
      "load",
    );
    if (current.status === 200) {
      const a = current.json();
      const r = request(
        "PUT",
        "/api/applications/" + a.id,
        {
          name: a.name,
          repository: a.repository,
          port: a.port,
          environment: a.environment,
          owner: a.owner,
          runtime: a.runtime,
          description: `Load revision ${exec.vu.idInTest}-${exec.vu.iterationInScenario}`,
          tags: a.tags,
          teamId: a.teamId,
          documentation: a.documentation,
          links: a.links,
          healthCheckPath: a.healthCheckPath,
          version: a.version,
        },
        u,
        "load",
        [200, 409],
      );
      if (r.status === 409) conflicts.add(1);
      else if (r.status === 200) writes.add(1);
      check(r, {
        "edit or explicit optimistic conflict": (r) =>
          r.status === 200 || r.status === 409,
      });
    } else check(current, { "read before edit": () => false });
  } else {
    const r = request(
      "POST",
      "/api/applications",
      {
        name: `new-${data.run}-${exec.vu.idInTest}-${exec.vu.iterationInScenario}`,
        repository: "https://github.com/example/load-fixture",
        port: 8080,
        environment: "staging",
        owner: "Load fixture",
      },
      u,
      "load",
    );
    check(r, { "create application": (r) => r.status === 201 });
    if (r.status === 201) {
      writes.add(1);
      const a = r.json();
      const archived = request(
        "POST",
        "/api/applications/" + a.id + "/archive",
        { version: a.version },
        u,
        "load",
      );
      check(archived, {
        "archive created application": (r) => r.status === 200,
      });
    }
  }
  sleep(0.1);
}
export function handleSummary(data) {
  const summary = {
    scenario:
      "12 users / 120 initial applications / search, details, edits, creation and archive / up to 50 VUs",
    requests: data.metrics.http_reqs.values,
    latency: data.metrics["http_req_duration{phase:load}"]?.values,
    failures: data.metrics["http_req_failed{phase:load}"]?.values,
    writes: data.metrics.catalog_writes?.values,
    optimisticConflicts: data.metrics.version_conflicts?.values,
    thresholdsPassed: Object.values(data.metrics).every(
      (m) => !m.thresholds || Object.values(m.thresholds).every((t) => t.ok),
    ),
  };
  return {
    "/results/operations-summary.json": JSON.stringify(data, null, 2),
    stdout: JSON.stringify(summary, null, 2) + "\n",
  };
}
