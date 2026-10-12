data "azurerm_resource_group" "project" {
  name = var.resource_group_name
}

data "azurerm_storage_account" "checkpoints" {
  name                = var.checkpoint_storage_account_name
  resource_group_name = data.azurerm_resource_group.project.name
}

resource "random_string" "suffix" {
  length  = 5
  upper   = false
  special = false
  numeric = true
}

resource "azurerm_log_analytics_workspace" "project" {
  name                = "law-telemetry-${random_string.suffix.result}"
  location            = data.azurerm_resource_group.project.location
  resource_group_name = data.azurerm_resource_group.project.name
  sku                 = "PerGB2018"
  retention_in_days   = 30
  tags                = var.tags
}

resource "azurerm_application_insights" "project" {
  name                 = "appi-telemetry-${random_string.suffix.result}"
  location             = data.azurerm_resource_group.project.location
  resource_group_name  = data.azurerm_resource_group.project.name
  workspace_id         = azurerm_log_analytics_workspace.project.id
  application_type     = "web"
  retention_in_days    = 30
  daily_data_cap_in_gb = 0.1
  tags                 = var.tags
}

resource "azurerm_eventhub_namespace" "project" {
  name                = "ehns-telemetry-${random_string.suffix.result}"
  location            = data.azurerm_resource_group.project.location
  resource_group_name = data.azurerm_resource_group.project.name
  sku                 = "Basic"

  minimum_tls_version           = "1.2"
  public_network_access_enabled = true
  local_authentication_enabled  = false
  tags                          = var.tags
}

resource "azurerm_eventhub" "telemetry" {
  name              = "telemetry"
  namespace_id      = azurerm_eventhub_namespace.project.id
  partition_count   = 4
  message_retention = 1
}

resource "azurerm_cosmosdb_account" "project" {
  name                = "cosmos-telemetry-${random_string.suffix.result}"
  location            = data.azurerm_resource_group.project.location
  resource_group_name = data.azurerm_resource_group.project.name
  offer_type          = "Standard"
  kind                = "GlobalDocumentDB"

  automatic_failover_enabled    = false
  public_network_access_enabled = true
  local_authentication_enabled  = false
  free_tier_enabled             = false

  capabilities {
    name = "EnableServerless"
  }

  consistency_policy {
    consistency_level = "Session"
  }

  geo_location {
    location          = data.azurerm_resource_group.project.location
    failover_priority = 0
  }

  tags = var.tags
}

resource "azurerm_cosmosdb_sql_database" "project" {
  name                = "telemetry"
  resource_group_name = data.azurerm_resource_group.project.name
  account_name        = azurerm_cosmosdb_account.project.name
}

resource "azurerm_cosmosdb_sql_container" "records" {
  name                = "records"
  resource_group_name = data.azurerm_resource_group.project.name
  account_name        = azurerm_cosmosdb_account.project.name
  database_name       = azurerm_cosmosdb_sql_database.project.name
  partition_key_paths = ["/device_id"]
}

resource "azurerm_servicebus_namespace" "project" {
  name                = "sbns-telemetry-${random_string.suffix.result}"
  location            = data.azurerm_resource_group.project.location
  resource_group_name = data.azurerm_resource_group.project.name
  sku                 = "Basic"

  minimum_tls_version = "1.2"
  local_auth_enabled  = false
  tags                = var.tags
}

resource "azurerm_servicebus_queue" "quarantine" {
  name         = "invalid-telemetry"
  namespace_id = azurerm_servicebus_namespace.project.id
}

resource "azurerm_consumption_budget_resource_group" "monthly" {
  name              = "telemetry-ingest-iot-monthly"
  resource_group_id = data.azurerm_resource_group.project.id
  amount            = var.budget_amount
  time_grain        = "Monthly"

  time_period {
    start_date = var.budget_start_date
    end_date   = "2036-10-07T00:00:00Z"
  }

  notification {
    enabled        = true
    threshold      = 50
    operator       = "GreaterThan"
    threshold_type = "Actual"
    contact_emails = var.budget_contact_emails
  }

  notification {
    enabled        = true
    threshold      = 80
    operator       = "GreaterThan"
    threshold_type = "Actual"
    contact_emails = var.budget_contact_emails
  }

  notification {
    enabled        = true
    threshold      = 100
    operator       = "GreaterThan"
    threshold_type = "Forecasted"
    contact_emails = var.budget_contact_emails
  }
}
