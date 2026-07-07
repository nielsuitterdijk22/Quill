# Running Quill on self-hosted Zitadel (production)

Quill authenticates against a self-hosted **Zitadel** IdP. This guide stands up
**Zitadel behind Caddy with TLS** on the deploy VM and switches Quill onto it.
Everything below is done in `/home/quill/quill` (the deploy checkout) and via the
project's `.env`.

The pieces:

- `zitadel` + `zitadel-db` services (compose profile `zitadel`).
- A Caddy vhost whose host **follows `ZITADEL_EXTERNALDOMAIN`** → `h2c://zitadel:8080`
  (already in the Caddyfile). It can be any host with a DNS record pointing at the
  VM — e.g. `auth.example.com` or a bare `auth.example.com` sibling — it does not
  have to sit under the main Quill domain.
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
first time.

> **Put `.env` at the repo root** (`/home/quill/quill/.env`), i.e. the directory
> the deploy runs `docker compose` from. Compose reads `.env` from the **working
> directory**, not the compose file's folder — a `.env` under `deploy/compose/`
> is silently ignored and every `ZITADEL_*` var falls back to its default
> (`ExternalDomain=localhost` → `Instance not found` 404s). The deploy reads
> `DEPLOY_EXTRA_PROFILES` from the same path.

In `/home/quill/quill/.env`:

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
(bind-mounted from the container). Run the bootstrap — it talks to the **public
issuer** (so Zitadel's host check passes). It auto-derives the URLs from
`ZITADEL_EXTERNALDOMAIN` + `QUILL_DOMAIN` in your `.env`, so from the repo root:

```bash
deploy/compose/zitadel/bootstrap.sh >> .env
```

`.env` is read by Compose, not by your shell — so if you'd rather be explicit (or
the values aren't in `.env`), pass them on the command line instead:

```bash
ZITADEL_PUBLIC_URL=https://auth.example.com \
QUILL_BASE_URL=https://example.com \
deploy/compose/zitadel/bootstrap.sh >> .env
```

It appends these to `.env`:

```dotenv
ZITADEL_ISSUER=https://auth.example.com
NEXT_PUBLIC_ZITADEL_ISSUER=https://auth.example.com
NEXT_PUBLIC_ZITADEL_CLIENT_ID=<generated>
NEXT_PUBLIC_ZITADEL_PROJECT_ID=<generated>
```

Also create a **service-user PAT** with org-management permission in the console
and add it for the backend (account deletion + future org/member provisioning):

```dotenv
ZITADEL_MANAGEMENT_TOKEN=<service-user PAT>
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

## Troubleshooting

**`Instance not found … (ExternalDomain is localhost)` / discovery 404, or
`Errors.Instance.Domain.AlreadyExists` during the `03_default_instance`
migration.** Zitadel was first initialised with a different external domain than
the one you're now requesting (often `localhost`, because `.env` wasn't read —
see §2). It's baked in at init, so re-init with the correct
`ZITADEL_EXTERNALDOMAIN` set first. **You must remove `zitadel-db` too** — while
it's running it holds the volume, so `docker volume rm` fails silently ("in use")
and the old instance survives:

```bash
C="docker compose -f deploy/compose/docker-compose.yml --profile production --profile zitadel"
$C config | grep -i externaldomain         # sanity: must show your domain, NOT localhost
$C rm -sf zitadel zitadel-init zitadel-db  # zitadel-db included — that's the trap
docker volume rm quill_zitadel-pgdata
docker volume ls | grep zitadel            # should now print nothing
$C up -d zitadel-db zitadel-init zitadel
```

**Zitadel container exits during init with `migration failed … permission denied`.**
The `zitadel-init` service makes `./zitadel/out` world-writable before boot, so a
full `up` (not just `up -d zitadel`) must run it. Bring up the whole profile:
`… --profile zitadel up -d`. (One-off manual fix: `chmod 0777 deploy/compose/zitadel/out`.)

**`ZITADEL_MASTERKEY` errors.** The master key must be exactly 32 characters
(`openssl rand -hex 16`). Changing it after init requires a `zitadel-pgdata` wipe.

## Falling back to local auth

Leave `ZITADEL_ISSUER` / `NEXT_PUBLIC_ZITADEL_ISSUER` unset and redeploy:
Quill falls back to local username/password auth. Stop Zitadel with
`docker compose -f deploy/compose/docker-compose.yml --profile zitadel down`.

## Notes

- The local port is bound to `127.0.0.1` only — external access is always via
  Caddy/TLS. `bootstrap.sh` must use the public `https://auth.…` URL (not
  localhost), or Zitadel rejects the request on a host mismatch.
- For a throwaway local trial (no TLS), the standalone `deploy/spike-zitadel/`
  stack runs Zitadel on `http://localhost:8081`.
