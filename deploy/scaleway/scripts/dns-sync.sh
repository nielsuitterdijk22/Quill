#!/usr/bin/env bash
# dns-sync.sh — when the Scaleway DNS zone for $QUILL_DOMAIN is delegated/active,
# upsert the git/ssh A records to the Forgejo VM's public IP (api/app/auth are
# managed by Terraform). When the zone is not active, DNS records live at the
# registrar (e.g. Cloudflare) and there is nothing to sync here.
# Uses the `scw` CLI v2 DNS namespace (`scw dns ...`; the old `scw domain
# zone/record` commands were removed in v2.65). The CI key has DNSFullAccess
# + InstancesReadOnly.
set -euo pipefail

PROJECT_ID="${SCW_PROJECT_ID:?set SCW_PROJECT_ID (terraform output project_id)}"
DOMAIN="${QUILL_DOMAIN:?set QUILL_DOMAIN}"

VM_IP="$(scw instance server list project-id="$PROJECT_ID" -o json | python3 -c '
import sys, json
for s in json.load(sys.stdin):
    if s.get("name") == "quill-forgejo":
        print(s["public_ip"]["address"]); break
')"
[ -n "$VM_IP" ] || { echo "quill-forgejo VM not found / no IP yet" >&2; exit 1; }
echo "VM IP: $VM_IP"

ZONE_STATUS="$(scw dns zone list project-id="$PROJECT_ID" domain="$DOMAIN" -o json | python3 -c '
import sys, json
zones = json.load(sys.stdin)
print(zones[0]["status"] if zones else "")
')"
if [ "$ZONE_STATUS" != "active" ]; then
  echo "Scaleway zone $DOMAIN is not active (status: ${ZONE_STATUS:-absent}) — records are managed at the registrar; nothing to sync."
  exit 0
fi

upsert() {
  local name="$1" ip="$2"
  # `dns record set` is add-or-replace for the (zone, name, type) triple.
  scw dns record set "$DOMAIN" name="$name" type=A "values.0=$ip" ttl=300 >/dev/null
  echo "A ${name}.${DOMAIN} -> ${ip}"
}

upsert git "$VM_IP"
upsert ssh "$VM_IP"
echo "DNS records synced."
