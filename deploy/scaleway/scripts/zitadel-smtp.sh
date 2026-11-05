#!/usr/bin/env bash
# Idempotently configure the active SMTP email provider on a RUNNING Zitadel
# instance via the admin API (v2.65: /admin/v1/email...).
#
# Why the API and not env vars: ZITADEL_DEFAULTINSTANCE_SMTPCONFIGURATION_* is
# only read when the instance is first created (setupSMTPSettings in
# internal/command/instance.go). The existing instance stores its SMTP config in
# the DB, so the admin API is the only way to enable mail on it.
#
# Env:
#   ZITADEL_URL     e.g. https://auth.quill.black-tulip.nl
#   ZITADEL_TOKEN   machine PAT with iam.write (GH secret ZITADEL_MANAGEMENT_TOKEN;
#                   the quill-api machine user from FirstInstance has IAM_OWNER)
#   SMTP_HOST       host:port, e.g. smtp-relay.brevo.com:587 (port required)
#   SMTP_USER       Brevo SMTP login (xxx@smtp-brevo.com)
#   SMTP_PASSWORD   Brevo SMTP key (xsmtpib-...)
#   SMTP_FROM       sender address, e.g. no-reply@quill.black-tulip.nl
#   SMTP_FROM_NAME  default "Quill"
#   SMTP_TLS        default "true" — implicit TLS with automatic STARTTLS
#                   fallback, which is right for Brevo port 587 (and works for 465)
#   SMTP_TEST_TO    optional receiver address for a test mail (POST /email/smtp/_test)
set -euo pipefail

: "${ZITADEL_URL:?set ZITADEL_URL, e.g. https://auth.quill.black-tulip.nl}"
: "${ZITADEL_TOKEN:?set ZITADEL_TOKEN (machine PAT with iam.write)}"
: "${SMTP_HOST:?set SMTP_HOST as host:port, e.g. smtp-relay.brevo.com:587}"
: "${SMTP_USER:?set SMTP_USER (Brevo SMTP login)}"
: "${SMTP_PASSWORD:?set SMTP_PASSWORD (Brevo SMTP key)}"
: "${SMTP_FROM:?set SMTP_FROM, e.g. no-reply@quill.black-tulip.nl}"
SMTP_FROM_NAME="${SMTP_FROM_NAME:-Quill}"
SMTP_TLS="${SMTP_TLS:-true}"
export SMTP_FROM_NAME SMTP_TLS

API="${ZITADEL_URL%/}/admin/v1"
AUTH=(-H "Authorization: Bearer ${ZITADEL_TOKEN}")
CT=(-H "Content-Type: application/json")

body() { # $1 = optional receiver_address
  python3 -c '
import json, os
b = {
    "sender_address": os.environ["SMTP_FROM"],
    "sender_name": os.environ["SMTP_FROM_NAME"],
    "tls": os.environ["SMTP_TLS"] == "true",
    "host": os.environ["SMTP_HOST"],
    "user": os.environ["SMTP_USER"],
    "password": os.environ["SMTP_PASSWORD"],
    "reply_to_address": os.environ["SMTP_FROM"],
    "description": "Brevo SMTP relay",
}
if len(os.sys.argv) > 1 and os.sys.argv[1]:
    b["receiver_address"] = os.sys.argv[1]
print(json.dumps(b))
' "$1"
}

get_id() { # extract provider id from a JSON blob on stdin
  python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
p = d.get("provider") or {}
print(p.get("id") or d.get("id") or "")
'
}

# 1. Is an email provider already active? -> update it (PUT activates on save).
active_code=$(curl -sS -o /tmp/zitadel-email.json -w "%{http_code}" "${AUTH[@]}" "$API/email" || echo 000)
id=""
if [ "$active_code" = "200" ]; then
  id=$(get_id </tmp/zitadel-email.json)
fi

if [ -n "$id" ]; then
  echo "Updating existing email provider ${id} (PUT /email/smtp/{id} activates it)"
  curl -fsS "${AUTH[@]}" "${CT[@]}" -X PUT -d "$(body)" "$API/email/smtp/$id" >/dev/null
else
  # 2. No active provider -> add one, then activate it explicitly.
  echo "Adding SMTP email provider (POST /email/smtp)"
  resp=$(curl -fsS "${AUTH[@]}" "${CT[@]}" -X POST -d "$(body)" "$API/email/smtp")
  id=$(echo "$resp" | get_id)
  if [ -z "$id" ]; then
    resp=$(curl -fsS "${AUTH[@]}" "${CT[@]}" -X POST -d '{}' "$API/email/_search")
    id=$(echo "$resp" | python3 -c '
import json, sys
try:
    for p in json.load(sys.stdin).get("result") or []:
        if p.get("smtp"):
            print(p.get("id") or ""); break
except Exception:
    pass
')
  fi
  if [ -z "$id" ]; then
    echo "ERROR: could not determine provider id after add" >&2
    exit 1
  fi
  echo "Activating provider ${id} (POST /email/{id}/_activate)"
  curl -fsS "${AUTH[@]}" -X POST "$API/email/$id/_activate" >/dev/null
fi

# 3. Optional test mail.
if [ -n "${SMTP_TEST_TO:-}" ]; then
  echo "Sending test mail to ${SMTP_TEST_TO} (POST /email/smtp/_test)"
  curl -fsS "${AUTH[@]}" "${CT[@]}" -X POST -d "$(body "$SMTP_TEST_TO")" "$API/email/smtp/_test" >/dev/null
fi

echo "OK: Zitadel active SMTP provider = ${SMTP_HOST} as ${SMTP_FROM} (${SMTP_FROM_NAME})"
rm -f /tmp/zitadel-email.json
