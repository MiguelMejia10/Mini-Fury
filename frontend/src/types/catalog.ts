import type { Environment } from './platform';
export interface CreateApplication { name: string; repository: string; port: number; environment: Environment; owner: string; runtime: string; description: string; tags: string[] }
export interface RegisteredApplication extends CreateApplication { id: string; status: 'registered' | 'archived'; createdBy: string | null; createdAt: string; updatedAt: string; archivedAt: string | null; version: number }
export interface CatalogFilters { q: string; state: 'active' | 'archived' | 'all'; owner: string; runtime: string; tag: string }
export const emptyFilters: CatalogFilters = { q: '', state: 'active', owner: '', runtime: '', tag: '' };
