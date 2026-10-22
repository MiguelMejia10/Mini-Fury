import { useEffect, useState } from "react";
import { request } from "../services/http";
import { listApplications } from "../services/applications";
import {
  emptyFilters,
  type Link,
  type RegisteredApplication,
} from "../types/catalog";
import type { Team } from "./Teams";
export function QualityFields({
  initial,
  onLinks,
  onDependencies,
}: {
  initial?: RegisteredApplication;
  onLinks: (v: Link[]) => void;
  onDependencies: (v: string[]) => void;
}) {
  const [teams, setTeams] = useState<Team[]>([]);
  const [teamMore, setTeamMore] = useState(false);
  const [teamOffset, setTeamOffset] = useState(0);
  const [links, setLinks] = useState<Link[]>(initial?.links || []);
  const [dependencies, setDependencies] = useState<string[]>(
    initial?.dependencies.map((d) => d.id) || [],
  );
  const [candidates, setCandidates] = useState<
    Array<{ id: string; name: string }>
  >(initial?.dependencies || []);
  const [query, setQuery] = useState("");
  const [error, setError] = useState("");
  const [searching, setSearching] = useState(false);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const c = new AbortController();
    request<{ teams: Team[]; hasMore: boolean }>(
      `/teams?limit=100&offset=${teamOffset}`,
      { signal: AbortSignal.any([c.signal, AbortSignal.timeout(12000)]) },
    )
      .then((data) => {
        if (!c.signal.aborted) {
          setTeams((old) =>
            teamOffset ? [...old, ...data.teams] : data.teams,
          );
          setTeamMore(data.hasMore);
        }
      })
      .catch((e) => {
        if (!c.signal.aborted) setError(e.message);
      });
    return () => c.abort();
  }, [teamOffset, revision]);
  function changeLinks(v: Link[]) {
    setLinks(v);
    onLinks(v);
  }
  async function search() {
    setSearching(true);
    setError("");
    try {
      const data = await listApplications(undefined, 0, {
        ...emptyFilters,
        q: query,
      });
      setCandidates((old) => {
        const selected = old.filter((a) => dependencies.includes(a.id));
        return [
          ...new Map(
            [
              ...selected,
              ...data.applications.filter((a) => a.id !== initial?.id),
            ].map((a) => [a.id, a]),
          ).values(),
        ];
      });
    } catch (e) {
      setError(
        e instanceof Error
          ? e.message
          : "No se pudieron consultar aplicaciones.",
      );
    } finally {
      setSearching(false);
    }
  }
  return (
    <div className="quality-fields">
      <h3>Equipo y calidad del catálogo</h3>
      {error && (
        <div role="alert" className="form-message bad">
          {error}
          <button
            className="secondary-button"
            type="button"
            onClick={() => setRevision((n) => n + 1)}
          >
            Reintentar
          </button>
        </div>
      )}
      <label>
        Equipo responsable
        <select
          name="teamId"
          defaultValue={initial?.teamId || ""}
          disabled={!!initial && !initial.canManageTeam}
        >
          <option value="">Personal / sin equipo</option>
          {initial?.teamId && !teams.some((t) => t.id === initial.teamId) && (
            <option value={initial.teamId}>Equipo actual</option>
          )}
          {teams
            .filter(
              (t) =>
                t.role === "admin" ||
                (!initial && t.role === "editor") ||
                t.id === initial?.teamId,
            )
            .map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
        </select>
        <small>
          La asignación requiere administrar el equipo. Los lectores no pueden
          modificar aplicaciones.
        </small>
      </label>
      {teamMore && (
        <button
          type="button"
          className="secondary-button"
          onClick={() => setTeamOffset((n) => n + 100)}
        >
          Cargar más equipos
        </button>
      )}
      <label>
        Ruta de health check
        <input
          name="healthCheckPath"
          maxLength={200}
          defaultValue={initial?.healthCheckPath}
          placeholder="/health"
        />
        <small>
          Solo configuración. Mini Fury todavía no verifica el endpoint remoto.
        </small>
      </label>
      <label>
        Documentación
        <textarea
          name="documentation"
          maxLength={20000}
          rows={6}
          defaultValue={initial?.documentation}
        />
        <small>
          Texto o Markdown. Se muestra como texto seguro, sin ejecutar HTML.
        </small>
      </label>
      <h3>Enlaces útiles</h3>
      {links.map((link, i) => (
        <div className="link-editor" key={i}>
          <label>
            Título
            <input
              value={link.label}
              maxLength={80}
              required
              onChange={(e) =>
                changeLinks(
                  links.map((l, n) =>
                    n === i ? { ...l, label: e.target.value } : l,
                  ),
                )
              }
            />
          </label>
          <label>
            URL HTTPS
            <input
              value={link.url}
              type="url"
              required
              maxLength={2048}
              onChange={(e) =>
                changeLinks(
                  links.map((l, n) =>
                    n === i ? { ...l, url: e.target.value } : l,
                  ),
                )
              }
            />
          </label>
          <button
            type="button"
            className="text-button"
            onClick={() => changeLinks(links.filter((_, n) => n !== i))}
          >
            Quitar enlace
          </button>
        </div>
      ))}
      <button
        type="button"
        className="secondary-button"
        disabled={links.length >= 10}
        onClick={() => changeLinks([...links, { label: "", url: "" }])}
      >
        Añadir enlace
      </button>
      <h3>Dependencias visibles</h3>
      <div className="form-row">
        <label>
          Buscar aplicaciones
          <input
            value={query}
            maxLength={200}
            onChange={(e) => setQuery(e.target.value)}
          />
        </label>
        <button
          type="button"
          className="secondary-button"
          disabled={searching}
          onClick={() => void search()}
        >
          {searching ? "Buscando…" : "Buscar dependencias"}
        </button>
      </div>
      <p className="catalog-intro">
        Hasta 20 dependencias. La API rechaza ciclos y referencias sin permiso.
        Se preservan las relaciones que tu cuenta no puede ver.
      </p>
      <div className="dependency-options">
        {candidates.map((a) => (
          <label key={a.id}>
            <input
              type="checkbox"
              checked={dependencies.includes(a.id)}
              onChange={(e) => {
                const ids = e.target.checked
                  ? [...dependencies, a.id]
                  : dependencies.filter((id) => id !== a.id);
                setDependencies(ids);
                onDependencies(ids);
              }}
            />
            {a.name}
          </label>
        ))}
      </div>
    </div>
  );
}
