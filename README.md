# Quill

**Quill** is a version-control platform for platform teams — code browsing, pull
requests, branch policies, and pipelines — built as a clean layer on top of
[Forgejo](https://forgejo.org/). Forgejo runs as a separate service and owns git
storage and low-level repo/PR operations; Quill wraps its REST API and adds the
platform layer (tenants, projects, branch policies, pipelines, pluggable auth)
with its own Postgres for metadata.

Quill is the home for two companion tools that surface inside it:

- **Forge** — confidential, ephemeral CI runners.
- **Yaly** — software catalog & self-service (Backstage-style), used for
  ownership and discovery ("who owns what").

> Status: early. Built foundation-first as a series of small PRs — see the
> [roadmap](#roadmap).

## Architecture

```
┌────────────┐        ┌───────────────────────────┐
│  Next.js   │  REST  │  Quill backend (Go)        │
│  frontend  │ ─────► │  chi · pgx · sqlc          │
│  :3001     │        │  :8080                     │
└────────────┘        │   ├─ metadata ─► Postgres  │
                      │   └─ git ops ──► Forgejo ───┼─► :3000
                      └───────────────────────────┘
```

- **Forgejo is wrapped, never forked.** All git/repo/PR primitives go through its
  REST API. Quill stores only what Forgejo can't: tenants, projects, ownership,
  branch policies, pipeline config, and auth identity mapping.
- **Auth is abstracted.** A local username/password provider issues Quill JWTs
  today; Keycloak / Entra / GitHub OIDC drop in behind the same `AuthProvider`
  interface later.
- **A flat MVP model: Tenant → Project → Resource.** A **Tenant** is the billing
  and SSO boundary; a **Project** is a team/app namespace that owns repositories
  and pipelines; **Resources** (repos, pipelines) live directly under a project.
  Each project maps 1:1 to a Forgejo org. The cross-cutting views (Repositories,
  Pull requests, Pipelines) are scoped to the current project, chosen via the
  switcher in the top-left under the signed-in user.

## Repository layout

| Path              | What                                                 |
| ----------------- | ---------------------------------------------------- |
| `backend/`        | Go API (module `github.com/nielsuitterdijk22/quill`) |
| `frontend/`       | Next.js 14 app-router UI + shared dark design system |
| `deploy/compose/` | Local dev stack (Forgejo + Postgres + api + web)     |
| `docs/`           | Design notes                                         |
| `.github/`        | CI                                                   |

## Quickstart

### Dev stack (hot reload) — recommended

```bash
make up       # Postgres + Forgejo in Docker; api, dispatch & web hot-reload on the host
make down     # stop the containers (Ctrl-C stops the host processes)
```

`make up` runs `scripts/dev-up.sh`: it starts Postgres + Forgejo in Docker
(stateful/slow), waits for both, then runs the API and pipeline dispatcher with
[air](https://github.com/air-verse/air) and the frontend with `next dev` — all
hot-reloading. Web is on http://localhost:3001, api on `:8080`, Forgejo on
`:3000`. Set `QUILL_FORGEJO_ADMIN_TOKEN` (see `deploy/compose/README.md`) so
repo/PR operations work.

### Full stack (Docker, no hot reload)

```bash
make stack   # build + run Forgejo + Postgres + api + web in Docker, http://localhost:3001
make logs
make down
```

### Run pieces natively

```bash
# backend  → http://localhost:8080
make be-run

# frontend → http://localhost:3001
make fe-install
make fe-dev
```

The dashboard shows the backend version when it can reach `:8080`.

Open `http://localhost:3001`, click **Create one**, and register. The first
account created becomes the admin. Auth uses a local username/password provider
that issues Quill JWTs; set `QUILL_JWT_SECRET` in production (required) and
optionally `QUILL_JWT_TTL` (default 24h).

## Development

```bash
make build   # build backend + frontend
make test    # backend tests
make lint    # go vet + next lint
```

Requirements: Go 1.24+, Node 22+, Docker (for the stack).

## Design system

The UI reuses Forge's dark theme and design tokens (purple `#7c5cff` accent,
212px sidebar shell) as a single shared CSS system in
`frontend/app/globals.css`, so Quill, Forge, and Yaly stay visually identical.

## Roadmap

Foundation-first; each item is one focused PR.

- [x] **PR 1 — Scaffold & dev harness**
- [x] **PR 2 — Postgres schema & store** (migrations, sqlc, core tables)
- [x] **PR 3 — Auth abstraction + local provider** (JWT, middleware, login)
- [x] **PR 4 — Forgejo integration** (admin client, provisioning, identity map)
- [x] **PR 5 — Project & repo browsing** (projects, repos, file tree, branches, commits)
- [x] **PR 6 — Pull requests** (list/create/view, diff, review, merge)
- [x] **PR 7 — Branch policies** (protected branches, required reviews, merge gate)
- [ ] **PR 8 — Pipelines** (runner integration, runs, logs, status checks)

## Why Quill?

Quill is built for developers and teams who want to own their infrastructure.
There is no vendor behind a paywall, no account required on a third-party
service, and no code leaving your server. Deploy it on a €5/month VPS or an
air-gapped machine — the choice is yours.

Quill makes no call-home requests and collects no usage telemetry by default.
There are no analytics, no tracking pixels, and no external fonts or scripts
served from third-party CDNs. Your users' IP addresses and code stay on your
hardware.

## License

Apache 2.0 — see [LICENSE](LICENSE).
