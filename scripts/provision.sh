#!/usr/bin/env bash
# provision.sh — bootstrap Forgejo on first boot.
#
# Creates the Forgejo admin user and mints an admin token, then writes
# FORGEJO_ADMIN_TOKEN into deploy/compose/.env so the api service
# can use it immediately (or after `make stack` to restart with the token).
#
# Usage:
#   make provision           # from the repo root
#   ./scripts/provision.sh   # directly
#
# The script is idempotent: re-running it after the user already exists
# will skip creation and only refresh the token in .env.

set -euo pipefail

COMPOSE="docker compose -f deploy/compose/docker-compose.yml"
ENV_FILE="deploy/compose/.env"
ADMIN_USER="${FORGEJO_ADMIN_USERNAME:-quill-admin}"
ADMIN_PASS="${FORGEJO_ADMIN_PASSWORD:-$(openssl rand -hex 16)}"
ADMIN_EMAIL="${FORGEJO_ADMIN_EMAIL:-admin@quill.local}"
TOKEN_NAME="quill-api-$(date +%s)"

# ---- helpers ----------------------------------------------------------------

log()  { echo "  [provision] $*"; }
die()  { echo "  [provision] ERROR: $*" >&2; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "Required command not found: $1"
}

# ---- preflight --------------------------------------------------------------

require_cmd docker
require_cmd curl

# Ensure the stack is up.
if ! $COMPOSE ps --format json 2>/dev/null | grep -q '"Service"'; then
  die "Stack is not running. Run 'make stack' first."
fi

# Wait until Forgejo is healthy.
log "Waiting for Forgejo to be healthy…"
for i in $(seq 1 30); do
  if curl -fsS http://localhost:3000/api/healthz >/dev/null 2>&1; then
    log "Forgejo is up."
    break
  fi
  [ "$i" -eq 30 ] && die "Forgejo did not become healthy in time."
  sleep 3
done

# ---- create admin user ------------------------------------------------------

log "Creating Forgejo admin user '${ADMIN_USER}'…"
$COMPOSE exec -u 1000 forgejo \
  forgejo admin user create \
  --admin \
  --username "${ADMIN_USER}" \
  --password "${ADMIN_PASS}" \
  --email "${ADMIN_EMAIL}" \
  --must-change-password=false 2>&1 | grep -v "already exists" || true

# ---- mint admin token -------------------------------------------------------

log "Minting Forgejo admin token…"
TOKEN_RESPONSE=$(curl -sf \
  -u "${ADMIN_USER}:${ADMIN_PASS}" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"${TOKEN_NAME}\",\"scopes\":[\"all\"]}" \
  http://localhost:3000/api/v1/users/"${ADMIN_USER}"/tokens) \
  || die "Failed to create Forgejo token. Check that the admin password is correct."

TOKEN_VALUE=$(echo "${TOKEN_RESPONSE}" | grep -o '"sha1":"[^"]*"' | cut -d'"' -f4)
[ -n "${TOKEN_VALUE}" ] || die "Token value not found in response: ${TOKEN_RESPONSE}"

# ---- write .env -------------------------------------------------------------

if [ ! -f "${ENV_FILE}" ]; then
  if [ -f "${ENV_FILE}.example" ]; then
    cp "${ENV_FILE}.example" "${ENV_FILE}"
    log "Created ${ENV_FILE} from example."
  else
    touch "${ENV_FILE}"
  fi
fi

# Update or append FORGEJO_ADMIN_TOKEN.
if grep -q "^FORGEJO_ADMIN_TOKEN=" "${ENV_FILE}"; then
  sed -i "s|^FORGEJO_ADMIN_TOKEN=.*|FORGEJO_ADMIN_TOKEN=${TOKEN_VALUE}|" "${ENV_FILE}"
else
  echo "FORGEJO_ADMIN_TOKEN=${TOKEN_VALUE}" >> "${ENV_FILE}"
fi

log "Token written to ${ENV_FILE}."
log ""
log "  Admin username : ${ADMIN_USER}"
log "  Admin password : ${ADMIN_PASS}"
log "  Token          : (written to .env)"
log ""
log "Run 'make stack' to restart the api with the new token."
