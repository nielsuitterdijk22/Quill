#!/usr/bin/env bash
# provision.sh — first-boot provisioning after `terraform apply`:
#   1. SSH to the Forgejo VM: create the quill-admin user (idempotent)
#   2. mint a Forgejo admin token
#   3. register the system-wide webhook: Forgejo -> https://api.<domain>/api/v1/webhooks/forgejo
#      (HMAC secret = /quill/app/webhook-secret in Secret Manager)
#   4. harvest the Zitadel machine PAT from the zitadel container logs
#   5. write both tokens to Secret Manager and print the terraform re-apply
#      command so the api container picks them up
#
# Requires: ssh, scw CLI (SCW_CI_* env), terraform state in ../terraform.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
TF_DIR="$HERE/../terraform"

DOMAIN="${QUILL_DOMAIN:-$(cd "$TF_DIR" && terraform output -raw hosts 2>/dev/null | python3 -c 'import sys,json;print(json.load(sys.stdin)["app"].split(".",1)[1])' 2>/dev/null || true)}"
: "${DOMAIN:?set QUILL_DOMAIN or run from a dir with terraform state}"

VM_IP="$(cd "$TF_DIR" && terraform output -raw vm_public_ip)"
SSH_KEY="$(cd "$TF_DIR" && terraform output -raw vm_ssh_private_key)"
PROJECT_ID="$(cd "$TF_DIR" && terraform output -raw project_id)"
ADMIN_USER="${FORGEJO_ADMIN_USERNAME:-quill-admin}"
ADMIN_PASS="${FORGEJO_ADMIN_PASSWORD:-$(openssl rand -hex 16)}"
ADMIN_EMAIL="${FORGEJO_ADMIN_EMAIL:-admin@${DOMAIN}}"

keyfile="$(mktemp)"
trap 'rm -f "$keyfile"' EXIT
printf '%s' "$SSH_KEY" > "$keyfile"
chmod 600 "$keyfile"
SSH="ssh -i $keyfile -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=/dev/null ubuntu@$VM_IP"

echo "==> waiting for the VM stack (forgejo healthy)"
for i in $(seq 1 60); do
  if $SSH "docker compose -f /opt/quill/docker-compose.yml ps --format json" 2>/dev/null | grep -q forgejo; then
    if $SSH "curl -fsS http://localhost:3000/api/healthz" >/dev/null 2>&1; then break; fi
  fi
  [ "$i" -eq 60 ] && { echo "forgejo not healthy on VM" >&2; exit 1; }
  sleep 5
done

echo "==> creating Forgejo admin user '${ADMIN_USER}' (idempotent)"
$SSH "docker compose -f /opt/quill/docker-compose.yml exec -T -u 1000 forgejo forgejo admin user create --admin --username '$ADMIN_USER' --password '$ADMIN_PASS' --email '$ADMIN_EMAIL' --must-change-password=false" 2>&1 | grep -v "already exists" || true

echo "==> minting Forgejo admin token"
TOKEN="$($SSH "curl -sf -u '$ADMIN_USER:$ADMIN_PASS' -H 'Content-Type: application/json' -d '{\"name\":\"quill-api\",\"scopes\":[\"all\"]}' http://localhost:3000/api/v1/users/$ADMIN_USER/tokens" | python3 -c 'import sys,json;print(json.load(sys.stdin)["sha1"])')"
[ -n "$TOKEN" ] || { echo "failed to mint token" >&2; exit 1; }

echo "==> registering Forgejo system webhook -> https://api.${DOMAIN}/api/v1/webhooks/forgejo"
WEBHOOK_SECRET="$(scw secret version get "$(scw secret secret list path=/quill/app name=app-webhook-secret project-id=$PROJECT_ID -o json | python3 -c 'import sys,json;print(json.load(sys.stdin)[0]["id"])')" -o json | python3 -c 'import sys,json,base64;print(base64.b64decode(json.load(sys.stdin)["data"]).decode())')"
$SSH "curl -sf -u '$ADMIN_USER:$TOKEN' -H 'Content-Type: application/json' -d '{\"type\":\"forgejo\",\"active\":true,\"events\":[\"push\",\"pull_request\"],\"config\":{\"url\":\"https://api.${DOMAIN}/api/v1/webhooks/forgejo\",\"content_type\":\"json\",\"secret\":\"${WEBHOOK_SECRET}\"}}' http://localhost:3000/api/v1/admin/hooks" >/dev/null || echo "  (webhook may already exist — continuing)"

echo "==> harvesting Zitadel machine PAT from container logs"
ZITADEL_CONTAINER="$(scw container container list namespace-id="$(cd "$TF_DIR" && terraform output -raw registry_endpoint >/dev/null 2>&1; scw container namespace list project-id=$PROJECT_ID -o json | python3 -c 'import sys,json;print([n["id"] for n in json.load(sys.stdin) if n["name"]=="quill"][0])')" -o json | python3 -c 'import sys,json;print([c["id"] for c in json.load(sys.stdin) if c["name"]=="zitadel"][0])')"
ZPAT="$(scw container logs "$ZITADEL_CONTAINER" 2>/dev/null | grep -o 'ZITADEL_MACHINE_PAT=.*' | head -1 | cut -d= -f2 || true)"

echo "==> writing tokens to Secret Manager"
sm_set() { # sm_set <path> <name> <value>
  local sid
  sid="$(scw secret secret list path="$1" name="$2" project-id=$PROJECT_ID -o json | python3 -c 'import sys,json;print(json.load(sys.stdin)[0]["id"])')"
  printf '%s' "$3" | base64 | scw secret version create secret-id="$sid" data=- description="set by provision.sh" >/dev/null
}
sm_set /quill/app app-forgejo-admin-token "$TOKEN"
[ -n "$ZPAT" ] && sm_set /quill/app app-zitadel-management-token "$ZPAT"

echo ""
echo "Provisioned. Admin user: ${ADMIN_USER}  password: ${ADMIN_PASS}  (store it — random unless set via env)"
echo ""
echo "Now re-apply Terraform so the api container gets the tokens:"
echo "  cd deploy/scaleway/terraform"
echo "  terraform apply -var forgejo_admin_token='$TOKEN'${ZPAT:+ -var zitadel_management_token='$ZPAT'}"
echo ""
echo "Zitadel console: https://auth.${DOMAIN} (admin / password from steps.yaml, forced change on first login)."
echo "Create the OIDC app there, then set repo vars NEXT_PUBLIC_ZITADEL_CLIENT_ID / NEXT_PUBLIC_ZITADEL_PROJECT_ID and re-run the deploy workflow to rebuild the web image."
