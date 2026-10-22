import type { Environment } from './platform';
export interface CreateApplication { teamId?:string|null;documentation?:string;links?:Link[];healthCheckPath?:string;dependencyIds?:string[]; name: string; repository: string; port: number; environment: Environment; owner: string; runtime: string; description: string; tags: string[] }
export interface RegisteredApplication extends CreateApplication { teamId:string|null;documentation:string;links:Link[];healthCheckPath:string;dependencies:Dependency[];canEdit:boolean;canManageTeam:boolean;readiness:Readiness; id: string; status: 'registered' | 'archived'; createdBy: string | null; createdAt: string; updatedAt: string; archivedAt: string | null; version: number }
export interface CatalogFilters { q: string; state: 'active' | 'archived' | 'all'; owner: string; runtime: string; tag: string }
export const emptyFilters: CatalogFilters = { q: '', state: 'active', owner: '', runtime: '', tag: '' };

export interface Link { label:string; url:string }
export interface Dependency { id:string;name:string;environment:string }
export interface Readiness {score:number;checks:Array<{key:string;label:string;passed:boolean}>}
