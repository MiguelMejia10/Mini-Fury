# mini-operador-TI / Mini Fury

Internal Developer Platform con interfaz React + TypeScript y API en Go. El catálogo permite registrar, listar y consultar aplicaciones reales persistidas en PostgreSQL, con sesión, roles y auditoría. Incluye equipos, documentación y dependencias del catálogo, y operación real de Mini Fury. Infraestructura y despliegues siguen usando demostraciones.

## Ejecutar en desarrollo local

Requiere Docker Desktop en ejecución, Node.js 22.12+ y npm. No requiere instalar Go en el host.

Desde la raíz del proyecto, antes del primer arranque:

```sh
cp .env.example .env
```

Configura `POSTGRES_PASSWORD` en `.env` con una contraseña alfanumérica local. No subas este archivo a Git. Si ya existe `.env`, conserva su contenido.

```sh
docker compose up -d --build postgres platform-api
cd frontend
npm install
npm run dev -- --port 5173
```

Abre http://127.0.0.1:5173/#applications y pulsa **Nueva aplicación**. Puedes usar una URL de repositorio de ejemplo: todavía no se comprueba acceso a Git ni se ejecutan builds. Selecciona el ambiente correspondiente para consultar sus aplicaciones.

El catálogo empieza vacío. Las aplicaciones guardan nombre, URL HTTPS del repositorio sin credenciales, puerto HTTP, ambiente, responsable y runtime. El nombre es único por ambiente. Una aplicación registrada aparece **sin desplegar**, sin métricas ni réplicas inventadas. El detalle consulta nuevamente la API por ID. Tema y ambiente se conservan en el navegador.

La API se publica solo en `127.0.0.1:8080`. PostgreSQL no publica puertos al host. Vite redirige `/api` al backend; no hay que configurar CORS. Este entorno usa autenticación y autorización. Sigue configurado solo para desarrollo local HTTP; antes de publicar hay que configurar HTTPS, COOKIE_SECURE=true y los dominios autorizados.

## Acceso y roles

La primera instalación usa un bootstrap explícito, sin contraseñas predeterminadas y sin registro público. Crea `.local/bootstrap.env` con permisos privados y estas variables:

```dotenv
ADMIN_EMAIL=admin@mini-fury.local
ADMIN_NAME=Administrador
ADMIN_PASSWORD=una-contraseña-local-unica-de-al-menos-12-caracteres
```

No reutilices la contraseña de ejemplo. `.local/` está excluida de Git. Ejecuta:

```sh
docker compose --profile setup run --rm --build user-bootstrap
```

El comando solo crea al primer administrador si la tabla de usuarios está vacía. Si ya hay usuarios, no cambia credenciales ni crea nuevas cuentas. En el entorno implementado se generó una contraseña aleatoria en `.local/bootstrap.env`; abre ese archivo localmente para entrar.

Entra desde la pantalla de login. En **Configuración** puedes cerrar sesión; el administrador además puede crear usuarios y consultar auditoría. Un desarrollador ve sus aplicaciones personales y las compartidas con sus equipos; los permisos de escritura dependen del rol en el equipo. Los registros anteriores al control de acceso quedan visibles únicamente para administradores.

La creación de usuarios requiere al menos 12 caracteres y un máximo de 128 bytes en la contraseña. El cambio de contraseña propia está en Configuración. La recuperación por correo y MFA siguen pendientes.

## API

Todas las rutas del catálogo requieren cookie de sesión. Las escrituras requieren `Origin` permitido y `X-CSRF-Token`, obtenido al iniciar sesión o consultar `/api/auth/me`.

- `POST /api/auth/login`: correo y contraseña; inicia sesión y devuelve usuario y token CSRF.
- `GET /api/auth/me`: sesión actual.
- `POST /api/auth/logout`: revoca la sesión.
- `GET /api/admin/users`: usuarios, solo admin.
- `POST /api/admin/users`: crea un usuario, solo admin.
- `GET /api/admin/audit`: auditoría, solo admin.

- `GET /api/health`: disponibilidad de API y base de datos (200/503).
- `GET /api/applications`: catálogo paginado (`limit` hasta 100, `offset` hasta 10000); filtro opcional `?environment=staging` o `production`.
- `GET /api/applications/{id}`: detalle de una aplicación.
- `POST /api/applications`: registro, requiere `Content-Type: application/json`.

Ejemplo de cuerpo:

```json
{
  "name": "payments-api",
  "repository": "https://gitlab.com/equipo/payments-api",
  "port": 8080,
  "environment": "staging",
  "owner": "Equipo Platform",
  "runtime": "Go"
}
```

Respuestas: 401 sin sesión; 403 sin permisos o CSRF inválido; 429 por exceso de intentos de acceso; 201 al crear; 400 para JSON o identificadores inválidos; 404 si no existe; 409 para duplicados; 415 para formato no soportado; 422 para campos inválidos; 503 si la base de datos no responde. Los errores de validación incluyen `message` y `fields`. El runtime vacío se registra como `Contenedor HTTP`.

## Persistencia y migraciones

PostgreSQL usa el volumen nombrado `postgres_data` del proyecto Compose. Las migraciones SQL se empaquetan en el binario y se ejecutan transaccionalmente al iniciar la API, con bloqueo para arranques concurrentes. Se registran en `schema_migrations`.

```sh
docker compose restart platform-api
# Las aplicaciones se conservan; recarga el catálogo.
docker compose down
# Detiene y retira contenedores, conservando el volumen de PostgreSQL.
```

No uses `docker compose down -v` si quieres conservar los datos: elimina el volumen. Cambiar la contraseña en `.env` no cambia la de un volumen ya inicializado.

## Validación

```sh
cd frontend
npm run build
```

Desde la raíz, con PostgreSQL en marcha:

```sh
docker compose --profile test run --rm --build test
```

Las pruebas verifican validación HTTP, duplicados, errores sin exposición de detalles internos, migraciones repetibles, inserciones concurrentes, separación por ambiente y lectura desde otra conexión. El perfil de pruebas usa `postgres-test`, una base desechable independiente de la base principal.

Con Go 1.25+ instalado también puedes ejecutar `go test ./...` y `go vet ./...` dentro de `platform-api`; la prueba de integración requiere `TEST_DATABASE_URL` y se omite si no está configurada.

## Estructura

- `frontend/src/components/Catalog.tsx`: catálogo, formulario y detalle.
- `frontend/src/hooks/useApplicationCatalog.ts`: carga, error y reintento.
- `frontend/src/services/applications.ts`: cliente HTTP.
- `frontend/src/services/demo.ts`: datos ilustrativos de las otras vistas.
- `platform-api/cmd/server`: arranque, conexión y cierre ordenado.
- `platform-api/internal/platform`: HTTP, catálogo, dependencias, repositorio y migraciones.
- `platform-api/internal/teams`: equipos, permisos e invitaciones.
- `platform-api/internal/telemetry`: métricas, estado operacional y alertas.
- `docker-compose.yml`: PostgreSQL, API y perfil de pruebas.
- `readme-Interfaz.md`: guía visual.
- `readmeImplementacion.md`: checklist del MVP y avances.

Pendiente: recuperación de contraseña por correo, MFA/SSO, validar capacidad con tráfico representativo de producción, CI/CD de aplicaciones, despliegues, secretos e integraciones con Kubernetes, GitLab, ArgoCD y observabilidad. El registro no construye imágenes ni crea infraestructura.

## Arquitectura y capacidad

Ver `docs/arquitectura-identidad.md` para módulos, matriz de permisos, sesiones compartidas, auditoría transaccional, límites de concurrencia y criterios antes de producción. No se promete una capacidad en usuarios sin medir el tráfico real.

## Versión 0.3

Ahora puedes editar, archivar y restaurar aplicaciones con auditoría y control de versiones. El catálogo tiene descripción, etiquetas y búsqueda/filtros en backend. En Configuración están el cambio de contraseña, cierre global de sesiones, desactivación/reactivación de usuarios y filtros de auditoría.

La operación completa, rutas nuevas, CI de GitHub/GitLab y comandos de respaldo/restauración están documentados en [docs/operacion-v0.3.md](docs/operacion-v0.3.md).

```sh
sh scripts/check.sh
python3 scripts/database.py backup
python3 scripts/database.py verify .local/backups/ARCHIVO.dump
```

La CI está configurada en archivos y pendiente de ejecutarse en el repositorio remoto elegido. Los despliegues reales de aplicaciones continúan aplazados.

## Versión 0.4

- **Equipos:** invitaciones dentro de la plataforma, roles administrador/editor/lector y aplicaciones compartidas.
- **Calidad:** documentación, enlaces HTTPS, ruta de health check declarada, dependencias sin ciclos y checklist de preparación.
- **Operación:** métricas reales de Mini Fury, salud de PostgreSQL, alertas persistentes con reconocimiento y exportación Prometheus protegida para administradores.

La ruta de salud del catálogo es configuración; todavía no se consulta la salud de las aplicaciones. Las métricas se calculan por instancia de Mini Fury. Las invitaciones no envían correo.

Guía de arquitectura, rutas y límites: [docs/operacion-v0.4.md](docs/operacion-v0.4.md). Evidencia de pruebas: [docs/validacion-v0.4.md](docs/validacion-v0.4.md).

```sh
sh scripts/load-test.sh
```

La prueba usa API y PostgreSQL desechables sin puertos públicos ni datos principales. El informe queda en `.local/perf/catalog-summary.json`.
