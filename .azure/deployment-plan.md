# Azure Deployment Plan

> **Status:** Bootstrap deployed and verified. The application stack remains unplanned, unvalidated, and not approved for deployment.

Generated: 2026-10-08 (bootstrap plan and pricing refreshed)

---

## 1. Project Overview

**Goal:** Build a reproducible IoT telemetry demo with a Go simulator, idempotent and order-independent processing, Azure ingestion/storage, API, observability, and an optional later ESP32-C3 balcony node.

**Path:** Add Components / prepare an existing study project for Azure

**Execution boundary:** The user approved autonomous local development and, on 2026-10-08, explicitly confirmed applying only the reviewed bootstrap plan in Visual Studio Enterprise Subscription and Central US. This authorization does not include the application stack. Do not apply any other plan, create application resources, destroy resources, or switch subscription without fresh explicit confirmation of the exact action and cost/plan.

**Infrastructure boundary:** Every Azure Resource Manager resource for the demo, including the dedicated Resource Group, must be managed by Terraform and reside in that dedicated Resource Group. Terraform state bootstrap resources are included in Terraform in the same Resource Group; initialize locally, create through Terraform, migrate state to the Storage backend, and document the reverse migration before a confirmed destroy. Microsoft Entra application/federated credential objects are tenant directory objects rather than ARM resources/RG children; if used, manage them with Terraform and scope Azure RBAC assignments to the dedicated Resource Group.

---

## 2. Requirements

| Attribute | Value |
|-----------|-------|
| Classification | Personal proof of concept |
| Scale | Small Azure ingestion baseline; simulator may generate up to 10,000 logical devices using HTTPS/Event Hubs rather than IoT Hub |
| Budget | Cost-optimized; USD 30/month Resource Group alert threshold confirmed by the user. The alert is not a spending cap; exact estimated costs and apply still require review. |
| **Subscription** | Visual Studio Enterprise Subscription, explicitly selected by user and confirmed enabled via Azure CLI; subscription ID is intentionally kept out of this public repository |
| **Location** | Central US, selected by user |
| Dedicated Resource Group | `rg-telemetry-ingest-iot-dev` (Terraform-managed) |
| Connectivity | HTTPS for load simulation; MQTT/TLS through IoT Hub for device path |
| Compliance | Personal study project; no employer-provided resources, production data, or confidential information |

**Policy constraints found:** Subscription policy assignment listing (including inherited/child scopes) returned `ASC Default` at subscription scope. No additional assignments were returned. Required providers except `Microsoft.Devices` were registered; `Microsoft.Devices` is not registered. Provider registration is a subscription-level change and must be explicitly reviewed before deployment. `Microsoft.Quota` was registered to enable quota checks.

---

## 3. Components Detected

| Component | Type | Technology | Path |
|-----------|------|------------|------|
| Telemetry event core | Library | Go | `internal/telemetry/` |
| Simulator | CLI | Go | `cmd/simulator/` |
| Local ingestion API | API | Go, HTTP, in-memory | `cmd/local-api/` |
| Cloud ingestion API | API | Go, HTTP | `cmd/ingest-api/` |
| Event processor | Worker | Go, Event Hubs consumer | `cmd/processor/` |
| Device-state API | API | Go, HTTP | `cmd/ingest-api/` |
| Azure infrastructure | IaC | Terraform / azurerm | `infra/` |
| Existing project brief | Documentation | Markdown | `study-project-brief.md` |
| Existing component inventory | Data/documentation | CSV | `Inventario.Componentes.csv` |

Workspace started as documentation only. The Go core, deterministic simulator, local in-memory API, and initial Terraform roots have since been added. Repository is public and hosted at `https://github.com/alulema/telemetry-ingest-iot`.

---

## 4. Recipe Selection

**Selected:** Terraform (without azd)

**Rationale:**
- User explicitly requires all Azure resources to be Terraform-managed in one dedicated Resource Group.
- Terraform ownership/state migration and a controlled `terraform plan`/`apply`/destroy lifecycle are explicit project learning goals.
- No AZD service orchestration is necessary for this small proof of concept.
- CI may use GitHub Actions and OIDC; no long-lived credentials are to be stored.

---

## 5. Architecture

**Stack:** Containers — Go API and worker on Azure Container Apps Consumption, with KEDA for the background processor.

### Service Mapping

| Component | Azure Service | SKU |
|-----------|---------------|-----|
| Project boundary | Resource Group | One dedicated `rg-telemetry-ingest-iot-dev` |
| IoT device ingress | Azure IoT Hub | Free F1 for the single device / limited MQTT validation; 8,000 messages/day quota |
| High-volume stream | Azure Event Hubs | Basic, one namespace and one hub for the project; partitions sized for worker scale |
| HTTP ingestion and query APIs | Azure Container Apps | Consumption, scale-to-zero when idle; separate API apps if needed |
| Go event processor | Azure Container Apps | Consumption; KEDA Event Hubs scaler; maximum replicas bounded by partition count |
| Event/state data | Cosmos DB for NoSQL | Serverless, one account and one partitioned container keyed by `/device_id` |
| Invalid-event quarantine | Azure Service Bus | Basic queue; use Microsoft Entra/RBAC if supported by selected tier and provider API |
| Terraform backend | Azure Blob Storage | Standard LRS; created in the same dedicated Resource Group with Terraform, then migrated from local state |
| Event Hubs checkpoints | Azure Blob Storage | Separate account from Terraform state for identity/security separation |
| Logs and platform metrics | Log Analytics Workspace | Per-GB, short retention for development |
| Application telemetry | Application Insights | Workspace-based; Go OpenTelemetry SDK exports through a Collector sidecar with bounded daily ingestion |
| Cost alert | Cost Management budget | Resource Group scope; alert only, not a spending cap |
| Image distribution | GitHub Container Registry | Public project images; avoid an additional Azure registry cost if anonymous image pull is validated |
| CI authentication | Microsoft Entra application + federated credential | Tenant directory objects managed through Terraform where permissions allow; role assignments scoped to project RG |

**Data flow:** Simulator HTTPS → Go ingestion API → Event Hubs → Go worker → Cosmos DB. Device MQTT/TLS → IoT Hub route → Event Hubs → same worker. Invalid payloads are sent to Service Bus quarantine. The state API reads Cosmos DB. The worker must tolerate replay and use deterministic event keys plus conditional/optimistic state projection.

**Cost boundary:** The IoT Hub free quota supports only limited device validation; the 10,000-device stress test uses the simulator's HTTPS/Event Hubs path. Serverless Cosmos DB and Container Apps Consumption are selected to avoid idle provisioned compute. The user confirmed a USD 30/month Resource Group alert and its recipient; the email is stored only in ignored local Terraform configuration. A budget is an alert, not a hard spending cap.

**Cost evidence (2026-10-07):** Azure Cost Management actual-cost and forecast queries scoped to `rg-telemetry-ingest-iot-dev` returned no rows. No project resources are deployed, so there is no project billing history or meaningful project forecast. The Azure Retail Prices API for Central US returned these USD list rates: Event Hubs Basic throughput unit `$0.015/hour` and ingress `$0.028/million events`; Cosmos DB serverless `$0.282/million RU` and `$0.25/GB-month` storage; Service Bus Basic `$0.05/million operations`; Container Apps Consumption `$0.000024/vCPU-second`, `$0.000003/GiB-second` active compute and `$0.40/million requests`. One continuously billed Event Hubs throughput unit would be `$10.95` for a 730-hour month, before ingress and other services. This is a rate-based partial estimate, not a total bill estimate: event volume, runtime, log ingestion, storage growth, free grants, Visual Studio benefits, taxes, and billing eligibility change the total. Recheck live prices and the concrete Terraform plan before any apply.

**Bootstrap-only cost estimate (refreshed 2026-10-08):** The Resource Group does
not have historical actual-cost data or a meaningful forecast. Immediately
after deployment, the Resource Group-scoped actual-cost query returned no rows;
billing data may not yet have arrived. The refreshed bootstrap plan proposed 9
additions, 0 changes, and 0 deletions. Of those,
the Resource Group, two Blob containers, two RBAC role assignments, provider
registration, and the local random value have no direct usage charge. The two
Standard LRS Storage Accounts have no fixed account fee; their costs are driven
by stored data and operations. The current Central US retail-rate API lists Hot
LRS Blob storage at `$0.0184/GB-month`, write operations at `$0.05/10,000`,
read operations at `$0.004/10,000`, and list/create-container operations at
`$0.05/10,000`. As an explicit usage illustration, 1 GB total stored across
both accounts plus 10,000 writes, 10,000 reads, and 100 container-list/create
operations in a month is about `$0.0729/month` before tax, bandwidth, discounts,
or benefits. An idle bootstrap retaining only tiny Terraform state/checkpoint
blobs and making few requests should cost cents or less monthly; the exact
amount depends on actual bytes and operations. This excludes all application
resources and the separate Resource Group budget alert. Rates are list prices,
not a quote or a guaranteed charge. Source: [Azure Retail Prices API](https://prices.azure.com/api/retail/prices?api-version=2023-01-01-preview).

**Deployment result (2026-10-08):** After the user confirmed bootstrap-only
deployment, the exact saved Terraform plan completed successfully: 9 added, 0
changed, 0 destroyed. The dedicated Resource Group, both Standard LRS storage
accounts, both private Blob containers, both account-scoped
`Storage Blob Data Contributor` role assignments, and `Microsoft.Devices`
provider registration are present. Storage accounts require HTTPS, TLS 1.2,
disallow public Blob access, and have shared-key access disabled. Terraform
state was migrated to the private Blob backend using Entra authentication.
Terraform state now tracks all bootstrap resources remotely, and a subsequent
plan returned “No changes.” Live role checks confirmed both assignments on the
intended Storage Account scopes. A Resource Group actual-cost query immediately
after deployment returned no rows; Cost Management reporting may lag, so this
does not establish zero accrued cost. The remote state blob was approximately
30 KB at verification. A local state backup remains in the ignored bootstrap
directory with mode `0600`; do not commit or share it. No application resources
or public endpoints were deployed.

### Supporting Services

| Service | Purpose |
|---------|---------|
| Log Analytics | Centralized short-retention logs |
| Application Insights | Workspace-based monitoring; Go OpenTelemetry traces and metrics are exported through an OpenTelemetry Collector sidecar |
| Managed Identity | Runtime service-to-service authorization with least-privilege RBAC |
| Microsoft Entra ID | GitHub OIDC federation; Terraform-managed directory identity if authorized |
| Azure Blob Storage | Terraform state and isolated Event Hubs checkpoint storage |

### Dedicated Resource Group and State Lifecycle

- All Azure ARM resources for this project, including the Resource Group, are declared in Terraform.
- The bootstrap initially created the dedicated Resource Group and backend
  Storage Account using local state. After validating the backend, the state was
  migrated with `terraform init -migrate-state` to private Azure Blob Storage.
- Configure the application root with a separate backend key/configuration only
  after preparing its Terraform inputs; its plan and deployment require separate
  cost review and user approval.
- State backend stays in the dedicated Resource Group. Before the eventual destroy, migrate state back to a protected local file, inspect the destroy plan, and request explicit user confirmation. Never delete the active remote state as an unreviewed side effect.
- Entra ID application/federated credential objects cannot be children of an Azure Resource Group. Terraform manages those directory objects separately; role assignments and all ARM resources are scoped to the dedicated group.
- Before deployment, a read-only plan of `infra/bootstrap` was generated with
  Terraform 1.16.5 and the selected Azure CLI subscription: 9 objects to add,
  0 changes, 0 destructions.
  The additions are the dedicated Resource Group, two Storage Accounts and their
  Blob containers, two RBAC assignments, the `Microsoft.Devices` provider
  registration, and one local random value. Planning itself created no
  resources. The saved plan was subsequently applied only after separate user
  approval. It was not an application-stack plan or approval. The app root now
  depends on the deployed bootstrap Resource Group and state backend. Current
  storage retail rates and an explicitly usage-based bootstrap estimate are
  recorded in the cost evidence above.

---

## 6. Provisioning Limit Checklist

Quota command results were collected for the confirmed Visual Studio Enterprise Subscription, region `centralus`, on 2026-10-07. The subscription ID is intentionally omitted from this public document. `Microsoft.Quota` CLI is the source where supported. For providers returning `BadRequest`, current resource counts use Azure Resource Graph and limits use the linked official service-limit documentation.

| Resource Type | Number to Deploy | Total After Deployment | Limit/Quota | Notes |
|---------------|------------------|------------------------|-------------|-------|
| `Microsoft.Resources/resourceGroups` | 1 | 7 | 980 per subscription | Current subscription count: 6. Fetched from `az group list`; limit from [Azure subscription limits](https://learn.microsoft.com/azure/azure-resource-manager/management/azure-subscription-service-limits). |
| `Microsoft.App/managedEnvironments` | 1 | 1 | 50 in Central US | `az quota show/usage show`: current 0, limit 50. |
| `Microsoft.Storage/storageAccounts` | 2 | 2 | 250 in Central US | `az quota show/usage show`: current 0, limit 250; Terraform state and checkpoint accounts. |
| `Microsoft.EventHub/namespaces` | 1 | 1 | 1,000 per subscription/region | `az quota list` returned `BadRequest`; Azure Resource Graph count in Central US: 0. Limit from [Event Hubs quotas](https://learn.microsoft.com/azure/event-hubs/event-hubs-quotas). |
| `Microsoft.ServiceBus/namespaces` | 1 | 1 | 1,000 per subscription/region | `az quota list` returned `BadRequest`; Azure Resource Graph count in Central US: 0. Limit from [Service Bus quotas](https://learn.microsoft.com/azure/service-bus-messaging/service-bus-quotas). |
| `Microsoft.DocumentDB/databaseAccounts` | 1 | 2 | 250 per subscription (default) | `az quota list` returned `BadRequest`; Azure Resource Graph count in Central US: 1. Subscription limit from [Cosmos DB limits](https://learn.microsoft.com/azure/cosmos-db/concepts-limits). |
| `Microsoft.Devices/IotHubs` | 1 | 1 | 50 per subscription; only one Free hub | Azure Resource Graph count in Central US: 0. Service quota from [IoT Hub quotas](https://learn.microsoft.com/azure/iot-hub/iot-hub-devguide-quotas-throttling); F1 hub quota 8,000 messages/day. |
| `Microsoft.OperationalInsights/workspaces` | 1 | 1 | 1,000 per subscription (service limit) | Azure Resource Graph count in Central US: 0; validate ARM/provider policy at apply. |
| `Microsoft.Insights/components` | 1 | 1 | Subscription/region limit not exposed by quota CLI | Azure Resource Graph count in Central US: 0; Application Insights component is workspace-based. |

**Status:** ✅ Planned counts are within listed default limits. This is not a guarantee of SKU availability, billing eligibility, or successful provisioning. Recheck dynamic quotas and policy immediately before deployment. Microsoft.Devices provider registration is expressed as a subscription-scoped Terraform registration resource in the bootstrap root; it is not a billable ARM resource and is not a child of the project Resource Group.

---

## 7. Execution Checklist

### Validation Proof — Bootstrap Only (2026-10-08)

- Subscription/location: `az account show` confirmed Visual Studio Enterprise
  Subscription (enabled); `az account list-locations` confirmed `centralus`.
- Authorization: user explicitly confirmed applying only the bootstrap plan,
  including its subscription-level `Microsoft.Devices` registration.
- Tools/authentication: Azure CLI 2.90.0; Terraform 1.16.5 checksum verified;
  current principal has Owner and User Access Administrator at subscription
  scope.
- Format/syntax: `terraform fmt -check -recursive` for `infra/bootstrap` and
  `infra/app`, and `terraform validate` for both roots — passed.
- State: bootstrap intentionally has no state file yet; it is initialized with
  local state before backend resources exist. `terraform state list` reports no
  state file, as expected before first apply.
- Policy/provider checks: subscription has the `ASC Default` policy assignment;
  `Microsoft.Resources`, `Microsoft.Storage`, and `Microsoft.Authorization` are
  registered. `Microsoft.Devices` is not registered and is explicitly included
  in the approved bootstrap plan.
- RBAC static review: both role assignments grant `Storage Blob Data
  Contributor` only on their respective Storage Account to the current user;
  no broader scope is used for these data-plane grants.
- Plan preview: `terraform plan -input=false -no-color
  -var=subscription_id=<selected subscription> -out=<temporary plan>` —
  `9 to add, 0 to change, 0 to destroy`. The saved plan is at
  `/tmp/telemetry-bootstrap-approved.tfplan` for this deployment only.
- Application resources are explicitly outside this validation and approval.

### Deployment Verification — Bootstrap Only (2026-10-08)

- Apply: `terraform apply /tmp/telemetry-bootstrap-approved.tfplan` —
  completed; 9 added, 0 changed, 0 destroyed.
- Outputs: dedicated Resource Group and Storage Account outputs were returned
  by Terraform; the state and checkpoint Storage Accounts are in Central US.
- Remote state: `terraform init -migrate-state -backend-config=backend.hcl
  -force-copy -input=false` succeeded. The state blob is present in the private
  `tfstate` container and Terraform reports all 9 managed resources.
- Drift: a subsequent `terraform plan -detailed-exitcode` reported no changes.
- Azure state: Resource Group exists in `centralus`; both Standard LRS accounts
  are present; `Microsoft.Devices` is registered.
- Security/RBAC: both containers are private; storage accounts enforce HTTPS
  and TLS 1.2, disable public Blob access and shared-key auth. Live queries
  confirmed each account-scoped Blob Data Contributor assignment.
- Cost: immediate month-to-date Cost Management query for this new Resource
  Group returned no rows; billing ingestion may lag. The documented bootstrap
  estimate remains usage-based, not a bill or a hard cap.
- Endpoints: none; bootstrap creates foundation resources only.
- Scope: no `infra/app` resources were planned or deployed.

### Phase 1: Planning
- [x] Analyze workspace and existing docs-only repository
- [x] Gather requirements (POC, cost-optimized, simulator up to 10,000 logical devices)
- [x] Confirm subscription with user: Visual Studio Enterprise Subscription (subscription ID intentionally omitted from this public document)
- [x] Confirm location with user: Central US
- [x] Prepare resource inventory
- [x] Fetch quotas / service limits and check policy assignments
- [x] Select pure Terraform based on explicit user requirement
- [x] Plan dedicated Resource Group architecture and backend-state lifecycle
- [x] User approved development plan and authorized autonomous progress

### Phase 2: Execution
- [x] Research Terraform schemas and Go SDK behavior for implemented service adapters; refresh cost and subscription benefit data before deployment
- [x] Implement Go core and property-style invariance tests
- [x] Implement deterministic simulator and truth ledger
- [ ] Implement local persistence/integration tests against Cosmos DB Emulator or service doubles
- [x] Declare Azure Resource Group and planned ARM resources in Terraform, including a scoped USD 30/month budget alert; recipient remains local-only
- [x] Implement HTTPS ingestion, Event Hubs worker, Cosmos persistence, and Service Bus quarantine
- [x] Implement cloud state API and declare IoT Hub MQTT device route; device client/hardware validation remains later
- [x] Add Go OpenTelemetry traces/metrics and Azure Monitor Collector sidecars; dashboards and live Azure alerts remain gated on deployment
- [x] Record deterministic local simulator baselines at 100, 1,000, and 10,000 devices; Azure replay/load tests remain pending
- [x] Build API, worker, and Collector images locally; GHCR publish and anonymous-pull verification remain pending
- [x] Confirm budget alert threshold and recipient; verify the selected subscription remains active
- [x] Review bootstrap plan: 9 additions, 0 changes, 0 destructions; later applied only after separate approval
- [x] Validate bootstrap usage-dependent costs and provider registration; review the exact bootstrap plan with the user
- [x] Receive explicit approval to apply only the bootstrap plan
- [x] Validate bootstrap root before deployment; the application root remains separately gated

### Phase 3: Validation
- [x] Targeted Go tests, race detector, and property-style invariance runs
- [x] `terraform fmt`, `terraform init -backend=false`, and `terraform validate` for both Terraform roots
- [ ] Static/security scans and an application-stack Terraform plan using confirmed local inputs
- [x] Validate bootstrap Terraform plan and deployment permissions without applying
- [x] Invoke `azure-validate` and record actual bootstrap validation evidence

### Phase 4: Deployment
- [x] Apply only the reviewed bootstrap plan to the confirmed subscription and dedicated RG
- [x] Verify bootstrap resources, remote state, provider registration, and RBAC
- [ ] Plan, review costs, and obtain separate approval before deploying `infra/app`
- [ ] Verify application endpoints, ingestion, quarantine, state, and monitoring after its separately approved deployment
- [ ] Record measured performance and actual Azure costs
- [ ] Any destroy requires a fresh explicit user confirmation and reviewed plan

---

## 8. Research Summary

- Container Apps supports event-driven scale via KEDA and can scale to zero; use a bounded maximum replica count and keep worker checkpoints in Blob Storage.
- IoT Hub Basic supports device-to-cloud messages, MQTT, and message routing; Free has all key capabilities for evaluation but an 8,000-message/day quota. Route device traffic only; high-volume simulation uses HTTPS/Event Hubs.
- Event Hubs Basic allows up to 32 partitions and up to 1,000 namespaces per subscription/region; event data retention and throughput are tier-bounded. Consumer replay requires idempotent projections.
- Service Bus Basic is queue-only, suitable for quarantine; use managed identity/RBAC when supported. Confirm chosen API/tier compatibility during Terraform validation.
- Cosmos DB serverless is single-region and consumption-based; use a single `/device_id`-partitioned container for transaction-batch-compatible event/state records. A transactional batch is limited to one logical partition and one container.
- Storage backend state is sensitive; exclude `.tfstate`, plan files, and local auth settings from Git. Separate state and workload checkpoint storage accounts.
- OIDC identity configuration lives in Microsoft Entra ID, not in the ARM Resource Group. Terraform can manage both directory objects and Azure ARM role assignments if the executing identity has appropriate permissions.

Sources:
- https://learn.microsoft.com/azure/container-apps/scale-app
- https://learn.microsoft.com/azure/iot-hub/iot-hub-scaling
- https://learn.microsoft.com/azure/iot-hub/iot-hub-devguide-quotas-throttling
- https://learn.microsoft.com/azure/iot-hub/iot-hub-devguide-messages-d2c
- https://learn.microsoft.com/azure/event-hubs/event-hubs-quotas
- https://learn.microsoft.com/azure/service-bus-messaging/service-bus-quotas
- https://learn.microsoft.com/azure/service-bus-messaging/service-bus-authentication-and-authorization
- https://learn.microsoft.com/azure/cosmos-db/concepts-limits
- https://learn.microsoft.com/azure/azure-resource-manager/management/azure-subscription-service-limits
- https://developer.hashicorp.com/terraform/language/backend/azurerm
- https://prices.azure.com/api/retail/prices?api-version=2023-01-01-preview

---

## 9. Files to Generate

| File | Purpose | Status |
|------|---------|--------|
| `.azure/deployment-plan.md` | Source-of-truth Azure plan | ✅ |
| `go.mod`, `cmd/`, `internal/`, tests | Go core, simulator, APIs, cloud worker/adapters | ✅ Local tests pass; Azure integration not deployed |
| `infra/bootstrap`, `infra/app` | Azure resources, identities, budget and dedicated RG | ⏳ Validated definitions; exact costs and live plan/permissions remain gated |
| `infra/*/terraform.tfvars.example`, `backend.hcl.example` | Safe bootstrap/app inputs and state setup | ✅ Placeholders only |
| `Dockerfile` / `Dockerfile.collector` | API, worker, and OpenTelemetry Collector container builds | ✅ Local image builds; Collector configuration validated |
| `otel-collector-config.yaml`, `internal/observability/` | OTLP receivers/export and Go spans/metrics | ✅ Local OTLP export test passes; Azure ingestion remains unverified |
| `.github/workflows/ci.yml` | Go/Terraform CI and GHCR publishing | ✅ Collector image included; remote run/public image pull unverified |
| `README.txt` / project docs | Setup, architecture, cost and cleanup | ✅ Updated for current implementation/gates |

---

## 10. Next Steps

> Current: Bootstrap deployed and verified. Application deployment remains gated.

1. Configure and validate the `infra/app` remote backend, then generate an application-stack plan.
2. Refresh the full application cost estimate and obtain separate review/approval before any application apply.
3. Verify Azure replay/load behavior and live Azure Monitor telemetry only after application deployment is approved.
4. Publish images through CI and verify anonymous GHCR pulls; add Terraform plan automation with OIDC if needed.

## Application stack deployment (2026-10-12)

- `infra/app` applied after explicit approval: 26 resources in `rg-telemetry-ingest-iot-dev` (Central US), state in the private Blob backend (key `telemetry-ingest-iot-app.tfstate`).
- Fixes needed during apply: Event Hubs Basic supports only the `$Default` consumer group; IoT Hub F1 requires 2 partitions; Container Apps environment needs `logs_destination = "log-analytics"`; KEDA identity value is `System`.
- Processor bug fixed: `ReceiveEvents` blocked waiting for a full batch; now uses a 5 s receive timeout (commit after `79c8f62`).
- Smoke test: POST /v1/events returned 202 (duplicate accepted idempotently), GET state returned 200 with the latest event after processing. RBAC for the processor identity verified live.
- Known benign drift: the provider reports the Azure-default `Consumption` workload profile as a change.
- Pending: Application Insights telemetry check, cloud load tests (100 then 1,000 devices), dashboard/alerts, real cost measurement.
