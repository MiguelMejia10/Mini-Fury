import { useEffect, useState, type FormEvent } from "react";
import { request } from "../services/http";
export function RecoveryAccess({
  reset,
  onBack,
  onReset,
}: {
  reset: boolean;
  onBack: () => void;
  onReset: () => void;
}) {
  const [token, setToken] = useState(
    () =>
      new URLSearchParams(location.hash.split("?")[1] || "").get("token") || "",
  );
  const [enabled, setEnabled] = useState<boolean | null>(null),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("");
  useEffect(() => {
    if (reset)
      history.replaceState(
        null,
        "",
        location.pathname + location.search + "#reset-password",
      );
    const c = new AbortController();
    request<{ enabled: boolean }>("/auth/recovery", { signal: c.signal })
      .then((v) => setEnabled(v.enabled))
      .catch(() =>
        setError("No se pudo consultar la recuperación. Intenta nuevamente."),
      );
    return () => c.abort();
  }, [reset]);
  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (busy) return;
    const form = e.currentTarget,
      d = new FormData(form);
    setBusy(true);
    setError("");
    setNotice("");
    try {
      if (reset) {
        if (d.get("password") !== d.get("confirm"))
          throw new Error("Las contraseñas no coinciden.");
        await request("/auth/reset-password", {
          method: "POST",
          body: JSON.stringify({ token, password: String(d.get("password")) }),
        });
        setToken("");
        onReset();
        setNotice(
          "Contraseña actualizada. Inicia sesión con tu nueva contraseña.",
        );
        form.reset();
      } else {
        await request("/auth/forgot-password", {
          method: "POST",
          body: JSON.stringify({ email: String(d.get("email")) }),
        });
        setNotice(
          "Si la cuenta está activa, recibirás un enlace de un solo uso que vence en 30 minutos.",
        );
        form.reset();
      }
    } catch (e) {
      setError(
        e instanceof Error
          ? e.message
          : "No se pudo completar la recuperación.",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="login-shell">
      <section className="login-panel">
        <div className="login-kicker">MINI FURY / RECUPERACIÓN</div>
        <h1>{reset ? "Nueva contraseña" : "Recuperar acceso"}</h1>
        <p>
          Los enlaces son de un solo uso. Al cambiar la contraseña se cierran
          las sesiones anteriores.
        </p>
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
        {enabled === false && (
          <p className="form-message bad">
            El administrador aún debe configurar el correo de recuperación.
          </p>
        )}
        {enabled === null && !error && (
          <p role="status">Consultando disponibilidad…</p>
        )}
        {enabled && !notice && (
          <form className="application-form" onSubmit={submit}>
            <fieldset disabled={busy}>
              {reset ? (
                <>
                  <label>
                    Nueva contraseña
                    <input
                      name="password"
                      type="password"
                      autoComplete="new-password"
                      minLength={12}
                      maxLength={128}
                      required
                    />
                  </label>
                  <label>
                    Confirmar contraseña
                    <input
                      name="confirm"
                      type="password"
                      autoComplete="new-password"
                      minLength={12}
                      maxLength={128}
                      required
                    />
                  </label>
                  {!token && (
                    <p role="alert">
                      Abre el enlace completo recibido por correo o solicita
                      otro.
                    </p>
                  )}
                </>
              ) : (
                <label>
                  Correo de tu cuenta
                  <input
                    name="email"
                    type="email"
                    autoComplete="username"
                    maxLength={254}
                    required
                  />
                </label>
              )}
            </fieldset>
            <button
              className="primary-button"
              disabled={busy || (reset && !token)}
            >
              {busy
                ? "Procesando…"
                : reset
                  ? "Cambiar contraseña"
                  : "Enviar instrucciones"}
            </button>
          </form>
        )}
        <button className="secondary-button" disabled={busy} onClick={onBack}>
          Volver al inicio de sesión
        </button>
      </section>
    </div>
  );
}
