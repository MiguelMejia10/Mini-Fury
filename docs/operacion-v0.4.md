# Mini Fury v0.4 — Equipos, calidad y operación

## Organización y permisos

Cualquier usuario activo puede crear un equipo y se convierte en su administrador. El administrador global puede consultar y administrar todos los equipos. Ser administrador de equipo no otorga privilegios globales.

- Administrador de equipo: miembros, invitaciones, asignación de aplicaciones y edición del catálogo compartido.
- Editor: registrar aplicaciones en su equipo, editar, archivar y restaurar las compartidas; no puede reasignarlas ni administrar integrantes.
- Lector: consultar catálogo, documentación y dependencias visibles.
- Sin membresía: sin acceso al catálogo de ese equipo, aunque haya creado originalmente una aplicación compartida.

Las aplicaciones sin equipo siguen siendo personales: creador y administrador global. El campo responsable es descriptivo y no otorga acceso. La migración conserva los registros existentes sin asignarlos automáticamente.

Para pasar una aplicación personal a un equipo, su dueño debe administrar el equipo de destino (o intervenir un administrador global). Para cambiar o quitar equipo se necesita administración de los equipos implicados. Los permisos se verifican de nuevo dentro de las transacciones.

Las invitaciones duran siete días y se aceptan desde Equipos usando exactamente el correo de la sesión. No crean cuentas ni envían email. El administrador global crea las cuentas desde Configuración. No se puede aceptar una invitación vencida, revocada, ajena o ya aceptada; las invitaciones a miembros existentes no sirven para promoverlos.

No se permite retirar, degradar o desactivar al último administrador activo de un equipo. Las operaciones administrativas comparten un bloqueo transaccional breve para preservar esta regla incluso bajo concurrencia con desactivaciones de cuentas. Las consultas ordinarias no toman ese bloqueo global.

## Calidad del catálogo

Cada aplicación admite documentación de texto hasta 20.000 caracteres, diez enlaces HTTPS con etiqueta, una ruta de salud local declarada hasta 200 caracteres y veinte dependencias. La documentación se representa como texto, sin ejecutar HTML. Las URLs no admiten credenciales incrustadas. La ruta de salud no ejecuta peticiones remotas.

Las dependencias requieren acceso al destino activo; se rechazan duplicados, referencias propias y ciclos. Un bloqueo transaccional específico serializa los cambios del grafo para impedir ciclos entre escrituras simultáneas. Solo se devuelven dependencias cuyo destino sigue siendo visible para el usuario. Una edición conserva enlaces ocultos; no revela ni borra relaciones inaccesibles accidentalmente.

La preparación cuenta seis comprobaciones: responsable, repositorio, descripción, documentación, ruta de salud y equipo. Su porcentaje mide completitud del registro; no certifica despliegue, disponibilidad ni calidad del software.

La edición y el archivado conservan control optimista de versión y auditoría transaccional. Las consultas y relaciones se cargan por lotes, con paginación, sin hacer una consulta adicional por cada aplicación.

## Operación real de Mini Fury

Observabilidad muestra datos de la API que atiende la consulta: identificador de instancia, uptime, tráfico, 5xx, latencia, solicitudes simultáneas, heap del proceso Go, goroutines, salud y uso del pool PostgreSQL. No son CPU, réplicas ni métricas de las aplicaciones del catálogo.

La ventana deslizante es de cinco minutos. El histograma usa intervalos fijos; p95 representa el límite superior del intervalo, no un percentil exacto. Las consultas `/api/operations*` se excluyen para evitar que el sondeo altere sus propias métricas. El sondeo ocurre cada diez segundos y se pausa cuando la pestaña está oculta. La memoria de métricas es acotada y no usa IDs de usuario ni URLs como etiquetas.

Se evalúan cada diez segundos dos reglas con un mínimo de veinte solicitudes en la ventana:

- Errores: proporción de 5xx mayor o igual a 5%.
- Latencia: límite superior estimado de p95 mayor a 1.000 ms.

Las alertas persisten en PostgreSQL. Reconocerlas registra al actor en auditoría, pero no las resuelve. Al desaparecer la condición se conserva el registro; una recurrencia incrementa el contador y elimina el reconocimiento anterior. Una señal sin evaluación durante dos minutos se muestra sin señal; no se infiere recuperación de una instancia detenida. No hay envío externo de notificaciones.

Las métricas se reinician con cada proceso. Configura un `INSTANCE_ID` estable y distinto por instancia si operas varias; el estado es por instancia, no agregado. La indisponibilidad de PostgreSQL aparece en el estado en vivo, pero no se promete persistir alertas mientras PostgreSQL está caído. El histograma se exporta en formato Prometheus bajo sesión de administrador. El scraping con identidad de servicio y la agregación externa quedan pendientes.

## API nueva

Todas las rutas requieren sesión. Las escrituras requieren origen permitido y token CSRF. Los listados aceptan `limit` y `offset` dentro de los límites habituales.

- `GET/POST /api/teams`
- `GET /api/teams/{id}/members`
- `PUT/DELETE /api/teams/{id}/members/{user}`; el PUT recibe `role`.
- `GET/POST /api/teams/{id}/invitations`; el POST recibe `email` y `role`.
- `DELETE /api/teams/{id}/invitations/{invitation}`
- `GET /api/team-invitations`
- `POST /api/team-invitations/{id}/accept`
- `GET /api/operations/status` (administrador global)
- `GET /api/operations/alerts` (administrador global)
- `POST /api/operations/alerts/{id}/ack` (administrador global)
- `GET /api/operations/metrics` (administrador global)

Crear/editar aplicaciones añade `teamId`, `documentation`, `links: [{label,url}]`, `healthCheckPath` y `dependencyIds`. La respuesta incluye `canEdit`, `canManageTeam`, `dependencies` y `readiness`. Las autorizaciones efectivas siempre se resuelven en backend.

## Pruebas, carga y recuperación

```sh
sh scripts/check.sh
sh scripts/load-test.sh
python3 scripts/database.py backup
python3 scripts/database.py verify .local/backups/ARCHIVO.dump
```

El perfil test usa PostgreSQL separado y desechable en tmpfs, sin puertos al host. El perfil perf usa su propia base y API aisladas, una cuenta exclusivamente desechable y hasta 25 usuarios virtuales k6. No utiliza las credenciales de bootstrap reales ni modifica la base principal. El script detiene únicamente sus servicios de carga al terminar. Los servicios test/perf son opcionales y sus volúmenes no contienen datos persistentes del producto.

El informe de carga se escribe en `.local/perf/catalog-summary.json`, excluido de Git. Los umbrales son menos de 1% de fallos, p95 menor a 500 ms, p99 menor a 1.000 ms y más de 99% de comprobaciones correctas. Mide lectura autenticada con un catálogo pequeño; no establece capacidad de producción ni rendimiento de escritura.

La migración 004 añade equipos, membresías, invitaciones, metadatos, dependencias y alertas sin eliminar datos anteriores. El verificador de respaldos comprueba también las tablas nuevas cuando existe la migración; los respaldos anteriores siguen verificándose. Las restauraciones siempre usan una base nueva y revocan sesiones restauradas.
