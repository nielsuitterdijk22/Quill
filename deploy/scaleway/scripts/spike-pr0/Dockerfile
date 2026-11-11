# PR-0 spike probe image: cross-compiled static binary + git, amd64 for Scaleway.
# Build context: deploy/scaleway/scripts/spike-pr0 (single-binary module, stdlib only).
FROM --platform=linux/amd64 alpine:3.20
RUN apk add --no-cache git
COPY spike-probe /probe
ENTRYPOINT ["/probe"]
