output "resource_group_name" {
  value       = azurerm_resource_group.project.name
  description = "Dedicated resource group shared by the bootstrap and application stacks."
}

output "terraform_state_storage_account_name" {
  value       = azurerm_storage_account.terraform_state.name
  description = "Storage account for the Terraform state backend."
}

output "checkpoint_storage_account_name" {
  value       = azurerm_storage_account.checkpoints.name
  description = "Storage account for Event Hubs consumer checkpoints."
}
