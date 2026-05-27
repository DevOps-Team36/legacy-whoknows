locals {
  deploy_user_data = <<-EOF
    #!/bin/bash
    set -e
    adduser --disabled-password --gecos "" deploy
    usermod -aG sudo deploy
    mkdir -p /home/deploy/.ssh
    cp /root/.ssh/authorized_keys /home/deploy/.ssh/authorized_keys
    chown -R deploy:deploy /home/deploy/.ssh
    chmod 700 /home/deploy/.ssh
    chmod 600 /home/deploy/.ssh/authorized_keys
    echo "deploy ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/deploy
  EOF
}

data "digitalocean_ssh_key" "deploy" {
  name = var.ssh_key_name
}

# ── App server ───────────────────────────────────────────────

resource "digitalocean_droplet" "whoknows" {
  name      = "whoknows"
  region    = var.region
  size      = var.droplet_size
  image     = "ubuntu-24-04-x64"
  ssh_keys  = [data.digitalocean_ssh_key.deploy.id]
  user_data = local.deploy_user_data
  tags      = ["whoknows"]
}

resource "cloudflare_record" "whoknows_a" {
  zone_id = var.cf_zone_id
  name    = "@"
  content = digitalocean_droplet.whoknows.ipv4_address
  type    = "A"
  ttl     = 1
  proxied = true
}

resource "digitalocean_firewall" "whoknows" {
  name        = "whoknows-firewall"
  droplet_ids = [digitalocean_droplet.whoknows.id]

  inbound_rule {
    protocol         = "tcp"
    port_range       = "22"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }

  inbound_rule {
    protocol         = "tcp"
    port_range       = "80"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }

  inbound_rule {
    protocol         = "tcp"
    port_range       = "443"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }

  outbound_rule {
    protocol              = "tcp"
    port_range            = "all"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }

  outbound_rule {
    protocol              = "udp"
    port_range            = "all"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }

  outbound_rule {
    protocol              = "icmp"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }
}

# ── Monitoring server ────────────────────────────────────────

resource "digitalocean_droplet" "monitoring" {
  name      = "whoknows-monitoring"
  region    = var.region
  size      = var.monitoring_droplet_size
  image     = "ubuntu-24-04-x64"
  ssh_keys  = [data.digitalocean_ssh_key.deploy.id]
  user_data = local.deploy_user_data
  tags      = ["whoknows-monitoring"]
}

resource "cloudflare_record" "monitoring_a" {
  zone_id = var.cf_zone_id
  name    = "monitor"
  content = digitalocean_droplet.monitoring.ipv4_address
  type    = "A"
  ttl     = 1
  proxied = true
}

resource "digitalocean_firewall" "monitoring" {
  name        = "whoknows-monitoring-firewall"
  droplet_ids = [digitalocean_droplet.monitoring.id]

  inbound_rule {
    protocol         = "tcp"
    port_range       = "22"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }

  inbound_rule {
    protocol         = "tcp"
    port_range       = "80"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }

  inbound_rule {
    protocol         = "tcp"
    port_range       = "443"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }

  # Loki — modtager logs fra Alloy på app-serveren
  inbound_rule {
    protocol         = "tcp"
    port_range       = "3100"
    source_addresses = ["${digitalocean_droplet.whoknows.ipv4_address}/32"]
  }

  # Prometheus UI
  inbound_rule {
    protocol         = "tcp"
    port_range       = "9090"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }

  outbound_rule {
    protocol              = "tcp"
    port_range            = "all"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }

  outbound_rule {
    protocol              = "udp"
    port_range            = "all"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }

  outbound_rule {
    protocol              = "icmp"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }
}
