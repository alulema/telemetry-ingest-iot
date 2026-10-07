# Brief técnico — Demo de ingesta de telemetría IoT en Azure (estudio para Halter)

> Objetivo: un proyecto personal pequeño pero serio que demuestre habilidades transferibles para la oferta
> de Halter (cloud/IaC, Go/Node, IoT), implementando equivalentes de Azure para el problema de "turning
> thousands of collars and billions of sensor events into a single, calm decision", con conectividad rural
> intermitente y hardware limitado.
> Dos fuentes de datos: (1) un simulador de "collares" a escala, y (2) opcional, un nodo físico que
> monitorea humedad del suelo y temperatura ambiente en el jardín del balcón.
>
> Reglas: proyecto de estudio independiente. La primera etapa de nube usará la **Visual Studio Enterprise
> Subscription** que seleccionaste; en 1–2 meses se evaluará una demo en `personal-website` y, antes de
> desplegarla, se decidirá si se migra a una suscripción personal de Azure. No usar código ni conocimiento
> interno/confidencial de Microsoft. En el resume solo van números medidos.
> En el resume solo van números medidos.

---

## 1. Qué tiene que demostrar (los "invariantes")
1. **Idempotencia:** el mismo evento recibido N veces cuenta una sola vez.
2. **Tolerancia al desorden:** si los eventos llegan desordenados, el estado final es el mismo que si
   hubieran llegado en orden.
3. **Store-and-forward:** un dispositivo sin conexión guarda los eventos y los envía al reconectarse, sin
   perder datos (dentro de su buffer).
4. **Una decisión calmada:** de muchos eventos ruidosos sale un estado o aviso claro y explicable (p. ej.
   "humedad del suelo baja" o "animal fuera del cerco").
5. **Degradación elegante:** si la nube no está disponible, el borde (edge) sigue midiendo y guarda eventos
   localmente para reenviarlos al reconectarse; el muestreo no depende de una decisión en la nube.

La prueba estrella: **mezclar, duplicar y reordenar el mismo conjunto de eventos y comprobar que el estado
final es idéntico** (property-based test).

---

## 2. Modelo de datos

### Envelope de evento (igual para el simulador y el hardware)
| Campo | Para qué |
|---|---|
| `device_id` | Identidad del dispositivo; también clave de partición |
| `boot_id` | Cambia en cada reinicio; evita choques de secuencia tras un reset |
| `seq` | Contador monotónico por `boot_id`; detecta huecos y duplicados |
| `event_id` | Determinístico: hash de (`device_id`, `boot_id`, `seq`). Base de la idempotencia |
| `recorded_at` | Hora del dispositivo (puede tener deriva) |
| `received_at` | Hora del servidor; con `recorded_at` mide el retraso |
| `schema_version` | Evolución del esquema sin romper consumidores |
| `fw_version` | Versión de firmware (útil para depurar) |
| `payload` | Lecturas según el tipo |

### Payloads
- **Collar simulado:** lat/lon, batería %, temperatura, actividad (acelerómetro resumido), estado de cue
  (audio/vibración).
- **Jardín (alcance inicial):** humedad del suelo % (calibrada) y temperatura ambiente.

### Contenedores (Azure Cosmos DB for NoSQL)
- `events`: clave de partición `/device_id`; `id = event_id`. Crear cada elemento condicionalmente; un
  conflicto (`409`) indica que el evento ya se guardó. El conflicto evita volver a contar el evento, pero una
  repetición puede tener que completar proyecciones idempotentes si el intento previo falló a mitad de camino.
  Si se habilita TTL, debe cubrir el horizonte completo de reintentos y replay: un TTL de 7 días solo garantiza
  dedupe durante esos 7 días.
- `device_state`: clave `/device_id`; último estado conocido. Actualizar con control optimista mediante
  `_etag`/`If-Match`, y aceptar el cambio solo si el evento es más nuevo según un comparador total y
  determinista: `recorded_at`, con `boot_id` y `seq` como desempate estable. Un evento viejo que llega tarde
  no pisa el estado actual.
- `events` también sirve como serie temporal para el MVP. Para volumen o retención baratos, exportar a Azure
  Blob Storage (opcionalmente Parquet para consultas analíticas).
- `aggregates`: ventanas de 5 minutos por dispositivo; recalcular desde eventos únicos si llega un evento
  tarde dentro de la ventana permitida (*watermark*), en vez de incrementar contadores sin protección ante
  reintentos.

La clave `/device_id` mantiene juntos los datos de un dispositivo. Si se necesita atomicidad entre dedupe y
actualización de estado/agregados, guardar esos elementos en el mismo contenedor y la misma partición lógica,
y usar un batch transaccional de Cosmos DB. Si quedan en contenedores distintos, cada proyección debe ser
idempotente y recuperable ante fallos parciales.

---

## 3. Arquitectura en Azure

```
[simulador Go / ESP32] --MQTT/TLS--> [Azure IoT Hub] --ruta administrada--> [Event Hubs]
             \--HTTPS--> [API Go en Container Apps] ----------/
        --> [worker Go en Azure Container Apps + KEDA]
        --> [Cosmos DB] --> [API: estado y eventos] --> [regla explicable / aviso]
              \--> [Service Bus: cuarentena de inválidos]
        --> [Azure Monitor + Application Insights + Log Analytics]
```

**Decisiones a estudiar y justificar (en la entrevista te van a preguntar el porqué):**
- **IoT Hub + MQTT** (realista para hardware; identidad y credenciales por dispositivo, incluido X.509)
  vs. HTTPS a una API de ingesta (más simple para el simulador). El nodo físico usa MQTT; el simulador puede
  probar ambos caminos. IoT Hub enruta los mensajes a un Event Hub configurado como endpoint personalizado;
  la identidad administrada del hub autoriza esa escritura. El endpoint integrado compatible con Event Hubs
  es una alternativa, con su propia configuración de acceso y retención.
- **Event Hubs** (stream de alto volumen, retención y orden por partición) vs. **Azure Storage Queues**
  (cola sencilla) vs. **Service Bus** (mensajería empresarial, sesiones y dead-letter subqueue). El MVP usa
  Event Hubs con una estrategia de particionado por `device_id` siempre que el productor permita fijar esa
  clave; verificar la asignación del endpoint IoT Hub elegido. Aunque Event Hubs conserva el orden dentro de
  una partición, reintentos, duplicados y concurrencia del consumidor obligan a tolerar eventos fuera de orden.
  Las sesiones y la detección de duplicados de Service Bus no sustituyen la idempotencia de la aplicación.
- **Worker Go en Azure Container Apps** con KEDA escalando según el retraso de Event Hubs: conserva Go
  como lenguaje del procesador sin presentar Azure Functions como runtime Go nativo. Azure Functions con
  *custom handler* es una alternativa si se quiere evaluar ese modelo serverless.
- La API Go opcional en Container Apps acepta HTTPS del simulador y publica en Event Hubs; la API de consulta
  lee estado/eventos de Cosmos DB. Se pueden desplegar como dos apps separadas para escalar y protegerlas por
  separado.
- Event Hubs no tiene una DLQ por evento equivalente a la de una cola. El worker valida cada mensaje y envía
  los inválidos a una cola de cuarentena de Service Bus, junto con el motivo y el evento original. Los errores
  transitorios no se checkpoint-ean hasta persistir el resultado; un replay es posible, así que el procesamiento
  debe ser idempotente.
- **Identidad y permisos:** Managed Identity para acceder a Cosmos DB, Event Hubs y Service Bus; RBAC con
  privilegio mínimo. GitHub Actions se autentica mediante OIDC y una credencial federada de Microsoft Entra ID,
  sin secretos de larga duración.

---

## 4. Simulador (en Go)
- **N dispositivos** configurables (empieza con 1.000 y prueba 10.000).
- Publicación configurable por MQTT a IoT Hub o por HTTPS a la API de ingesta; usa la misma semilla y verdad
  de referencia para ambos transportes.
- **Perfiles de conectividad** por dispositivo: modelo on/off (cadena de Markov), pérdida de paquetes %,
  latencia variable.
- **Fallos inyectados:** tasa de duplicados (reintentos), ventana de reordenamiento, deriva del reloj,
  reinicios (nuevo `boot_id`), ráfagas al reconectar (*thundering herd*).
- **Buffer local** acotado (ring buffer) con política explícita cuando se llena (descartar lo más viejo y
  contarlo).
- **Reintentos** con *exponential backoff + jitter*.
- **Semilla fija** para que cada corrida sea reproducible.
- **"Verdad de referencia":** el simulador registra lo que realmente generó, para comparar con lo que
  quedó en la base y medir pérdidas o doble conteo.

---

## 5. Terraform
- Módulos: `iot` (IoT Hub, rutas y acceso de dispositivos), `ingest` (Event Hubs), `processing` (Azure
  Container Apps, worker Go, KEDA e identidad administrada), `storage` (Cosmos DB y Blob Storage),
  `quarantine` (Service Bus), `api` y `observability` (Application Insights, Log Analytics, métricas y alertas).
- **Estado remoto** de Terraform en Azure Blob Storage, con bloqueo por lease del blob.
- **CI con GitHub Actions + OIDC hacia Microsoft Entra ID** y credencial federada (sin llaves de larga duración).
- **Guardarraíles de costo:** budget de Azure Cost Management con alertas, `terraform destroy` fácil y recursos
  en una sola región. El budget alerta; no impone por sí solo un límite de gasto.

---

## 6. Observabilidad (lo que vas a medir)
- Eventos por segundo ingeridos.
- **% de duplicados descartados** y **% de eventos fuera de orden** (comparar con lo inyectado).
- **Retraso** (`received_at − recorded_at`): p50/p95/p99, sobre todo después de una reconexión.
- **Latencia de punta a punta** (de ingesta a estado actualizado): p50/p95.
- Profundidad de la cola de cuarentena, errores/reinicios del worker y retraso del consumidor/partición de
  Event Hubs.
- Costo por millón de eventos (estimado con la factura real de las pruebas).
- Dashboard y alertas en Azure Monitor, telemetría de aplicación en Application Insights y logs estructurados
  (JSON) en Log Analytics. Trazas con OpenTelemetry (opcional).

---

## 7. Pruebas
- **Unitarias** de la lógica de dedupe y de "solo actualizar si es más nuevo".
- **Property-based** (la estrella): para cualquier permutación con duplicados del mismo conjunto de eventos,
  el estado final es idéntico. En Go puedes usar `testing/quick` o `rapid`.
- **Integración local** con Cosmos DB Emulator y Azurite para los servicios compatibles. IoT Hub y Event Hubs
  no tienen un emulador local completo: aislarlos tras interfaces y reservar las pruebas reales para una
  suscripción de desarrollo de bajo costo.
- **Carga** con el simulador (1.000 → 10.000 dispositivos) y **caos**: cortar la conectividad de todos y
  reconectarlos a la vez.
- **Replay:** reinyectar un día completo de eventos y comprobar que no cambia nada (idempotencia a escala).

---

## 8. Hardware opcional: monitoreo del jardín del balcón (versión 2)
**Alcance inicial: medir únicamente humedad del suelo y temperatura ambiente; no controlar bomba ni válvula.**

**Componentes disponibles en el inventario:**
| Pieza | Por qué |
|---|---|
| **Xiao ESP32-C3** (3 disponibles) | Wi-Fi integrado, deep sleep y TLS para MQTT con IoT Hub |
| **Sensor capacitivo de humedad de suelo** (por comprar, v1.2 o similar, 3.3–5.5 V, salida analógica) | Sensor definitivo de humedad del suelo: no se corroe como el resistivo; sellar bordes y cable |
| **HW-103** (14 disponibles) | Resistivo: solo para desarrollo de firmware y calibración inicial mientras llegan los capacitivos |
| **Módulo DHT11** (del Sensor Kit for Arduino; verificar el chip) | Temperatura ambiente: ±2 °C, resolución 1 °C, rango 0–50 °C, máximo 1 lectura/s; opera a 3.3 V |
| **CJMCU-680** (2 disponibles, alternativa) | Posible BME680, más preciso para temperatura; verificar el chip y la placa exactos |
| **DS3231** (6 disponibles, opcional) | RTC para conservar referencia de tiempo durante cortes de red; sincronizar con NTP al reconectar |
| **Alimentación USB** | Simplifica la primera prueba; batería y panel solar quedan fuera del MVP de hardware |

El sensor definitivo de humedad del suelo será el capacitivo. Mientras llega, usar el HW-103 (resistivo) solo
para desarrollo, alimentado desde un GPIO únicamente durante la lectura y en pruebas cortas para limitar la
corrosión. Al cambiar de sensor, recalibrar con suelo seco y húmedo, porque los valores crudos son distintos. El DHT11 es suficiente para la temperatura ambiente del balcón, pero no mide bajo 0 °C: protegerlo
de lluvia y sol directo, ubicarlo lejos del ESP32 y validarlo frente a un termómetro de referencia. Si se
necesita más precisión, usar el BME680 (que puede calentarse durante la lectura de gas). No se presupone que
haya bomba, válvula ni fuente adecuada para una carga de riego.

**Firmware (Arduino/ESP-IDF; MicroPython o TinyGo si quieres probar Go en el dispositivo):**
- Mismo *envelope* que el simulador: `boot_id`, `seq` y `event_id` determinístico.
- Lecturas de humedad del suelo y temperatura ambiente; calibrar la humedad con muestras de suelo seco y húmedo.
- **Store-and-forward** en flash (LittleFS) cuando no hay Wi-Fi, y envío por lotes al reconectar.
- Hora por NTP; si no hay hora, marcar el evento y corregirlo en el servidor.
- **MQTT sobre TLS a Azure IoT Hub**, con identidad propia por dispositivo.
- Si se desea una salida durante una desconexión, usar un aviso local simple; el nodo no acciona equipo de riego.

**Aviso en la nube (opcional):** indicar "humedad baja" al superar un umbral calibrado, con histéresis y tiempo
de enfriamiento para evitar alertas repetidas. La temperatura ambiente aporta contexto, pero no se automatiza
el riego. La regla se calcula en código y es explicable; no necesita un LLM.

---

## 9. Plan de desarrollo
Orden pensado para validar primero la lógica (sin nube), luego la nube, y dejar el hardware al final.
Cada fase termina con un criterio de salida verificable.

| Fase | Trabajo | Criterio de salida |
|---|---|---|
| 0. Base | Repo, módulo Go, esquema del envelope y `event_id`; suscripción personal de Azure, budget con alertas, estado remoto de Terraform en Blob Storage, OIDC de GitHub Actions | `terraform plan` corre desde CI sin secretos |
| 1. Núcleo (sin nube) | Lógica pura en Go: validación, dedupe, comparador total `recorded_at/boot_id/seq`, "actualizar solo si es más nuevo", agregados con watermark; pruebas unitarias y **property-based** | La prueba de mezclar/duplicar/reordenar pasa con miles de casos |
| 2. Simulador | Dispositivos configurables, semilla fija, perfiles de conectividad, fallos inyectados, buffer acotado, backoff con jitter, verdad de referencia | Dos corridas con la misma semilla producen la misma verdad de referencia |
| 3. Integración local | Adaptadores de Cosmos DB (emulador) y Azurite; escrituras condicionales y `_etag`; el simulador contra el procesador local | Verdad de referencia = estado en Cosmos, con duplicados y desorden |
| 4. Nube: ingesta HTTPS | Terraform de `ingest`, `storage`, `quarantine`, `processing`; API de ingesta → Event Hubs → worker en Container Apps (KEDA) → Cosmos DB; identidad administrada y RBAC | Una corrida con 1.000 dispositivos coincide con la verdad de referencia en Azure |
| 5. API e IoT Hub | `GET /devices/{id}/state`; módulo `iot`; identidades de dispositivo y ruta a Event Hubs; el simulador publica por MQTT | Mismo resultado por MQTT y por HTTPS |
| 6. Observabilidad y pruebas de estrés | Dashboard, alertas, métricas de la sección 6; carga 1.000 → 10.000, caos (reconexión masiva), replay de un día | Métricas medidas y replay sin cambios de estado |
| 7. Hardware (al final) | Firmware en el Xiao ESP32-C3: envelope, DHT11, humedad de suelo, store-and-forward en LittleFS, MQTT a IoT Hub. Empezar con el HW-103 para desarrollo; al llegar los capacitivos, calibrar y cambiar | El nodo real sobrevive a un corte de Wi-Fi y sus eventos llegan sin duplicar |
| 8. Cierre | Aviso por umbral (opcional), README con arquitectura y decisiones, cifras para el resume, `terraform destroy` | Números reales anotados y recursos destruidos |

**Notas de secuencia:**
- La identidad del dispositivo en IoT Hub se crea en la fase 5 y se prueba con el simulador, así el nodo real
  solo agrega firmware.
- Los capacitivos llegan en ~10 días: la fase 7 casi seguro cae después de recibirlos. El firmware se puede
  adelantar con el HW-103 si sobra tiempo, pero no bloquea nada.
- Si hay prisa, recortar en este orden: aviso por umbral, agregados, 10.000 dispositivos, hardware.

### Alcance mínimo (MVP)
**Lo mínimo que ya sirve para la entrevista (fases 0–6):**
1. Simulador en Go con duplicados, desorden y desconexiones.
2. Ingesta (MQTT por IoT Hub o HTTPS) → Event Hubs → worker Go en Container Apps → Cosmos DB (dedupe + estado).
3. API `GET /devices/{id}/state`.
4. Terraform completo con CI por OIDC.
5. El property-based test y un dashboard con 4–5 métricas.

**Extras si sobra tiempo:** agregados con watermark, replay de un día, el nodo ESP32-C3 real, aviso por umbral
de humedad y una UI mínima.

---

## 10. Qué llevar al resume y a la entrevista (solo números medidos)
- Ejemplo de bullet (completar con tus cifras reales): *"Built an IoT telemetry pipeline on Azure (Go, Terraform,
  IoT Hub, Event Hubs, Container Apps, Cosmos DB) that ingested N events/s from X simulated devices with Y% injected
  duplicates and Z% out-of-order events, with zero double-counting and identical final state across replays;
  p95 end-to-end latency of W ms."*
- Si haces el hardware: *"…plus an ESP32-C3 field node measuring soil moisture and ambient temperature with
  offline store-and-forward."*
- Historia para la entrevista: por qué diseñaste para el desorden en vez de confiar en el orden, qué falló en
  la prueba de reconexión masiva y cómo lo resolviste.
- Honestidad: es un proyecto personal de estudio. No lo presentes como experiencia de producción en IoT.

## Referencias de Azure
- [IoT Hub: enrutamiento de mensajes device-to-cloud](https://learn.microsoft.com/azure/iot-hub/iot-hub-devguide-messages-d2c)
- [Azure Event Hubs: conceptos y selección de mensajería](https://learn.microsoft.com/azure/event-hubs/event-hubs-about)
- [Azure Container Apps: escalado con KEDA](https://learn.microsoft.com/azure/container-apps/scale-app)
- [Azure Cosmos DB: TTL](https://learn.microsoft.com/azure/cosmos-db/nosql/time-to-live)
- [Terraform: backend de Azure Blob Storage](https://developer.hashicorp.com/terraform/language/backend/azurerm)
