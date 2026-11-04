#!/bin/sh
# backup-pg.sh — dump all Quill databases from the managed RDB instance and
# upload them to Scaleway Object Storage (S3), pruning old objects.
#
# Env: PGHOST PGPORT PGUSER PGPASSWORD PGSSLMODE, S3_ENDPOINT, BACKUP_BUCKET,
#      AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, BACKUP_RETENTION_DAYS
set -eu

: "${PGHOST:?PGHOST required}" "${BACKUP_BUCKET:?BACKUP_BUCKET required}"
: "${S3_ENDPOINT:=https://s3.fr-par.scw.cloud}"
: "${BACKUP_RETENTION_DAYS:=30}"
RETENTION_DAYS="$BACKUP_RETENTION_DAYS"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"

DATABASES="quill forgejo zitadel"

for db in $DATABASES; do
  echo "[backup] dumping ${db} at ${STAMP}"
  tmp="/tmp/${db}-${STAMP}.sql.gz"
  PGDATABASE="$db" pg_dump --no-owner --format=plain | gzip -9 > "$tmp"
  aws s3 cp "$tmp" "s3://${BACKUP_BUCKET}/postgres/${db}/${db}-${STAMP}.sql.gz" \
    --endpoint-url "$S3_ENDPOINT"
  rm -f "$tmp"
  echo "[backup] uploaded postgres/${db}/${db}-${STAMP}.sql.gz"
done

# Prune objects older than the retention window (per database prefix).
CUTOFF="$(date -u -d "-${RETENTION_DAYS} days" +%Y%m%dT%H%M%SZ 2>/dev/null || date -u -v-${RETENTION_DAYS}d +%Y%m%dT%H%M%SZ)"
for db in $DATABASES; do
  aws s3 ls "s3://${BACKUP_BUCKET}/postgres/${db}/" --endpoint-url "$S3_ENDPOINT" \
    | awk -v cutoff="$CUTOFF" '$2 < cutoff {print $4}' \
    | while read -r key; do
        [ -n "$key" ] || continue
        echo "[backup] pruning s3://${BACKUP_BUCKET}/${key}"
        aws s3 rm "s3://${BACKUP_BUCKET}/${key}" --endpoint-url "$S3_ENDPOINT"
      done
done

echo "[backup] done"
