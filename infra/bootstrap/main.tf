data "azurerm_client_config" "current" {}

resource "random_string" "suffix" {
  length  = 5
  upper   = false
  special = false
  numeric = true
}

resource "azurerm_resource_group" "project" {
  name     = var.resource_group_name
  location = var.location
  tags     = var.tags
}

resource "azurerm_resource_provider_registration" "devices" {
  name = "Microsoft.Devices"
}

resource "azurerm_storage_account" "terraform_state" {
  name                            = "teltf${random_string.suffix.result}"
  resource_group_name             = azurerm_resource_group.project.name
  location                        = azurerm_resource_group.project.location
  account_tier                    = "Standard"
  account_replication_type        = "LRS"
  min_tls_version                 = "TLS1_2"
  https_traffic_only_enabled      = true
  public_network_access           = "Enabled"
  allow_nested_items_to_be_public = false
  shared_access_key_enabled       = false
  tags                            = var.tags
}

resource "azurerm_storage_account" "checkpoints" {
  name                            = "telcp${random_string.suffix.result}"
  resource_group_name             = azurerm_resource_group.project.name
  location                        = azurerm_resource_group.project.location
  account_tier                    = "Standard"
  account_replication_type        = "LRS"
  min_tls_version                 = "TLS1_2"
  https_traffic_only_enabled      = true
  public_network_access           = "Enabled"
  allow_nested_items_to_be_public = false
  shared_access_key_enabled       = false
  tags                            = var.tags
}

resource "azurerm_role_assignment" "state_blob_access" {
  scope                = azurerm_storage_account.terraform_state.id
  role_definition_name = "Storage Blob Data Contributor"
  principal_id         = data.azurerm_client_config.current.object_id
}

resource "azurerm_role_assignment" "checkpoint_blob_access" {
  scope                = azurerm_storage_account.checkpoints.id
  role_definition_name = "Storage Blob Data Contributor"
  principal_id         = data.azurerm_client_config.current.object_id
}

resource "azurerm_storage_container" "terraform_state" {
  name                  = "tfstate"
  storage_account_id    = azurerm_storage_account.terraform_state.id
  container_access_type = "private"

  depends_on = [azurerm_role_assignment.state_blob_access]
}

resource "azurerm_storage_container" "checkpoints" {
  name                  = "event-checkpoints"
  storage_account_id    = azurerm_storage_account.checkpoints.id
  container_access_type = "private"

  depends_on = [azurerm_role_assignment.checkpoint_blob_access]
}
