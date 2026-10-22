import type { Application, Deployment } from '../types/platform';
// Adaptador de demostración. Las futuras llamadas a platform-api se encapsulan aquí.
export const applications: Application[] = [
  {name:'payments-api',runtime:'Go 1.23',version:'v1.4.2',environment:'production',health:'healthy',cpu:32,memory:48,replicas:3},
  {name:'users-api',runtime:'Java 21',version:'v2.1.0',environment:'staging',health:'healthy',cpu:24,memory:56,replicas:3},
  {name:'notifications-worker',runtime:'Go 1.23',version:'v0.8.1',environment:'production',health:'healthy',cpu:18,memory:34,replicas:2},
  {name:'orders-api',runtime:'Java 21',version:'v1.0.4',environment:'production',health:'healthy',cpu:41,memory:61,replicas:3},
  {name:'catalog-api',runtime:'Node.js 22',version:'v2.3.0',environment:'production',health:'warning',cpu:62,memory:82,replicas:2},
  {name:'inventory-worker',runtime:'Go 1.23',version:'v1.2.0',environment:'production',health:'healthy',cpu:21,memory:38,replicas:2},
];
export const deployments: Deployment[] = [
  {id:'DEP-028',app:'payments-api',version:'v1.4.2',environment:'production',status:'success',time:'hace 12 min',commit:'a8f31c2'},
  {id:'DEP-027',app:'users-api',version:'v2.1.0',environment:'staging',status:'success',time:'hace 28 min',commit:'e92b710'},
  {id:'DEP-026',app:'notifications-worker',version:'v0.8.1',environment:'production',status:'success',time:'hace 1 hora',commit:'c4d91ab'},
  {id:'DEP-025',app:'orders-api',version:'v1.0.5',environment:'production',status:'failed',time:'hace 2 horas',commit:'d71f390'},
];
export const events = [
  ['23:14:22','INFO','Deployment payments-api v1.4.2 completed successfully'],
  ['23:12:10','INFO','Scaled users-api to 3 replicas'],
  ['23:10:45','WARN','High memory usage on catalog-api (82%)'],
  ['23:08:12','INFO','ArgoCD synchronized: inventory-worker'],
  ['23:05:33','ERROR','Deployment orders-api v1.0.5 failed (readiness probe)'],
];
