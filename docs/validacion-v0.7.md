# Validación v0.7

La prueba real `scripts/lab/verify-expansion.py` completó los puntos 6–10 en el entorno aislado. Evidencia: `.local/kube-lab/test/validation-v07.json`.

- Incidente creado, asignado, reconocido, silenciado, reactivado y resuelto; cronología persistente.
- Aplicación Go instrumentada compilada y desplegada por digest. Tráfico real con respuestas 200/503; SLI de disponibilidad y latencia calculados desde deltas por pod. Agotamiento de presupuesto detectado e incidente de burn creado.
- ZIP Go y Node emitidos por la API, descargados y construidos con Docker; las pruebas incluidas en ambos starters pasaron.
- PostgreSQL y Redis reales en StatefulSets, con PVC y secretos referenciados. Se insertó una fila en PostgreSQL, se suspendió/reanudó el servicio y se comprobó que la fila persistía.
- Tarifas de referencia y presupuesto guardados; estimación positiva, alerta visual de presupuesto, consumo de Metrics Server y costo facturado nulo.
- Suite Go completa con PostgreSQL desechable y detector de carreras aprobada, incluidos los módulos anteriores. Casos nuevos de aislamiento, optimistic locking, escalamiento concurrente, idempotencia, cuotas, gobierno de costos y archivos de plantilla.
- Restauración real de un respaldo del laboratorio con nueve migraciones, incidentes, mediciones y ambos servicios: SLO deshabilitados, contadores eliminados, escalamiento detenido y servicios marcados para reconciliación. La copia desechable se eliminó al terminar.
- TypeScript y build Vite aprobados. Sintaxis Python/shell y pruebas de seguridad del worker de respaldos aprobadas.

Las notificaciones externas no se activaron porque no existe destino configurado. La revisión visual de las cinco pantallas con sesión del usuario y las pruebas de carga de producción continúan pendientes. La inspección visual sin sesión confirmó que el laboratorio requiere autenticación y muestra v0.7.
