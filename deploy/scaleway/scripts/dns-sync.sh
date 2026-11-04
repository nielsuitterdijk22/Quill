#!/usr/bin/env bash
# dns-sync.sh — read the Forgejo VM's public IP and upsert the git/ssh A records
# in the Scaleway DNS zone (api/app/auth CNAMEs are managed by Terraform).
# Uses the `scw` CLI (the CI key has DNSFullAccess + InstancesReadOnly).
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
TF_DIR="$HERE/../terraform"
REGION="${SCW_DEFAULT_REGION:-fr-par}"
PROJECT_ID="${SCW_PROJECT_ID:?set SCW_PROJECT_ID (terraform output project_id)}"
DOMAIN="${QUILL_DOMAIN:?set QUILL_DOMAIN}"

VM_IP="$(scw instance server list region="$REGION" project-id="$PROJECT_ID" -o json | python3 -c '
import sys, json
for s in json.load(sys.stdin):
    if s.get("name") == "quill-forgejo":
        print(s["public_ip"]["address"]); break
')"
[ -n "$VM_IP" ] || { echo "quill-forgejo VM not found / no IP yet" >&2; exit 1; }
echo "VM IP: $VM_IP"

ZONE_ID="$(scw domain zone list project-id="$PROJECT_ID" -o json | python3 -c '
import sys, json
domain = "'"$DOMAIN"'"
root = ".".join(domain.split(".")[-2:])
sub = ".".join(domain.split(".")[:-2])
for z in json.load(sys.stdin):
    if z.get("domain") == root and z.get("subdomain") == sub:
        print(z["id"]); break
')"
[ -n "$ZONE_ID" ] || { echo "DNS zone for $DOMAIN not found" >&2; exit 1; }

upsert() {
  local name="$1" ip="$2" existing
  existing="$(scw domain record list dns-zone="$ZONE_ID" -o json | python3 -c '
import sys, json
name = "'"$name"'"
for r in json.load(sys.stdin):
    if r.get("name") == name and r.get("type") == "A":
        print(r["id"]); break
')"
  if [ -n "$existing" ]; then
    scw domain record update "$existing" data="$ip" ttl=300 >/dev/null
  else
    scw domain record create dns-zone="$ZONE_ID" name="$name" type=A data="$ip" ttl=300 >/dev/null
  fi
  echo "A ${name}.${DOMAIN} -> ${ip}"
}

upsert git "$VM_IP"
upsert ssh "$VM_IP"
echo "DNS records synced."
