#!/usr/bin/env bash
# sync-config.sh — write k8s/.env.k8s from Terraform outputs (local state) with
# environment-variable overrides (CI sets the vars directly instead of running
# this). The values are non-secret infrastructure coordinates.
#
# Usage: deploy/scaleway/scripts/sync-config.sh
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
TF_DIR="$HERE/../terraform"
OUT="$HERE/../k8s/.env.k8s"

tf_out() { (cd "$TF_DIR" && terraform output -raw "$1" 2>/dev/null) || true; }

QUILL_DOMAIN="${QUILL_DOMAIN:-$(tf_out domain 2>/dev/null || true)}"
# domain is a variable, not an output — read it from tfvars when terraform is absent.
if [ -z "${QUILL_DOMAIN:-}" ]; then
  QUILL_DOMAIN="$(grep -E '^domain' "$TF_DIR/terraform.tfvars" 2>/dev/null | head -1 | sed -E 's/.*= *"([^"]+)".*/\1/' || true)"
fi
SCW_REGION="${SCW_REGION:-${SCW_DEFAULT_REGION:-fr-par}}"
RDB_HOST="${RDB_HOST:-$(tf_out rdb_private_ip)}"
BACKUP_BUCKET="${BACKUP_BUCKET:-$(tf_out backup_bucket)}"
REGISTRY="${REGISTRY:-$(tf_out registry_endpoint)}"
BACKUP_RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-30}"
LETSENCRYPT_EMAIL="${LETSENCRYPT_EMAIL:-}"

: "${QUILL_DOMAIN:?QUILL_DOMAIN required (terraform.tfvars domain= or env)}"
: "${RDB_HOST:?RDB_HOST required (terraform output rdb_private_ip)}"
: "${BACKUP_BUCKET:?BACKUP_BUCKET required}"
: "${REGISTRY:?REGISTRY required (terraform output registry_endpoint)}"
: "${LETSENCRYPT_EMAIL:?LETSENCRYPT_EMAIL required (ACME account email)}"

cat > "$OUT" <<EOF
QUILL_DOMAIN=$QUILL_DOMAIN
SCW_REGION=$SCW_REGION
RDB_HOST=$RDB_HOST
BACKUP_BUCKET=$BACKUP_BUCKET
REGISTRY=$REGISTRY
BACKUP_RETENTION_DAYS=$BACKUP_RETENTION_DAYS
LETSENCRYPT_EMAIL=$LETSENCRYPT_EMAIL
EOF
chmod 600 "$OUT"
echo "Wrote $OUT"
