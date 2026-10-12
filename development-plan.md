# Plan detallado de desarrollo

**Proyecto:** Demo de ingesta de telemetría IoT en Azure
**Estado:** Bootstrap de Azure desplegado y verificado; el stack de aplicación sigue sujeto a plan, revisión de costos y aprobación explícita.
**Disponibilidad indicada:** fines de semana completos.
**Suscripción inicial indicada:** Visual Studio Enterprise Subscription.
**Hardware:** los sensores capacitivos se esperan aproximadamente 10 días después de la compra; mientras tanto se puede desarrollar con el sensor resistivo disponible.
**Infraestructura:** todos los recursos ARM de Azure se administrarán con Terraform dentro de un Resource Group dedicado.

**Avance:** el core Go, simulador, API local/cloud, worker y adaptadores de Event Hubs/Cosmos DB/Service Bus ya están implementados. Se agregaron trazas y métricas OpenTelemetry en Go con un Collector sidecar para exportarlas a Application Insights. El 2026-10-08 se aplicó el plan bootstrap de Terraform (9 adiciones, 0 cambios, 0 destrucciones), se verificaron recursos/RBAC y se migró el state al backend privado Blob. Un plan posterior no detectó cambios. No se han desplegado recursos de la aplicación. La estimación bootstrap y el resultado actual de Cost Management están en `.azure/deployment-plan.md`; la consulta inmediata del RG no devolvió filas y los cargos pueden tardar en aparecer.

## 1. Objetivo y resultado esperado

Construir una demo pequeña, reproducible y medible que simule dispositivos IoT y demuestre:

1. Un evento repetido no se cuenta más de una vez.
2. La llegada fuera de orden no hace que el estado más reciente retroceda.
3. Un dispositivo puede almacenar eventos localmente sin red y reenviarlos al reconectarse.
4. Los eventos ruidosos producen un estado o aviso único y explicable.
5. Un nodo físico mide humedad del suelo y temperatura ambiente; no automatiza el riego.

El resultado debe permitir comparar eventos generados con eventos persistidos y presentar cifras medidas de rendimiento, duplicados, desorden y latencia. No se escribirán cifras estimadas como resultados del proyecto.

## 2. Decisiones de alcance

### Incluido en el MVP

- Simulador en Go con semilla fija, perfiles de conectividad y fallos inyectables.
- Prueba property-based sobre mezcla, duplicación y reordenamiento.
- Azure IoT Hub, Event Hubs, worker Go en Azure Container Apps, Cosmos DB for NoSQL y API de estado.
- HTTPS de ingesta para el simulador y MQTT/TLS para el dispositivo físico.
- Cuarentena de mensajes inválidos, métricas y dashboard básico.
- Infraestructura Terraform y autenticación GitHub Actions → Microsoft Entra ID mediante OIDC.
- Nodo de balcón opcional al final: Xiao ESP32-C3, humedad de suelo y temperatura ambiente.

### Fuera del alcance inicial

- Bomba, relé, válvula, decisión automática de riego o interfaz de control.
- Pronóstico meteorológico, LLM, UI completa, multi-región o alta disponibilidad avanzada.
- Presentar el proyecto como producción o declarar resultados de rendimiento antes de medirlos.
- Crear recursos o ejecutar despliegues antes de aprobar este plan.
- Publicar una demo en `personal-website` ahora. Se evaluará en 1–2 meses como fase posterior y requerirá revisar cuenta, costo y arquitectura.

## 3. Arquitectura objetivo

```text
Simulador Go --HTTPS--> API de ingesta Go --+
                                            +--> Event Hubs
Xiao ESP32-C3 --MQTT/TLS--> IoT Hub --------+       |
                                                    v
                                       Worker Go en Container Apps
                                          |              |
                                          v              v
                                    Cosmos DB       Service Bus
                                    estado/eventos   cuarentena
                                          |
                                          v
                                  API GET de estado

                                  API y worker --OTLP--> Collector sidecar --> Application Insights
                                  Azure Monitor / Log Analytics
                                  Terraform + GitHub Actions OIDC + Microsoft Entra ID
```

IoT Hub debe tener una ruta configurada a un Event Hub personalizado, con autorización del endpoint otorgada a la identidad administrada del hub. Validar antes de codificar la ruta el comportamiento de particionamiento disponible para esa ruta; no asumir que IoT Hub puede fijar `device_id` como partition key. Si no puede garantizarse, conservar el orden lógico mediante el comparador del estado y la idempotencia de aplicación, no por la entrega del broker.

Para Cosmos DB, mantener eventos únicos y proyecciones idempotentes. Si el diseño necesita atomicidad entre escritura del evento y estado, usar elementos de tipos distintos en el mismo contenedor y la misma partición `/device_id`, con transactional batch; una transacción no abarca contenedores o particiones distintos. El worker debe poder reintentar y reparar proyecciones después de fallos parciales.

## 4. Orden de trabajo y estimaciones

Las horas son de trabajo enfocado, no fechas garantizadas. Un fin de semana completo ofrece tiempo suficiente para avanzar varias fases, pero las fases de nube pueden requerir iteraciones por cuotas, permisos o configuración de Azure. Mantener la revisión del plan como puerta de entrada.

### Fase 0 — Preparación segura y repositorio (1–2 h)

- Revisar y aprobar este plan.
- Confirmar suscripción y tenant activos por nombre e ID; seleccionar explícitamente Visual Studio Enterprise Subscription solo al iniciar trabajo de Azure.
- Crear y administrar el Resource Group dedicado mediante Terraform. No crear recursos del proyecto fuera de ese grupo.
- Revisar si la suscripción permite crear los servicios previstos, sus cuotas, presupuestos, créditos y facturación.
- Establecer un budget/alertas antes de desplegar recursos facturables.
- Definir cómo bootstrapear el backend Terraform de Blob Storage y trasladar el state local a remoto.
- Configurar GitHub Actions OIDC con federated credential y permisos mínimos, sin claves persistentes.

**Salida:** permisos, costos previsibles, backend remoto y camino de CI documentados; ningún secreto en el repositorio.

### Fase 1 — Dominio y modelo de eventos en Go (3–5 h)

- Definir el envelope compartido: `device_id`, `boot_id`, `seq`, `event_id`, tiempos, versiones y payload.
- Derivar `event_id` de forma determinista y especificar normalización/serialización para que el hash sea estable.
- Definir comparador total y determinista de eventos, incluidos reinicios y relojes con deriva.
- Implementar validación de esquema y lógica pura de proyección de estado.
- Añadir unit tests para duplicados, eventos viejos, empates y reinicios.
- Añadir property-based test con generadores de eventos, permutaciones y duplicación arbitraria.

**Salida:** la propiedad principal pasa repetidamente y no depende de Azure ni del reloj real.

### Fase 2 — Simulador reproducible (4–7 h)

- Crear N dispositivos configurables; empezar con 100 y 1.000 antes de intentar 10.000.
- Modelar perfiles on/off, latencia, pérdida, reconexión y ráfagas.
- Inyectar duplicados, desorden, deriva del reloj y nuevos `boot_id`.
- Implementar ring buffer acotado, política de buffer lleno y contador de descarte.
- Añadir backoff exponencial con jitter y semilla fija.
- Registrar verdad de referencia, contadores de fallos y configuración de la corrida.

**Salida:** misma configuración y semilla producen el mismo conjunto de eventos y verdad de referencia.

**Mediciones locales (2026-10-07, semilla `20261007`, 60 eventos/dispositivo):**

| Dispositivos | Generados | Publicados | Duplicados inyectados/descartados | Fuera de orden | Pendientes offline | Estado final |
|--------------|-----------|------------|----------------------------------|---------------|-------------------|--------------|
| 100 | 6.000 | 6.136 | 185/185 | 4.547 | 49 | 100 dispositivos |
| 1.000 | 60.000 | 61.463 | 1.815/1.815 | 45.230 | 352 | 1.000 dispositivos |
| 10.000 | 600.000 | 615.016 | 18.164/18.164 | 452.019 | 3.148 | 10.000 dispositivos |

La corrida de 10.000 dispositivos se repitió con la misma semilla y produjo los
mismos contadores. Son resultados del simulador local en memoria; no representan
rendimiento, latencia ni costo de Azure.

### Fase 3 — Persistencia local y recuperación (3–5 h)

- Definir interfaces de persistencia que permitan probar sin servicios cloud.
- Probar escrituras condicionales, conflicto por `event_id`, concurrencia de actualizaciones y ETag.
- Usar Cosmos DB Emulator donde sea viable; usar Azurite solo para los componentes de Storage que correspondan.
- Probar el patrón de reparación de proyecciones tras un fallo parcial.

**Salida:** pruebas locales demuestran que reintentar no duplica eventos ni hace retroceder estado.

### Fase 4 — Terraform y cimientos de Azure (4–7 h)

- Crear módulos pequeños para resource group (incluido en Terraform), observabilidad, Event Hubs, Cosmos DB, Service Bus y Container Apps.
- Configurar tags, región única, nombres, límites de escala a cero cuando proceda y opciones de bajo costo.
- [x] Aprovisionar el RG dedicado y storage de state/checkpoints; migrar el state del bootstrap al backend privado remoto. No versionar state.
- Crear Managed Identity y asignaciones RBAC mínimas.
- Configurar pipeline de CI con OIDC; separar `plan` de `apply` y requerir aprobación para cambios de infraestructura.
- Configurar la alerta mensual de USD 30 ya confirmada, guardar su destinatario solo en configuración Terraform local ignorada y documentar que no es un hard cap.
- Registrar costos variables y volver a validar tarifas/beneficios con un plan concreto antes de `apply`.

**Salida:** `terraform fmt`/`validate` pasan; el bootstrap se aplicó y su state remoto no muestra drift. El plan y `apply` del stack de aplicación siguen bajo aprobación explícita.

### Fase 5 — Ingesta cloud y worker Go (5–9 h)

- Implementar API HTTP de ingesta para el simulador y publicación de eventos en Event Hubs.
- Implementar worker Go para leer Event Hubs, validar, escribir el evento y proyectar estado.
- Guardar inválidos en Service Bus con payload original, causa de rechazo y metadatos útiles.
- Para errores transitorios, no avanzar checkpoint antes de persistir resultados; asumir que el evento puede reproducirse.
- Configurar escalado KEDA según retraso de Event Hubs con límites de réplicas y capacidad explícitos.
- Asegurar acceso mediante Managed Identity y RBAC; evitar connection strings/secretos en código y repositorio.

**Salida:** prueba cloud pequeña coincide con la verdad de referencia; duplicados y replay son seguros.

### Fase 6 — API de lectura e IoT Hub (3–6 h)

- Implementar `GET /devices/{id}/state` y la lectura acotada de eventos.
- Configurar identidades por dispositivo y autenticación MQTT/TLS de IoT Hub.
- Configurar ruta de IoT Hub a Event Hubs y permisos administrados.
- Probar MQTT con el simulador antes del hardware.
- Comparar rutas MQTT y HTTPS con los mismos eventos y criterios de estado final.

**Salida:** los dos transportes producen el mismo estado, con errores identificables y sin duplicación lógica.

### Fase 7 — Observabilidad y validación de carga (3–6 h)

- Emitir métricas para ingestión, duplicados, desorden, retraso de ingesta, duración de procesamiento/checkpoint, fallos y cuarentena.
- Exportar trazas, métricas HTTP, resultados de procesamiento y duración de checkpoints a Application Insights mediante OpenTelemetry Collector.
- Configurar dashboard y alertas de costo/errores tras una aprobación independiente del despliegue.
- Medir primero con 100 y 1.000 dispositivos; probar 10.000 solo cuando la cuota y el presupuesto estén revisados.
- Ejecutar desconexión/reconexión masiva y replay.
- Capturar metodología, configuración, fecha, costos facturados y resultados reales.

**Avance:** la instrumentación OpenTelemetry y el export OTLP local se validaron contra un servidor de prueba. El simulador local pasó una corrida reproducible de 10.000 dispositivos, pero no mide el backend Azure. El dashboard, la carga/replay cloud y la verificación de recepción real en Application Insights requieren un despliegue aprobado y siguen pendientes.

### Fase 8 — Hardware de balcón (al final; 4–8 h más el tiempo de llegada)

- Antes de recibir los capacitivos, portar el envelope y store-and-forward al Xiao ESP32-C3 con HW-103 resistivo para pruebas breves.
- Conectar el DHT11 para temperatura; verificar identificación del módulo, precisión observada, alimentación y ubicación física.
- Mantener el sensor resistivo energizado solo durante la lectura y no dejarlo como sensor de uso continuo.
- Cuando lleguen los capacitivos, revisar voltaje/salida analógica y compatibilidad con el ADC del ESP32-C3; proteger placa/cable frente a humedad.
- Recalibrar valores con suelo seco y húmedo; no reutilizar calibración del resistivo.
- Implementar LittleFS, recuperación tras reinicio, envío por lotes MQTT/TLS y confirmación de eliminación local solo tras ACK.
- Probar corte de Wi-Fi, buffer lleno, reinicio y replay.

**Salida:** nodo mide solo humedad de suelo y temperatura ambiente, reenvía eventos almacenados y no acciona dispositivos de riego.

### Fase 9 — Cierre, documentación y destrucción controlada (1–3 h)

- Actualizar README con arquitectura, instrucciones, límites conocidos y pasos de despliegue/destrucción.
- Registrar resultados medidos y explicar las decisiones de orden, dedupe, costo y tradeoffs.
- Ejecutar `terraform plan` para confirmar alcance de destrucción y, tras revisar, `terraform destroy`.
- Confirmar que no queden recursos facturables, imágenes, logs o state sin considerar.
- Dejar la futura `personal-website` como una iniciativa separada; antes de iniciar, decidir qué publicar, qué suscripción usar y cómo migrar sin exponer secretos ni datos.

**Salida:** demo reproducible, cifras respaldadas y recursos temporales eliminados o justificados.

## 5. Estrategia para fines de semana

### Primer fin de semana

- Sesión 1: fase 0 y fase 1; dejar estable el envelope y la prueba property-based.
- Sesión 2: fase 2; implementar simulador y verdad de referencia.
- Sesión 3: primeras pruebas de persistencia local de fase 3.
- Cierre del fin de semana: revisar avance, deuda técnica, costos previstos y decidir el siguiente bloque antes de tocar Azure.

### Siguiente bloque de trabajo

- Completar fase 3; preparar Terraform, backend remoto y OIDC (fase 4).
- Desplegar solo después de confirmar presupuesto y permisos.
- Implementar fases 5–7 en incrementos pequeños, destruyendo recursos cuando no se necesiten.

### Tras la llegada de los sensores capacitivos

- Completar y recalibrar fase 8. La espera del envío no bloquea el desarrollo del simulador ni del backend.
- La fase de `personal-website` queda para dentro de 1–2 meses y requiere un plan/decisión separado.

## 6. Puertas de aprobación y límites operativos

- **Autorización recibida:** el usuario autorizó avanzar de forma autónoma hasta donde sea posible y detenerse ante bloqueos; seleccionó Visual Studio Enterprise Subscription y Central US. Se puede continuar con implementación local y preparación de Terraform.
- Antes de ejecutar `terraform apply` y crear recursos facturables, mostrar plan exacto y presupuesto/costo a confirmar por el usuario.
- Crear y administrar el Resource Group dedicado mediante Terraform; todos los recursos ARM de Azure de la demo residirán en él. Los objetos de Microsoft Entra son recursos de directorio y se gestionarán con Terraform, pero no pueden ser hijos de un Resource Group.
- Usar la Visual Studio Enterprise Subscription elegida inicialmente, verificando su tipo de beneficio, presupuesto, cargos y permiso para uso personal. No asumir que créditos/beneficios duran todo el proyecto.
- Elegir explícitamente la suscripción activa antes de cada operación que cree o modifique recursos; no depender de la suscripción predeterminada del CLI.
- No guardar credenciales, tokens, certificados privados, `*.tfstate`, archivos de entorno ni datos reales de dispositivos en Git.
- Aplicar límites de escala y logging; revisar retención y costo de Cosmos DB, Event Hubs, Container Apps y Log Analytics.
- Destruir recursos temporales cuando no se usen, verificando dependencias y state primero.

## 7. Criterios de aceptación del MVP

- Prueba property-based confirma estado final equivalente bajo permutación y duplicación.
- La verdad de referencia coincide con eventos y estado persistidos en una corrida de integración.
- Replay del mismo lote no cambia el conteo ni revierte el estado.
- Ingesta HTTP y MQTT llegan al mismo modelo lógico.
- La cuarentena contiene inválidos explicables y no bloquea el procesamiento de eventos válidos.
- Métricas de duplicados, fuera de orden, retraso y latencia se calculan desde datos observados.
- La solución se despliega y destruye con Terraform/CI sin secretos de larga duración en GitHub.
- El nodo opcional mide las dos señales acordadas y recupera eventos tras un corte de red.
- Se publican solo cifras medidas; costos y límites se documentan con evidencia real.
