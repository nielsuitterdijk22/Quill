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
ENTRYPOINT ["/bin/sh", "-c"]
CMD ["/app/zitadel start-from-init --masterkey \"$ZITADEL_MASTERKEY\" --tlsMode external --steps /steps.yaml & ZPID=$!; for i in $(seq 1 90); do if [ -f /output/quill-api.pat ]; then echo \"ZITADEL_MACHINE_PAT=$(cat /output/quill-api.pat)\"; break; fi; sleep 2; done; wait $ZPID"]
