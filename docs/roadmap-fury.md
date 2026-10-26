# Roadmap de operación DevOps para Mini Fury

Investigación pública y propuesta para el proyecto — 22 de octubre de 2026, America/Bogota.

## Qué describe públicamente Mercado Libre

Fury cubre inventario y ciclo de vida de activos, cadena de entrega de software, operación de runtimes y acceso de autoservicio a datos. Su cadena incluye código fuente, pruebas, construcción, artefactos, configuración y despliegue. No es únicamente una consola de Kubernetes. Fuente primaria: [The true power of an Internal Developer Platform, Mercado Libre Tech](https://medium.com/mercadolibre-tech/the-true-power-of-an-internal-developer-platform-ade8e88626f4).

La operación de despliegues utiliza aplicaciones con scopes, distingue construcción de versión y despliegue, admite estrategias progresivas, mide resultados y recuperaciones, utiliza operaciones idempotentes y organiza alertas con guardias y escalamiento. Fuente primaria: [30.000 deployments per day, Mercado Libre Tech](https://medium.com/mercadolibre-tech/30-000-deployments-per-day-heres-how-we-operate-without-losing-our-minds-0eddc0480fb9).

Fury ofrece servicios consumibles por aplicaciones: SQL, clave-valor, documentos, búsqueda, mensajería, secretos, objetos y auditoría. Fuente primaria: [For devs, by devs, Mercado Libre Tech](https://medium.com/mercadolibre-tech/for-devs-by-devs-67a395a03c3f).

Su integración con Kubernetes abstrae proveedores y describe un módulo de cómputo relacionado con Cluster API. Fuente primaria: [Kubernetes at Mercado Libre, Mercado Libre Tech](https://medium.com/mercadolibre-tech/kubernetes-at-mercado-libre-ec331bea1866).

Esta investigación no accede a la plataforma privada ni certifica su catálogo interno actual. Las siguientes prioridades, implementaciones y criterios son propuestas propias para Mini Fury; no se atribuyen a Mercado Libre.

## Punto de partida

Ya existen identidad, equipos y roles, catálogo persistente, documentación y dependencias, auditoría, métricas y alertas. El avance v0.5 añade histórico Grafana/Loki/Prometheus, recuperación por correo, cola cifrada de entregas, despliegue HTTPS independiente, respaldos cifrados configurables y carga mixta. La activación de dominio y proveedores externos continúa pendiente porque todavía no se han contratado/configurado.

La brecha principal es el control real del ciclo de vida de los workloads: Kubernetes y los despliegues siguen sin conectarse. Más pantallas de demostración no resolverían esa brecha.

## Prioridad inmediata: operar una aplicación real

### 1. Conector Kubernetes de lectura

Vincular aplicación y equipo con clúster, namespace y workload. Guardar referencias a credenciales de servicio y comprobar salud del conector. Listar pods, contenedores, eventos y réplicas autorizados, con paginación y tiempos de espera.

Criterio de aceptación: un lector ve únicamente workloads de sus equipos; no recibe kubeconfig ni tokens. Un namespace escrito a mano en la URL no amplía su acceso.

### 2. Logs de pods con aislamiento

Añadir selección de aplicación, pod y contenedor; últimas líneas y seguimiento cancelable. Incorporar Alloy en Kubernetes para enviar a Loki con etiquetas verificadas de aplicación/equipo. La API debe construir el selector desde asociaciones autorizadas, sin aceptar LogQL libre del navegador.

Criterio: límites por consulta y por usuario, acceso revocado al salir del equipo y ninguna lectura entre namespaces ajenos. La etiqueta de un log por sí sola no concede acceso.

### 3. Scopes o instancias de ejecución

Separar la definición de aplicación de cada ejecución: ambiente, versión/digest, región, namespace, recursos, dominio y criticidad. Conservar el historial de cambios.

Criterio: la misma aplicación puede tener staging y producción con configuraciones y permisos independientes. Una versión es inmutable y puede promoverse sin reconstruirse.

## Prioridad siguiente: entrega controlada

### 4. Builds y registro de artefactos

Conectar GitHub/GitLab, recibir webhooks firmados, ejecutar CI y registrar commit, pruebas, imagen y digest. Separar las credenciales del proveedor de los datos visibles del catálogo.

Criterio: cada imagen se vincula a un commit y a resultados verificables. Repetir un webhook no duplica el trabajo.

### 5. Despliegues GitOps y recuperación

Integrar ArgoCD y plantillas versionadas. Crear operaciones asíncronas con estados, idempotencia, límites, reintentos y auditoría. Empezar con rollout estándar; incorporar canary o blue/green después de medir salud real.

Criterio: se despliega una imagen por digest, se explica el fallo y se recupera la versión previa sin perder trazabilidad. La cola de entregas de v0.5 no debe convertirse sin diseño adicional en un motor de despliegues.

### 6. Configuración y secretos

Administrar variables no sensibles y referencias a Vault/External Secrets por scope. Añadir rotación, validación, separación de permisos y auditoría.

Criterio: el frontend no recibe valores secretos. Los cambios registran referencias y versiones; nunca el contenido.

### 7. Dominios y tráfico

Gestionar ingress, certificados, rutas, redirecciones y políticas de red mediante cambios revisables. Incorporar pesos de tráfico únicamente cuando existan despliegues progresivos.

Criterio: validación de dominio y certificado, vista previa del cambio y recuperación auditable. No abrir exposición pública automáticamente al registrar una aplicación.

## Prioridad posterior: operación de equipos y plataforma

### 8. Incidentes, guardias y escalamiento

Agrupar alertas por incidente, equipo y scope; añadir responsables, cronología, silencios temporales, escalamiento y runbook. Mantener separadas las acciones reconocer, silenciar y resolver.

Criterio: un incidente crítico tiene responsable, canal y reglas de escalamiento; una notificación fallida es visible. Integrar proveedores reales cuando existan destinos autorizados.

### 9. SLO y presupuesto de error

Definir objetivos de disponibilidad y latencia por servicio, periodos de evaluación, consumo de presupuesto y alertas por velocidad de deterioro.

Criterio: las gráficas provienen de métricas reales. No calcular disponibilidad a partir del porcentaje de preparación documental.

### 10. Plantillas de autoservicio

Ofrecer plantillas Go/Node con pruebas, Dockerfile, CI, salud, telemetría y manifiestos. Versionarlas e informar qué aplicaciones requieren actualización.

Criterio: una plantilla crea un repositorio funcional y despliega un ejemplo repetible; no se limita a copiar archivos sin comprobarlos.

### 11. Servicios administrados

Solicitar PostgreSQL, Redis, colas y almacenamiento desde catálogos aprobados. Implementar adaptadores de infraestructura y flujos de plan/aprobación/aplicación antes de automatizar recursos cloud.

Criterio: cuotas, propietario, coste estimado identificado como tal, política de respaldo y baja controlada. Sin borrados irreversibles desde una acción ambigua.

### 12. Capacidad y costes

Mostrar requests/limits, consumo, capacidad, escalado y costes por equipo/scope. Empezar por recursos Kubernetes y añadir facturación real de los proveedores más adelante.

Criterio: separar consumo medido, coste facturado y estimaciones. Usar presupuestos y límites antes de habilitar escalado autónomo.

### 13. Seguridad de entrega y gobierno

Incorporar escaneo de imágenes, SBOM, firma de artefactos, políticas por ambiente y aprobación para producción. Añadir SSO/MFA cuando se elija un proveedor de identidad.

Criterio: una política bloquea artefactos no conformes y deja evidencia; las excepciones tienen responsable y vencimiento.

### 14. Trabajos operativos y recuperación

Añadir jobs/cronjobs, mantenimiento, pruebas de restauración periódicas y registro de ejecuciones. Operaciones de reinicio o escalado requieren permisos explícitos, límites y auditoría.

Criterio: cada ejecución tiene estado, salida saneada, cancelación y tiempos de espera; no se permite ejecutar shell arbitrario con credenciales del clúster.

## Orden recomendado de implementación

Conector Kubernetes → logs y estado real → scopes → builds y versiones → despliegue GitOps → recuperación → secretos/tráfico → incidentes/SLO → autoservicio y costes.

Antes de activar producción de Mini Fury: dominio y DNS, SMTP autorizado, destino de alertas, repositorio externo cifrado, custodia de claves y un ensayo de restauración con objetivos de recuperación definidos. La arquitectura debe mantener adaptadores por proveedor, permisos en backend y operaciones duraderas, sin introducir microservicios hasta que una necesidad medida lo justifique.
