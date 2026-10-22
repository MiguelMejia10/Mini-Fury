import { useEffect, useRef, useState, type FormEvent } from "react";
import { request } from "../services/http";
import { useAuth } from "./Auth";
export interface Team {
  id: string;
  name: string;
  description: string;
  role: "admin" | "editor" | "reader";
}
interface Member {
  id: string;
  name: string;
  email: string;
  role: string;
  active: boolean;
}
interface Invite {
  id: string;
  teamId: string;
  teamName: string;
  email: string;
  role: string;
  expiresAt: string;
}
const roles = [
  ["admin", "Administrador"],
  ["editor", "Editor"],
  ["reader", "Lector"],
];
export function Teams() {
  const { user } = useAuth();
  const [teams, setTeams] = useState<Team[]>([]);
  const [selected, setSelected] = useState<Team | null>(null);
  const [members, setMembers] = useState<Member[]>([]);
  const [incoming, setIncoming] = useState<Invite[]>([]);
  const [pending, setPending] = useState<Invite[]>([]);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(false);
  const [revision, setRevision] = useState(0);
  const [teamPage, setTeamPage] = useState(0);
  const [memberPage, setMemberPage] = useState(0);
  const [invitePage, setInvitePage] = useState(0);
  const [moreTeams, setMoreTeams] = useState(false);
  const [moreMembers, setMoreMembers] = useState(false);
  const [moreInvites, setMoreInvites] = useState(false);
  const [pendingPage, setPendingPage] = useState(0);
  const [morePending, setMorePending] = useState(false);
  const [confirm, setConfirm] = useState<{
    path: string;
    method: string;
    body?: unknown;
    message: string;
  } | null>(null);
  const flight = useRef(false);
  useEffect(() => {
    const c = new AbortController();
    setLoading(true);
    setError("");
    Promise.all([
      request<{ teams: Team[]; hasMore: boolean }>(
        `/teams?limit=20&offset=${teamPage * 20}`,
        { signal: AbortSignal.any([c.signal, AbortSignal.timeout(12000)]) },
      ),
      request<{ invitations: Invite[]; hasMore: boolean }>(
        `/team-invitations?limit=20&offset=${invitePage * 20}`,
        { signal: AbortSignal.any([c.signal, AbortSignal.timeout(12000)]) },
      ),
    ])
      .then(([t, i]) => {
        if (c.signal.aborted) return;
        setTeams(t.teams);
        setMoreTeams(t.hasMore);
        setIncoming(i.invitations);
        setMoreInvites(i.hasMore);
        setSelected((old) =>
          old ? t.teams.find((v) => v.id === old.id) || null : null,
        );
      })
      .catch((e) => {
        if (!c.signal.aborted) setError(e.message);
      })
      .finally(() => {
        if (!c.signal.aborted) setLoading(false);
      });
    return () => c.abort();
  }, [revision, teamPage, invitePage]);
  useEffect(() => {
    if (!selected) {
      setMembers([]);
      setPending([]);
      return;
    }
    const c = new AbortController();
    setMembers([]);
    setPending([]);
    setError("");
    const signal = AbortSignal.any([c.signal, AbortSignal.timeout(12000)]);
    request<{ members: Member[]; hasMore: boolean }>(
      `/teams/${selected.id}/members?limit=20&offset=${memberPage * 20}`,
      { signal },
    )
      .then((data) => {
        if (!c.signal.aborted) {
          setMembers(data.members);
          setMoreMembers(data.hasMore);
        }
      })
      .catch((e) => {
        if (!c.signal.aborted) setError(e.message);
      });
    if (selected.role === "admin")
      request<{ invitations: Invite[]; hasMore: boolean }>(
        `/teams/${selected.id}/invitations?limit=20&offset=${pendingPage * 20}`,
        { signal },
      )
        .then((data) => {
          if (!c.signal.aborted) {
            setPending(data.invitations);
            setMorePending(data.hasMore);
          }
        })
        .catch((e) => {
          if (!c.signal.aborted) setError(e.message);
        });
    return () => c.abort();
  }, [selected, memberPage, pendingPage, revision]);
  async function mutate(path: string, method: string, body?: unknown) {
    if (flight.current) return false;
    flight.current = true;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await request(path, {
        method,
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      setNotice("Operación de equipo completada.");
      setConfirm(null);
      setRevision((n) => n + 1);
      return true;
    } catch (e) {
      setError(e instanceof Error ? e.message : "No se pudo completar.");
      return false;
    } finally {
      flight.current = false;
      setBusy(false);
    }
  }
  async function create(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = e.currentTarget;
    const d = new FormData(form);
    if (
      await mutate("/teams", "POST", {
        name: String(d.get("name")),
        description: String(d.get("description")),
      })
    ) {
      form.reset();
      setTeamPage(0);
    }
  }
  async function invite(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!selected) return;
    const form = e.currentTarget;
    const d = new FormData(form);
    if (
      await mutate(`/teams/${selected.id}/invitations`, "POST", {
        email: String(d.get("email")),
        role: String(d.get("role")),
      })
    )
      form.reset();
  }
  return (
    <>
      <div className="info-strip">
        Las invitaciones aparecen al destinatario al iniciar sesión con el mismo
        correo. No se envían emails ni se crean cuentas automáticamente.
      </div>
      {error && (
        <div role="alert" className="form-message bad">
          {error}
          <button
            className="secondary-button"
            onClick={() => setRevision((n) => n + 1)}
          >
            Reintentar
          </button>
        </div>
      )}
      {notice && (
        <div role="status" className="form-message good">
          {notice}
        </div>
      )}
      {confirm && (
        <div className="confirm-box">
          <p>{confirm.message}</p>
          <div className="form-actions">
            <button
              disabled={busy}
              className="secondary-button"
              onClick={() => setConfirm(null)}
            >
              Cancelar
            </button>
            <button
              disabled={busy}
              className="secondary-button"
              onClick={() =>
                void mutate(confirm.path, confirm.method, confirm.body)
              }
            >
              Confirmar
            </button>
          </div>
        </div>
      )}
      <section className="panel">
        <header className="panel-head">
          <h2>INVITACIONES RECIBIDAS</h2>
        </header>
        <div className="admin-body section-body">
          {incoming.map((i) => (
            <div key={i.id} className="team-row">
              <span>
                {i.teamName}
                <small>
                  {i.role} · vence{" "}
                  {new Date(i.expiresAt).toLocaleDateString("es-CO")}
                </small>
              </span>
              <button
                disabled={busy}
                className="secondary-button"
                onClick={() =>
                  setConfirm({
                    path: `/team-invitations/${i.id}/accept`,
                    method: "POST",
                    body: {},
                    message: `¿Aceptar el rol ${i.role} en ${i.teamName}?`,
                  })
                }
              >
                Aceptar invitación
              </button>
            </div>
          ))}
          {!incoming.length && !loading && (
            <p className="muted">No tienes invitaciones pendientes.</p>
          )}
          <div className="pagination">
            <button
              className="secondary-button"
              disabled={invitePage === 0 || busy}
              onClick={() => setInvitePage((n) => n - 1)}
            >
              Anterior
            </button>
            <button
              className="secondary-button"
              disabled={!moreInvites || busy}
              onClick={() => setInvitePage((n) => n + 1)}
            >
              Más invitaciones
            </button>
          </div>
        </div>
      </section>
      <div className="collaboration-grid">
        <section className="panel">
          <header className="panel-head">
            <h2>EQUIPOS</h2>
          </header>
          <div className="admin-body section-body">
            {loading && <p role="status">Cargando equipos…</p>}
            {teams.map((t) => (
              <button
                key={t.id}
                className={`team-choice ${selected?.id === t.id ? "selected" : ""}`}
                onClick={() => {
                  setSelected(t);
                  setMemberPage(0);
                  setPendingPage(0);
                  setConfirm(null);
                }}
              >
                <strong>{t.name}</strong>
                <span>{t.role}</span>
                <small>{t.description || "Sin descripción"}</small>
              </button>
            ))}
            {!teams.length && !loading && (
              <p className="muted">Aún no hay equipos visibles.</p>
            )}
            <div className="pagination">
              <button
                className="secondary-button"
                disabled={!teamPage || loading}
                onClick={() => setTeamPage((n) => n - 1)}
              >
                Anterior
              </button>
              <button
                className="secondary-button"
                disabled={!moreTeams || loading}
                onClick={() => setTeamPage((n) => n + 1)}
              >
                Más equipos
              </button>
            </div>
            <form className="application-form" onSubmit={create}>
              <h3>Crear equipo</h3>
              <fieldset disabled={busy}>
                <label>
                  Nombre
                  <input name="name" maxLength={80} required />
                </label>
                <label>
                  Descripción
                  <input name="description" maxLength={500} />
                </label>
              </fieldset>
              <button disabled={busy} className="primary-button">
                Crear equipo
              </button>
            </form>
          </div>
        </section>
        <section className="panel">
          <header className="panel-head">
            <h2>{selected?.name || "SELECCIONA UN EQUIPO"}</h2>
          </header>
          <div className="admin-body section-body">
            {selected && (
              <>
                <p className="catalog-intro">
                  Tu rol: {selected.role}.{" "}
                  {user.role === "admin"
                    ? "Acceso administrativo de plataforma."
                    : ""}
                </p>
                <div className="table-wrap">
                  <table>
                    <thead>
                      <tr>
                        <th>Miembro</th>
                        <th>Rol</th>
                        <th>Acciones</th>
                      </tr>
                    </thead>
                    <tbody>
                      {members.map((m) => (
                        <tr key={m.id}>
                          <td>
                            {m.name}
                            <small className="table-subtitle">
                              {m.email} · {m.active ? "Activo" : "Desactivado"}
                            </small>
                          </td>
                          <td>{m.role}</td>
                          <td>
                            {selected.role === "admin" && (
                              <div className="member-actions">
                                <select
                                  aria-label={`Cambiar rol de ${m.email}`}
                                  value={m.role}
                                  disabled={busy}
                                  onChange={(e) =>
                                    setConfirm({
                                      path: `/teams/${selected.id}/members/${m.id}`,
                                      method: "PUT",
                                      body: { role: e.target.value },
                                      message: `¿Cambiar a ${m.email} al rol ${e.target.value}?`,
                                    })
                                  }
                                >
                                  {roles.map(([v, l]) => (
                                    <option key={v} value={v}>
                                      {l}
                                    </option>
                                  ))}
                                </select>
                                <button
                                  disabled={busy}
                                  className="text-button"
                                  onClick={() =>
                                    setConfirm({
                                      path: `/teams/${selected.id}/members/${m.id}`,
                                      method: "DELETE",
                                      message: `¿Quitar a ${m.email} del equipo? Perderá su acceso a las aplicaciones compartidas.`,
                                    })
                                  }
                                >
                                  Quitar
                                </button>
                              </div>
                            )}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                <div className="pagination">
                  <button
                    className="secondary-button"
                    disabled={!memberPage}
                    onClick={() => setMemberPage((n) => n - 1)}
                  >
                    Anterior
                  </button>
                  <button
                    className="secondary-button"
                    disabled={!moreMembers}
                    onClick={() => setMemberPage((n) => n + 1)}
                  >
                    Más miembros
                  </button>
                </div>
                {selected.role === "admin" && (
                  <>
                    <form className="application-form" onSubmit={invite}>
                      <h3>Invitar integrante</h3>
                      <fieldset disabled={busy}>
                        <label>
                          Correo
                          <input
                            name="email"
                            type="email"
                            maxLength={254}
                            required
                          />
                        </label>
                        <label>
                          Rol
                          <select name="role" defaultValue="reader">
                            {roles.map(([v, l]) => (
                              <option key={v} value={v}>
                                {l}
                              </option>
                            ))}
                          </select>
                        </label>
                      </fieldset>
                      <button className="primary-button" disabled={busy}>
                        Crear invitación
                      </button>
                    </form>
                    <h3>Invitaciones pendientes</h3>
                    {pending.map((i) => (
                      <div key={i.id} className="team-row">
                        <span>
                          {i.email}
                          <small>{i.role}</small>
                        </span>
                        <button
                          disabled={busy}
                          className="text-button"
                          onClick={() =>
                            setConfirm({
                              path: `/teams/${selected.id}/invitations/${i.id}`,
                              method: "DELETE",
                              message: `¿Revocar la invitación a ${i.email}?`,
                            })
                          }
                        >
                          Revocar
                        </button>
                      </div>
                    ))}
                    <div className="pagination">
                      <button
                        className="secondary-button"
                        disabled={!pendingPage || busy}
                        onClick={() => setPendingPage((n) => n - 1)}
                      >
                        Anterior
                      </button>
                      <button
                        className="secondary-button"
                        disabled={!morePending || busy}
                        onClick={() => setPendingPage((n) => n + 1)}
                      >
                        Más invitaciones pendientes
                      </button>
                    </div>
                  </>
                )}
              </>
            )}
          </div>
        </section>
      </div>
    </>
  );
}
