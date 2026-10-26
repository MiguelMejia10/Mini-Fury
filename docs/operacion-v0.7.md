# Mini Fury v0.7 — puntos 6 al 10

El Centro DevOps (`#devops`) continúa el bloque v0.6 en este orden: 6 incidentes, 7 SLO, 8 plantillas, 9 servicios administrados y 10 capacidad/costos. Son funciones persistentes con permisos del catálogo; las pruebas operativas se ejecutan en el laboratorio aislado.

## 6. Incidentes

Cada incidente pertenece a una aplicación y opcionalmente a uno de sus scopes. Incluye severidad, responsable autorizado, runbook HTTPS, plazo de escalamiento, cronología y control de versión. Reconocer, silenciar, resolver y reabrir son acciones distintas. El silencio tiene vencimiento y no resuelve el incidente. La cronología muestra hasta 200 eventos; el resto permanece en PostgreSQL.

El evaluador revisa vencimientos cada minuto, reclama filas con bloqueo y evita escalamiento duplicado entre instancias. Si existe un webhook de alertas configurado, utiliza la cola cifrada de entregas con reintentos. Si no existe, registra `not_configured`; el detalle consulta el estado real de la entrega. Reabrir permite un nuevo escalamiento. No hay rotaciones de guardia/calendario ni proveedores de paging conectados todavía.

## 7. SLO y presupuesto de error

Objetivos inmutables de disponibilidad HTTP o latencia (100, 300 o 1000 ms), con ventanas de 1, 7 o 28 días y objetivo entre 90% y 99.999%. Se pueden deshabilitar/habilitar sin borrar el historial.

Las plantillas exponen `/mini-fury/metrics`, un protocolo JSON de contadores acumulativos por proceso:

```json
{"epoch":"identificador del arranque","total":1000,"errors":1,"le100":950,"le300":990,"le1000":999}
```

`total` cuenta solicitudes de aplicación; `errors`, errores 5xx; `le100/le300/le1000`, solicitudes dentro de cada umbral. Salud y scrapes quedan excluidos. El recolector obtiene cada pod autorizado mediante el proxy de Kubernetes; nunca recibe destinos arbitrarios del navegador. Requiere `get pods/proxy` en el namespace. Comprueba la cadena de propiedad del pod y excluye pods de servicios administrados.

La primera lectura establece una línea base. Reinicios, disminución de contadores, huecos mayores de dos minutos o pods ausentes producen cobertura incompleta; no se convierten en disponibilidad del 100%. Los intervalos incompletos no se usan para calcular el SLI. El resultado distingue ausencia de datos, cobertura parcial y medición desactualizada. La ventana empieza a acumularse desde las primeras mediciones: crear un SLO de 28 días no fabrica 28 días de historia.

Disponibilidad = buenas/total. Presupuesto permitido = total × (1 − objetivo). Burn = proporción de malas / (1 − objetivo). Se muestran presupuesto restante y burn de 5 minutos/1 hora. Un intervalo completo con al menos 20 solicitudes y burn >=14.4 abre un incidente deduplicado; es una regla inicial de intervalo, **no** una política multiwindow completa ni un bloqueo automático de releases. Retención: 35 días. El evaluador procesa hasta 20 objetivos pendientes por ciclo; no se ha certificado su capacidad para cientos de aplicaciones.

Fuentes conceptuales: [Google SRE: Implementing SLOs](https://sre.google/workbook/implementing-slos/) y [Error Budget Policy](https://sre.google/workbook/error-budget-policy/). El protocolo JSON local no pretende ser un adaptador Prometheus universal.

## 8. Plantillas

Go HTTP y Node HTTP v1.0.0 incluyen pruebas, Dockerfile, salud, contadores, CI GitHub/GitLab, manifiesto Kubernetes y procedencia de plantilla. Crear desde una plantilla registra la aplicación y su procedencia en una sola transacción, y permite descargar el ZIP bajo los mismos permisos del catálogo. El estado informa versión instalada/disponible.

Se prueban los ZIP emitidos por la API construyendo sus imágenes Docker, incluidas las pruebas del starter. El ZIP no crea un repositorio remoto, no publica imágenes ni despliega recursos. La URL del repositorio se registra para conectarla después. GitLab requiere runner Docker; el manifiesto exige sustituir la imagen por un digest aprobado. La aplicación se despliega desde Operación al configurar su conexión. Las imágenes base y acciones CI se deben mantener y verificar como parte de la política de entrega.

## 9. Servicios administrados

Perfiles de PostgreSQL y Redis para desarrollo, definidos por el operador en `serviceProfiles` de `RUNTIME_CONNECTIONS_FILE`: ID/nombre, engine, imagen por digest, alias de Secret aprobado, CPU/memoria y StorageClass. No se aceptan imágenes, comandos, kubeconfigs o credenciales desde el navegador.

Cada scope admite dos servicios de 1–10 GiB. La UI presenta el plan (perfil, recursos, almacenamiento, red interna y política de respaldo); confirmar crea la reserva y el trabajo persistente en una sola transacción. El worker reconcilia StatefulSet y Service interno, verifica propiedad y espera disponibilidad. El estado real se consulta por separado del resultado de la última operación.

Suspender escala a cero; reanudar conserva el PVC. No hay borrado de datos desde la UI. Las políticas de retención de PVC son `Retain` al suspender o eliminar el StatefulSet. Referencia: [Kubernetes StatefulSet API](https://kubernetes.io/docs/reference/kubernetes-api/apps/stateful-set-v1/).

Los Secrets son creados por el operador y referenciados por el workload. La API no puede leer sus valores ni ejecutar comandos dentro del pod. PostgreSQL usa un alias de contraseña; Redis monta un archivo de configuración secreto con autenticación. Los servicios no reciben tokens de ServiceAccount y ejecutan procesos sin root.

Estos perfiles están **deshabilitados para producción** hasta contar con respaldos externos verificados, restauración de los datos del servicio, políticas de red/TLS y operación de alta disponibilidad. Conservar un PVC no equivale a un respaldo. Los respaldos de la base de Mini Fury no incluyen los datos PostgreSQL/Redis aprovisionados.

## 10. Capacidad y costos

Muestra recursos declarados por aplicación/scope y consulta de consumo real del scope mediante Metrics Server. Las estimaciones usan los requests declarados del worker, almacenamiento retenido y tarifas de referencia ingresadas por un administrador de equipo/plataforma. Los totales incluyen todos los scopes, aunque la lista muestra hasta 50.

Estimación mensual = 730 × (cores solicitados × tarifa/core-hora + GiB memoria solicitados × tarifa/GiB-hora) + GiB almacenamiento × tarifa/GiB-mes. Suspender un servicio excluye su cómputo de la estimación y conserva su almacenamiento. Las tarifas y el presupuesto tienen versión optimista. La UI indica si la estimación supera el presupuesto.

No hay importación de facturas ni costos reales cloud: `invoicedCost` permanece nulo. No incluye red, impuestos, control plane ni costos compartidos. La estimación representa recursos declarados, incluso cuando aún no están desplegados. No reserva capacidad ni bloquea despliegues automáticamente.

## Probar el bloque completo

Desde la raíz del proyecto:

```sh
python3 scripts/lab/start.py
python3 scripts/lab/verify-expansion.py
```

El ensayo abre un incidente, despliega la aplicación instrumentada, genera tráfico real con errores, comprueba ambos SLO, descarga y compila Go/Node, aprovisiona PostgreSQL/Redis, inserta una fila SQL y comprueba su conservación tras suspensión/reanudación. Finalmente configura tarifas de prueba y verifica consumo, presupuesto y ausencia de factura.

La evidencia privada queda en `.local/kube-lab/test/validation-v07.json`. La cuenta está en `.local/kube-lab/test/account.env`; no compartir secretos. La UI del laboratorio usa `http://127.0.0.1:5174/#devops`; la principal, `http://127.0.0.1:5173/#devops`. Vite se inicia según la guía v0.6.

## Recuperación y límites

El respaldo conserva los registros nuevos. Al restaurar a una base nueva se revocan sesiones, entregas, leases y trabajos pendientes; además se eliminan líneas base de contadores, se deshabilitan SLO, se detienen escalaciones históricas y los servicios quedan pendientes de reconciliación (`failed`). Esto impide que la copia restaurada opere el clúster automáticamente. La reactivación debe ocurrir después de revisar conexiones y estado externo.

La aplicación sigue siendo un monolito modular Go con PostgreSQL y workers acotados. Los proveedores externos inexistentes continúan pendientes. El ensayo local y `go test -race` no certifican 100–200 usuarios concurrentes ni sustituyen una prueba de carga representativa de operación Kubernetes.
