TELEMETRY-INGEST-IOT

Demo personal de ingesta de telemetria IoT en Azure, inspirada en el reto de
recibir eventos de dispositivos con conectividad intermitente y producir un
estado claro, explicable y resistente a duplicados y desorden.

ESTADO

Documentacion y plan en revision. No se ha iniciado la implementacion ni se
han creado recursos de Azure. El desarrollo comenzara solo despues de que el
autor apruebe el plan.

DOCUMENTOS

- study-project-brief.md: requerimientos, modelo, arquitectura Azure, hardware
  y metricas.
- development-plan.md: plan detallado por fases, tareas, estimaciones y
  criterios de salida.
- Inventario.Componentes.csv: inventario de componentes disponible.

RESUMEN DE LA ARQUITECTURA PROPUESTA

- Simulador en Go y, opcionalmente, un nodo Xiao ESP32-C3.
- Azure IoT Hub para MQTT del dispositivo; HTTPS para el simulador.
- Azure Event Hubs para ingesta y desacoplamiento.
- Worker y APIs en Go sobre Azure Container Apps.
- Azure Cosmos DB for NoSQL para deduplicacion, estado y eventos.
- Azure Service Bus para cuarentena de mensajes invalidos.
- Azure Monitor y Application Insights para metricas y diagnostico.
- Terraform y GitHub Actions con OIDC para infraestructura y CI.

ALCANCE DEL HARDWARE

El nodo del balcon medira solamente humedad del suelo y temperatura ambiente.
Se usara inicialmente el sensor resistivo disponible para pruebas de firmware;
el sensor capacitivo comprado sera el sensor definitivo cuando llegue. El
DHT11 es la opcion prevista para temperatura. El nodo no controlara riego.

CUENTA CLOUD

La primera etapa de nube esta prevista para la Visual Studio Enterprise
Subscription seleccionada por el autor. No desplegar recursos hasta aprobar el
plan y comprobar presupuesto, limites y cargos de la suscripcion. La futura
demo en personal-website, estimada en 1-2 meses, queda fuera del arranque y
requiere una decision separada sobre cuenta, arquitectura y despliegue.

PRIVACIDAD Y SECRETOS

Este repositorio es publico. No incluir credenciales, tokens, claves,
connection strings, archivos de configuracion local ni datos personales.
Consultar .gitignore antes de agregar artefactos generados.
