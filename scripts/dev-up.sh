#!/usr/bin/env bash
# dev-up.sh — bring up the Quill local dev stack with hot reload.
#
# Steps:
#   1. start Postgres + Forgejo via docker compose and wait until both are ready
#      (these are stateful/slow, so they stay containerised);
#   2. run the three Quill services on the host with hot reload — the API (air),
#      the pipeline dispatcher (air), and the frontend (next dev) — tearing them
#      all down on Ctrl-C.
#
# The API applies its embedded migrations on boot, so there is no separate
# migrate step. Requires Go, Node and Docker on the host.
#
# For a fully containerised stack without hot reload, use `make stack` instead.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

COMPOSE=(docker compose -f deploy/compose/docker-compose.yml)
POSTGRES_USER="${POSTGRES_USER:-quill}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"

# go.mod pins a newer Go than some hosts ship; let the toolchain auto-download.
export GOTOOLCHAIN="${GOTOOLCHAIN:-auto}"

# Point the host processes at the dockerised Postgres + Forgejo and at each other.
export QUILL_DATABASE_URL="${QUILL_DATABASE_URL:-postgres://quill:quill@localhost:${POSTGRES_PORT}/quill?sslmode=disable}"
export QUILL_LOG_FORMAT="${QUILL_LOG_FORMAT:-text}"
export QUILL_JWT_SECRET="${QUILL_JWT_SECRET:-dev-insecure-secret-change-me}"
export QUILL_CORS_ALLOWED_ORIGINS="${QUILL_CORS_ALLOWED_ORIGINS:-http://localhost:3001}"
export FORGEJO_BASE_URL="${FORGEJO_BASE_URL:-http://localhost:3000}"
# Pass through the Forgejo admin token (e.g. exported by .envrc). Most repo/PR
# operations need it; warn rather than fail so UI-only iteration still works.
export FORGEJO_ADMIN_TOKEN="${FORGEJO_ADMIN_TOKEN:-}"
# API <-> dispatcher wiring (host ports).
export QUILL_PIPELINE_DISPATCH_URL="${QUILL_PIPELINE_DISPATCH_URL:-http://localhost:8090}"
export QUILL_PIPELINE_DISPATCH_SECRET="${QUILL_PIPELINE_DISPATCH_SECRET:-dev-dispatch-secret}"
# Frontend server components call the API directly; the browser uses the rewrite.
export QUILL_API_BASE_URL="${QUILL_API_BASE_URL:-http://localhost:8080}"

log() { printf '\033[36m[dev-up]\033[0m %s\n' "$*"; }

pids=()
cleanup() {
  log "shutting down…"
  for pid in "${pids[@]:-}"; do
    if [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null; then
      kill "${pid}" 2>/dev/null || true
    fi
  done
  wait 2>/dev/null || true
}
trap cleanup INT TERM EXIT

# 1. Infrastructure (Postgres + Forgejo) ------------------------------------
log "starting Postgres + Forgejo…"
"${COMPOSE[@]}" up -d postgres forgejo

log "waiting for Postgres to accept connections…"
for _ in $(seq 1 60); do
  if "${COMPOSE[@]}" exec -T postgres pg_isready -U "${POSTGRES_USER}" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if ! "${COMPOSE[@]}" exec -T postgres pg_isready -U "${POSTGRES_USER}" >/dev/null 2>&1; then
  log "Postgres did not become ready in time" >&2
  exit 1
fi
log "Postgres is ready."

log "waiting for Forgejo to answer healthz…"
for _ in $(seq 1 60); do
  if curl -fsS http://localhost:3000/api/healthz >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if ! curl -fsS http://localhost:3000/api/healthz >/dev/null 2>&1; then
  log "Forgejo did not become ready in time" >&2
  exit 1
fi
log "Forgejo is ready."

if [[ -z "${FORGEJO_ADMIN_TOKEN}" ]]; then
  log "warning: FORGEJO_ADMIN_TOKEN is unset — repo/PR operations against Forgejo will fail."
  log "         create one (see deploy/compose/README.md) and export it, e.g. via .envrc."
fi

# 2. Hot-reload Quill services ----------------------------------------------
# Prefer an installed `air`, otherwise run a pinned version via `go run`.
if command -v air >/dev/null 2>&1; then
  AIR=(air)
else
  AIR=(go run github.com/air-verse/air@v1.61.7)
fi

log "starting API (air) on http://localhost:8080 …"
( cd backend && QUILL_HTTP_ADDR=":8080" exec "${AIR[@]}" -c .air.toml ) &
pids+=("$!")

log "starting pipeline dispatcher (air) on http://localhost:8090 …"
( cd backend && QUILL_HTTP_ADDR=":8090" exec "${AIR[@]}" -c .air.dispatch.toml ) &
pids+=("$!")

# Frontend: install deps on first run, then next dev.
if [[ ! -d frontend/node_modules ]]; then
  log "installing frontend dependencies…"
  ( cd frontend && npm install )
fi
log "starting frontend (next dev) on http://localhost:3001 …"
( cd frontend && exec npm run dev ) &
pids+=("$!")

log "stack is up — web http://localhost:3001 · api http://localhost:8080 · forgejo http://localhost:3000 (Ctrl-C to stop)"

# Exit (and trigger cleanup) as soon as any process dies.
wait -n
