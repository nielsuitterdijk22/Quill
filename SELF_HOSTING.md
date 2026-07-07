# Self-hosting Quill

Quill is designed to run on a single €5–10/month VPS. This guide covers
everything from first boot to GDPR compliance for European operators.

---

## Requirements

| Component | Minimum | Comfortable |
| --------- | ------- | ----------- |
| RAM       | 512 MB  | 1 GB        |
| CPU       | 1 vCPU  | 2 vCPUs     |
| Disk      | 5 GB    | 20 GB+      |

A 1 vCPU / 1 GB VPS is enough for a small team with a few repositories and
occasional pipeline runs. Forgejo is the heaviest component; the Quill API
and frontend add minimal overhead.

**Software required on the host:**

- Docker Engine 24+ and Docker Compose v2
- `curl`, `openssl` (for the provision script)

---

## Quick start

```bash
# 1. Clone the repository
git clone https://github.com/nielsuitterdijk22/quill
cd quill

# 2. Copy and edit the environment file
cp deploy/compose/.env.example deploy/compose/.env
$EDITOR deploy/compose/.env   # set QUILL_JWT_SECRET and QUILL_ENV=production at minimum

# 3. Start the stack
make stack

# 4. First-boot provisioning (creates Forgejo admin + writes token to .env)
make provision

# 5. Restart the API with the new token
make stack
```

After step 5 you can register your first Quill account at `http://localhost:3001`
(or your domain if running behind a reverse proxy).

---

## Required environment variables

These must be set in `deploy/compose/.env` before starting the stack:

| Variable              | Required?        | Description                                                                                                             |
| --------------------- | ---------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `QUILL_ENV`           | Always           | Set to `production` on any public instance. Enforces the `Secure` cookie flag and rejects a missing `QUILL_JWT_SECRET`. |
| `QUILL_JWT_SECRET`    | In production    | Long random string used to sign session cookies. Generate with `openssl rand -hex 32`.                                  |
| `FORGEJO_ADMIN_TOKEN` | After first boot | Set automatically by `make provision`.                                                                                  |
| `FORGEJO_PUBLIC_URL`  | In production    | The public URL Forgejo is reachable at from a browser (e.g. `https://git.example.com`). Used in clone URLs.             |

See `deploy/compose/.env.example` for the full list with descriptions.

---

## HTTPS (required for production)

Running without HTTPS leaks session cookies. Always terminate TLS in front
of the Quill frontend.

### Caddy (recommended — automatic Let's Encrypt)

1. Point your domain's DNS A/AAAA record at the server.

2. Create `deploy/compose/Caddyfile`:

   ```
   your-domain.example.com {
       reverse_proxy web:3001
   }
   ```

3. Add a `caddy` service to `docker-compose.yml`:

   ```yaml
   services:
     caddy:
       image: caddy:2-alpine
       restart: unless-stopped
       ports:
         - "80:80"
         - "443:443"
         - "443:443/udp"
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

4. Remove the `ports` mapping from the `web` service (Caddy is the only
   listener on 80/443; the frontend should not be reachable directly).

5. Set `QUILL_ENV=production` in `.env`.

### nginx with Certbot

```bash
certbot certonly --nginx -d your-domain.example.com
```

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

---

## Exposing Forgejo's SSH

If you want users to push over SSH (`git@your-domain.example.com`) rather
than HTTPS:

1. Map port `2222` from the `forgejo` service to the host (the compose file
   already does this by default).
2. Set `FORGEJO__server__SSH_DOMAIN` to your public domain in the forgejo
   service environment.
3. Set `FORGEJO_PUBLIC_URL` to include the SSH host so Quill shows the
   correct SSH clone URL.

---

## First-boot provisioning

`make provision` automates what the compose README describes as a manual step:

1. Waits until Forgejo's health endpoint responds.
2. Creates a `quill-admin` Forgejo account (if it doesn't exist yet).
3. Mints a Forgejo admin token and writes it to `deploy/compose/.env`.

You can override the admin username, password, and email:

```bash
FORGEJO_ADMIN_USERNAME=my-admin \
FORGEJO_ADMIN_PASSWORD=supersecret \
FORGEJO_ADMIN_EMAIL=me@example.com \
make provision
```

The script is idempotent — re-running it will skip user creation if the
account already exists and simply issue a new token.

After provisioning, run `make stack` once more so the api service picks up
the updated `FORGEJO_ADMIN_TOKEN` value.

---

## Upgrading

1. Pull the new source:

   ```bash
   git pull
   ```

2. Rebuild and restart the stack:

   ```bash
   make stack
   ```

   Schema migrations run automatically on API startup; no manual `make migrate`
   step is needed unless the release notes say otherwise.

3. Check the release notes for breaking changes, new required environment
   variables, or Forgejo version bumps that require a Forgejo migration step.

> Always take a backup before upgrading. See the Backup section below.

---

## Backup and restore

### Backing up

```bash
# Postgres — dump both databases
docker compose -f deploy/compose/docker-compose.yml exec postgres \
  pg_dump -U quill quill > quill-$(date +%Y%m%d).sql

docker compose -f deploy/compose/docker-compose.yml exec postgres \
  pg_dump -U quill forgejo > forgejo-$(date +%Y%m%d).sql

# Forgejo data volume (git repos, avatars, attachments)
docker run --rm \
  -v quill-forgejo-data:/data \
  -v "$(pwd)":/backup \
  alpine tar czf /backup/forgejo-data-$(date +%Y%m%d).tar.gz /data
```

Store all three files off-server (S3, Backblaze, encrypted external drive).

### Restoring

```bash
# Stop everything first
docker compose -f deploy/compose/docker-compose.yml down

# Restore Forgejo volume
docker run --rm \
  -v quill-forgejo-data:/data \
  -v "$(pwd)":/backup \
  alpine sh -c "cd / && tar xzf /backup/forgejo-data-YYYYMMDD.tar.gz"

# Start Postgres only, then restore databases
docker compose -f deploy/compose/docker-compose.yml up -d postgres
# Wait for postgres to be ready, then:
docker compose -f deploy/compose/docker-compose.yml exec -T postgres \
  psql -U quill quill < quill-YYYYMMDD.sql
docker compose -f deploy/compose/docker-compose.yml exec -T postgres \
  psql -U quill forgejo < forgejo-YYYYMMDD.sql

# Start everything else
docker compose -f deploy/compose/docker-compose.yml up -d
```

---

## GDPR operator checklist

Quill is designed to be deployable in the EU without a lawyer.

### Cookie consent

Quill sets **one cookie**: `quill_token`, a session JWT. This is strictly
necessary for authentication and is exempt from GDPR's consent requirement.
**No cookie consent banner is needed** as long as you do not add third-party
integrations (analytics, CDNs, fonts from external domains).

### No telemetry

Quill makes no call-home requests and collects no usage telemetry by default.
There are no analytics, tracking pixels, or external fonts. Your users' IP
addresses and code stay on your hardware.

### Data export

Users can export their own data at any time via **Settings → Export my data**.
This produces a JSON document satisfying GDPR Article 20 portability covering:
profile, repository metadata, PR history, and pipeline run metadata.

### Account deletion

Users can self-delete their account via **Settings → Delete my account**. This:

- Removes their Quill database record and all auth identities
- Revokes all git access tokens
- Deletes the mirrored Forgejo account
- Removes them from all project memberships

An audit log entry is written for the operator's records.

### Privacy policy

A template privacy policy for instance operators is provided in
`docs/operator-privacy-policy.md`. Fill in your contact details and adapt it
to your deployment before making your instance public.

---

## Security notes

- **Always use HTTPS** in production — see the HTTPS section above.
- Set a strong, unique `QUILL_JWT_SECRET` (at least 32 random bytes).
- Set `QUILL_ENV=production` to enforce the `Secure` cookie flag.
- Change the default Postgres password before exposing the host.
- Do not expose port `3000` (Forgejo) or `8080` (Quill API) directly on the
  public interface — route traffic through your reverse proxy.
- Set `QUILL_WEBHOOK_SECRET` to verify Forgejo webhook payloads.

---

## Runner security: rootless vs Docker socket

By default the Quill dispatch service mounts the host Docker socket
(`/var/run/docker.sock`) so pipeline jobs can run containers. This gives
pipeline code **root on the host**. Accept this trade-off only if you trust
all users who can trigger pipelines.

### Option A — restrict socket access (simple)

Limit who can push to the repository and limit CI to trusted contributors.
Add the dispatch service to a dedicated Docker group with restricted access to
the socket, or use Docker's `userns-remap` feature to remap container UIDs.

### Option B — Podman (rootless, recommended for untrusted users)

[Podman](https://podman.io) can run containers without daemon privileges.

1. Install Podman on the host (`apt install podman` / `dnf install podman`).
2. Start the Podman system service as a non-root user:
   ```bash
   systemctl --user enable --now podman.socket
   ```
3. In `docker-compose.yml`, replace the socket mount on the `dispatch` service:
   ```yaml
   volumes:
     - /run/user/1000/podman/podman.sock:/var/run/docker.sock:ro
   ```
4. Set `DOCKER_HOST=unix:///var/run/docker.sock` in the dispatch environment.

Podman's rootless mode means a compromised job cannot escalate to host root.
The trade-off is that some Docker-in-Docker patterns do not work, and
`privileged: true` containers are not available without extra configuration.

### Option C — firecracker / nsjail (advanced)

For full isolation, run pipeline jobs inside a micro-VM (Firecracker) or
inside an nsjail. This is out of scope for the hobby tier but the
`pipeline.Runner` interface in `internal/pipeline/` is designed so alternative
runners can be plugged in.

---

## Git LFS

Forgejo ships with built-in Git LFS support. It works through Quill's git
token auth flow without any extra configuration.

### Verify LFS is enabled

Check that `FORGEJO__server__LFS_START_SERVER=true` is set in the Forgejo
service environment (the default compose file enables this). Forgejo stores
LFS objects in its data volume alongside the repository objects.

### Cloning with LFS

```bash
# Install git-lfs on the client first
git lfs install

# Clone normally — git-lfs hooks negotiate the LFS endpoint automatically
git clone https://your-domain.example.com/forgejo/owner/repo.git

# Authenticate as usual: use a Quill git token as the HTTP password
# (Settings → Git tokens → Create token)
```

### Limitations

- LFS objects count toward the Forgejo data volume size. Include the volume
  in your backup procedure (see the Backup section).
- Very large files (> several GB each) may hit Forgejo's upload timeout.
  Increase `FORGEJO__server__LFS_JWT_TTL` and Nginx/Caddy's client body
  timeout if you hit this.

---

## Troubleshooting

**`make stack` fails to start the api service**

The api service waits for both Postgres and Forgejo to be healthy before
starting. Check `make logs` for details. If Forgejo takes more than ~2 minutes
to initialise (e.g. on very slow disk), it may time out; re-run `make stack`.

**The api starts but Forgejo operations fail with "git unavailable"**

`FORGEJO_ADMIN_TOKEN` is missing or invalid. Run `make provision` and
then `make stack` again.

**Users can't clone over SSH**

Ensure port `2222` is open on the host firewall and `FORGEJO__server__SSH_DOMAIN`
is set to the public hostname. Users should clone with:

```
git clone ssh://git@your-domain.example.com:2222/owner/repo.git
```

**Pipelines stay pending forever**

The dispatch service requires Docker socket access (`/var/run/docker.sock`).
Check that the socket exists on the host and that the dispatch container
started successfully (`make ps`).
