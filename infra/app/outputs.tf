output "resource_group_name" {
  value       = data.azurerm_resource_group.project.name
  description = "Dedicated project resource group."
}

output "eventhub_namespace_fqdn" {
  value       = azurerm_eventhub_namespace.project.name
  description = "Event Hubs namespace name used by the Go services."
}

output "cosmos_endpoint" {
  value       = azurerm_cosmosdb_account.project.endpoint
  description = "Cosmos DB endpoint; runtime authorization uses managed identity."
}

output "servicebus_namespace_fqdn" {
  value       = azurerm_servicebus_namespace.project.name
  description = "Service Bus namespace name used by the quarantine sender."
}

output "checkpoint_storage_account_name" {
  value       = data.azurerm_storage_account.checkpoints.name
  description = "Blob storage account used for Event Hubs checkpoints."
}
