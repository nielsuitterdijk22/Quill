#!/usr/bin/env bash
# dns-sync.sh — read the actual LB IPs from the cluster and upsert the DNS A
# records in the Scaleway DNS zone (app/git/auth → ingress LB, ssh → forgejo
# SSH LB). Uses the `scw` CLI (DNSFullAccess needed; the CI key has it).
#
# Alternatively, set terraform vars ingress_lb_ip/ssh_lb_ip and re-apply —
# this script just automates discovering those values.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
NS="${KUBE_NAMESPACE:-quill}"
REGION="${SCW_DEFAULT_REGION:-fr-par}"
PROJECT_ID="${SCW_PROJECT_ID:?set SCW_PROJECT_ID (terraform output project_id)}"
DOMAIN="${QUILL_DOMAIN:?set QUILL_DOMAIN}"

INGRESS_IP="$(kubectl -n kube-system get svc traefik -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || true)"
SSH_IP="$(kubectl -n "$NS" get svc forgejo-ssh -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || true)"
echo "ingress LB IP: ${INGRESS_IP:-<pending>}   ssh LB IP: ${SSH_IP:-<pending>}"
[ -n "$INGRESS_IP" ] || { echo "ingress LB has no IP yet — is Traefik up?" >&2; exit 1; }

ZONE_ID="$(scw domain zone list project-id="$PROJECT_ID" -o json | python3 -c '
import sys, json
d = json.load(sys.stdin)
domain = "'"$DOMAIN"'"
root = ".".join(domain.split(".")[-2:])
sub = ".".join(domain.split(".")[:-2])
for z in d:
    if z.get("domain") == root and z.get("subdomain") == sub:
        print(z["id"]); break
')"
[ -n "$ZONE_ID" ] || { echo "DNS zone for $DOMAIN not found in project $PROJECT_ID" >&2; exit 1; }

upsert() { # upsert <name> <ip>
  local name="$1" ip="$2"
  [ -n "$ip" ] || return 0
  local existing
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

upsert app "$INGRESS_IP"
upsert git "$INGRESS_IP"
upsert auth "$INGRESS_IP"
upsert ssh "$SSH_IP"

echo "DNS records synced."
