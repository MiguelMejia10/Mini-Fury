// Session cookie is HttpOnly; only the CSRF token lives in JS memory.
let csrfToken = '';
export function setCSRF(token: string) { csrfToken = token; }
export class ApiError extends Error {
  constructor(message: string, public fields: Record<string, string> = {}, public status = 0, public code = '') { super(message); }
}
export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  let response: Response;
  try {
    response = await fetch(`/api${path}`, { ...init, credentials: 'same-origin', signal: init.signal ?? AbortSignal.timeout(12000), headers: { 'Content-Type': 'application/json', ...(csrfToken ? { 'X-CSRF-Token': csrfToken } : {}), ...init.headers } });
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') throw error;
    throw new ApiError('No pudimos conectar con la API. Intenta de nuevo en unos segundos.');
  }
  let body: unknown;
  try { body = await response.json(); } catch { throw new ApiError('El servicio no está disponible. Intenta de nuevo.'); }
  if (!response.ok) {
    if (response.status === 401 && path !== '/auth/login' && path !== '/auth/me') window.dispatchEvent(new Event('session-expired'));
    const error = body as { message?: string; fields?: Record<string, string>; code?: string };
    throw new ApiError(error.message || 'No se pudo completar la operación.', error.fields, response.status, error.code);
  }
  return body as T;
}
