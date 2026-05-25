#!/bin/bash
# Kør fra server_go-mappen: bash deploy/spinup.sh [app|monitoring|all]
set -euo pipefail

MODE="${1:-all}"

if [[ "$MODE" != "app" && "$MODE" != "monitoring" && "$MODE" != "all" ]]; then
  echo "Brug: bash deploy/spinup.sh [app|monitoring|all]"
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TERRAFORM_DIR="$SCRIPT_DIR/terraform"
ANSIBLE_DIR="$SCRIPT_DIR/ansible"
INVENTORY="$ANSIBLE_DIR/inventory.yml"

# --- Terraform ---
echo "==> Initialiserer Terraform..."
cd "$TERRAFORM_DIR"

if [ ! -f terraform.tfvars ]; then
  echo "FEJL: $TERRAFORM_DIR/terraform.tfvars mangler."
  echo "Kopiér terraform.tfvars.example og udfyld værdierne."
  exit 1
fi

terraform init -upgrade

case "$MODE" in
  app)
    terraform apply -auto-approve \
      -target=digitalocean_droplet.whoknows \
      -target=digitalocean_firewall.whoknows \
      -target=cloudflare_record.whoknows_a
    APP_IP=$(terraform output -raw droplet_ip)
    echo "==> App-server IP: $APP_IP"
    sed -i.bak "/whoknows-vm/{n;s/ansible_host: .*/ansible_host: $APP_IP/}" "$INVENTORY"
    sed -i.bak "s/server_ip: .*/server_ip: \"$APP_IP\"/" "$INVENTORY"
    rm -f "$INVENTORY.bak"
    ;;
  monitoring)
    terraform apply -auto-approve \
      -target=digitalocean_droplet.monitoring \
      -target=digitalocean_firewall.monitoring \
      -target=cloudflare_record.monitoring_a
    MONITORING_IP=$(terraform output -raw monitoring_ip)
    echo "==> Monitoring-server IP: $MONITORING_IP"
    sed -i.bak "/monitoring-vm/{n;s/ansible_host: .*/ansible_host: $MONITORING_IP/}" "$INVENTORY"
    rm -f "$INVENTORY.bak"
    ;;
  all)
    terraform apply -auto-approve
    APP_IP=$(terraform output -raw droplet_ip)
    MONITORING_IP=$(terraform output -raw monitoring_ip)
    echo "==> App-server IP:        $APP_IP"
    echo "==> Monitoring-server IP: $MONITORING_IP"
    sed -i.bak "/whoknows-vm/{n;s/ansible_host: .*/ansible_host: $APP_IP/}" "$INVENTORY"
    sed -i.bak "/monitoring-vm/{n;s/ansible_host: .*/ansible_host: $MONITORING_IP/}" "$INVENTORY"
    sed -i.bak "s/server_ip: .*/server_ip: \"$APP_IP\"/" "$INVENTORY"
    rm -f "$INVENTORY.bak"
    ;;
esac

# --- Vent på at cloud-init er klar ---
echo "==> Venter 60 sekunder på at server(e) er klar..."
sleep 60

# --- Ansible ---
echo "==> Kører Ansible playbook..."
cd "$ANSIBLE_DIR/.."

case "$MODE" in
  app)
    ansible-playbook deploy/ansible/playbook.yml --limit production
    echo ""
    echo "==> Færdig! App:        https://huw.dk ($APP_IP)"
    ;;
  monitoring)
    ansible-playbook deploy/ansible/playbook.yml --limit monitoring
    echo ""
    echo "==> Færdig! Monitoring: https://monitor.huw.dk ($MONITORING_IP)"
    ;;
  all)
    ansible-playbook deploy/ansible/playbook.yml
    echo ""
    echo "==> Færdig!"
    echo "    App:        https://huw.dk         ($APP_IP)"
    echo "    Monitoring: https://monitor.huw.dk ($MONITORING_IP)"
    ;;
esac

echo "    DNS er automatisk opdateret af Terraform (Cloudflare)."
