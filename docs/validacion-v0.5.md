# Validación de Mini Fury v0.5

Fecha de trabajo: 22 de octubre de 2026, America/Bogota. Los timestamps de los respaldos usan UTC y pueden corresponder al día 23.

## Código y pruebas

- Frontend: TypeScript y build Vite correctos; 1.595 módulos procesados.
- Go: `go vet ./...` y suite completa con `go test -race -count=1 -timeout=180s ./...`, correctos, utilizando PostgreSQL desechable.
- Verificación posterior específica de recuperación y telemetría tras endurecer el mínimo Unicode de contraseña: correcta.
- Siete pruebas de seguridad de respaldo: correctas. Incluyen checksum, destino existente, limpieza solo de bases creadas, rechazo de subida tras fallo de pg_dump y rechazo de prune tras fallo de subida.
- Las configuraciones productivas combinadas se validaron sin publicar servicios ni imprimir secretos; API y PostgreSQL sin puertos, observabilidad en red privada y backups con salida controlada.

## Recuperación y entregas

Prueba HTTP/base de datos con cuenta exclusivamente desechable:

- Origen ajeno rechazado.
- Respuesta idéntica para correo activo e inexistente.
- Enlace obtenido de un emisor fixture, sin imprimir el token ni enviarlo a un destinatario externo.
- Dos consumiciones simultáneas: exactamente una completa y la otra se rechaza.
- Cambio de hash, revocación de sesiones y auditoría verificados.
- Tokens vencidos o asociados a otra versión de credenciales rechazados.
- Contraseña Unicode con menos de doce caracteres rechazada aunque tenga más de doce bytes.
- Payload cifrado, autenticación del tipo de mensaje y borrado tras entrega comprobados.
- Dedupe de trabajos, reintento tras fallo de proveedor y reclamación concurrente por workers comprobados.
- Dos alertas activas generan dos trabajos; repetir evaluación no los duplica. La recuperación genera dos trabajos adicionales, entregados al fixture.
- Webhook HTTP de producción rechazado; fixture local entrega con ID y firma; las redirecciones no se siguen.
- Entrega SMTP real al buzón local Mailpit comprobada con destinatario `smtp-fixture@mini-fury.local`, sin tokens de recuperación ni cuentas reales.

La recepción en SMTP externo, Slack o Discord reales no se validó porque el usuario todavía no dispone de esos proveedores/destinos.

## Histórico operativo

- Grafana 13.2.4 arrancó en 127.0.0.1:3300; se verificó el dashboard provisionado con cuatro paneles.
- Prometheus v3.15.1 reportó el target `mini-fury` en estado up usando token de servicio.
- Alloy v1.21.2 leyó los registros JSON reales y Loki 3.7.9 los devolvió en consultas históricas.
- `python3 scripts/verify-observability.py --restart` conservó muestras anteriores al reinicio: tres líneas históricas y 33 puntos de métricas en la ventana fija consultada. Son los resultados del ensayo, no totales del sistema.
- Las rutas de histórico rechazan usuarios sin rol administrador y una consulta anónima a logs/histórico devuelve 401.
- Los tests verifican rangos inválidos, autenticación de scrape y exclusión de información sensible en rutas registradas.

La retención configurada es de siete días, con límite adicional de 1 GiB para Prometheus y rotación de archivos previa al recolector. No se esperaron siete días para certificar la eliminación eventual de Loki. La topología es local y de una sola instancia, sin promesa de alta disponibilidad.

## HTTPS

`sh scripts/test-https.sh` pasó en un proyecto Docker exclusivo y desechable:

- Frontend servido mediante HTTPS con CA local verificada explícitamente por el cliente de prueba.
- HSTS y CSP presentes.
- `/internal/metrics` bloqueado en el proxy con 404.
- Login de fixture emite cookie `__Host-mini_fury_session` con Secure, HttpOnly y SameSite=Strict.
- Direcciones reenviadas se aceptan solamente desde el proxy declarado.
- Proyecto y volúmenes de prueba eliminados al terminar; ningún certificado instalado en el almacén de confianza del sistema.

No se obtuvo certificado público ni se configuró DNS porque aún no existe dominio. El entorno principal conserva HTTP local de desarrollo.

## Carga mixta k6

Ejecución `sh scripts/load-test.sh operations`, API y PostgreSQL desechables:

- Doce cuentas de desarrollo, 120 aplicaciones iniciales y hasta cincuenta usuarios virtuales.
- Búsqueda paginada, detalle, lectura previa a edición, actualización con versión, creación y archivado.
- Etapas de carga configuradas: 100 s, además de preparación.
- 30.257 solicitudes totales; 30.111 durante la carga.
- Cero fallos HTTP; 28.072 comprobaciones correctas y cero fallidas.
- 2.543 altas/ediciones contadas; el archivado también se comprobó, pero no incrementa ese contador.
- Latencia de carga: media 2,60 ms; mediana 1,56 ms; p95 9,38 ms; p99 15,30 ms; máximo 42,79 ms.
- Todos los umbrales cumplieron. Los 409 por control de versiones se aceptan como conflictos explícitos y se cuentan por separado cuando aparecen; no aparecieron en este ensayo.
- Informe local completo: `.local/perf/operations-summary.json`, excluido de Git.

No representa tráfico observado de producción ni prueba autenticaciones masivas, escrituras prolongadas, datasets grandes, caídas de proveedores o conectores Kubernetes. No se compara como mejora directa con v0.4 porque los escenarios son distintos. Los servicios de carga se detuvieron al terminar.

## Respaldos y conservación de datos

Se conservó la base principal: un usuario, una aplicación y dos eventos de auditoría. Las migraciones pasaron de cuatro a cinco. No se crearon cuentas ni enlaces de recuperación de muestra en esa base.

- Respaldo previo: `.local/backups/20261023T024342Z-74d9c9d8.dump`.
- Respaldo posterior: `.local/backups/20261023T030308Z-d4f61ba1.dump`.
- Ambos se verificaron mediante restauraciones aisladas y se eliminaron únicamente las bases creadas para esos ensayos.
- Repositorio restic local cifrado inicializado; respaldo, integridad, retención y descarga/restauración de `/database.dump` correctos.
- La restauración cifrada mostró cinco migraciones y una aplicación, e invalidó sesiones, intentos de acceso, enlaces de recuperación y entregas restauradas.

El repositorio cifrado local no es almacenamiento externo. El adaptador S3 está configurado por entorno y documentado, pero no se probó contra un bucket remoto porque todavía no existe. El worker periódico está disponible; no se activó automáticamente un calendario de respaldos externos.

## Límites y pendientes

- Activar y probar dominio, SMTP real, webhook autorizado y S3 externo cuando estén disponibles.
- Revisión visual de las pantallas autenticadas pendiente por decisión previa del usuario; se validaron compilación y comportamiento por API/base de datos.
- Kubernetes, logs de pods y despliegues siguen fuera de este avance.
- La investigación de Fury usa fuentes públicas citadas en `docs/roadmap-fury.md`; no acceso a su plataforma interna.
- Cambios locales, sin commit/push ni CI remota en este avance.
