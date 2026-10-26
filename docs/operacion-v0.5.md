# Mini Fury v0.5 — Históricos y preparación para producción

## Alcance y activación

El código incluye Loki, Alloy, Prometheus y Grafana; visor histórico en Mini Fury; recuperación por correo; entrega externa de alertas mediante cola cifrada; una composición HTTPS independiente; respaldos cifrados con soporte S3 y una prueba de carga mixta.

El usuario confirmó que aún no dispone de dominio, SMTP externo, destino de alertas ni bucket. Por ello el entorno actual usa servicios locales y no envía información a servicios externos. La activación pública queda pendiente de esos proveedores y de un ensayo con configuración real. Los logs de pods y los despliegues de aplicaciones siguen requiriendo Kubernetes.

## Instalar el entorno local

Desde la raíz:

```sh
python3 scripts/setup-operations.py
docker compose -f docker-compose.yml -f compose.observability.yml up -d --build
```

El generador crea secretos aleatorios con permisos 0600 dentro de `.local/` y conserva cualquier archivo existente. No cambia usuarios, contraseñas del producto ni el bootstrap anterior. La API continúa en 127.0.0.1:8080 y la interfaz Vite en 127.0.0.1:5173.

- Mini Fury → Observabilidad: histórico, filtros de logs y enlace al dashboard, solo administrador de plataforma.
- Grafana: http://127.0.0.1:3300; usuario `admin`; contraseña independiente en `.local/grafana_password`. No es la cuenta de Mini Fury. `GRAFANA_PORT` permite cambiar el puerto.
- Mailpit: http://127.0.0.1:8025, buzón exclusivamente local para pruebas de correo. No envía mensajes a Internet.
- Loki, Prometheus y Alloy no publican puertos al host. No se monta el socket Docker.

Al administrar este entorno usa los mismos archivos Compose combinados. No utilices `--remove-orphans` con una composición parcial: podrías detener servicios opcionales que sí siguen formando parte del proyecto. No uses `down -v` sobre el proyecto principal si quieres conservar sus datos.

## Flujo histórico, límites y retención

La API escribe registros JSON operativos en un volumen. Solo incluyen hora, instancia, método HTTP normalizado, ruta normalizada, estado y duración. Se excluyen consultas, cuerpos, cabeceras, correos, contraseñas, cookies y tokens. Tampoco se envían automáticamente los errores internos completos a Loki.

La rotación conserva el archivo activo de hasta 5 MiB y cinco archivos rotados. Alloy lee el volumen y conserva posiciones en otro volumen; Loki almacena índices, chunks, WAL y metadatos en un volumen persistente. La retención de Loki es siete días mediante Compactor. La eliminación es eventual y requiere un servicio operativo, no ocurre exactamente en el instante de cumplir siete días. Un corte prolongado del recolector que exceda la capacidad de archivos locales puede perder registros; para alta disponibilidad se requiere almacenamiento y topología distintos.

Prometheus conserva hasta siete días de métricas o 1 GiB, lo que se alcance primero. Consulta `/internal/metrics` con un token de servicio independiente. El proxy público bloquea esta ruta. El endpoint de sesión `/api/operations/metrics` sigue disponible solo al administrador.

El histórico de Mini Fury usa consultas prefijadas y acotadas, no un proxy abierto de LogQL o PromQL. Admite ventanas de hasta 24 horas dentro de los últimos siete días; logs entre 1 y 500 líneas, por defecto 200, y filtro literal de hasta 200 caracteres. Los upstreams tienen tiempo de espera y límite de respuesta de 2 MiB. No devuelve credenciales de los servicios. El gráfico no inventa muestras anteriores a la instalación.

Las reglas activas siguen siendo errores 5xx y latencia p95. Se evalúan con al menos veinte solicitudes; una muestra insuficiente no declara recuperación de una alerta anterior. Cuando no se evalúa durante dos minutos la señal se considera antigua. Las métricas de ventana son por instancia; el dashboard agrega contadores históricos. El p95 histórico se estima a partir del histograma.

Verificación reproducible:

```sh
python3 scripts/verify-observability.py
python3 scripts/verify-observability.py --restart
```

La segunda opción reinicia únicamente Loki y Prometheus y comprueba muestras anteriores al reinicio. Produce una breve interrupción de consultas. Los volúmenes se conservan.

## Recuperación de contraseña

En el login: **Olvidé mi contraseña**. El correo local se consulta en Mailpit. El enlace incluye el token en el fragmento del navegador y la interfaz lo retira de la barra de dirección al cargar; no aparece en URLs HTTP ni Referer. Recargar tras retirarlo exige volver a abrir el enlace completo.

La API usa tokens aleatorios de 32 bytes, almacena solo el digest, expira a los 30 minutos y permite una única consumición transaccional. Solicitar otro invalida el anterior. Los tokens están vinculados a la versión de credenciales: un cambio de contraseña, revocación global o desactivación no puede revivirse usando un enlace viejo. El restablecimiento incrementa esa versión, revoca sesiones, elimina tokens y registra auditoría. Una cuenta inactiva no puede recuperarse.

La petición de correo devuelve el mismo mensaje para cuentas activas y direcciones inexistentes; el envío se realiza fuera de la petición. Hay límites compartidos por IP/correo y límites de trabajo Argon2. No existe garantía criptográfica de igualdad temporal absoluta, pero se evita ejecutar SMTP o el hash de contraseña en la solicitud de correo y se fija una duración mínima de respuesta.

Rutas públicas, con origen permitido y JSON:

- `GET /api/auth/recovery`: disponibilidad de la función.
- `POST /api/auth/forgot-password`: `{email}`.
- `POST /api/auth/reset-password`: `{token,password}`.

Para SMTP real configura `SMTP_ADDRESS=host:587`, `SMTP_FROM`, `SMTP_USER`, `SMTP_PASSWORD`, `DELIVERY_KEY` y `APP_PUBLIC_URL=https://tu-dominio`. La implementación requiere STARTTLS en SMTP real; no implementa SMTPS implícito en el puerto 465. `SMTP_ALLOW_LOCAL=true` es exclusivo del buzón de desarrollo y debe estar desactivado en producción.

## Alertas externas y cola de entregas

Configura en el archivo privado de runtime `ALERT_WEBHOOK_URL` y, si el receptor lo soporta, `ALERT_WEBHOOK_SECRET`. Se envía JSON con `text`, `content`, `event` e `id`, compatible con receptores de webhook que acepten esos campos; valida las políticas del destino elegido. No se enviaron alertas a Slack o Discord reales durante esta implementación.

Se notifican transiciones a activa y resuelta. El evento se registra con un trigger en la misma transacción que cambia la alerta. La creación del trabajo es idempotente por ID de transición. Tras configurar un destino, solo se encolan transiciones de las últimas 24 horas. Configurarlo podría por tanto enviar incidentes recientes; revisa el entorno antes de activarlo.

La cola en PostgreSQL cifra cada payload con AES-256-GCM y una clave independiente `DELIVERY_KEY` de 32 bytes codificada en base64. Incluye tokens y destinatarios de correo dentro del cifrado, no en campos de diagnóstico. Un worker reclama trabajos mediante `FOR UPDATE SKIP LOCKED`, lease de 30 segundos, timeout de entrega de 10 segundos y hasta seis intentos con espera exponencial. No mantiene una transacción abierta durante la llamada de red.

La semántica de entrega es al menos una vez: una caída tras aceptación del receptor pero antes de confirmar en PostgreSQL puede repetir el envío. Los webhooks incluyen `Idempotency-Key` y firma HMAC-SHA256 cuando se configura secreto. Los receptores deben deduplicar por ID si requieren evitar repeticiones; SMTP no ofrece esa garantía.

Los endpoints de webhook requieren HTTPS, no siguen redirecciones y rechazan IP privadas/loopback/link-local mediante resolución y conexión comprobadas. `WEBHOOK_ALLOW_LOCAL=true` se reserva para fixtures locales. Los errores persistidos son códigos genéricos; no contienen URLs privadas ni respuestas del proveedor. Tras éxito o fallo terminal se borra el payload cifrado. Se conservan estados de entrega y transiciones durante treinta días.

Observabilidad muestra si el destino está configurado y las cantidades de trabajos pendientes/fallidos. Reconocer una alerta no equivale a resolverla. La cola depende de PostgreSQL; para detectar una caída total de Mini Fury se requiere monitoreo externo independiente.

## HTTPS y despliegue separado

`compose.production.yml` es independiente del entorno de desarrollo: PostgreSQL y API no publican puertos; Caddy sirve el frontend compilado y redirige `/api`. Cookies `__Host-` con Secure/HttpOnly/SameSite, cabeceras de seguridad y CSP. El backend solo acepta `X-Real-IP` desde la IP del proxy declarada; ignora cabeceras de IP de clientes directos.

Copia los archivos de ejemplo a `.local/production.env` y `.local/production-runtime.env` y completa el dominio y secretos. Usa credenciales independientes y un nuevo bootstrap para una instalación nueva; no publiques las credenciales locales actuales. Si la subred fija 172.30.50.0/24 coincide con otra red, ajústala junto con la IP exacta de proxy confiable.

```sh
python3 scripts/production-preflight.py
docker compose --env-file .local/production.env -f compose.production.yml up -d --build
```

El enlace público usa DNS y puertos 80/443 accesibles; Caddy obtiene/renueva certificados para un dominio apto cuando se configure. El bind predeterminado es loopback y la publicación requiere decidir explícitamente `BIND_IP`. Este avance no configura DNS, firewall público, certificados de un dominio real ni disponibilidad multiinstancia.

Con monitorización, combina también `compose.observability.yml` y `compose.observability.production.yml`. Crea los archivos de secretos de producción indicados por `METRICS_TOKEN_FILE` y `GRAFANA_PASSWORD_FILE`, y usa el mismo token en el runtime de la API. El overlay conecta los servicios por la red privada y evita arrancar Mailpit por defecto. Grafana permanece en loopback; publicarlo requiere su propio proxy y política de acceso, no habilitar acceso anónimo.

Ensayo HTTPS local independiente:

```sh
sh scripts/test-https.sh
```

Usa un proyecto Docker exclusivo, CA local de prueba, puertos 8443/8085 y cuentas desechables. Comprueba el certificado con esa CA, cabeceras, bloqueo de métricas internas y cookie Secure. Al terminar elimina únicamente su proyecto y volúmenes temporales. No instala una CA en el sistema ni cambia el desarrollo HTTP principal.

## Respaldos cifrados, destino externo y recuperación

El motor usa pg_dump custom y restic. `setup-operations.py` genera un repositorio local cifrado para comprobar el flujo; eso no es un respaldo fuera del equipo.

```sh
docker compose -f docker-compose.yml -f compose.backups.yml --profile backups run --rm --build backup init
docker compose -f docker-compose.yml -f compose.backups.yml --profile backups run --rm backup backup
docker compose -f docker-compose.yml -f compose.backups.yml --profile backups run --rm backup verify
```

`init` se ejecuta una sola vez para un repositorio nuevo. El respaldo guarda `/database.dump`, verifica el repositorio y aplica retención acotada a host/tag de Mini Fury: últimos siete, catorce diarios, ocho semanales y seis mensuales. `verify` descarga desde el repositorio, crea una base aleatoria nueva, restaura transaccionalmente, consulta el esquema y elimina exclusivamente esa base creada. No restaura sobre la base activa. Se eliminan sesiones, intentos de acceso, tokens de recuperación y trabajos de entrega de la restauración para no revivir accesos ni reenviar mensajes históricos.

Para un destino S3 compatible usa `infra/backup/backup.env.example` como guía de `.local/backup.env`: repositorio `s3:https://endpoint/bucket/prefijo`, contraseña de cifrado independiente y credenciales limitadas al bucket/prefijo. El bucket debe existir. No se utilizó ni verificó un bucket remoto porque aún no existe. Custodia la contraseña fuera del equipo; sin ella no se recuperan los datos.

Para ejecución periódica, tras inicializar y verificar el destino:

```sh
docker compose -f docker-compose.yml -f compose.backups.yml --profile backups up -d --build backup-scheduler
```

Ejecuta inmediatamente y después según `BACKUP_INTERVAL_SECONDS` (mínimo 300 s, predeterminado 86400 s). Es un intervalo, no una hora fija del calendario. Los fallos se registran en el contenedor; el plan de operación debe vigilarlos desde fuera y ensayar restauraciones. La retención de restic no sustituye políticas de inmutabilidad/versionado del proveedor.

En producción combina `compose.production.yml`, `compose.backups.yml` y `compose.backups.production.yml`, con `--env-file .local/production.env`. Ese overlay permite acceso a PostgreSQL privado y salida hacia S3. No mezcles las redes de desarrollo con producción.

Los respaldos custom anteriores siguen disponibles mediante `scripts/database.py`. Su verificador detecta la migración 005 y elimina también tokens/trabajos restaurados. La migración es aditiva; no elimina usuarios o aplicaciones existentes.

## Validación y capacidad

```sh
sh scripts/check.sh
sh scripts/load-test.sh operations
```

El escenario mixto usa doce cuentas desechables, 120 aplicaciones iniciales y hasta cincuenta usuarios virtuales, con búsqueda, detalle, edición, creación y archivo. Los conflictos 409 se consideran control optimista esperado y se cuentan aparte; los otros fallos incumplen umbrales. API/base de datos están aisladas del producto y se detienen al terminar.

Es un modelo sintético más amplio que la lectura simple, no una garantía de capacidad de producción. Faltan datos de uso reales, pruebas largas, degradación de dependencias, volúmenes mayores y los futuros conectores Kubernetes. Resultados en `docs/validacion-v0.5.md`.

El roadmap de operación DevOps y el análisis con fuentes públicas de Fury están en `docs/roadmap-fury.md`.

## Referencias técnicas

- [Loki: instalación y componentes](https://grafana.com/docs/loki/latest/setup/install/docker/).
- [Loki: retención mediante Compactor](https://grafana.com/docs/loki/latest/operations/storage/retention/).
- [Alloy: lectura de archivos](https://grafana.com/docs/alloy/latest/reference/components/loki/loki.source.file/).
- [Caddy: HTTPS automático](https://caddyserver.com/docs/automatic-https).
- [OWASP: recuperación de contraseña](https://cheatsheetseries.owasp.org/cheatsheets/Forgot_Password_Cheat_Sheet.html).
- [restic: repositorios cifrados y remotos](https://restic.readthedocs.io/en/stable/030_preparing_a_new_repo.html).
