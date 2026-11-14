#!/usr/bin/env python3
"""Write the deploy's secret terraform variables from Scaleway Secret Manager.

The Quill CI deploy (.github/workflows/quill-deploy.yml) holds only a Scaleway
API key; everything else it needs lives in Secret Manager under /quill/deploy
(exported from GitHub on 2026-11-14). This reads those secrets and writes a
terraform -var-file (JSON, mode 0600). It never prints a value.

usage: deploy-tfvars.py <out.json>   (env: SCW_SECRET_KEY, SCW_DEFAULT_PROJECT_ID)
"""
import base64
import json
import os
import sys
import urllib.parse
import urllib.request

API = "https://api.scaleway.com/secret-manager/v1beta1/regions/fr-par"

# terraform variable -> Secret Manager name under /quill/deploy
VARS = {
    "zitadel_client_secret": "deploy-zitadel-client-secret",
    "zitadel_management_token": "deploy-zitadel-management-token",
    "github_client_id": "deploy-gh-client-id",
    "github_client_secret": "deploy-gh-client-secret",
    "brevo_smtp_user": "deploy-brevo-smtp-name",
    "brevo_smtp_password": "deploy-brevo-smtp-key",
    "creem_api_key": "deploy-creem-api-key",
    "creem_webhook_secret": "deploy-creem-webhook-secret",
    "dispatch_secret": "deploy-dispatch-secret",
    "sdb_user": "deploy-sdb-user",
    "sdb_password": "deploy-sdb-password",
    "forgejo_admin_token": "deploy-forgejo-admin-token",
}


def call(path: str) -> dict:
    req = urllib.request.Request(API + path, headers={"X-Auth-Token": os.environ["SCW_SECRET_KEY"]})
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.loads(r.read())


def main() -> int:
    out = sys.argv[1]
    project = os.environ["SCW_DEFAULT_PROJECT_ID"]
    values = {}
    missing = []
    for var, name in VARS.items():
        q = urllib.parse.urlencode({"project_id": project, "name": name, "path": "/quill/deploy"})
        found = call("/secrets?" + q).get("secrets", [])
        if not found:
            missing.append(name)
            continue
        data = call(f"/secrets/{found[0]['id']}/versions/latest/access")["data"]
        values[var] = base64.b64decode(data).decode()
    if missing:
        print("missing in Secret Manager (/quill/deploy): " + ", ".join(missing), file=sys.stderr)
        return 1
    fd = os.open(out, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as f:
        json.dump(values, f)
    print(f"wrote {len(values)} secret variables to {out}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
