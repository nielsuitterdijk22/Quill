# syntax=docker/dockerfile:1

# ---- build ----
FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/quill-api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/quill-dispatch ./cmd/dispatch

# ---- run ----
FROM alpine:3.22
RUN apk add --no-cache ca-certificates git
# No -H: create the home directory. The pipeline dispatcher runs act from this
# image, and act resolves caches/tooling under HOME; a missing home made every
# `uses:` step fail silently (run aborted with all steps "skipped").
RUN adduser -D -u 10001 quill
COPY --from=build /out/quill-api /quill-api
COPY --from=build /out/quill-dispatch /quill-dispatch
EXPOSE 8080
USER quill:quill
CMD ["/quill-api"]
