#!/usr/bin/env bash
# k8s-provision.sh — first-boot provisioning on the Kapsule cluster:
#   1. wait for Forgejo to be healthy
#   2. create the quill-admin Forgejo user (idempotent)
#   3. mint a Forgejo admin token
#   4. write the token into the quill-app-secrets Secret AND Scaleway Secret
#      Manager (/quill/app/forgejo-admin-token)
#   5. restart the api deployment so it picks up the token
#
# Requires: kubectl configured for the cluster; scw CLI authenticated when
# STORE_IN_SECRET_MANAGER=1 (default) to keep Secret Manager in sync.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
NS="${KUBE_NAMESPACE:-quill}"
ADMIN_USER="${FORGEJO_ADMIN_USERNAME:-quill-admin}"
ADMIN_PASS="${FORGEJO_ADMIN_PASSWORD:-$(openssl rand -hex 16)}"
ADMIN_EMAIL="${FORGEJO_ADMIN_EMAIL:-admin@${QUILL_DOMAIN:-localhost}}"
STORE_IN_SECRET_MANAGER="${STORE_IN_SECRET_MANAGER:-1}"
REGION="${SCW_DEFAULT_REGION:-fr-par}"
PROJECT_ID="${SCW_PROJECT_ID:-$(cd "$HERE/../terraform" && terraform output -raw project_id 2>/dev/null || true)}"

echo "==> waiting for forgejo"
kubectl -n "$NS" wait --for=condition=ready pod -l app=forgejo --timeout=600s

echo "==> creating Forgejo admin user '${ADMIN_USER}' (idempotent)"
kubectl -n "$NS" exec deploy/forgejo -c forgejo -- \
  forgejo admin user create \
    --admin \
    --username "$ADMIN_USER" \
    --password "$ADMIN_PASS" \
    --email "$ADMIN_EMAIL" \
    --must-change-password=false 2>&1 | grep -v "already exists" || true

echo "==> minting Forgejo admin token"
# Forgejo is only reachable in-cluster; proxy through the pod.
TOKEN_JSON="$(kubectl -n "$NS" exec deploy/forgejo -c forgejo -- sh -c \
  "curl -sf -u '${ADMIN_USER}:${ADMIN_PASS}' -H 'Content-Type: application/json' \
     -d '{\"name\":\"quill-api-k8s\",\"scopes\":[\"all\"]}' \
     http://localhost:3000/api/v1/users/${ADMIN_USER}/tokens")"
TOKEN="$(printf '%s' "$TOKEN_JSON" | python3 -c 'import sys,json;print(json.load(sys.stdin)["sha1"])')"
[ -n "$TOKEN" ] || { echo "failed to parse token from: $TOKEN_JSON" >&2; exit 1; }

echo "==> updating quill-app-secrets Secret"
kubectl -n "$NS" patch secret quill-app-secrets --type merge \
  -p "{\"stringData\":{\"FORGEJO_ADMIN_TOKEN\":\"${TOKEN}\"}}"

if [ "$STORE_IN_SECRET_MANAGER" = "1" ] && command -v scw >/dev/null 2>&1 && [ -n "$PROJECT_ID" ]; then
  echo "==> updating Secret Manager /quill/app/forgejo-admin-token"
  SECRET_ID="$(scw secret secret list path=/quill/app name=forgejo-admin-token project-id="$PROJECT_ID" region="$REGION" -o json | python3 -c 'import sys,json;print(json.load(sys.stdin)[0]["id"])')"
  printf '%s' "$TOKEN" | base64 | scw secret version create secret-id="$SECRET_ID" region="$REGION" data=- description="minted by k8s-provision.sh" >/dev/null
fi

echo "==> restarting api to pick up the token"
kubectl -n "$NS" rollout restart deploy/api
kubectl -n "$NS" rollout status deploy/api --timeout=300s

echo ""
echo "Provisioned. Admin user: ${ADMIN_USER}  password: ${ADMIN_PASS}"
echo "(password is random unless FORGEJO_ADMIN_PASSWORD was set — store it in a password manager)"
