#!/usr/bin/env bash
# Provision the Quill OIDC application in a running Zitadel instance.
#
# Reads the machine PAT that FirstInstance wrote (./out/quill-api.pat), then
# creates the Quill project and a public PKCE OIDC app with JWT access tokens,
# and prints the .env lines to paste into the deployment. Idempotent enough for a
# one-time setup; re-running creates additional projects/apps, so run it once.
#
# Run it AGAINST THE PUBLIC ISSUER (so Zitadel's Host matches its external
# domain), from this directory or any:
#
#   ZITADEL_PUBLIC_URL=https://auth.example.com \
#   QUILL_BASE_URL=https://quill.example.com \
#   deploy/compose/zitadel/bootstrap.sh
set -euo pipefail
cd "$(dirname "$0")"

# Convenience: if the public URLs aren't passed in the environment, derive them
# from ZITADEL_EXTERNALDOMAIN / QUILL_DOMAIN found in the deployment's .env
# (repo root, or deploy/compose/.env). A plain shell script can't see compose's
# .env on its own, so read the keys we need here.
read_env_key() { # read_env_key KEY -> first value found across the .env files
  local key="$1" f v
  for f in ../../../.env ../.env; do
    [ -f "$f" ] || continue
    v="$(grep -E "^${key}=" "$f" | tail -1 | cut -d= -f2- | sed 's/[[:space:]]*#.*$//' | tr -d '"'\''\r' | xargs 2>/dev/null || true)"
    if [ -n "$v" ]; then printf '%s' "$v"; return; fi
  done
}
: "${ZITADEL_EXTERNALDOMAIN:=$(read_env_key ZITADEL_EXTERNALDOMAIN)}"
: "${QUILL_DOMAIN:=$(read_env_key QUILL_DOMAIN)}"
: "${ZITADEL_PUBLIC_URL:=${ZITADEL_EXTERNALDOMAIN:+https://$ZITADEL_EXTERNALDOMAIN}}"
: "${QUILL_BASE_URL:=${QUILL_DOMAIN:+https://$QUILL_DOMAIN}}"

: "${ZITADEL_PUBLIC_URL:?set ZITADEL_PUBLIC_URL (or ZITADEL_EXTERNALDOMAIN in .env), e.g. https://auth.example.com}"
: "${QUILL_BASE_URL:?set QUILL_BASE_URL (or QUILL_DOMAIN in .env), e.g. https://quill.example.com}"
echo "using issuer=$ZITADEL_PUBLIC_URL app-base=$QUILL_BASE_URL" >&2

if [ ! -f out/quill-api.pat ]; then
  echo "out/quill-api.pat not found — is Zitadel up and the ./out volume mounted?" >&2
  exit 1
fi
PAT="$(cat out/quill-api.pat)"
auth=(-H "Authorization: Bearer $PAT")
json=(-H "Content-Type: application/json")

ORG="$(curl -fsS "${auth[@]}" "$ZITADEL_PUBLIC_URL/auth/v1/users/me" \
  | python3 -c 'import sys,json;u=json.load(sys.stdin)["user"];print(u.get("details",{}).get("resourceOwner") or u["resourceOwner"])')"
echo "org=$ORG" >&2

PROJ_ID="$(curl -fsS -X POST "${auth[@]}" -H "x-zitadel-orgid: $ORG" "${json[@]}" \
  "$ZITADEL_PUBLIC_URL/management/v1/projects" \
  -d '{"name":"Quill","projectRoleAssertion":true}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')"
echo "project=$PROJ_ID" >&2

APP="$(curl -fsS -X POST "${auth[@]}" -H "x-zitadel-orgid: $ORG" "${json[@]}" \
  "$ZITADEL_PUBLIC_URL/management/v1/projects/$PROJ_ID/apps/oidc" \
  -d "$(cat <<JSON
{
  "name": "Quill Web",
  "redirectUris": ["$QUILL_BASE_URL/api/auth/callback/zitadel"],
  "postLogoutRedirectUris": ["$QUILL_BASE_URL/sign-in"],
  "responseTypes": ["OIDC_RESPONSE_TYPE_CODE"],
  "grantTypes": ["OIDC_GRANT_TYPE_AUTHORIZATION_CODE", "OIDC_GRANT_TYPE_REFRESH_TOKEN"],
  "appType": "OIDC_APP_TYPE_WEB",
  "authMethodType": "OIDC_AUTH_METHOD_TYPE_NONE",
  "accessTokenType": "OIDC_TOKEN_TYPE_JWT",
  "accessTokenRoleAssertion": true,
  "idTokenRoleAssertion": true,
  "idTokenUserinfoAssertion": true
}
JSON
)")"
CLIENT_ID="$(echo "$APP" | python3 -c 'import sys,json;print(json.load(sys.stdin)["clientId"])')"
echo "client_id=$CLIENT_ID" >&2

# Emit the .env block on stdout so it can be appended/redirected cleanly.
cat <<EOF
# --- Zitadel app (from bootstrap.sh) — paste into /home/quill/quill/.env ---
QUILL_ZITADEL_ISSUER=$ZITADEL_PUBLIC_URL
NEXT_PUBLIC_ZITADEL_ISSUER=$ZITADEL_PUBLIC_URL
NEXT_PUBLIC_ZITADEL_CLIENT_ID=$CLIENT_ID
NEXT_PUBLIC_ZITADEL_PROJECT_ID=$PROJ_ID
EOF
