# Define or declare the provider
terraform {
  required_providers {
    azurerm = {
      source = "hashicorp/azurerm"
      version = "~> 3.100.0"
    }
  }
}

provider "azurerm" {
  features {}
}

# Resource group
resource "azurerm_resource_group" "rg" {
  location = "australiaeast"
  name     = "rg-hospitality-capstone"
}

# $10 Hard Limit Budget Alarm / The FinOps Control
resource "azurerm_consumption_budget_resource_group" "budget" {
  amount            = 10.0
  name              = "capstone-strict-budget"
  resource_group_id = azurerm_resource_group.rg.id
  time_grain = "Monthly"

  time_period {
    start_date = "2026-05-01T00:00:00Z" # Start of current billing cycle
    end_date = "2027-05-01T00:00:00Z"
  }

  notification {
    enabled = true
    operator  = "GreaterThan"
    threshold = 90.0
    contact_emails = ["jayzsec@gmail.com"]
  }
}

# Azure Cosmos DB (NoSQL) / Database
resource "azurerm_cosmosdb_account" "db" {
  location            = azurerm_resource_group.rg.location
  name                = "cosmos-hospitality-analytics-202600180"
  offer_type          = "Standard"
  resource_group_name = azurerm_resource_group.rg.name
  kind = "GlobalDocumentDB"

  # Enforces the perpetual free tier / Important flag
  free_tier_enabled = true

  consistency_policy {
    consistency_level = "Session"
  }

  geo_location {
    failover_priority = 0
    location          = azurerm_resource_group.rg.location
  }
}

# Azure Container Registry / Where to store docker images
resource "azurerm_container_registry" "acr" {
  location            = azurerm_resource_group.rg.location
  name                = "acrhospitalitycapstone"
  resource_group_name = azurerm_resource_group.rg.name
  sku                 = "Basic"
  admin_enabled = true
}
