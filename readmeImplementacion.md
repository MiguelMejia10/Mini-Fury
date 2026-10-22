# Mini Fury / mini-operador-TI — Checklist de implementación del MVP

## Objetivo

Permitir completar este flujo con servicios reales:

**Registrar una aplicación → desplegarla → consultar su estado y logs → recuperar una versión anterior si falla.**

Actualmente tenemos la interfaz y el catálogo de aplicaciones conectado a una API Go con persistencia PostgreSQL. Las demás vistas usan datos de demostración. Este checklist registra el alcance necesario para alcanzar un producto mínimo viable.

## 1. Definir el alcance

- [ ] Elegir el primer usuario: desarrollador que despliega sus servicios.
- [ ] Limitar la primera versión a un clúster y un ambiente de pruebas.
- [ ] Elegir un proveedor Git y un registro de imágenes.
- [ ] Soportar inicialmente un tipo de aplicación: API HTTP en contenedor.
- [ ] Documentar qué incluye el MVP y qué queda para después.

## 2. Plataforma donde ejecutar las aplicaciones

- [ ] Preparar un clúster Kubernetes de desarrollo.
- [ ] Configurar acceso por dominio, ingress y HTTPS.
- [ ] Configurar el registro de imágenes.
- [ ] Instalar y configurar ArgoCD.
- [ ] Crear una plantilla estándar con recursos, health checks y configuración de red.

## 3. Backend y persistencia

- [x] Crear la API en Go.
- [x] Añadir PostgreSQL y migraciones.
- [ ] Guardar aplicaciones, ambientes, despliegues y su historial.
- [x] Implementar validaciones, manejo de errores y logs estructurados.
- [ ] Implementar seguimiento de operaciones asíncronas y límites de tiempo.
- [ ] Evitar que solicitudes repetidas creen despliegues duplicados.

## 4. Acceso y seguridad básica

- [x] Implementar inicio y cierre de sesión.
- [x] Definir permisos mínimos: administrador y desarrollador.
- [x] Proteger las operaciones desde el backend.
- [ ] Gestionar credenciales de integraciones sin exponerlas en el navegador.
- [ ] Registrar quién ejecutó cada despliegue o cambio de configuración.
- [ ] Evitar que logs y respuestas expongan secretos.

## 5. Aplicaciones y despliegues reales

- [x] Registrar una aplicación con nombre, repositorio, puerto y responsable.
- [ ] Construir y publicar una imagen mediante CI.
- [ ] Desplegar una versión concreta mediante ArgoCD.
- [ ] Mostrar estados reales: pendiente, en progreso, exitoso y fallido.
- [ ] Mostrar la causa de un fallo.
- [ ] Consultar historial con versión, commit, fecha y usuario.
- [ ] Volver a una versión anterior.
- [ ] Configurar variables y referencias a secretos de forma segura.

## 6. Operación y observabilidad

- [ ] Consultar salud y disponibilidad de cada aplicación.
- [ ] Consultar logs con filtros básicos.
- [ ] Mostrar CPU, memoria y réplicas reales.
- [ ] Consultar eventos relevantes del despliegue.
- [ ] Definir cómo detectar y avisar de una aplicación caída.
- [ ] Mostrar cuándo se actualizaron los datos y cuándo una integración no está disponible.

## 7. Completar la interfaz

- [ ] Conectar las pantallas con la API.
- [ ] Crear formularios para registrar aplicaciones y desplegar versiones.
- [ ] Añadir estados de carga, error, vacío y reintento.
- [ ] Sustituir las métricas y notificaciones de demostración.
- [ ] Confirmar acciones que afecten aplicaciones en ejecución.
- [ ] Revisar accesibilidad y uso en escritorio y móvil.

## 8. Calidad y entrega

- [ ] Automatizar compilación y validaciones en CI.
- [ ] Probar permisos, despliegues fallidos y recuperación.
- [ ] Probar el flujo completo con una aplicación de ejemplo.
- [ ] Preparar configuración reproducible del entorno.
- [ ] Documentar instalación, uso y resolución de fallos.
- [x] Configurar y comprobar respaldo y restauración local de PostgreSQL.

## Criterio para considerar listo el MVP

- [ ] Un usuario puede iniciar sesión y registrar una aplicación.
- [ ] Puede desplegar un commit y acceder a su API por HTTPS.
- [ ] Puede ver su estado, consumo y logs.
- [ ] Puede entender un fallo y volver a una versión anterior.
- [ ] El historial persiste después de reiniciar Mini Fury.
- [ ] Otro desarrollador puede completar el flujo siguiendo la documentación.

## Fuera del alcance inicial

Podemos dejar para después:

- Multiclúster.
- Múltiples proveedores cloud.
- Autoscaling avanzado.
- Facturación.
- Marketplace de plantillas.
- Topología interactiva avanzada.

Vault y Grafana también pueden entrar después si resolvemos desde el MVP la gestión segura de secretos y la observabilidad básica.

## Avance: registro persistente de aplicaciones

- [x] API Go para registrar, listar y consultar aplicaciones por ID.
- [x] PostgreSQL y migraciones versionadas al iniciar la API.
- [x] Restricción de nombre único por ambiente.
- [x] Formulario con validaciones y mensajes de error o éxito.
- [x] Catálogo conectado, filtros por ambiente y detalle desde la API.
- [x] Estados de carga, catálogo vacío, errores de conexión y reintento en el catálogo.
- [x] Docker Compose para levantar API y base de datos con volumen persistente.
- [x] Pruebas HTTP y prueba de integración PostgreSQL incluidas.

Este avance cubre el registro de aplicaciones. El historial de despliegues, las métricas y el resto de integraciones continúan pendientes; las otras pantallas usan datos de demostración. El catálogo ahora requiere sesión y permisos; el entorno sigue configurado para desarrollo local.

### Validación realizada

- Compilación del frontend: `npm run build`.
- Pruebas HTTP y PostgreSQL: `docker compose --profile test run --rm --build test`.
- Registro desde el navegador de `demo-registro-api`, consulta del detalle y rechazo de un registro duplicado.
- Reinicio de la API y recarga del navegador: la aplicación permanece en el catálogo.

## Avance: identidad, roles y auditoría

- [x] Login/logout y sesiones compartidas en PostgreSQL con expiración y revocación.
- [x] Argon2id, cookie HttpOnly/SameSite y protección de origen/CSRF.
- [x] Roles administrador y desarrollador, aplicados en la API.
- [x] Aplicaciones vinculadas al creador y aislamiento entre desarrolladores.
- [x] Creación de usuarios y consulta de auditoría desde Configuración para administradores.
- [x] Auditoría transaccional para login, logout, usuarios y registro de aplicaciones.
- [x] Límites de solicitudes simultáneas, trabajo de contraseñas y conexiones a la base de datos.
- [x] Rate limit compartido entre instancias y consultas paginadas.
- [x] Pruebas de autorización, CSRF, aislamiento, revocación y concurrencia incluidas.
- [ ] Medir capacidad con una prueba de carga representativa antes de publicar.
- [ ] Configurar HTTPS, cookie Secure y proxy de confianza para un entorno público.
- [x] Cambio de contraseña propia y desactivación/reactivación de usuarios (v0.3).
- [ ] Recuperación de contraseña por correo y MFA/SSO.

Los despliegues aún no están integrados; su auditoría continúa pendiente. Detalles de arquitectura y límites en `docs/arquitectura-identidad.md`.

## Avance v0.3: funcionalidades sin despliegues reales

- [x] Edición de aplicaciones con control de versiones para evitar sobrescrituras concurrentes.
- [x] Archivado y restauración reversible, conservando permisos y auditoría.
- [x] Descripciones y etiquetas normalizadas.
- [x] Búsqueda y filtros del catálogo en backend, con paginación e índices.
- [x] Cambio de contraseña propia con verificación de la actual y revocación de sesiones.
- [x] Cierre de todas las sesiones propias.
- [x] Desactivación/reactivación de usuarios, con protección del administrador activo.
- [x] Filtros de auditoría por actor, acción, recurso y fechas.
- [x] Scripts de verificación y definiciones de CI para GitLab y GitHub.
- [ ] Ejecutar el primer pipeline remoto al elegir y conectar el proveedor/repositorio.
- [x] Herramienta de respaldo con checksum y restauración a una base nueva aislada.
- [x] Pruebas de seguridad para impedir sobrescritura de bases existentes.
- [ ] Definir destino externo, cifrado, retención y frecuencia de respaldos para producción.

Los despliegues reales de aplicaciones se mantienen aplazados por decisión del usuario. La CI de este bloque verifica Mini Fury; no integra GitLab/ArgoCD con las aplicaciones del catálogo.

### Validación de v0.3

- [x] Compilación de TypeScript y build de producción.
- [x] Pruebas de integración con PostgreSQL y detector de carreras de Go.
- [x] Pruebas de conflictos de edición, archivo/restauración y aislamiento de permisos.
- [x] Pruebas de cambio de contraseña, cierre global de sesiones y desactivaciones concurrentes.
- [x] Validación sintáctica de YAML de GitHub/GitLab y pruebas de seguridad de respaldo.
- [x] Respaldo previo y posterior a la migración; ambos restaurados y consultados en bases aisladas.
- [ ] Revisión visual de pantallas autenticadas: aplazada por solicitud del usuario.
