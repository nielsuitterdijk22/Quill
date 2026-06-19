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
`make stack` again to apply.

## Production deployment (HTTPS required)

Running Quill over plain HTTP in production is **unsafe**: the session cookie
travels unencrypted and can be intercepted by anyone on the network. Always
terminate TLS before traffic reaches the Quill frontend.

The simplest option is [Caddy](https://caddyserver.com), which handles
certificate provisioning from Let's Encrypt automatically.

### Caddy

1. Point your domain's DNS A/AAAA record at the server's public IP.

2. Create `deploy/compose/Caddyfile` next to your compose file:

   ```
   your-domain.example.com {
       reverse_proxy web:3001
   }
   ```

3. Add a `caddy` service to your compose file and the required volumes:

   ```yaml
   services:
     caddy:
       image: caddy:2-alpine
       restart: unless-stopped
       ports:
         - "80:80"
         - "443:443"
         - "443:443/udp"   # HTTP/3
       volumes:
         - ./Caddyfile:/etc/caddy/Caddyfile:ro
         - caddy_data:/data
         - caddy_config:/config
       depends_on:
         - web

   volumes:
     caddy_data:
     caddy_config:
   ```

4. Remove the `ports` mapping from the `web` service (it should only be
   reachable through Caddy, not directly on port 3001):

   ```yaml
   web:
     # ports: - "3001:3001"   ← remove this line
   ```

5. Set `QUILL_ENV=production` in the `api` service so the JWT cookie's
   `Secure` flag is enforced and the server rejects a missing `QUILL_JWT_SECRET`.

### nginx with Certbot

If you prefer nginx, obtain a certificate with
`certbot certonly --nginx -d your-domain.example.com` and add a server block:

```nginx
server {
    listen 443 ssl;
    server_name your-domain.example.com;

    ssl_certificate     /etc/letsencrypt/live/your-domain.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/your-domain.example.com/privkey.pem;

    location / {
        proxy_pass http://localhost:3001;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}

server {
    listen 80;
    server_name your-domain.example.com;
    return 301 https://$host$request_uri;
}
```

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
