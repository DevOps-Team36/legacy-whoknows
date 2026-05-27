variable "do_token" {
  description = "DigitalOcean API token"
  type        = string
  sensitive   = true
}

variable "ssh_key_name" {
  description = "Name of SSH key registered in DigitalOcean. Find it with: doctl compute ssh-key list"
  type        = string
}

variable "region" {
  description = "DigitalOcean region"
  type        = string
  default     = "fra1"
}

variable "droplet_size" {
  description = "Droplet size slug. See: doctl compute size list"
  type        = string
  default     = "s-1vcpu-2gb"
}

variable "domain" {
  description = "Domain name pointing to the droplet"
  type        = string
  default     = "huw.dk"
}

variable "monitoring_droplet_size" {
  description = "Droplet size for the monitoring server"
  type        = string
  default     = "s-1vcpu-1gb"
}

variable "cf_api_token" {
  description = "Cloudflare API token med DNS Edit-rettigheder til zonen. Opret på: Cloudflare Dashboard → My Profile → API Tokens → Edit zone DNS"
  type        = string
  sensitive   = true
}

variable "cf_zone_id" {
  description = "Cloudflare Zone ID for domænet. Find det på: Cloudflare Dashboard → dit domæne → Overview → Zone ID (højre side)"
  type        = string
}
