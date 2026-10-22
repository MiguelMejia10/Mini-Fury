# Mini Fury v0.3 — Catálogo, cuentas, auditoría y calidad

## Funciones de producto

### Aplicaciones

- Registrar y editar nombre, repositorio, puerto, ambiente, responsable, runtime, descripción y etiquetas.
- Descripciones de hasta 2.000 caracteres y hasta diez etiquetas normalizadas, sin duplicados.
- Archivar y restaurar con confirmación. No se borra información ni infraestructura. El nombre continúa reservado en su ambiente.
- Una aplicación archivada no se puede editar hasta restaurarla.
- Cada modificación requiere la versión leída. Si otra sesión la cambió, la API devuelve 409 con `code=stale_version`; no sobrescribe datos silenciosamente.
- Permisos: desarrollador sobre sus propias aplicaciones; administrador sobre todo el catálogo. El responsable textual no concede permisos.
- Búsqueda en backend por palabras de nombre, descripción, responsable y runtime, además de prefijo del nombre; filtros exactos por responsable, runtime y etiqueta, ambiente y estado activo/archivado/todos.
- Documento de búsqueda con índice GIN de PostgreSQL; etiquetas con índice GIN. Las consultas respetan permisos y paginación antes de devolver datos.

### Cuentas

En Configuración se puede cambiar la contraseña usando la actual o cerrar todas las sesiones propias. Ambas operaciones revocan las sesiones, incluida la actual. El cambio exige al menos 12 caracteres, hasta 128 bytes y un valor distinto de la contraseña anterior.

El administrador puede desactivar/reactivar usuarios con confirmación. La desactivación revoca las sesiones e impide nuevos logins. Al reactivar se requiere una nueva sesión; no se recuperan tokens revocados. No se permite desactivarse a sí mismo ni dejar sin administrador activo.

Login, cambios de contraseña y revocaciones toman un bloqueo consistente por usuario. Una versión de credenciales impide que un login que verificó una contraseña antes de una revocación vuelva a crear una sesión con esa verificación antigua. Los cambios mantienen auditoría transaccional y límites de trabajo Argon2id.

No se incluye todavía recuperación de contraseña por correo, MFA ni SSO.

### Auditoría

En Configuración → Administración → Auditoría se filtra por correo exacto del actor, acción, ID del recurso y fechas. Las fechas elegidas en el navegador se convierten de la zona local a UTC; el día final se incluye completo mediante un límite superior exclusivo.

Los registros incluyen ediciones, archivo/restauración, cambio de contraseña, cierre global de sesiones y activación/desactivación. Una operación fallida o rechazada por versión no crea un evento de éxito.

## Nuevas rutas

Todas requieren sesión. Las escrituras requieren origen autorizado y CSRF.

- `PUT /api/applications/{id}`: campos completos de aplicación y `version`.
- `POST /api/applications/{id}/archive`: `{ "version": 1 }`.
- `POST /api/applications/{id}/restore`: `{ "version": 2 }`.
- `GET /api/applications`: `q`, `environment`, `state`, `owner`, `runtime`, `tag`, `limit`, `offset`.
- `POST /api/auth/password`: `currentPassword`, `newPassword`; cierra todas las sesiones al completar.
- `POST /api/auth/logout-all`: cierra todas las sesiones propias.
- `PATCH /api/admin/users/{id}/status`: `{ "active": false }` o true; solo admin.
- `GET /api/admin/audit`: `actor`, `action`, `resource`, `from`, `until`, `limit`, `offset`. Fechas RFC3339, desde inclusivo y hasta exclusivo.

## Verificación local y CI

```sh
sh scripts/check.sh
```

Ejecuta las pruebas Go con PostgreSQL en Docker, instalación reproducible y build del frontend, y pruebas de seguridad del respaldo. Las pruebas de integración trabajan exclusivamente con usuarios y aplicaciones de prueba y limpian sus registros.

Se incluyen `.gitlab-ci.yml` y `.github/workflows/ci.yml`. Ambos usan PostgreSQL desechable y credenciales exclusivas de CI; verifican formato Go, análisis estático, pruebas con `-race`, compilación del backend, build del frontend y pruebas de la herramienta de respaldo. No usan las credenciales ni datos locales, ni despliegan aplicaciones.

Los archivos están listos para ejecutarse al subir el proyecto al proveedor elegido; no se ha ejecutado un pipeline remoto desde este entorno. Los scripts compartidos son `scripts/ci-backend.sh` y `scripts/ci-frontend.sh`.

## Respaldo y restauración

Con PostgreSQL de Docker Compose en marcha, desde la raíz:

```sh
python3 scripts/database.py backup
```

Genera un archivo `.dump` comprimido en formato custom y un manifiesto con SHA-256, dentro de `.local/backups/`, con permisos privados. Contiene datos y hashes de credenciales; no se incluye en Git ni se publica como artefacto de CI. El checksum detecta alteraciones accidentales; no es una firma de autenticidad. Usa solo respaldos propios de confianza.

```sh
python3 scripts/database.py verify .local/backups/ARCHIVO.dump
```

Verifica el checksum, crea una base nueva con nombre aleatorio `mini_fury_restore_*`, restaura transaccionalmente, consulta tablas y cantidades de filas, invalida las sesiones restauradas y elimina exclusivamente la base creada para verificar. La base principal no se toca.

Para conservar una restauración aislada:

```sh
python3 scripts/database.py restore .local/backups/ARCHIVO.dump --database mini_fury_restore_revision
```

El destino debe ser nuevo y empezar por `mini_fury_restore_`. Si existe, el comando falla y no lo elimina. No se usa `--clean` ni se permite restaurar encima de `mini_fury`. La API no cambia de base automáticamente; un cambio de conexión para recuperación real requiere un procedimiento operativo separado. Se borran las sesiones restauradas para no revivir accesos previamente revocados.

Este bloque comprueba recuperación local; no añade programación periódica ni almacenamiento externo/cifrado/retención. Esos aspectos requieren definir objetivos de recuperación y destino de los respaldos.

## Alcance pendiente

Continúan aplazados CI/CD de las aplicaciones, Kubernetes y ArgoCD. Las pantallas de infraestructura y observabilidad conservan sus datos de demostración. La CI aquí verifica el código de Mini Fury; no despliega servicios registrados.
