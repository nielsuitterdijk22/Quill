#!/usr/bin/env bash
# sync-secrets.sh — render k8s/secret.yaml from Scaleway Secret Manager and
# apply it to the cluster as the `quill-app-secrets` Secret.
#
# Requires: scw CLI authenticated (SCW_ACCESS_KEY/SCW_SECRET_KEY with
# SecretManagerReadOnly), kubectl configured for the Kapsule cluster.
# Env overrides: set any of the QUILL_* / AWS_* vars to skip the Secret Manager
# lookup for that key (useful in CI where values come from GitHub secrets).
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
TF_DIR="$HERE/../terraform"
K8S="$HERE/../k8s"
NS="${KUBE_NAMESPACE:-quill}"
PROJECT_ID="${SCW_PROJECT_ID:-$(cd "$TF_DIR" && terraform output -raw project_id 2>/dev/null || true)}"
REGION="${SCW_DEFAULT_REGION:-fr-par}"

sm() { # sm <path> <name> — read latest secret version payload
  local path="$1" name="$2"
  scw secret version get "$(scw secret secret list path="$path" name="$name" project-id="$PROJECT_ID" region="$REGION" -o json 2>/dev/null | python3 -c 'import sys,json;print(json.load(sys.stdin)[0]["id"])')" region="$REGION" -o json 2>/dev/null \
    | python3 -c 'import sys,json,base64;print(base64.b64decode(json.load(sys.stdin)["data"]).decode())'
}

get() { # get <env-name> <sm-path> <sm-name>
  local env_name="$1" path="$2" name="$3"
  local v="${!env_name:-}"
  if [ -z "$v" ] && [ -n "$PROJECT_ID" ]; then
    v="$(sm "$path" "$name" || true)"
  fi
  printf '%s' "$v"
}

export QUILL_JWT_SECRET="${QUILL_JWT_SECRET:-$(get QUILL_JWT_SECRET /quill/app jwt-secret)}"
export QUILL_SECRET_ENCRYPTION_KEY="${QUILL_SECRET_ENCRYPTION_KEY:-$(get QUILL_SECRET_ENCRYPTION_KEY /quill/app secret-encryption-key)}"
export QUILL_WEBHOOK_SECRET="${QUILL_WEBHOOK_SECRET:-$(get QUILL_WEBHOOK_SECRET /quill/app webhook-secret)}"
export QUILL_PIPELINE_DISPATCH_SECRET="${QUILL_PIPELINE_DISPATCH_SECRET:-$(get QUILL_PIPELINE_DISPATCH_SECRET /quill/app dispatch-secret)}"
export FORGEJO_ADMIN_TOKEN="${FORGEJO_ADMIN_TOKEN:-$(get FORGEJO_ADMIN_TOKEN /quill/app forgejo-admin-token)}"
export QUILL_DATABASE_URL="${QUILL_DATABASE_URL:-$(get QUILL_DATABASE_URL /quill/app quill-database-url)}"
export RDB_ADMIN_PASSWORD="${RDB_ADMIN_PASSWORD:-$(get RDB_ADMIN_PASSWORD /quill/rdb rdb-admin-password)}"
export ZITADEL_DB_PASSWORD="${ZITADEL_DB_PASSWORD:-$(get ZITADEL_DB_PASSWORD /quill/rdb zitadel-db-password)}"
export ZITADEL_MASTERKEY="${ZITADEL_MASTERKEY:-$(get ZITADEL_MASTERKEY /quill/zitadel zitadel-masterkey)}"
export ZITADEL_MANAGEMENT_TOKEN="${ZITADEL_MANAGEMENT_TOKEN:-}"
export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-$(get AWS_ACCESS_KEY_ID /quill/backup s3-access-key)}"
export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-$(get AWS_SECRET_ACCESS_KEY /quill/backup s3-secret-key)}"
export AUTH_SECRET="${AUTH_SECRET:-$QUILL_JWT_SECRET}"
export GITHUB_CLIENT_ID="${GITHUB_CLIENT_ID:-}"
export GITHUB_CLIENT_SECRET="${GITHUB_CLIENT_SECRET:-}"

: "${QUILL_JWT_SECRET:?could not resolve QUILL_JWT_SECRET}"
: "${QUILL_DATABASE_URL:?could not resolve QUILL_DATABASE_URL}"
: "${RDB_ADMIN_PASSWORD:?could not resolve RDB_ADMIN_PASSWORD}"
: "${AWS_ACCESS_KEY_ID:?could not resolve AWS_ACCESS_KEY_ID}"

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
if command -v envsubst >/dev/null 2>&1; then
  envsubst < "$K8S/secret.yaml" > "$tmp"
else
  perl -pe 's/\$\{(\w+)\}/exists $ENV{$1} ? $ENV{$1} : "\${$1}"/ge' "$K8S/secret.yaml" > "$tmp"
fi
kubectl apply -f "$tmp"
echo "Applied quill-app-secrets to namespace $NS"
