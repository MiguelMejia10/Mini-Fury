import type { CatalogFilters, CreateApplication, RegisteredApplication } from '../types/catalog';
import { request } from './http';
export { ApiError } from './http';
export interface CatalogPage { applications: RegisteredApplication[]; hasMore: boolean; nextOffset: number }
export function listApplications(signal?: AbortSignal, offset = 0, filters?: CatalogFilters, environment = '') {
 const query = new URLSearchParams({limit:'100',offset:String(offset),environment,...filters});
 return request<CatalogPage>(`/applications?${query}`,{signal});
}
export const getApplication = (id: string, signal?: AbortSignal) => request<RegisteredApplication>(`/applications/${encodeURIComponent(id)}`, { signal });
export const createApplication = (data: CreateApplication) => request<RegisteredApplication>('/applications', { method: 'POST', body: JSON.stringify(data) });
export const updateApplication = (app: RegisteredApplication, data: CreateApplication) => request<RegisteredApplication>(`/applications/${app.id}`, { method: 'PUT', body: JSON.stringify({...data,version:app.version}) });
export const archiveApplication = (app: RegisteredApplication) => request<RegisteredApplication>(`/applications/${app.id}/${app.archivedAt?'restore':'archive'}`,{method:'POST',body:JSON.stringify({version:app.version})});
