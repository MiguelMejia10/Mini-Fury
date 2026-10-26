# Validación v0.6 — 23 de octubre de 2026

## Resultado

Los puntos 1–5 se probaron con una aplicación de ejemplo real en kind, API/cuenta/PostgreSQL/registry separados del entorno principal. La evidencia privada reproducible está en `.local/kube-lab/test/validation.json`; el ensayo se ejecuta con `python3 scripts/lab/verify.py`.

- Scope independiente creado y consultado; consultas antes de desplegar reconocen la ausencia del workload.
- Build Docker real y publicación en registry local; versión inmutable por digest.
- Despliegue con readiness, pods y logs reales del contenedor seleccionado.
- Revisión de variables y referencia a Secret guardada y aplicada. API sin permiso Kubernetes para leer Secrets ni listar pods de otro namespace.
- Escalado a dos réplicas; cancelación y reintento de reinicio; vuelta a una réplica.
- Despliegue deliberadamente defectuoso: detectado como fallido. Rollback recupera imagen/configuración/recursos del snapshot anterior y comprueba su disponibilidad.
- CPU y memoria reales disponibles mediante Metrics Server tras la recuperación.
- Restauración aislada de un respaldo del laboratorio con un trabajo activo: se cancela y se invalidan todos los leases; no se modifica el clúster ni se reanuda la operación.
- Respaldo previo de cinco migraciones y respaldo posterior de ocho migraciones restaurados y eliminados en bases desechables. El entorno principal conserva un usuario y una aplicación, con ocho migraciones y sin scopes de prueba.

## Pruebas automatizadas

TypeScript y build Vite aprobados. Suite Go completa con PostgreSQL desechable y `-race` aprobada. Casos específicos: roles de equipo/producción, aislamiento de aplicaciones, configuración optimista, secretos por alias, HMAC del agente, repetición de callback, idempotencia de solicitudes, reclamaciones concurrentes, lease vencido y propietario anterior rechazado, revocación de membresía, TLS/rotación del token Kubernetes y propiedad de workloads. Regresiones cubiertas: una réplica antigua disponible no completa una revisión nueva y un fallo de una generación anterior no invalida inmediatamente el rollback.

## Límites de esta validación

No certifica capacidad de 100–200 usuarios ni disponibilidad de producción. Las integraciones remotas GitHub/GitLab están preparadas, pero no activadas ni verificadas con un repositorio real. Sin ArgoCD, ingress/DNS/TLS de aplicaciones ni proveedores externos. La revisión visual autenticada completa sigue pendiente. Los puntos 6–10 no se implementaron en este bloque.
