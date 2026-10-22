# Validación v0.3

## Ejecutado correctamente

- `npm run build`: tipos y paquete de producción.
- `go vet ./...` y `go test -race -count=1 ./...` en contenedor temporal con PostgreSQL.
- Suite existente y `TestCatalogAndAccountFeatures`:
  - normalización de etiquetas, metadatos y búsqueda con filtros;
  - permiso de edición por creador/admin, sin transferencia implícita del creador;
  - ocho ediciones concurrentes con la misma versión: una aplicada y siete rechazadas por conflicto;
  - archivo/restauración y conservación de nombres reservados;
  - rechazo de edición de aplicaciones archivadas;
  - auditoría con actor, acción, recurso y rango temporal;
  - rechazo de fechas inválidas y ausencia de eventos de éxito en mutaciones fallidas;
  - cierre global, cambio de contraseña y rechazo de un login con versión de credenciales revocada;
  - rechazo de contraseña anterior, cuenta desactivada y operaciones administrativas de developers;
  - reactivación con nueva sesión y desactivaciones concurrentes de administradores sin pérdida de todos los accesos administrativos de prueba.
- Cuatro pruebas Python de seguridad de la herramienta de respaldo: checksum alterado, bloqueo del destino principal, fallo por base ya existente y limpieza de una restauración temporal fallida.
- YAML de GitHub/GitLab cargado correctamente por el parser local. No se ejecutó un pipeline remoto: el proyecto aún no tiene repositorio Git configurado.
- Respaldo anterior a la migración: restaurado con dos migraciones, un usuario, una aplicación y dos eventos de auditoría.
- Respaldo posterior: restaurado con tres migraciones, un usuario, una aplicación y dos eventos de auditoría.
- Ambas verificaciones usaron bases nuevas `mini_fury_restore_*`, eliminadas tras comprobarlas. Las sesiones de la base restaurada se invalidaron; la base principal no fue modificada por la restauración.
- API en ejecución y PostgreSQL saludable tras actualizar la versión.

## Pendiente explícito

La revisión visual de pantallas autenticadas fue aplazada por el usuario. No se creó una cuenta temporal de administrador: la revisión automática rechazó esa ampliación de accesos. Los cambios de contraseña y estado de cuentas se probaron solo con las cuentas de fixture de la suite, sin alterar las credenciales del usuario.

La suite verifica funcionalidad y concurrencia acotada; no determina capacidad de producción ni latencias p95/p99. No se configuró despliegue real de aplicaciones.
