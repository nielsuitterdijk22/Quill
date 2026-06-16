# Local development stack

Brings up Postgres, Forgejo, the Quill backend (`api`), the pipeline dispatcher
(`dispatch`), and the Quill frontend (`web`) on one Docker network.

```bash
# from the repo root
make up        # docker compose up -d --build
make logs      # tail logs
make down      # stop everything
```

| Service  | URL                     | Notes                                   |
| -------- | ----------------------- | --------------------------------------- |
| web      | http://localhost:3001   | Quill UI                                |
| api      | http://localhost:8080   | Quill backend (`/healthz`, `/api/v1`)   |
| dispatch | internal `:8090`        | Pipeline dispatcher + Docker-backed runner |
| forgejo  | http://localhost:3000   | Git backend wrapped by Quill            |
| postgres | localhost:5432          | `quill` + `forgejo` databases           |

## First boot

Postgres creates two databases (`quill`, `forgejo`) via `initdb/`. Forgejo boots
with the install wizard locked and self-service registration disabled — Quill
provisions accounts through the admin API.

## Forgejo admin token (needed from PR 4)

Once the stack is healthy, create an admin user and token, then set
`QUILL_FORGEJO_ADMIN_TOKEN` in `deploy/compose/.env`:

```bash
# create an admin user inside the running Forgejo container
docker compose -f deploy/compose/docker-compose.yml exec -u 1000 forgejo \
  forgejo admin user create --admin --username quill-admin \
  --password "change-me" --email admin@quill.local --must-change-password=false

# then create a token via the API (or the Forgejo UI: Settings → Applications)
curl -s -u quill-admin:change-me \
  -H 'Content-Type: application/json' \
  -d '{"name":"quill","scopes":["all"]}' \
  http://localhost:3000/api/v1/users/quill-admin/tokens
```

Copy the returned `sha1` token into `QUILL_FORGEJO_ADMIN_TOKEN` and
`make up` again to apply.

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
