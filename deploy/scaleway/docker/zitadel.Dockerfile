# syntax=docker/dockerfile:1
# Zitadel image + the FirstInstance bootstrap steps baked in, and an /output
# dir the non-root container user can write the machine PAT to (the container
# wrapper cats it to the logs on first boot — see terraform/serverless.tf).
FROM ghcr.io/zitadel/zitadel:v2.65.1

USER root
COPY steps.yaml /steps.yaml
RUN mkdir -p /output && chmod 0777 /output && chown 1000:1000 /output

USER 1000:1000
