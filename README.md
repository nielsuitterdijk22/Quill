# Quill

**Quill** is a version-control platform for platform teams — code browsing, pull
requests, branch policies, and pipelines — built as a clean layer on top of
[Forgejo](https://forgejo.org/). Forgejo runs as a separate service and owns git
storage and low-level repo/PR operations; Quill wraps its REST API and adds the
platform layer (orgs, teams, branch policies, pipelines, pluggable auth) with its
own Postgres for metadata.

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
  REST API. Quill stores only what Forgejo can't: orgs/teams, ownership, branch
  policies, pipeline config, and auth identity mapping.
- **Auth is abstracted.** A local username/password provider issues Quill JWTs
  today; Keycloak / Entra / GitHub OIDC drop in behind the same `AuthProvider`
  interface later.
- **Namespaces are shallow.** Org → Repo with Teams and an explicit owning team.
  The schema supports nesting (`parent_id`) so groups can be enabled later
  without a painful migration; discovery is handled by the catalog (Yaly).

## Repository layout

| Path               | What                                                        |
| ------------------ | ----------------------------------------------------------- |
| `backend/`         | Go API (module `github.com/nielsuitterdijk22/quill`)        |
| `frontend/`        | Next.js 14 app-router UI + shared dark design system        |
| `deploy/compose/`  | Local dev stack (Forgejo + Postgres + api + web)            |
| `docs/`            | Design notes                                                |
| `.github/`         | CI                                                          |

## Quickstart

### Full stack (Docker)

```bash
make up      # Forgejo + Postgres + api + web, http://localhost:3001
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
- [ ] **PR 4 — Forgejo integration** (admin client, provisioning, identity map)
- [ ] **PR 5 — Org & repo browsing** (orgs, repos, file tree, branches, commits)
- [ ] **PR 6 — Pull requests** (list/create/view, diff, review, merge)
- [ ] **PR 7 — Branch policies** (protected branches, required reviews/checks)
- [ ] **PR 8 — Pipelines** (runner integration, runs, logs, status checks)

## License

Apache 2.0 — see [LICENSE](LICENSE).
