export type Environment = 'production' | 'staging';
export interface Application { name: string; runtime: string; version: string; environment: Environment; health: 'healthy' | 'warning'; cpu: number; memory: number; replicas: number }
export interface Deployment { id: string; app: string; version: string; environment: Environment; status: 'success' | 'failed'; time: string; commit: string }
