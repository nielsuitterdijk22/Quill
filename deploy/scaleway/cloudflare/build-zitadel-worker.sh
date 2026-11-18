#!/usr/bin/env bash
# Writes dist/zitadel-login-worker.js: the Worker with the CSS inlined as a JS
# string. Paste it into Cloudflare (Workers & Pages → Create → Worker) and add the
# route auth.black-tulip.nl/ui/login/* (zone black-tulip.nl).
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p dist
python3 - <<'PY'
import json
css = open("zitadel-login.css").read()
src = open("zitadel-login-worker.js").read().replace('"__ZITADEL_LOGIN_CSS__"', json.dumps(css))
open("dist/zitadel-login-worker.js", "w").write(src)
print("wrote dist/zitadel-login-worker.js")
PY
