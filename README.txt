TELEMETRY-INGEST-IOT

Demo personal de ingesta de telemetria IoT en Azure, inspirada en el reto de
recibir eventos de dispositivos con conectividad intermitente y producir un
estado claro, explicable y resistente a duplicados y desorden.

ESTADO ACTUAL

- Plan: aprobado; desarrollo local autonomo autorizado.
- Go: core de eventos, pruebas de invariantes, simulador determinista, API local
  efimera, API cloud HTTPS, producer de Event Hubs, worker con checkpoint Blob,
  persistencia Cosmos transaccional y cuarentena Service Bus implementados.
- Terraform: bootstrap aplicado y verificado (9 objetos; sin cambios ni
  destrucciones adicionales); el state remoto ya funciona. El stack de
  aplicacion aun no tiene plan ni aprobacion de despliegue.
- Observabilidad: la API y el worker emiten trazas y metricas OpenTelemetry; un
  Collector sidecar las exporta a Application Insights. Esa parte aun no esta
  desplegada ni tiene telemetria Azure confirmada.
- Azure: solo se desplego el bootstrap (RG, almacenamiento de state/checkpoints,
  contenedores privados, permisos y registro de provider). El usuario confirmo una alerta mensual de
  USD 30 y su destinatario, guardado solo en configuracion Terraform local
  ignorada. La consulta de costos inmediata al bootstrap no devolvio filas; la
  contabilizacion puede tener demora. La alerta no limita el gasto; no desplegar
  la aplicacion con `terraform apply` sin revisar su plan y costos.

PRUEBA LOCAL

    go test ./...
    go test -race ./...
    go vet ./...
    go run ./cmd/simulator -devices 1000 -events-per-device 60 -seed 20261007
    go run ./cmd/local-api

The local API is ephemeral and in-memory; it is not the Azure persistence
implementation. It accepts POST /v1/events and serves
GET /v1/devices/{device_id}/state.

The cloud API and worker use Azure DefaultAzureCredential; in Container Apps
they are designed to use managed identities and require the Terraform RBAC
assignments. Local cloud execution requires an authenticated Azure CLI identity
with the corresponding data-plane roles and the documented environment values.
The public cloud API requires `Authorization: Bearer <token>` for ingestion and
state reads. Terraform generates the token; retrieve it locally with
`terraform -chdir=infra/app output -raw ingestion_token` after deployment and do
not paste it into the repository or CI logs.

## Terraform

- `infra/bootstrap`: Terraform-managed dedicated Resource Group, state/checkpoint
  storage, Blob containers, and the required `Microsoft.Devices` provider
  registration. Provider registration is subscription-scoped, not a billable
  resource or a child of the Resource Group.
- `infra/app`: Event Hubs, IoT Hub F1, Cosmos DB serverless, Service Bus Basic,
  Container Apps, managed identities/RBAC, monitoring, and a scoped monthly
  budget alert.
- Bootstrap now uses the private Azure Blob backend with Entra authentication;
  its local `backend.hcl` is ignored. For `infra/app`, copy
  `backend.hcl.example` to `backend.hcl`, fill in the checkpoint/state outputs
  as appropriate, and add the backend block before initialization. Copy
  `terraform.tfvars.example` to local `terraform.tfvars`; do not commit local
  configuration or state.
- Runtime data access uses Microsoft Entra ID and managed identity. State files
  contain sensitive infrastructure metadata and must remain private.
- Images are built and published to GHCR after CI passes on `main`; verify
  anonymous image pulls and set package visibility to public before deployment.
- App Insights/Log Analytics, the Collector sidecars, and all application
  Azure resources will stay in the dedicated Resource Group. The Application Insights
  daily ingestion cap is 0.1 GB; the Terraform budget is an alert, not a hard
  spending cap. Terraform `apply` remains gated on user review.
- The approved Terraform bootstrap plan was applied: 9 objects added, no
  changes or deletions. A follow-up plan reports no changes. The app stack has
  not been deployed.
- The bootstrap uses two Standard LRS storage accounts, billed by actual data
  and operations rather than a fixed account fee. Current Central US list rates
  and an example low-use estimate (~$0.07/month under stated usage assumptions)
  are documented in `.azure/deployment-plan.md`; this excludes the application
  stack and is not a quote or a spending cap.
- The current rate-based partial cost review is recorded in
  `.azure/deployment-plan.md`; the first post-bootstrap Cost Management query
  returned no rows (billing data may lag), and the one-throughput-unit Event Hubs assumption is about
  USD 10.95/month before usage-based charges.

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
