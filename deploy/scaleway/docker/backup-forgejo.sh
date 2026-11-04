#!/bin/sh
# backup-forgejo.sh — daily `forgejo dump` of the git data volume, uploaded to
# Object Storage. Runs as a sidecar in the forgejo pod (shares the /data PVC).
#
# Env: S3_ENDPOINT, BACKUP_BUCKET, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY,
#      BACKUP_RETENTION_DAYS, FORGEJO_CONF (default /data/forgejo/conf/app.ini)
set -eu

: "${BACKUP_BUCKET:?BACKUP_BUCKET required}"
: "${S3_ENDPOINT:=https://s3.fr-par.scw.cloud}"
: "${BACKUP_RETENTION_DAYS:=30}"
: "${FORGEJO_CONF:=/data/forgejo/conf/app.ini}"
INTERVAL_SECONDS=86400

# Run once at startup (so a fresh cluster gets an immediate baseline dump),
# then every 24h.
while true; do
  STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
  tmp="/tmp/forgejo-dump-${STAMP}.zip"
  mkdir -p /data/git/repositories 2>/dev/null || true
  echo "[backup] forgejo dump at ${STAMP}"
  if forgejo dump --config "$FORGEJO_CONF" --file "$tmp" --type zip; then
    aws s3 cp "$tmp" "s3://${BACKUP_BUCKET}/forgejo/forgejo-dump-${STAMP}.zip" \
      --endpoint-url "$S3_ENDPOINT"
    rm -f "$tmp"
    echo "[backup] uploaded forgejo/forgejo-dump-${STAMP}.zip"
  else
    echo "[backup] forgejo dump FAILED at ${STAMP}" >&2
    rm -f "$tmp"
  fi

  sleep "$INTERVAL_SECONDS"
done
