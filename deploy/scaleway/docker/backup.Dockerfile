# syntax=docker/dockerfile:1
# Backup image: Forgejo CLI (for `forgejo dump`) + Postgres client (pg_dump) +
# AWS CLI (S3 upload to Scaleway Object Storage). Used by the pg-backup
# CronJob and the forgejo backup sidecar.
#
# The base is OUR registry mirror of codeberg.org/forgejo/forgejo:15
# (quill/forgejo-mirror:15, pushed 2026-11-12 from the VM's cached image):
# Codeberg's registry 403s GitHub runner IPs on HEAD requests, which broke
# the "Build backup image" step on every deploy. Require the forgejo major
# to track the compose stack's forgejo:15.
FROM rg.fr-par.scw.cloud/quill/forgejo-mirror:15

USER root
RUN if command -v apt-get >/dev/null 2>&1; then \
      apt-get update && \
      apt-get install -y --no-install-recommends \
        ca-certificates curl awscli postgresql-client && \
      rm -rf /var/lib/apt/lists/*; \
    else \
      apk add --no-cache ca-certificates curl aws-cli postgresql16-client; \
    fi

COPY backup-pg.sh /usr/local/bin/backup-pg.sh
COPY backup-forgejo.sh /usr/local/bin/backup-forgejo.sh
RUN chmod 755 /usr/local/bin/backup-pg.sh /usr/local/bin/backup-forgejo.sh

ENTRYPOINT []
USER 1000:1000
