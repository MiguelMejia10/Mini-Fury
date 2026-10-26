# Mini Fury v0.6: puntos 1–5, en orden

## Funciones implementadas

1. **Scopes por aplicación:** staging/producción, criticidad, región y dominio como metadatos, namespace autorizado, réplicas y límites de CPU/memoria. Edición optimista y permisos por equipo; producción exige administrador de equipo o plataforma.
2. **Centro Kubernetes:** estado de Deployment, pods, eventos, CPU/memoria medidos por Metrics Server, logs del contenedor seleccionado (últimas 200 líneas, instancia anterior), reinicio y escalado auditados. La consulta de logs valida propiedad Pod → ReplicaSet → Deployment. No hay terminal ni ejecución arbitraria.
3. **Versiones y despliegues:** builds desde un agente autorizado, imágenes inmutables por digest, despliegue con comprobación de salud y recuperación de snapshots anteriores. El laboratorio compila realmente una aplicación Go y publica su imagen en un registry local. El hash de origen del laboratorio corresponde al contenido de sus archivos, no a un commit Git ficticio.
4. **Configuración:** revisiones inmutables de variables no sensibles y referencias a Secrets permitidos por el operador. Guardar no aplica: el usuario elige desplegar o aplicar configuración. La API no tiene permiso de leer valores de Secrets. Las referencias requieren que el Secret exista en el namespace.
5. **Trabajos:** cola PostgreSQL, dos workers acotados por API, leases recuperables con token de propietario, serialización por scope y build/aplicación, idempotencia, eventos, cancelación y reintento. Cancelar no deshace efectos ya aplicados. El rollback es una operación explícita.

## Probar sin afectar el entorno principal

Requisitos: Docker Desktop, kubectl, Python 3, Node/npm del proyecto y acceso a imágenes/releases oficiales. El instalador descarga kind con checksum y usa exclusivamente `.local/kube-lab/admin-kubeconfig` y el contexto `kind-mini-fury-lab`; no cambia el contexto Kubernetes global.

```sh
python3 scripts/lab/start.py
cd frontend
npm run dev -- --config vite.lab.config.ts --port 5174 --strictPort
```

Abre `http://127.0.0.1:5174/#runtime`. La cuenta desechable está en `.local/kube-lab/test/account.env` (archivo privado, no versionado). Usa sus datos solo en el laboratorio; no compartas su contraseña en el chat. La cookie del laboratorio es distinta de la principal.

En otra terminal, desde la raíz:

```sh
python3 scripts/lab/runner.py --print-revision
python3 scripts/lab/runner.py --target test
```

Copia la revisión impresa en el formulario de build. Crea un scope, solicita una versión, selecciona su digest y despliega. Los nombres de build que comienzan con `broken-` activan exclusivamente en esta aplicación de ejemplo un fallo de readiness para ensayar recuperación.

Prueba automatizada real (genera un scope nuevo y conserva evidencia):

```sh
python3 scripts/lab/verify.py
```

Evidencia: `.local/kube-lab/test/validation.json`. Base de datos, volúmenes, API `18080`, Vite `5174`, clúster y registry son independientes del entorno principal `8080/5173`. El registry HTTP y la excepción de certificado de kubelet de Metrics Server son solo para este laboratorio. La API Kubernetes siempre verifica su CA.

Los tokens Kubernetes duran 24 horas. `start.py` los renueva y vuelve a copiar las credenciales privadas; reinicia la API si cambias conexiones. No subas `.local`, `.env`, kubeconfigs o tokens a Git.

## Conectar una aplicación propia

El operador define `RUNTIME_CONNECTIONS_FILE`: clusters con servidor HTTPS/CA/tokenFile y bindings de aplicación/namespace; repositories con proveedor, repositorio exacto, ref, workflow, agentId e imagePrefixes autorizados; agents con tokenFile. Ejemplo real de estructura, sin secretos: `platform-api/internal/runtime/config.go`.

El token de la API requiere solo get/list de pods/eventos/ReplicaSets/métricas, get de pods/log y get/create/update de Deployments y Services en el namespace vinculado. No recibe Docker socket ni kubeconfig de administrador. Los usuarios no pueden registrar destinos de red ni credenciales desde el navegador.

Los adaptadores GitHub Actions y GitLab solicitan pipelines y esperan un reporte firmado del agente. Su activación remota y workflow del repositorio **siguen pendientes**: no se han probado contra una cuenta/proyecto remoto. El flujo íntegro validado es el local. No hay sincronización ArgoCD/GitOps, ingress/DNS/TLS de aplicaciones, autoscaling ni rotación automática de Secrets todavía. El dominio del scope no publica tráfico.

El API principal incluye los módulos pero no vincula automáticamente el repositorio existente a un clúster. `compose.kubernetes.yml` es optativo para conexiones preparadas por el operador. La aplicación de ejemplo del laboratorio no suplanta ese repositorio.

## Concurrencia y recuperación

Las llamadas al clúster tienen timeout y límites de conexión; la observación permite hasta dos consultas simultáneas por cuenta y ocho por instancia. Hay paginación, logs hasta 500 líneas/128 KiB y límites de recursos por scope (1–10 réplicas, 50–2000m CPU, 64–2048 MiB). No equivalen a una capacidad certificada para 200 usuarios: faltan pruebas representativas del flujo Kubernetes y dimensionamiento.

Un respaldo incluye scopes, versiones, configuración y trabajos. La restauración a base nueva revoca sesiones, enlaces de recuperación, entregas pendientes y cancela trabajos queued/running; invalida leases para impedir replay. No restaura el estado externo del clúster ni imágenes/Secrets del registry: requieren su política de respaldo propia.

Los siguientes cinco puntos permanecen pendientes y se abordarán después de este bloque: incidentes, SLO, plantillas de aplicaciones, servicios administrados y costos. Consulta el roadmap para su detalle.
