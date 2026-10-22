# Validación de identidad — v0.2.0

Comprobaciones ejecutadas correctamente en el entorno local:

- `npm run build`: TypeScript y empaquetado de producción del frontend.
- `go vet ./...`: análisis estático de Go.
- `go test -race ./...`: sin carreras detectadas en las pruebas ejecutadas en el host. Las pruebas PostgreSQL se omiten en este comando porque TEST_DATABASE_URL no está configurado en el host.
- `docker compose --profile test run --rm --build test`: pruebas unitarias y de integración con PostgreSQL, incluyendo:
  - validación, duplicados y migraciones repetibles;
  - login, cookies, CSRF y origen;
  - creación de usuarios por admin y rechazo de developer;
  - bloqueo del bootstrap después de inicializar usuarios;
  - propietario de aplicaciones asignado por backend;
  - aislamiento de catálogo y detalle entre desarrolladores;
  - auditoría de registro de aplicaciones;
  - sesión válida en otra instancia y revocación compartida al cerrar sesión;
  - tokens de sesión resumidos en la base de datos;
  - 32 consultas autenticadas concurrentes sin errores;
  - límite compartido de intentos de login bajo concurrencia;
  - rechazo de exceso de solicitudes sobre 64 simultáneas mediante un repositorio bloqueado de prueba;
  - rechazo de trabajo Argon2id cuando los dos slots están ocupados.

La pantalla de inicio de sesión fue revisada visualmente. El primer administrador se creó mediante bootstrap explícito. No se escribieron credenciales en los logs ni en esta documentación.

Estos resultados son pruebas funcionales y de concurrencia acotadas. No constituyen un benchmark de usuarios, ni una prueba de carga de producción; no se ha medido todavía un SLO de latencia p95/p99.
