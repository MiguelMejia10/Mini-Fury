# Validación local — Mini Fury v0.4

Ejecución: 22 de octubre de 2026 UTC. Los resultados corresponden al equipo local con Docker Desktop; no equivalen a capacidad de producción.

## Compilación y análisis

- TypeScript y build Vite: correctos, 1.593 módulos transformados.
- Backend: `go vet ./...` y `go test -race -count=1 -timeout=120s ./...`, correctos.
- Imagen de pruebas reconstruida con el código final antes de ejecutar la suite.
- PostgreSQL de integración independiente, desechable, sin puertos publicados. Ninguna cuenta de prueba creada en la base principal.
- Cuatro pruebas de seguridad del respaldo: correctas.
- `git diff --check`: correcto.

## Comportamientos comprobados por HTTP y PostgreSQL

- Crear equipos como desarrollador sin obtener administración de plataforma.
- Invitar editores y lectores, aceptar con el correo correcto y rechazar aceptación ajena, repetida, vencida o revocada.
- Lectura compartida por equipo, rechazo de escrituras del lector y de administración por el editor.
- Retirada de membresía elimina acceso incluso al creador de una aplicación que pasó a ser compartida.
- Aplicaciones personales conservan el aislamiento anterior.
- Desactivación, retirada y degradación del último administrador activo rechazadas.
- Dos administradores degradándose simultáneamente: una operación completa y la otra recibe conflicto; queda un administrador activo.
- Documentación, enlaces, ruta declarada y preparación del catálogo persistidas; enlaces inseguros y rutas remotas rechazados.
- Dependencias persistidas y rechazo de ciclos y referencias propias.
- Dos dependencias opuestas creadas simultáneamente en aplicaciones distintas: una completa y la otra se rechaza; no aparece un ciclo.
- Estado operacional y exportación Prometheus restringidos al administrador global.
- Alertas por errores y latencia, reconocimiento auditado, conflicto ante reconocimiento repetido y rechazo a usuarios sin permiso.
- Recuperación conserva historia; recurrencia incrementa ocurrencias y elimina reconocimiento anterior.
- Rechazo del reconocimiento de señales antiguas.
- Métricas concurrentes, expiración de ventana, histograma acumulativo y exclusión del sondeo operacional verificadas.

Las pruebas previas de identidad, CSRF, revocación de sesiones, edición concurrente y archivado también siguen pasando.

## Carga aislada k6

Escenario: lectura autenticada del catálogo, rampa de 0 a 10 usuarios virtuales, luego hasta 25 y descenso a 0. Catálogo pequeño con una aplicación. Cada iteración tiene una pausa de 100 ms. Duración de etapas configurada: 65 s. API y PostgreSQL exclusivos del perfil perf; sin acceso a datos principales.

- 9.127 consultas del catálogo verificadas; 9.130 solicitudes totales incluyendo preparación.
- Cero consultas fallidas; 100% de comprobaciones correctas.
- Latencia del catálogo: media 5,27 ms, mediana 3,06 ms, p95 16,31 ms, máximo 71,30 ms.
- Umbrales cumplidos: fallos menores a 1%, p95 menor a 500 ms, p99 menor a 1.000 ms y comprobaciones mayores a 99%.
- El resumen no exportó el valor numérico de p99; sí registró que su umbral pasó.
- Informe completo local: `.local/perf/catalog-summary.json`, excluido de Git.
- Servicios de carga detenidos al terminar.

El ensayo mide lecturas con una sesión compartida de prueba. No mide logins masivos, contención de escrituras, catálogos grandes, infraestructura productiva ni máxima capacidad sostenible. Para dimensionar producción se requiere un escenario representativo de esos factores.

## Migración y recuperación

Respaldo anterior: `.local/backups/20261022T060210Z-ca933ea1.dump`. Se restauró en una base nueva, se consultó y se eliminó solo esa base temporal.

Se actualizó la API local y se aplicó la migración 004. Se conservaron las cantidades anteriores: un usuario, una aplicación y dos eventos de auditoría. Las migraciones pasaron de tres a cuatro; no se crearon equipos ni cuentas de muestra en la base principal.

Respaldo posterior: `.local/backups/20261022T060958Z-001f7ba7.dump`. Restauración aislada correcta incluyendo equipos, membresías, invitaciones, dependencias y alertas. Esas tablas estaban vacías en el entorno principal al verificar. Las sesiones restauradas se invalidaron; las sesiones principales y las credenciales existentes no se modificaron.

Comprobación de la API actualizada: `/api/health` devolvió 200. Sin sesión, `/api/teams`, `/api/operations/status` y `/api/operations/metrics` devolvieron 401.

## Pendiente

La revisión visual de pantallas autenticadas continúa aplazada por decisión del usuario. Se comprobó compilación y comportamiento mediante pruebas HTTP/base de datos, sin dar por validada esa revisión. No se ejecutó CI remota ni se publicó un commit en este avance.
