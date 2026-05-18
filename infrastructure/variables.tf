variable "subscription_id" {
  type      = string
  sensitive = true
}

variable "cloudflare_zone_id" {
  type        = string
  description = "The Cloudflare Zone ID for the domain"
}