variable "subscription_id" {
  type        = string
  description = "Azure subscription ID selected for this deployment."
}

variable "location" {
  type        = string
  description = "Azure region for the project resources."
  default     = "centralus"
}

variable "resource_group_name" {
  type        = string
  description = "Dedicated resource group for all project resources."
  default     = "rg-telemetry-ingest-iot-dev"
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
