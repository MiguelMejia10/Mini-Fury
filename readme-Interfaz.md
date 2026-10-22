# Mini Fury --- Guía de Interfaz

## 1. Objetivo

La interfaz de **Mini Fury** debe transmitir la sensación de una
herramienta técnica de infraestructura, inspirada en terminales, planos
técnicos y software de ingeniería, pero sin sacrificar legibilidad ni
usabilidad moderna.

El diseño será **minimalista, retro-técnico y profesional**. No debe
parecer una terminal real ni un dashboard SaaS genérico. La identidad
visual se basa en tipografía monoespaciada, líneas finas, paneles
geométricos, alto aprovechamiento del espacio y una paleta contenida.

La aplicación tendrá dos temas equivalentes:

-   **Light:** gris claro / blanco cálido con acentos café claro.
-   **Dark:** negro / gris carbón con acentos rojo suave tirando a
    rosado pastel.

La estructura, jerarquía y funcionalidad deben ser idénticas en ambos
temas. El cambio de tema únicamente modifica la presentación visual.

------------------------------------------------------------------------

## 2. Identidad visual

### Estilo

Conceptos que definen la interfaz:

`Retro Terminal` · `Blueprint` · `DevOps` · `Infrastructure` · `Minimal`
· `Technical UI`

Características principales:

-   Tipografía monoespaciada.
-   Bordes rectos y finos.
-   Esquinas discretamente marcadas como planos técnicos.
-   Muy pocas sombras o ninguna.
-   Separación de áreas mediante líneas y contraste, no mediante
    tarjetas flotantes.
-   Uso moderado del color.
-   Datos técnicos como protagonistas.
-   Gráficos sencillos y precisos.
-   Iconografía lineal.
-   Alta densidad de información sin saturación visual.
-   Estados fácilmente identificables.
-   Apariencia de herramienta profesional para ingeniería y operaciones.

------------------------------------------------------------------------

## 3. Tema Light

El tema claro utiliza una base neutra y cálida.

### Paleta conceptual

  Elemento            Color aproximado
  ------------------- --------------------------------
  Fondo principal     blanco cálido / gris muy claro
  Sidebar             gris cálido muy claro
  Paneles             blanco hueso
  Bordes              gris beige
  Texto principal     negro carbón
  Texto secundario    gris oscuro
  Acento principal    café claro / taupe
  Acento secundario   beige
  Success             verde suave
  Warning             beige/ámbar apagado
  Error               rojo pastel

El **café claro** debe utilizarse para navegación seleccionada,
gráficas, barras de utilización, detalles de bordes y elementos
interactivos.

No utilizar naranja intenso.

La interfaz debe sentirse limpia, técnica y ligeramente inspirada en
planos impresos o herramientas de arquitectura.

------------------------------------------------------------------------

## 4. Tema Dark

El tema oscuro utiliza negro y diferentes niveles de gris carbón.

### Paleta conceptual

  Elemento            Color aproximado
  ------------------- ------------------------
  Fondo principal     negro carbón
  Sidebar             negro profundo
  Paneles             gris carbón
  Bordes              gris medio oscuro
  Texto principal     gris/blanco suave
  Texto secundario    gris claro
  Acento principal    rojo rosado pastel
  Acento secundario   rosa grisáceo
  Success             verde menta suave
  Warning             amarillo/crema apagado
  Error               rojo pastel

El acento **no debe ser naranja ni rojo intenso**. Debe utilizar un rojo
suave con una ligera tendencia a rosado pastel.

El resultado debe sentirse técnico y elegante, evitando una estética
gamer o cyberpunk.

------------------------------------------------------------------------

## 5. Tipografía

La interfaz debe priorizar una fuente monoespaciada moderna.

Opciones recomendadas:

-   JetBrains Mono
-   IBM Plex Mono
-   Geist Mono
-   Source Code Pro

La tipografía refuerza el concepto de terminal sin convertir toda la
experiencia en una CLI.

Los títulos pueden representarse como:

`[ OVERVIEW ]`

`[ INFRAESTRUCTURA ]`

`[ APPLICATIONS ]`

También pueden utilizarse terminaciones visuales como:

`MINI FURY_`

`OVERVIEW_`

------------------------------------------------------------------------

## 6. Layout principal

La aplicación utiliza una estructura de tres zonas:

``` text
┌─────────────────────────────────────────────────────┐
│ Sidebar │               Top Bar                     │
│         ├───────────────────────────────────────────┤
│         │                                           │
│         │              Workspace                    │
│         │                                           │
│         │                                           │
└─────────────────────────────────────────────────────┘
```

### Sidebar

Debe permanecer visualmente estable y contener:

-   MINI FURY\_
-   Inicio
-   Aplicaciones
-   Despliegues
-   Infraestructura
-   Observabilidad
-   Secretos
-   Configuración

En la parte inferior:

-   Versión de la plataforma
-   Cluster activo
-   Usuario
-   Estado del sistema

Ejemplo:

``` text
PLATFORM v0.1.0
CLUSTER: prod-01
USER: jorge

● SYSTEM ONLINE
```

Puede incorporarse una ilustración wireframe discreta relacionada con
infraestructura, redes o arquitectura.

### Top Bar

Contendrá:

-   Buscador global.
-   Notificaciones.
-   Avatar.
-   Nombre del usuario.
-   Selector de perfil/opciones.

Ejemplo de búsqueda:

`Buscar aplicaciones, despliegues, logs...`

------------------------------------------------------------------------

## 7. Dashboard / Overview

Es la página principal de Mini Fury.

Encabezado:

``` text
[ OVERVIEW ]

Estado de la plataforma en tiempo real
                         PRODUCTION
```

### Indicadores principales

Mostrar cuatro métricas:

-   Aplicaciones
-   Servicios OK
-   Despliegues hoy
-   Incidentes

Ejemplo:

``` text
APLICACIONES       SERVICIOS OK       DESPLIEGUES HOY       INCIDENTES

12                 11 / 12            28                    1
+2 esta semana     91.7%              +12%                  -75%
```

------------------------------------------------------------------------

## 8. Tráfico de plataforma

Gráfico compacto para visualizar solicitudes.

Debe permitir cambiar métricas como:

-   Requests/sec
-   Requests/min
-   Latencia
-   Errores

La gráfica utilizará el color de acento correspondiente al tema.

Light → café claro.

Dark → rojo/rosado pastel.

------------------------------------------------------------------------

## 9. Estado de servicios

Gráfico circular que representa:

-   Saludables
-   Con problemas
-   Caídos

Ejemplo:

``` text
       12
      TOTAL

● Saludables       11
● Con problemas     1
● Caídos            0
```

Los colores semánticos de estado deben mantenerse independientemente del
tema.

------------------------------------------------------------------------

## 10. Despliegues recientes

Tabla con información operacional:

  Servicio               Versión   Ambiente     Estado    Fecha
  ---------------------- --------- ------------ --------- --------------
  payments-api           v1.4.2    producción   Exitoso   hace 12 min
  users-api              v2.1.0    staging      Exitoso   hace 28 min
  notifications-worker   v0.8.1    producción   Exitoso   hace 1 hora
  orders-api             v1.0.5    producción   Fallido   hace 2 horas

Cada registro podrá abrir posteriormente el detalle del deployment.

------------------------------------------------------------------------

## 11. Recursos del clúster

Mostrar indicadores técnicos compactos:

``` text
CPU             MEMORIA          PODS             LATENCIA P95

61%             73%              42 / 44          187 ms
━━━━━━          ━━━━━━━          ━━━━━━━          ━━━━━
```

Cada indicador puede incorporar un pequeño sparkline con su tendencia
reciente.

------------------------------------------------------------------------

## 12. Actividad en tiempo real

Panel estilo log/terminal para eventos relevantes de la plataforma.

Ejemplo:

``` text
23:14:22  INFO   Deployment payments-api v1.4.2 completed successfully
23:12:10  INFO   Scaled users-api to 3 replicas
23:10:45  WARN   High memory usage on catalog-api (82%)
23:08:12  INFO   ArgoCD synchronized: inventory-worker
23:05:33  ERROR  Deployment orders-api v1.0.5 failed (readiness probe)
```

Los niveles deben distinguirse visualmente:

-   INFO
-   WARN
-   ERROR

El panel debe parecer una consola integrada, pero seguir siendo
completamente legible.

------------------------------------------------------------------------

## 13. Vista de Infraestructura

Esta pantalla adopta con mayor fuerza la estética **Blueprint**.

Encabezado:

``` text
[ INFRAESTRUCTURA ]

Vista del clúster y recursos en tiempo real
```

Tabs principales:

``` text
Cluster | Nodos | Namespaces | Red | Almacenamiento | Ingress | Configuración
```

La visualización central representará la arquitectura:

``` text
                    INTERNET
                       │
                       ▼
                LOAD BALANCER
                       │
                       ▼
                    INGRESS
                 nginx / traefik
                       │
          ┌────────────┴────────────┐
          │                         │
     production                  staging
          │                         │
   ┌──────┼──────┐                  │
   │      │      │                  │
payments users  orders          catalog
 api     api     api              api
```

Debe permitir entender visualmente cómo están relacionados los
componentes del clúster.

------------------------------------------------------------------------

## 14. Panel lateral de infraestructura

Puede mostrar:

### Capas

-   Internet
-   Load Balancer
-   Ingress
-   Namespaces
-   Aplicaciones
-   Datos
-   Servicios

### Estado del clúster

``` text
Nodes      3 / 3
Pods      42 / 44
CPU          61%
Memory       73%
Storage      42%
```

### Nodos

``` text
node-01     Ready
node-02     Ready
node-03     Ready
```

### Eventos recientes

Eventos de Kubernetes, deployments y plataforma.

------------------------------------------------------------------------

## 15. Servicios internos de plataforma

La vista Blueprint también puede representar los componentes que hacen
funcionar Mini Fury:

``` text
┌────────────────────────────────────────────┐
│          SERVICIOS DE PLATAFORMA           │
│                                            │
│  ArgoCD    Vault    Prometheus    Grafana  │
│                                            │
└────────────────────────────────────────────┘
```

Y servicios de datos:

``` text
PostgreSQL
Redis
S3 / Object Storage
```

Esto permite que Mini Fury muestre visualmente qué herramientas están
operando detrás de la plataforma.

------------------------------------------------------------------------

## 16. Principio UX fundamental

Mini Fury debe **abstraer Kubernetes y las herramientas DevOps**, no
simplemente crear otra interfaz para administrarlas.

El developer debería pensar en:

``` text
Aplicación
Ambiente
Deployment
Logs
Métricas
Secrets
Recursos
```

y no necesariamente en:

``` text
kubectl
ReplicaSets
Helm commands
Terraform CLI
Vault CLI
PromQL
ArgoCD CLI
```

Mini Fury traduce acciones sencillas del usuario en operaciones sobre la
plataforma.

Ejemplo:

``` text
Developer
    │
    ▼
Mini Fury
    │
    ├── GitLab
    ├── Docker / Registry
    ├── ArgoCD
    ├── Kubernetes
    ├── Vault
    ├── Prometheus
    ├── Grafana
    └── Terraform
```

------------------------------------------------------------------------

## 17. Regla visual final

La interfaz debe evitar:

-   Gradientes excesivos.
-   Sombras grandes.
-   Glassmorphism.
-   Colores neón.
-   Botones excesivamente redondeados.
-   Tarjetas flotantes tipo SaaS genérico.
-   Saturación de iconos.
-   Animaciones innecesarias.
-   Apariencia gamer.

Debe favorecer:

-   Líneas finas.
-   Retículas.
-   Tipografía monoespaciada.
-   Contraste moderado.
-   Datos técnicos.
-   Jerarquía clara.
-   Estados operacionales.
-   Espaciado consistente.
-   Blueprint/wireframes.
-   Interacciones discretas.

El resultado buscado es una **Internal Developer Platform moderna con
identidad retro-técnica**, a medio camino entre una terminal, un plano
de ingeniería y una herramienta profesional de Platform Engineering.
