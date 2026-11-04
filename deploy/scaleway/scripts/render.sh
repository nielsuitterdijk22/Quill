#!/usr/bin/env bash
# render.sh — render k8s/*.yaml placeholders (${VAR}) into a build dir.
#
# Usage:
#   source deploy/scaleway/scripts/sync-config.sh   # writes k8s/.env.k8s from TF outputs
#   export $(cat deploy/scaleway/k8s/.env.k8s | xargs)   # or let CI set the vars
#   IMAGE_TAG=abc123 REGISTRY=rg.fr-par.scw.cloud/quill \
#     deploy/scaleway/scripts/render.sh [outdir]
#
# Rendered output goes to deploy/scaleway/k8s/rendered/ (git-ignored) by default.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
K8S="$HERE/../k8s"
OUT="${1:-$K8S/rendered}"

: "${QUILL_DOMAIN:?set QUILL_DOMAIN (from sync-config.sh / CI)}"
: "${SCW_REGION:?set SCW_REGION}"
: "${RDB_HOST:?set RDB_HOST (terraform output rdb_private_ip)}"
: "${BACKUP_BUCKET:?set BACKUP_BUCKET}"
: "${REGISTRY:?set REGISTRY (terraform output registry_endpoint)}"
: "${IMAGE_TAG:?set IMAGE_TAG (git sha or semver)}"
: "${BACKUP_RETENTION_DAYS:=30}"
: "${LETSENCRYPT_EMAIL:?set LETSENCRYPT_EMAIL for the ACME account}"

mkdir -p "$OUT"
rm -f "$OUT"/*.yaml

# secret.yaml is intentionally NOT rendered/applied here: its values are secret
# and come from Secret Manager via sync-secrets.sh, never from placeholders.
render_one() {
  local f="$1" base
  base="$(basename "$f")"
  [ "$base" = "secret.yaml" ] && return 0
  if command -v envsubst >/dev/null 2>&1; then
    envsubst < "$f" > "$OUT/$base"
  else
    # Portable fallback (macOS without gettext): perl one-liner.
    perl -pe 's/\$\{(\w+)\}/exists $ENV{$1} ? $ENV{$1} : "\${$1}"/ge' "$f" > "$OUT/$base"
  fi
}

for f in "$K8S"/*.yaml; do
  render_one "$f"
done

echo "Rendered manifests to $OUT"
