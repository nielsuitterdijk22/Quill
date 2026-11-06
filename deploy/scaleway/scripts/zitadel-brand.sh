#!/usr/bin/env bash
# Idempotently brand the Zitadel instance as Quill: label policy (logo/colors,
# no watermark) + custom mail texts for init / password reset / email verify.
# Env: ZITADEL_URL, ZITADEL_TOKEN, plus optional QUILL_APP_URL (logo host).
set -euo pipefail

: "${ZITADEL_URL:?set ZITADEL_URL}" "${ZITADEL_TOKEN:?set ZITADEL_TOKEN}"
APP_URL="${QUILL_APP_URL:-https://app.${ZITADEL_URL#https://auth.}}"
API="${ZITADEL_URL%/}/admin/v1"
H=(-H "Authorization: Bearer ${ZITADEL_TOKEN}")
CT=(-H "Content-Type: application/json")

curl -fsS "${H[@]}" "${CT[@]}" -X PUT "$API/policies/label" -d "{
  \"primaryColor\": \"#7c5cff\",
  \"backgroundColor\": \"#10131a\",
  \"fontColor\": \"#ffffff\",
  \"primaryColorDark\": \"#7c5cff\",
  \"backgroundColorDark\": \"#10131a\",
  \"fontColorDark\": \"#ffffff\",
  \"warnColor\": \"#ff3860\",
  \"warnColorDark\": \"#ff3860\",
  \"disableWatermark\": true,
  \"logoUrl\": \"${APP_URL}/favicon.svg\",
  \"iconUrl\": \"${APP_URL}/favicon.svg\",
  \"logoUrlDark\": \"${APP_URL}/favicon.svg\",
  \"iconUrlDark\": \"${APP_URL}/favicon.svg\"
}" >/dev/null
curl -fsS "${H[@]}" -X POST "$API/policies/label/_activate" >/dev/null

set_text() { # path, json body
  curl -fsS "${H[@]}" "${CT[@]}" -X PUT "$API/text/message/$1/en" -d "$2" >/dev/null
}

set_text init '{
  "subject": "Activate your Quill account",
  "title": "Activate your Quill account",
  "preHeader": "Set your password to activate your Quill account",
  "greeting": "Hello {{.DisplayName}},",
  "text": "You have been invited to Quill. Click the button below to activate your account and set your password. (Code {{.Code}}) If you did not expect this email, you can ignore it.",
  "buttonText": "Activate Quill account",
  "footer": "Quill"
}'
set_text passwordreset '{
  "subject": "Reset your Quill password",
  "title": "Reset your Quill password",
  "preHeader": "Reset your Quill password",
  "greeting": "Hello {{.DisplayName}},",
  "text": "Click the button below to reset your Quill password. If you did not request this, you can ignore this email.",
  "buttonText": "Reset password",
  "footer": "Quill"
}'
set_text verifyemail '{
  "subject": "Verify your Quill email",
  "title": "Verify your Quill email",
  "preHeader": "Verify your email address for Quill",
  "greeting": "Hello {{.DisplayName}},",
  "text": "Click the button below to verify your email address for Quill. If you did not request this, you can ignore this email.",
  "buttonText": "Verify email",
  "footer": "Quill"
}'

echo "OK: Zitadel branded as Quill (label policy active, mail texts customized)"
