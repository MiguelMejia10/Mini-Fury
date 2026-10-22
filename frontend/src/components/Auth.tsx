import { createContext, useCallback, useContext, useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { Command, LockKeyhole, LogOut, RefreshCw } from 'lucide-react';
import { ApiError, request, setCSRF } from '../services/http';
export interface User { id: string; email: string; name: string; role: 'admin' | 'developer'; active: boolean }
interface Session { user: User; csrfToken: string }
interface AuthState { user: User; logout: () => Promise<void>; signingOut: boolean; endSession: (message: string) => void }
const AuthContext = createContext<AuthState | null>(null);
export function useAuth() { const value = useContext(AuthContext); if (!value) throw new Error('AuthProvider required'); return value; }
export function LogoutButton() { const { logout, signingOut } = useAuth(); return <button className="secondary-button" disabled={signingOut} onClick={()=>void logout()}><LogOut size={14}/>{signingOut?'Cerrando…':'Cerrar sesión'}</button>; }
export function AuthProvider({children}: {children: ReactNode}) {
  const epoch=useRef(0);
  const [user, setUser] = useState<User | null>(null);
  const [checking, setChecking] = useState(true);
  const [serviceError, setServiceError] = useState('');
  const [loginError, setLoginError] = useState('');
  const [busy, setBusy] = useState(false);
  const [signingOut, setSigningOut] = useState(false);
  const [logoutError, setLogoutError] = useState('');
  const endSession=useCallback((message:string)=>{epoch.current++;setCSRF('');setUser(null);setLoginError(message);setServiceError('');setChecking(false);},[]);
  const accept = useCallback((s: Session) => { setCSRF(s.csrfToken); setUser(s.user); }, []);
  const check = useCallback(async (signal?: AbortSignal) => {
    const version=epoch.current;
    setServiceError('');
    try { const s = await request<Session>('/auth/me', {signal:signal?AbortSignal.any([signal,AbortSignal.timeout(12000)]):undefined}); if (!signal?.aborted&&version===epoch.current) accept(s); }
    catch (error) { if (signal?.aborted||version!==epoch.current) return; if (error instanceof ApiError && error.status === 401) { setCSRF(''); setUser(null); } else { setServiceError(error instanceof Error ? error.message : 'No se pudo verificar la sesión.'); } }
    finally { if (!signal?.aborted) setChecking(false); }
  }, [accept]);
  useEffect(()=>{ const controller = new AbortController(); void check(controller.signal); return ()=>{controller.abort();}; },[check]);
  useEffect(()=>{ const expired=()=>{epoch.current++;setUser(null);setCSRF('');setLoginError('Tu sesión terminó. Inicia sesión de nuevo.');}; window.addEventListener('session-expired',expired); return ()=>window.removeEventListener('session-expired',expired); },[]);
  useEffect(()=>{if(!user)return;const refresh=()=>void check();window.addEventListener('focus',refresh);return ()=>window.removeEventListener('focus',refresh);},[user,check]);
  async function login(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (busy) return;
    const form = event.currentTarget; const data = new FormData(form); epoch.current++; setBusy(true); setLoginError('');
    try { const session = await request<Session>('/auth/login',{method:'POST',body:JSON.stringify({email:String(data.get('email')),password:String(data.get('password'))})}); form.reset(); accept(session); }
    catch(error){ setLoginError(error instanceof Error?error.message:'No se pudo iniciar sesión.'); const password=form.elements.namedItem('password') as HTMLInputElement; password.value='';password.focus(); }
    finally{setBusy(false);}
  }
  async function logout(){epoch.current++;setSigningOut(true);setLogoutError('');try{await request('/auth/logout',{method:'POST',body:'{}'});setCSRF('');setUser(null);}catch(error){if(!(error instanceof ApiError&&error.status===401))setLogoutError(error instanceof Error?error.message:'No se pudo cerrar sesión.');}finally{setSigningOut(false);}}
  if(user)return <AuthContext.Provider value={{user,logout,signingOut,endSession}}>{logoutError&&<div role="alert" className="session-banner">{logoutError}</div>}{children}</AuthContext.Provider>;
  return <div className="login-shell"><section className="login-panel"><a href="#overview" className="brand"><span className="brand-symbol"><Command size={22}/></span><div>MINI FURY<span className="accent">_</span><small>DEVELOPER PLATFORM</small></div></a><div className="login-kicker">IDENTITY / WORKSPACE ACCESS</div><LockKeyhole size={26} className="accent"/><h1>[ {checking?'VERIFICANDO':'INICIAR SESIÓN'} ]</h1><p>Tu plataforma. Tus aplicaciones. Un acceso seguro.</p>{checking?<div className="empty" role="status">Consultando sesión…</div>:serviceError?<div className="form-message bad" role="alert">{serviceError}<button className="secondary-button" onClick={()=>{setChecking(true);void check();}}><RefreshCw size={14}/> Reintentar</button></div>:<form className="application-form" onSubmit={login}>{loginError&&<div role="alert" className="form-message bad">{loginError}</div>}<label>Correo<input name="email" type="email" required maxLength={254} autoComplete="username" placeholder="tu@equipo.com" disabled={busy}/></label><label>Contraseña<input name="password" type="password" required maxLength={128} autoComplete="current-password" disabled={busy}/></label><button className="primary-button" disabled={busy}>{busy?'Verificando…':'Entrar al workspace'}</button></form>}<div className="login-footer">Acceso por invitación del administrador.<br/>Sesión de hasta 8 horas · permisos según tu rol.</div></section><span className="login-caption">MINI FURY / PLATFORM v0.3.0</span></div>;
}
