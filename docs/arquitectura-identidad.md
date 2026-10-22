# Identidad, permisos y capacidad — v0.2.0

## Decisión de arquitectura

Monolito modular con puertos de repositorio y adaptadores PostgreSQL/HTTP. Mantiene un despliegue simple sin acoplar las reglas de identidad al transporte. No introduce microservicios ni Redis antes de necesitarlo.

- `internal/identity/model.go`: usuarios, roles, contexto del actor y puerto Repository.
- `internal/identity/service.go`: casos de uso de login y creación de usuarios; admisión del trabajo de contraseñas.
- `internal/identity/password.go`: Argon2id, aleatoriedad criptográfica y resúmenes de tokens.
- `internal/identity/postgres.go`: sesiones compartidas, límites de acceso y auditoría transaccional.
- `internal/identity/http.go`: cookies, CSRF, validación de origen y autorización administrativa.
- `internal/platform`: catálogo y reglas de visibilidad aplicadas en las consultas de datos.
- `cmd/server`: composición de dependencias, configuración y cierre ordenado.
- `cmd/user`: bootstrap explícito del primer administrador, permitido únicamente si no hay usuarios.
- `frontend`: proveedor de sesión, cliente HTTP compartido, catálogo y administración.

## Reglas de acceso

- Sin sesión: solo login y health; el resto responde 401.
- Developer: registrar aplicaciones y consultar únicamente las creadas por su usuario.
- Admin: consultar todo el catálogo, crear usuarios con rol admin/developer y consultar auditoría.
- Aplicaciones anteriores a la migración: creador nulo, solo visibles para admin. Se conserva el responsable textual; no equivale a un permiso.
- El creador se toma del contexto autenticado en el servidor. No se acepta desde el cliente.
- Una aplicación ajena responde 404 para no confirmar su existencia.
- Los permisos se verifican en backend; ocultar un botón no concede ni revoca acceso.

## Sesiones y contraseñas

Argon2id con sal aleatoria de 16 bytes y parámetros versionados (64 MiB, tres pasadas, dos lanes). Máximo dos operaciones simultáneas por instancia: aproximadamente 128 MiB de memoria de Argon2 además del runtime y del resto de la API. El login de una cuenta inexistente también verifica un hash para reducir diferencias de tiempo.

La cookie contiene 32 bytes aleatorios codificados en base64url. PostgreSQL guarda únicamente SHA-256 del token. La cookie es HttpOnly, SameSite=Strict, Path=/ y expira a las ocho horas. Con HTTPS se usa Secure y el prefijo __Host-. HTTP sin Secure está permitido por la configuración de desarrollo local, no es una configuración para publicar.

La sesión no va en localStorage ni sessionStorage. El token CSRF derivado con separación de dominio permanece en memoria del cliente. Cada escritura requiere origen explícitamente autorizado y token CSRF vinculado a la cookie. Login requiere origen permitido y JSON, sin permitir CORS cruzado. Logout revoca la sesión en PostgreSQL. Reautenticar reemplaza la sesión presentada para el mismo usuario y se limitan a cinco sesiones activas por usuario.

Cambios de rol o desactivación se reflejan en la siguiente solicitud porque no se cachea el usuario en la API. En v0.3 se incorporan cambio de contraseña propia y desactivación/reactivación administrativa, con revocación transaccional de sesiones y versión de credenciales.

## Control de capacidad

- 64 solicitudes admitidas simultáneamente por instancia. El exceso recibe 503 y Retry-After, sin una cola ilimitada dentro de la aplicación.
- Dos trabajos Argon2id simultáneos; el exceso recibe 503 recuperable.
- Límite compartido en PostgreSQL: diez intentos por cuenta y 60 por IP en ventanas fijas de 15 minutos; se cuentan intentos exitosos y fallidos. El exceso recibe 429. Las ventanas fijas admiten un pico adicional en sus límites temporales.
- Se usa la IP de la conexión; no se confía en X-Forwarded-For. Detrás de Vite/proxy los usuarios comparten el límite IP. Antes de publicar debe definirse una cadena de proxies de confianza y límites en el ingress.
- Pool configurable de 2 a 100 conexiones por instancia; predeterminado diez. Hay que presupuestar `réplicas × pool` y reservar conexiones para migraciones, administración y monitoreo.
- Plazo por solicitud de ocho segundos y timeouts de servidor; cuerpos de escritura limitados a 16 KiB.
- Catálogo con páginas de hasta 100 elementos y controles de offset; usuarios y auditoría también paginados. El frontend carga más catálogo por demanda.
- Limpieza periódica de sesiones vencidas y contadores antiguos en lotes de hasta 1.000 filas, con índices.
- Las instancias comparten sesiones, usuarios y rate limits a través de PostgreSQL; no requieren afinidad de sesión.

Los límites evitan trabajo ilimitado dentro de una instancia. No garantizan una cantidad de usuarios concurrentes ni sustituyen dimensionamiento y pruebas con el tráfico real. El límite de entrada del proxy también debe cubrir conexiones y cabeceras.

## Auditoría

Se registran bootstrap, creación de usuarios, login, logout y creación de aplicaciones. El evento y la mutación se guardan en la misma transacción. Si falla el evento, se revierte la operación. No se guardan contraseñas, cookies ni tokens CSRF. Los accesos HTTP y rechazos de login también dejan logs estructurados sin cuerpos de solicitud.

La API solo inserta y consulta auditoría; no expone edición ni eliminación. Esto no impide que un administrador de PostgreSQL altere registros. Retención, particiones, exportación y almacenamiento inmutable son trabajo posterior.

## Validación y límites del alcance

Pruebas: contraseñas, cookies, expiración/revocación, CSRF y origen, roles, aislamiento de aplicaciones, creador del lado servidor, sesión en otra instancia, rate limit compartido concurrente, admisión de solicitudes y consultas concurrentes autenticadas.

Antes de producción: HTTPS y cookie Secure, dominios explícitos, proxy/ingress configurado, presupuesto de conexiones, pruebas de carga representativas con p95/p99 y monitoreo, respaldo, MFA/SSO si aplica, recuperación de contraseñas por correo y ampliación del ciclo de vida de usuarios y retención de auditoría. Las demás pantallas de infraestructura continúan en demostración.

Referencias:

- https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html
- https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html
- https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html

La extensión de catálogo, cuentas y operaciones de v0.3 se detalla en `operacion-v0.3.md`.
