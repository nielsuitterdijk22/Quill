# Switching the live deployment from Clerk to Zitadel

Everything is flag-gated (`QUILL_AUTH_PROVIDER` / `NEXT_PUBLIC_AUTH_PROVIDER`,
default `clerk`), so this is a configuration + one-deploy operation. Do it on the
deploy VM (`/home/quill/quill`).

> Prerequisite: the deploy VM must be reachable (CI deploys over SSH on :22). If
> deploys fail with `dial tcp …:22: i/o timeout`, the VM is down/unreachable —
> fix that first.

## 1. Bring up Zitadel next to the stack

Zitadel + its database live behind the `zitadel` compose profile, so they only
start when you ask:

```bash
cd /home/quill/quill
# Set a real master key + admin password first (see .env below), then:
docker compose -f deploy/compose/docker-compose.yml \
  --profile production --profile zitadel up -d zitadel-db zitadel
# wait until healthy:
docker compose -f deploy/compose/docker-compose.yml ps zitadel
```

In production, front Zitadel with Caddy at a dedicated host (e.g.
`auth.<your-domain>`) and set `ZITADEL_EXTERNALDOMAIN`/`ZITADEL_EXTERNALSECURE`
accordingly. For a quick test you can expose `:8081` directly and set the issuer
to `http://<vm-ip>:8081` (HTTP, not for real use).

## 2. Provision the org + OIDC app

```bash
# PAT was written by FirstInstance to the zitadel container; or create a service
# user in the console. Then, pointing BASE at your Zitadel:
BASE=https://auth.<your-domain> ./deploy/spike-zitadel/bootstrap.sh
# bootstrap.sh prints ZITADEL_CLIENT_ID / ZITADEL_PROJECT_ID / ZITADEL_ORG_ID.
```

The OIDC app's redirect URI must be your app's callback:
`https://<your-domain>/api/auth/callback/zitadel` (bootstrap.sh registers the
localhost one — update it in the console for production, or edit the script).

For the backend Management token, create a service-user PAT with org-management
permissions and use it as `QUILL_ZITADEL_MANAGEMENT_TOKEN`.

## 3. Set the env (`/home/quill/quill/.env`)

```dotenv
# --- switch both halves to Zitadel ---
QUILL_AUTH_PROVIDER=zitadel
NEXT_PUBLIC_AUTH_PROVIDER=zitadel

# --- backend verifier + management ---
QUILL_ZITADEL_ISSUER=https://auth.<your-domain>
QUILL_ZITADEL_MANAGEMENT_TOKEN=<service-user PAT>

# --- frontend (Auth.js); NEXT_PUBLIC_* are build args, so a rebuild is needed ---
NEXT_PUBLIC_ZITADEL_ISSUER=https://auth.<your-domain>
NEXT_PUBLIC_ZITADEL_CLIENT_ID=<from bootstrap>
NEXT_PUBLIC_ZITADEL_PROJECT_ID=<from bootstrap>
AUTH_SECRET=<openssl rand -base64 32>
AUTH_URL=https://<your-domain>

# --- Zitadel container hardening (override the dev defaults) ---
ZITADEL_MASTERKEY=<exactly 32 chars>
ZITADEL_EXTERNALDOMAIN=auth.<your-domain>
ZITADEL_EXTERNALSECURE=true
ZITADEL_DB_PASSWORD=<strong>
ZITADEL_DB_USER_PASSWORD=<strong>
ZITADEL_ADMIN_PASSWORD=<strong>
```

## 4. Deploy

Because `NEXT_PUBLIC_*` are inlined at build time, the `web` image must be
rebuilt with the new args. The CI deploy runs `docker compose … build` then
`up -d`, so just trigger a deploy (push/PR to `main`) — or on the VM:

```bash
docker compose -f deploy/compose/docker-compose.yml --profile production build web api
docker compose -f deploy/compose/docker-compose.yml --profile production up -d web api
```

## 5. Verify

- Visit the app → you're redirected to Zitadel's hosted login.
- Sign in → land back in Quill; the dashboard loads (the backend provisions the
  Quill user from the Zitadel token on first login).
- Account deletion removes the Zitadel user (Management API) — no resurrection.

## Rollback

Set `QUILL_AUTH_PROVIDER=clerk` and `NEXT_PUBLIC_AUTH_PROVIDER=clerk` (or unset),
redeploy. The Clerk path is untouched, so this is instant. Leave the Zitadel
containers running or `docker compose … --profile zitadel down`.
