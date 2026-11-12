# syntax=docker/dockerfile:1
# Zitadel's official image is distroless (no shell). We rebuild on alpine with
# the static zitadel binary copied in, and a wrapper CMD that starts zitadel
# and prints the FirstInstance machine PAT to the container logs on first boot
# (harvested by scripts/provision.sh). Masterkey + DB password come from env
# (ZITADEL_MASTERKEY / ZITADEL_DATABASE_POSTGRES_*), set by Terraform.
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 1000 -h /home/zitadel zitadel
COPY --from=ghcr.io/zitadel/zitadel:v2.65.1 /app/zitadel /app/zitadel
COPY steps.yaml /steps.yaml
RUN mkdir -p /output /home/zitadel && chmod 0777 /output && chown -R zitadel:zitadel /home/zitadel
USER zitadel
# Run mode ONLY (serverless epic, C1): plain `zitadel start`. Migrations and
# first-instance init never run in the container boot path — `zitadel setup`
# runs as a Serverless Job (terraform job_definition zitadel-setup; CI starts
# it after each apply). The original start-from-init wrapper (instance
# creation + PAT harvest via /output) was a bootstrap-only convenience: the
# instance data now migrates with the database and the machine PAT lives in
# Secret Manager. The wrapper shell form stays (ENTRYPOINT is /bin/sh -c) so
# env expansion works on the serverless runtime.
ENTRYPOINT ["/bin/sh", "-c"]
CMD ["exec /app/zitadel start --masterkey \"$ZITADEL_MASTERKEY\" --tlsMode external"]
