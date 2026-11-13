# syntax=docker/dockerfile:1

# ---- build ----
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/quill-api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/quill-dispatch ./cmd/dispatch && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/quill-broker ./cmd/broker && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/quill-ciworker ./cmd/ciworker && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/quill ./cmd/quill

# ---- syft (SBOM generation on default-branch pushes) ----
# Pinned: the SBOM format/content must not drift with an upstream "latest".
FROM anchore/syft:v1.18.1 AS syft

# ---- run ----
FROM alpine:3.22
RUN apk add --no-cache ca-certificates git
# No -H: create the home directory. The pipeline dispatcher runs act from this
# image, and act resolves caches/tooling under HOME; a missing home made every
# `uses:` step fail silently (run aborted with all steps "skipped").
RUN adduser -D -u 10001 quill
COPY --from=build /out/quill-api /quill-api
COPY --from=build /out/quill-dispatch /quill-dispatch
COPY --from=build /out/quill-broker /quill-broker
COPY --from=build /out/quill-ciworker /quill-ciworker
COPY --from=build /out/quill /quill
COPY --from=syft /syft /usr/local/bin/syft
EXPOSE 8080
USER quill:quill
CMD ["/quill-api"]
