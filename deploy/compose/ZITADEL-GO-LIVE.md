# Running Quill on self-hosted Zitadel (production)

Quill's auth provider is flag-gated (`QUILL_AUTH_PROVIDER` / `NEXT_PUBLIC_AUTH_PROVIDER`,
default `clerk`). This guide stands up **Zitadel behind Caddy with TLS** on the
deploy VM and switches Quill onto it. Everything below is done in
`/home/quill/quill` (the deploy checkout) and via the project's `.env`.

The pieces:
- `zitadel` + `zitadel-db` services (compose profile `zitadel`).
- Caddy vhost `auth.<your-domain>` → `h2c://zitadel:8080` (already in the Caddyfile).
- `zitadel/steps.yaml` — first-boot seed (admin login + a machine PAT).
- `zitadel/bootstrap.sh` — creates the Quill OIDC app and prints the `.env` lines.

---

## 1. DNS

Point an **`auth.` subdomain** at the VM (same IP as your main domain):

```
auth.example.com   A   <vm-ip>
```

Caddy issues the TLS cert automatically once this resolves.

## 2. Set the Zitadel env BEFORE first boot

`ZITADEL_EXTERNALDOMAIN` is baked into the instance at init and **cannot be
changed later** without wiping the `zitadel-pgdata` volume — set it correctly the
first time. In `/home/quill/quill/.env`:

```dotenv
# Enable the zitadel profile in the CI deploy (so it's managed + not orphan-removed)
DEPLOY_EXTRA_PROFILES=zitadel

# Zitadel instance (public host fronted by Caddy)
ZITADEL_EXTERNALDOMAIN=auth.example.com
ZITADEL_EXTERNALSECURE=true
ZITADEL_EXTERNALPORT=443
ZITADEL_MASTERKEY=<exactly 32 characters>     # openssl rand -hex 16
ZITADEL_DB_PASSWORD=<strong>                  # admin DB user
ZITADEL_DB_USER_PASSWORD=<strong>             # zitadel DB user
```

## 3. Deploy → Zitadel comes up behind Caddy

Trigger a deploy (push/PR to `main`, or on the VM run the same compose). The
`zitadel` profile is now active, so `zitadel-db` + `zitadel` start, Caddy serves
`https://auth.example.com`, and `steps.yaml` seeds the instance on first boot.

Verify:

```bash
curl -fsS https://auth.example.com/.well-known/openid-configuration | jq .issuer
# -> "https://auth.example.com"
```

First-login admin: username `admin`, password `ChangeMeOnFirstLogin1!`
(**change it immediately** — you're forced to on first login). Set a real admin
email in the console too.

## 4. Provision the Quill OIDC app

`steps.yaml` wrote a machine PAT to `deploy/compose/zitadel/out/quill-api.pat`
(bind-mounted from the container). Run the bootstrap against the **public issuer**
(so Zitadel's host check passes):

```bash
ZITADEL_PUBLIC_URL=https://auth.example.com \
QUILL_BASE_URL=https://example.com \
deploy/compose/zitadel/bootstrap.sh >> .env
```

It appends these to `.env`:

```dotenv
QUILL_ZITADEL_ISSUER=https://auth.example.com
NEXT_PUBLIC_ZITADEL_ISSUER=https://auth.example.com
NEXT_PUBLIC_ZITADEL_CLIENT_ID=<generated>
NEXT_PUBLIC_ZITADEL_PROJECT_ID=<generated>
```

Also create a **service-user PAT** with org-management permission in the console
and add it for the backend (account deletion + future org/member provisioning):

```dotenv
QUILL_ZITADEL_MANAGEMENT_TOKEN=<service-user PAT>
```

## 5. Flip Quill onto Zitadel

Add to `.env`:

```dotenv
QUILL_AUTH_PROVIDER=zitadel
NEXT_PUBLIC_AUTH_PROVIDER=zitadel
AUTH_SECRET=<openssl rand -base64 32>
AUTH_URL=https://example.com
```

Redeploy. `NEXT_PUBLIC_*` are inlined at build time, so the deploy's
`compose build` step rebuilds `web` with them — a normal deploy is enough.

## 6. Verify the round-trip

- Visit `https://example.com` → redirected to `https://auth.example.com` to log in.
- Log in → back in Quill; the dashboard loads (the backend provisions the Quill
  user from the Zitadel token on first login).
- Delete a test account → the Zitadel user is removed (Management API), so the
  session can't resurrect it.

## Rollback (instant)

Set `QUILL_AUTH_PROVIDER=clerk` and `NEXT_PUBLIC_AUTH_PROVIDER=clerk` (or remove
them) and redeploy. The Clerk path is untouched. Leave Zitadel running or
`docker compose -f deploy/compose/docker-compose.yml --profile zitadel down`.

## Notes

- The local port is bound to `127.0.0.1` only — external access is always via
  Caddy/TLS. `bootstrap.sh` must use the public `https://auth.…` URL (not
  localhost), or Zitadel rejects the request on a host mismatch.
- For a throwaway local trial (no TLS), the standalone `deploy/spike-zitadel/`
  stack runs Zitadel on `http://localhost:8081`.
