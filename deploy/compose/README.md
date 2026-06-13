# Local development stack

Brings up Postgres, Forgejo, the Quill backend (`api`), and the Quill frontend
(`web`) on one Docker network.

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
