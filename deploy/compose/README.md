# Local development stack

Brings up Postgres, Forgejo, the Quill backend (`api`), the pipeline dispatcher
(`dispatch`), and the Quill frontend (`web`) on one Docker network.

```bash
# from the repo root
make stack     # build + run the whole stack in Docker (docker compose up -d --build)
make logs      # tail logs
make down      # stop everything
```

For day-to-day development prefer `make up`, which keeps only Postgres + Forgejo
in Docker and hot-reloads the api, dispatcher, and web on the host (see the root
`README.md`). `make down` stops the containers either way.

| Service  | URL                   | Notes                                      |
| -------- | --------------------- | ------------------------------------------ |
| web      | http://localhost:3001 | Quill UI                                   |
| api      | http://localhost:8080 | Quill backend (`/healthz`, `/api/v1`)      |
| dispatch | internal `:8090`      | Pipeline dispatcher + Docker-backed runner |
| forgejo  | http://localhost:3000 | Git backend wrapped by Quill               |
| postgres | localhost:5432        | `quill` + `forgejo` databases              |

## First boot

Postgres creates two databases (`quill`, `forgejo`) via `initdb/`. Forgejo boots
with the install wizard locked and self-service registration disabled — Quill
provisions accounts through the admin API.

## Forgejo admin token

Quill talks to Forgejo entirely through its REST API using a single **admin
access token**. You can't invent this value — it must be a real token minted by
Forgejo, or every authenticated call fails with `401 access token does not
exist`. `FORGEJO_ADMIN_TOKEN` is empty by default, so mint one on first boot.

The token is created in two steps, both run against the **running** Forgejo
container (start it first with `make up` or `make stack`):

```bash
COMPOSE="docker compose -f deploy/compose/docker-compose.yml"

# 1. Create the admin user once (idempotent — re-running errors harmlessly).
#    The password is only used to bootstrap the account; Quill never needs it.
$COMPOSE exec -u git forgejo forgejo admin user create \
  --admin --username quill-admin --email admin@quill.local \
  --password "$(openssl rand -hex 16)" --must-change-password=false

# 2. Mint an access token for that user. --raw prints just the token value.
$COMPOSE exec -u git forgejo forgejo admin user generate-access-token \
  --username quill-admin --token-name quill-api --scopes all --raw
```

Copy the token string that step 2 prints into `FORGEJO_ADMIN_TOKEN`:

- **Host-run dev (`make up`)** — the api runs on your host and reads the repo
  root `.envrc`. Set it there:
  ```bash
  export FORGEJO_ADMIN_TOKEN=<token from step 2>
  ```
  Run `direnv allow`, then restart the api (`make up`).

- **Full Docker stack (`make stack`)** — the api runs in a container that reads
  `deploy/compose/.env`. Set it there instead:
  ```
  FORGEJO_ADMIN_TOKEN=<token from step 2>
  ```
  Then `make stack` again to restart the api with it.

Verify the token authenticates (expects `200`):

```bash
curl -sS -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: token $FORGEJO_ADMIN_TOKEN" \
  http://localhost:3000/api/v1/user
```

> `make provision` (via `scripts/provision.sh`) automates the same two steps and
> writes the token into `deploy/compose/.env` for you. It's handy for the full
> Docker stack; for host-run dev you still copy the value into `.envrc`.

> The token name (`quill-api`) must be unique — minting a second token with the
> same name errors. To rotate, mint one with a new name (e.g. `quill-api-2`) or
> delete the old one first: `... forgejo admin user delete-access-token
> --username quill-admin --token quill-api`.

## Reloading environment variables

Compose reads `deploy/compose/.env` and interpolates the values into each
service's `environment:` block. After you edit `.env`, the running containers
keep their **old** values until you recreate them.

> **`restart` does not reload env.** `docker compose restart <service>` reuses
> the existing container, so it keeps the environment it was created with. Use
> `up -d`, which recreates the container when its config (including interpolated
> env) has changed.

```bash
COMPOSE="docker compose -f deploy/compose/docker-compose.yml"

# 1. Edit deploy/compose/.env with the new value(s).

# 2. Recreate only the affected service(s). Compose detects the changed env and
#    replaces the container in place; unrelated services are left running.
$COMPOSE up -d api            # e.g. after changing FORGEJO_ADMIN_TOKEN

# Or recreate the whole stack (equivalent to `make stack` without a rebuild):
$COMPOSE up -d
```

Which service to recreate depends on the variable — `POSTGRES_*` affects
`postgres` (and `forgejo`/`api`, which build their connection strings from it),
`FORGEJO_*` and the auth/dispatch settings affect `api`, and so on. When in
doubt, recreate the whole stack.

> **`web` build args need a rebuild.** The frontend's `NEXT_PUBLIC_*` values are
> inlined into the bundle at build time, so changing them requires rebuilding the
> image, not just recreating the container:
> ```bash
> $COMPOSE up -d --build web
> ```

## Production

This stack is for **local development only**. Production runs on Scaleway —
see [deploy/scaleway/README.md](../scaleway/README.md) (serverless containers +
serverless SQL + one small Forgejo VM, ≈ €10/mo, TLS included).

## Notes

Quill sets one strictly-necessary cookie (`quill_token`, a session JWT) — no consent
banner needed; there is no analytics, no third-party scripts, no external fonts.

Backups, upgrades and TLS for production live in the Scaleway runbook:
[deploy/scaleway/README.md](../scaleway/README.md).

## Pipeline runner

Pipeline execution is split across two services:

1. Quill `api` receives manual triggers and Forgejo webhooks, reads workflow YAML
   from Forgejo, creates the run record, and sends a signed dispatch request to
   `dispatch`.
2. `dispatch` runs the workflow through `nektos/act`, checks out the Forgejo repo
   from the authenticated clone URL, executes `run:` and `uses:` steps in Docker
   job containers, and returns the structured job/step/log result to Quill.

Only `dispatch` mounts `/var/run/docker.sock`; the API does not need Docker
daemon access. API-to-dispatch requests are signed with
`QUILL_PIPELINE_DISPATCH_SECRET` when the secret is set.

You can run the dispatcher separately from compose:

```bash
QUILL_HTTP_ADDR=:8090 \
QUILL_PIPELINE_DISPATCH_SECRET=dev-dispatch-secret \
DOCKER_HOST=unix:///var/run/docker.sock \
make dispatch-run
```

### Follow-ups

- Make dispatch asynchronous: return an accepted dispatch ID immediately, then
  stream job/step/log callbacks back to Quill instead of holding one HTTP request.
- Add a durable queue and runner registration/lease model so multiple runner
  containers can pull jobs from dispatch.
- Replace direct Docker-socket access with rootless or otherwise confined runner
  containers for safer local and production execution.
- Add a Forge-compatible runner adapter so dispatch can hand off jobs to Forgejo
  Actions-style runners while keeping Quill's run/log callback contract.
- Move branch/path `on:` pre-filtering into dispatch; today Quill does event-level
  filtering before dispatch and `act` evaluates the remaining workflow semantics.
