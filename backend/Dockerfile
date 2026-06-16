# syntax=docker/dockerfile:1

# ---- build ----
FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/quill-api ./cmd/api

# ---- run ----
FROM alpine:3.22
RUN apk add --no-cache ca-certificates git
RUN adduser -D -H -u 10001 quill
COPY --from=build /out/quill-api /quill-api
EXPOSE 8080
USER quill:quill
ENTRYPOINT ["/quill-api"]
