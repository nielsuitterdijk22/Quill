#!/bin/sh
# backup-pg.sh — dump every Quill database (connection URIs in *_DATABASE_URL
# env vars) and upload to Scaleway Object Storage (S3), pruning old objects.
# Runs as a Serverless Job (cron) — see terraform/serverless.tf.
#
# Env: QUILL_DATABASE_URL, FORGEJO_DATABASE_URL,
#      S3_ENDPOINT, BACKUP_BUCKET, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY,
#      BACKUP_RETENTION_DAYS
set -eu

: "${BACKUP_BUCKET:?BACKUP_BUCKET required}" "${QUILL_DATABASE_URL:?QUILL_DATABASE_URL required}"
: "${S3_ENDPOINT:=https://s3.fr-par.scw.cloud}"
: "${BACKUP_RETENTION_DAYS:=30}"
RETENTION_DAYS="$BACKUP_RETENTION_DAYS"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"

dump_one() { # dump_one <name> <postgres-uri>
  name="$1"; url="$2"
  echo "[backup] dumping ${name} at ${STAMP}"
  tmp="/tmp/${name}-${STAMP}.sql.gz"
  pg_dump "$url" --no-owner | gzip -9 > "$tmp"
  aws s3 cp "$tmp" "s3://${BACKUP_BUCKET}/postgres/${name}/${name}-${STAMP}.sql.gz" \
    --endpoint-url "$S3_ENDPOINT"
  rm -f "$tmp"
  echo "[backup] uploaded postgres/${name}/${name}-${STAMP}.sql.gz"
}

dump_one quill "$QUILL_DATABASE_URL"
dump_one forgejo "$FORGEJO_DATABASE_URL"


echo "[backup] done (retention via bucket lifecycle rule)"
