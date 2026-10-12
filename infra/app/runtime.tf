resource "azurerm_container_app_environment" "project" {
  name                       = "cae-telemetry-${random_string.suffix.result}"
  location                   = data.azurerm_resource_group.project.location
  resource_group_name        = data.azurerm_resource_group.project.name
  log_analytics_workspace_id = azurerm_log_analytics_workspace.project.id
  tags                       = var.tags
}

resource "random_password" "ingestion_token" {
  length  = 48
  special = false
}

resource "azurerm_iothub" "project" {
  name                         = "iot-telemetry-${random_string.suffix.result}"
  location                     = data.azurerm_resource_group.project.location
  resource_group_name          = data.azurerm_resource_group.project.name
  local_authentication_enabled = false
  min_tls_version              = "1.2"
  event_hub_partition_count    = 4

  sku {
    name     = "F1"
    capacity = "1"
  }

  identity {
    type = "SystemAssigned"
  }

  tags = var.tags
}

resource "azurerm_iothub_endpoint_eventhub" "telemetry" {
  name                = "telemetry-events"
  resource_group_name = data.azurerm_resource_group.project.name
  iothub_id           = azurerm_iothub.project.id
  authentication_type = "identityBased"
  endpoint_uri        = "sb://${azurerm_eventhub_namespace.project.name}.servicebus.windows.net/"
  entity_path         = azurerm_eventhub.telemetry.name

  depends_on = [azurerm_role_assignment.iothub_eventhub_sender]
}

resource "azurerm_iothub_route" "telemetry" {
  name                = "device-telemetry"
  resource_group_name = data.azurerm_resource_group.project.name
  iothub_name         = azurerm_iothub.project.name
  source              = "DeviceMessages"
  condition           = "true"
  endpoint_names      = [azurerm_iothub_endpoint_eventhub.telemetry.name]
  enabled             = true
}

resource "azurerm_role_assignment" "iothub_eventhub_sender" {
  scope                = azurerm_eventhub_namespace.project.id
  role_definition_name = "Azure Event Hubs Data Sender"
  principal_id         = azurerm_iothub.project.identity[0].principal_id
}

resource "azurerm_role_assignment" "ingest_eventhub_sender" {
  scope                = azurerm_eventhub_namespace.project.id
  role_definition_name = "Azure Event Hubs Data Sender"
  principal_id         = azurerm_container_app.ingest.identity[0].principal_id
}

resource "azurerm_role_assignment" "processor_eventhub_receiver" {
  scope                = azurerm_eventhub_namespace.project.id
  role_definition_name = "Azure Event Hubs Data Receiver"
  principal_id         = azurerm_container_app.processor.identity[0].principal_id
}

resource "azurerm_cosmosdb_sql_role_assignment" "api_cosmos_reader" {
  resource_group_name = data.azurerm_resource_group.project.name
  account_name        = azurerm_cosmosdb_account.project.name
  role_definition_id  = "${azurerm_cosmosdb_account.project.id}/sqlRoleDefinitions/00000000-0000-0000-0000-000000000001"
  principal_id        = azurerm_container_app.ingest.identity[0].principal_id
  scope               = azurerm_cosmosdb_account.project.id
}

resource "azurerm_cosmosdb_sql_role_assignment" "processor_cosmos_writer" {
  resource_group_name = data.azurerm_resource_group.project.name
  account_name        = azurerm_cosmosdb_account.project.name
  role_definition_id  = "${azurerm_cosmosdb_account.project.id}/sqlRoleDefinitions/00000000-0000-0000-0000-000000000002"
  principal_id        = azurerm_container_app.processor.identity[0].principal_id
  scope               = azurerm_cosmosdb_account.project.id
}

resource "azurerm_role_assignment" "processor_checkpoint_access" {
  scope                = data.azurerm_storage_account.checkpoints.id
  role_definition_name = "Storage Blob Data Contributor"
  principal_id         = azurerm_container_app.processor.identity[0].principal_id
}

resource "azurerm_role_assignment" "processor_quarantine_sender" {
  scope                = azurerm_servicebus_namespace.project.id
  role_definition_name = "Azure Service Bus Data Sender"
  principal_id         = azurerm_container_app.processor.identity[0].principal_id
}

resource "azurerm_container_app" "ingest" {
  name                         = "ca-telemetry-ingest"
  container_app_environment_id = azurerm_container_app_environment.project.id
  resource_group_name          = data.azurerm_resource_group.project.name
  revision_mode                = "Single"
  identity {
    type = "SystemAssigned"
  }

  secret {
    name  = "ingestion-token"
    value = random_password.ingestion_token.result
  }
  secret {
    name  = "app-insights-connection-string"
    value = azurerm_application_insights.project.connection_string
  }

  ingress {
    external_enabled = true
    target_port      = 8080
    transport        = "http"

    traffic_weight {
      percentage      = 100
      latest_revision = true
    }
  }

  template {
    min_replicas = 0
    max_replicas = 8

    http_scale_rule {
      name                = "http-requests"
      concurrent_requests = 20
    }

    container {
      name   = "ingest-api"
      image  = "ghcr.io/alulema/telemetry-ingest-iot/ingest-api:main"
      cpu    = 0.25
      memory = "0.5Gi"

      env {
        name  = "HTTP_ADDR"
        value = ":8080"
      }
      env {
        name  = "EVENTHUB_NAMESPACE"
        value = "${azurerm_eventhub_namespace.project.name}.servicebus.windows.net"
      }
      env {
        name  = "EVENTHUB_NAME"
        value = azurerm_eventhub.telemetry.name
      }
      env {
        name  = "COSMOS_ENDPOINT"
        value = azurerm_cosmosdb_account.project.endpoint
      }
      env {
        name  = "COSMOS_DATABASE"
        value = azurerm_cosmosdb_sql_database.project.name
      }
      env {
        name  = "COSMOS_CONTAINER"
        value = azurerm_cosmosdb_sql_container.records.name
      }
      env {
        name        = "INGESTION_TOKEN"
        secret_name = "ingestion-token"
      }
      env {
        name  = "OTEL_EXPORTER_OTLP_ENDPOINT"
        value = "http://localhost:4318"
      }
    }

    container {
      name   = "otel-collector"
      image  = "ghcr.io/alulema/telemetry-ingest-iot/otel-collector:main"
      cpu    = 0.25
      memory = "0.5Gi"

      env {
        name        = "APPLICATIONINSIGHTS_CONNECTION_STRING"
        secret_name = "app-insights-connection-string"
      }
    }
  }

  tags = var.tags

}

resource "azurerm_container_app" "processor" {
  name                         = "ca-telemetry-processor"
  container_app_environment_id = azurerm_container_app_environment.project.id
  resource_group_name          = data.azurerm_resource_group.project.name
  revision_mode                = "Single"

  identity {
    type = "SystemAssigned"
  }

  secret {
    name  = "app-insights-connection-string"
    value = azurerm_application_insights.project.connection_string
  }

  template {
    min_replicas = 0
    max_replicas = 4

    custom_scale_rule {
      name             = "eventhub-backlog"
      custom_rule_type = "azure-eventhub"
      identity_id      = "system"
      metadata = {
        eventHubNamespace         = azurerm_eventhub_namespace.project.name
        eventHubName              = azurerm_eventhub.telemetry.name
        consumerGroup             = azurerm_eventhub_consumer_group.processor.name
        unprocessedEventThreshold = "32"
        blobContainer             = "event-checkpoints"
        storageAccountName        = data.azurerm_storage_account.checkpoints.name
        checkpointStrategy        = "goSdk"
      }
    }

    container {
      name   = "processor"
      image  = "ghcr.io/alulema/telemetry-ingest-iot/processor:main"
      cpu    = 0.25
      memory = "0.5Gi"

      env {
        name  = "EVENTHUB_NAMESPACE"
        value = "${azurerm_eventhub_namespace.project.name}.servicebus.windows.net"
      }
      env {
        name  = "EVENTHUB_NAME"
        value = azurerm_eventhub.telemetry.name
      }
      env {
        name  = "EVENTHUB_CONSUMER_GROUP"
        value = azurerm_eventhub_consumer_group.processor.name
      }
      env {
        name  = "CHECKPOINT_STORAGE_ACCOUNT"
        value = data.azurerm_storage_account.checkpoints.name
      }
      env {
        name  = "CHECKPOINT_CONTAINER"
        value = "event-checkpoints"
      }
      env {
        name  = "COSMOS_ENDPOINT"
        value = azurerm_cosmosdb_account.project.endpoint
      }
      env {
        name  = "COSMOS_DATABASE"
        value = azurerm_cosmosdb_sql_database.project.name
      }
      env {
        name  = "COSMOS_CONTAINER"
        value = azurerm_cosmosdb_sql_container.records.name
      }
      env {
        name  = "SERVICEBUS_NAMESPACE"
        value = "${azurerm_servicebus_namespace.project.name}.servicebus.windows.net"
      }
      env {
        name  = "SERVICEBUS_QUEUE"
        value = azurerm_servicebus_queue.quarantine.name
      }
      env {
        name  = "OTEL_EXPORTER_OTLP_ENDPOINT"
        value = "http://localhost:4318"
      }
    }

    container {
      name   = "otel-collector"
      image  = "ghcr.io/alulema/telemetry-ingest-iot/otel-collector:main"
      cpu    = 0.25
      memory = "0.5Gi"

      env {
        name        = "APPLICATIONINSIGHTS_CONNECTION_STRING"
        secret_name = "app-insights-connection-string"
      }
    }
  }

  tags = var.tags

}

output "ingest_api_url" {
  value       = "https://${azurerm_container_app.ingest.ingress[0].fqdn}"
  description = "Public HTTPS endpoint for simulated telemetry ingestion."
}

output "ingestion_token" {
  value       = random_password.ingestion_token.result
  sensitive   = true
  description = "Bearer token for the public ingestion and state API."
}

output "iothub_name" {
  value       = azurerm_iothub.project.name
  description = "IoT Hub device endpoint; device provisioning is not included yet."
}
