variable "subscription_id" {
  type        = string
  description = "Azure subscription ID selected for this deployment."
}

variable "resource_group_name" {
  type        = string
  description = "Existing dedicated resource group created by the bootstrap stack."
  default     = "rg-telemetry-ingest-iot-dev"
}

variable "checkpoint_storage_account_name" {
  type        = string
  description = "Checkpoint storage account name output by infra/bootstrap."
}

variable "budget_amount" {
  type        = number
  description = "Monthly Azure cost alert threshold in the subscription billing currency; confirm before apply."

  validation {
    condition     = var.budget_amount > 0
    error_message = "Set a positive monthly budget amount confirmed by the project owner."
  }
}

variable "budget_contact_emails" {
  type        = set(string)
  description = "Email recipients for the monthly cost budget notifications."

  validation {
    condition     = length(var.budget_contact_emails) > 0
    error_message = "Set at least one confirmed recipient for budget notifications."
  }
}

variable "budget_start_date" {
  type        = string
  description = "First day of the budget period in UTC, formatted as YYYY-MM-01T00:00:00Z."

  validation {
    condition     = can(regex("^\\d{4}-\\d{2}-01T00:00:00Z$", var.budget_start_date))
    error_message = "The budget start date must be the first day of a month in UTC (YYYY-MM-01T00:00:00Z)."
  }
}

variable "tags" {
  type        = map(string)
  description = "Tags applied to project resources."
  default = {
    project     = "telemetry-ingest-iot"
    environment = "dev"
    managed_by  = "terraform"
  }
}
