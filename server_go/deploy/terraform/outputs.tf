output "droplet_ip" {
  description = "Public IP address of the WhoKnows droplet"
  value       = digitalocean_droplet.whoknows.ipv4_address
}

output "droplet_id" {
  description = "DigitalOcean droplet ID"
  value       = digitalocean_droplet.whoknows.id
}

output "dns_record" {
  description = "Cloudflare A-record der peger på droplet"
  value       = "${var.domain} → ${digitalocean_droplet.whoknows.ipv4_address}"
}

output "monitoring_ip" {
  description = "Public IP address of the monitoring droplet"
  value       = digitalocean_droplet.monitoring.ipv4_address
}

output "monitoring_dns_record" {
  description = "Cloudflare A-record for monitor.huw.dk"
  value       = "monitor.${var.domain} → ${digitalocean_droplet.monitoring.ipv4_address}"
}
