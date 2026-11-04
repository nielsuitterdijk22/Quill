#!/usr/bin/env bash
# sync-config.sh — write .env.scaleway from Terraform outputs (local state) with
# environment-variable overrides (CI sets the vars directly from repo vars).
# Non-secret infrastructure coordinates only.
#
# Usage: deploy/scaleway/scripts/sync-config.sh
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
TF_DIR="$HERE/../terraform"
OUT="$HERE/../.env.scaleway"

tf_out() { (cd "$TF_DIR" && terraform output -raw "$1" 2>/dev/null) || true; }
tf_json() { (cd "$TF_DIR" && terraform output -json "$1" 2>/dev/null) || true; }

QUILL_DOMAIN="${QUILL_DOMAIN:-$(tf_json hosts | python3 -c 'import sys,json;print(json.load(sys.stdin)["value"]["app"].split(".",1)[1])' 2>/dev/null || true)}"
if [ -z "${QUILL_DOMAIN:-}" ]; then
  QUILL_DOMAIN="$(grep -E '^domain' "$TF_DIR/terraform.tfvars" 2>/dev/null | head -1 | sed -E 's/.*= *"([^"]+)".*/\1/' || true)"
fi
SCW_REGION="${SCW_REGION:-${SCW_DEFAULT_REGION:-fr-par}}"
SCW_PROJECT_ID="${SCW_PROJECT_ID:-$(tf_out project_id)}"
BACKUP_BUCKET="${BACKUP_BUCKET:-$(tf_out backup_bucket)}"
REGISTRY="${REGISTRY:-$(tf_out registry_endpoint)}"
VM_IP="${VM_IP:-$(tf_out vm_public_ip)}"
LETSENCRYPT_EMAIL="${LETSENCRYPT_EMAIL:-}"

: "${QUILL_DOMAIN:?QUILL_DOMAIN required (terraform.tfvars domain= or env)}"
: "${SCW_PROJECT_ID:?SCW_PROJECT_ID required}"
: "${BACKUP_BUCKET:?BACKUP_BUCKET required}"
: "${REGISTRY:?REGISTRY required}"

cat > "$OUT" <<EOF
QUILL_DOMAIN=$QUILL_DOMAIN
SCW_REGION=$SCW_REGION
SCW_PROJECT_ID=$SCW_PROJECT_ID
BACKUP_BUCKET=$BACKUP_BUCKET
REGISTRY=$REGISTRY
VM_IP=$VM_IP
LETSENCRYPT_EMAIL=$LETSENCRYPT_EMAIL
EOF
chmod 600 "$OUT"
echo "Wrote $OUT"
