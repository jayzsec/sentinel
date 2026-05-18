# Define or declare the provider
terraform {
  required_providers {
    azurerm = {
      source = "hashicorp/azurerm"
      version = "~> 4.71.0"
    }
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.19.1"
    }
  }
}

provider "azurerm" {
  features {}
}

provider "cloudflare" {
  # Terraform automatically looks for the CLOUDFLARE_API_TOKEN env var
}

# 1. Create the CNAME for ai.cabang.dev
resource "cloudflare_dns_record" "api_gateway" {
  zone_id = var.cloudflare_zone_id
  name    = "ai"
  content = azurerm_container_app.api_gateway.ingress[0].fqdn
  type    = "CNAME"
  proxied = false
  ttl     = 1 # 1 equals 'Auto' in Cloudflare
}

# 2. Create the secret TXT record Azure needs to "trust" you
resource "cloudflare_dns_record" "azure_verification" {
  zone_id = var.cloudflare_zone_id
  name    = "asuid.ai"
  content = azurerm_container_app.api_gateway.custom_domain_verification_id
  type    = "TXT"
  ttl     = 1
}

# 3. Tell Azure to bind the domain
resource "azurerm_container_app_custom_domain" "binding" {
  container_app_id = azurerm_container_app.api_gateway.id
  name             = "ai.cabang.dev"
  # defaults to null
  # certificate_binding_type = "SniEnabled"
  # our cert id
  # container_app_environment_managed_certificate_id = azurerm_container_app_environment_managed_certificate.ai_cert.id
  # Wait for Cloudflare to finish before Azure verifies
  depends_on = [cloudflare_dns_record.azure_verification]
  lifecycle {
    ignore_changes = [
      certificate_binding_type,
      container_app_environment_certificate_id
    ]
  }
}

resource "azurerm_container_app_environment_managed_certificate" "ai_cert" {
  # The name at the very end of your manual certificate ID string
  name                         = "ai.cabang.dev-rg-hospi-260515142946"
  container_app_environment_id = azurerm_container_app_environment.env.id
  subject_name                 = "ai.cabang.dev"
  domain_control_validation    = "CNAME"
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

# Log Analytics Workspace / Observability
resource "azurerm_log_analytics_workspace" "logs" {
  location            = azurerm_resource_group.rg.location
  name                = "logs-hospitality-capstone"
  resource_group_name = azurerm_resource_group.rg.name
  sku = "PerGB2018"
  retention_in_days = 30
}

# Container App Environment / Cluster
resource "azurerm_container_app_environment" "env" {
  location                   = azurerm_resource_group.rg.location
  name                       = "env-hospitality-capstone"
  resource_group_name        = azurerm_resource_group.rg.name
  log_analytics_workspace_id = azurerm_log_analytics_workspace.logs.id
}

# C# Analytics Engine (Container App)
resource "azurerm_container_app" "analytics" {
  name                         = "analytics-engine"
  resource_group_name          = azurerm_resource_group.rg.name
  container_app_environment_id = azurerm_container_app_environment.env.id
  revision_mode                = "Single"

  registry {
    server = azurerm_container_registry.acr.login_server
    username = azurerm_container_registry.acr.admin_username
    password_secret_name = "acr-password"
  }
  # We configure the ingress to allow internal traffic from the Generator
  ingress {
    allow_insecure_connections = false
    external_enabled = true
    target_port = 8080
    traffic_weight {
      percentage = 100
      latest_revision = true
    }
  }

  # Implementing the Version 2 Secrets Requirement / STRICT COMPLIANCE
  secret {
    name = "cosmos-connection-string-v2"
    # In a fully mature environment, this value pulls from Azure Key Vault targeting the v2 hash.
    # Here, we directly map the Cosmos DB primary string to the v2 secret definition.
    value = azurerm_cosmosdb_account.db.primary_sql_connection_string
  }
  secret {
    name = "acr-password"
    value = azurerm_container_registry.acr.admin_password
  }

  secret {
    name  = "cosmos-key" # This is the internal reference name
    value = azurerm_cosmosdb_account.db.primary_key
  }

  template {
    container {
      cpu    = 0.25
      # For now, we use a placeholder image. In a full CI/CD pipeline,
      # GitHub Actions would push our compiled C# image to ACR and update this tag.
      image  = "${azurerm_container_registry.acr.login_server}/analytics-engine:v3"
      memory = "0.5Gi"
      name   = "analytics-engine"

      # Passing the Version 2 secret into the container's Environment Variables
      env {
        name = "COSMOS_DB_CONNECTION"
        secret_name = "cosmos-connection-string-v2"
      }
      env {
        name = "COSMOS_ENDPOINT"
        value = azurerm_cosmosdb_account.db.endpoint
      }
      env {
        name = "COSMOS_KEY"
        secret_name = "cosmos-key"
      }
      env {
        name  = "APPLICATIONINSIGHTS_CONNECTION_STRING"
        value = azurerm_application_insights.appinsights.connection_string
      }
    }
  }
}

# REDIS Distributed State Store
resource "azurerm_redis_cache" "redis" {
  name                = "redis-sentinel-soc-${random_string.suffix.result}"
  location            = azurerm_resource_group.rg.location
  resource_group_name = azurerm_resource_group.rg.name
  capacity            = 0
  family              = "C"
  sku_name            = "Basic"
  minimum_tls_version = "1.2"
}

# Required to ensure the Redis name is globally unique
resource "random_string" "suffix" {
  length  = 6
  special = false
  upper   = false
}

# Go Sentinel SOC (Container App)
resource "azurerm_container_app" "sentinel" {
  name                         = "sentinel-soc"
  resource_group_name          = azurerm_resource_group.rg.name
  container_app_environment_id = azurerm_container_app_environment.env.id
  revision_mode                = "Single"

  registry {
    server = azurerm_container_registry.acr.login_server
    username = azurerm_container_registry.acr.admin_username
    password_secret_name = "acr-password"
  }

  ingress {
    allow_insecure_connections = false
    external_enabled = false
    target_port = 8080
    traffic_weight {
      percentage = 100
      latest_revision = true
    }
  }

  secret {
    name = "acr-password"
    value = azurerm_container_registry.acr.admin_password
  }

  secret {
    name  = "redis-password"
    value = azurerm_redis_cache.redis.primary_access_key
  }

  template {
    container {
      cpu    = 0.25
      image  = "${azurerm_container_registry.acr.login_server}/sentinel-soc:v6"
      memory = "0.5Gi"
      name   = "sentinel-soc"

      env {
        name  = "ANALYTICS_URL"
        value = "https://${azurerm_container_app.analytics.ingress[0].fqdn}/ingest"
      }

      # Redis update
      env {
        name  = "REDIS_ADDR"
        # Azure Redis uses port 6380 for SSL connections
        value = "${azurerm_redis_cache.redis.hostname}:6380"
      }
      env {
        name        = "REDIS_PASSWORD"
        secret_name = "redis-password"
      }
      env {
        name  = "APPLICATIONINSIGHTS_CONNECTION_STRING"
        value = azurerm_application_insights.appinsights.connection_string
      }
    }
  }
}

# Go POS Data Generator (Container App)
resource "azurerm_container_app" "generator" {
  container_app_environment_id = azurerm_container_app_environment.env.id
  name                         = "pos-generator"
  resource_group_name          = azurerm_resource_group.rg.name
  revision_mode                = "Single"

  registry {
    server = azurerm_container_registry.acr.login_server
    username = azurerm_container_registry.acr.admin_username
    password_secret_name = "acr-password"
  }

  secret {
    name = "acr-password"
    value = azurerm_container_registry.acr.admin_password
  }

  template {
    container {
      cpu    = 0.25
      image  = "${azurerm_container_registry.acr.login_server}/pos-generator:v4"
      memory = "0.5Gi"
      name   = "pos-generator"

      # Dynamically injecting the internal network URLs of our other services!
      env {
        name = "SENTINEL_URL"
        value = "https://${azurerm_container_app.sentinel.ingress[0].fqdn}/events"
      }
      env {
        name = "ANALYTICS_URL"
        value = "https://${azurerm_container_app.analytics.ingress[0].fqdn}/ingest"
      }
      env {
        name  = "APPLICATIONINSIGHTS_CONNECTION_STRING"
        value = azurerm_application_insights.appinsights.connection_string
      }
    }
  }
}

# Active FinOps SRE Bot (CronJob)
resource "azurerm_container_app_job" "finops_bot" {
  name                         = "finops-bot-job"
  container_app_environment_id = azurerm_container_app_environment.env.id
  resource_group_name          = azurerm_resource_group.rg.name
  location                     = azurerm_resource_group.rg.location

  # Fail-safe: If the script gets stuck, Azure will kill it after 60 seconds
  replica_timeout_in_seconds = 60
  replica_retry_limit        = 1

  # FIX: Enable System-Assigned Managed Identity
  identity {
    type = "SystemAssigned"
  }

  schedule_trigger_config {
    cron_expression = "0 * * * *" # Runs every hour, on the hour
    parallelism     = 1           # Ensures only one instance runs at a time
  }

  registry {
    server               = azurerm_container_registry.acr.login_server
    username             = azurerm_container_registry.acr.admin_username
    password_secret_name = "acr-password"
  }

  secret {
    name  = "acr-password"
    value = azurerm_container_registry.acr.admin_password
  }

  template {
    container {
      name   = "finops-bot"
      image  = "${azurerm_container_registry.acr.login_server}/finops-bot:v2"
      cpu    = 0.25
      memory = "0.5Gi"

      # Passing our configuration into the Go Adapter Factory
      env {
        name  = "CLOUD_PROVIDER"
        value = "azure"
      }
      # NOTE: In a production pipeline, this ID would be fetched from Azure Key Vault
      # Github Actions
      env {
        name  = "AZURE_SUBSCRIPTION_ID"
        value = var.subscription_id
      }
    }
  }
}

# FIX: Grant the bot permission to read Billing/Cost Data
# We scope this to the whole subscription so it can read the Cost Management API
resource "azurerm_role_assignment" "finops_billing_reader" {
  scope                = "/subscriptions/${var.subscription_id}"
  role_definition_name = "Cost Management Reader"
  principal_id         = azurerm_container_app_job.finops_bot.identity[0].principal_id
}

# FIX: Grant the bot permission to scale down apps
# We scope this to the Resource Group so it can modify the other container apps
resource "azurerm_role_assignment" "finops_rg_contributor" {
  scope                = azurerm_resource_group.rg.id
  role_definition_name = "Contributor"
  principal_id         = azurerm_container_app_job.finops_bot.identity[0].principal_id
}

# Log Analytics Workspace (The storage engine)
resource "azurerm_log_analytics_workspace" "law" {
  name                = "law-sentinel-soc"
  location            = azurerm_resource_group.rg.location
  resource_group_name = azurerm_resource_group.rg.name
  sku                 = "PerGB2018"
  retention_in_days   = 30
}

# Application Insights (The Single Pane of Glass)
resource "azurerm_application_insights" "appinsights" {
  name                = "appinsights-sentinel-soc"
  location            = azurerm_resource_group.rg.location
  resource_group_name = azurerm_resource_group.rg.name
  workspace_id        = azurerm_log_analytics_workspace.law.id
  application_type    = "web"
}

locals {
  envoy_config = <<EOF
admin:
  address:
    socket_address: { address: 0.0.0.0, port_value: 9901 }
static_resources:
  listeners:
  - name: listener_0
    address:
      socket_address: { address: 0.0.0.0, port_value: 8080 }
    filter_chains:
    - filters:
      - name: envoy.filters.network.http_connection_manager
        typed_config:
          "@type": type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager
          stat_prefix: ingress_http
          route_config:
            name: local_route
            virtual_hosts:
            - name: backend
              domains: ["*"]
              routes:
              # 1. Hard block the ingest endpoint from the public
              - match: { prefix: "/ingest" }
                direct_response:
                  status: 403
                  body: { inline_string: "Direct ingestion forbidden by Sentinel SOC API Gateway." }
              # 2. Route the Dashboard UI to the internal C# engine
              - match: { prefix: "/" }
                route:
                  cluster: analytics_engine
                  timeout: 60s
                  auto_host_rewrite: true
          http_filters:
          - name: envoy.filters.http.router
            typed_config:
              "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
  clusters:
  - name: analytics_engine
    type: LOGICAL_DNS
    dns_lookup_family: V4_ONLY
    load_assignment:
      cluster_name: analytics_engine
      endpoints:
      - lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: ${azurerm_container_app.analytics.ingress[0].fqdn}
                port_value: 443
    transport_socket:
      name: envoy.transport_sockets.tls
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext
        sni: ${azurerm_container_app.analytics.ingress[0].fqdn}
EOF
}

resource "azurerm_container_app" "api_gateway" {
  name                         = "api-gateway"
  container_app_environment_id = azurerm_container_app_environment.env.id
  resource_group_name          = azurerm_resource_group.rg.name
  revision_mode                = "Single"

  template {
    container {
      name    = "envoy-proxy"
      image   = "envoyproxy/envoy:v1.30-latest"
      cpu     = 0.25
      memory  = "0.5Gi"

      # FIX: Write the decoded YAML to /tmp/ instead of /etc/
      command = ["/bin/sh", "-c"]
      args    = ["echo $ENVOY_CONF_B64 | base64 -d > /tmp/envoy.yaml && envoy -c /tmp/envoy.yaml"]
      env {
        name  = "ENVOY_CONF_B64"
        value = base64encode(local.envoy_config)
      }
    }
  }

  ingress {
    allow_insecure_connections = false
    external_enabled           = true  # The only public entry point
    target_port                = 8080
    traffic_weight {
      percentage      = 100
      latest_revision = true
    }
  }
}