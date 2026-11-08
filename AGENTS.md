# Repository guidance for coding agents

Quill is a VCS platform (Go backend + Next.js frontend) layered on Forgejo. This
file orients automated contributors. Keep changes small, typed, and tested.

## Golden rules

- **Forgejo is wrapped, never forked.** Git/repo/PR primitives go through Forgejo's
  REST API (`internal/forgejo`, added in PR 4). Quill's Postgres holds only the
  platform metadata Forgejo can't: tenants, projects, ownership, branch policies,
  pipelines, and auth identity mapping.
- **Auth stays behind an interface.** Never call a specific provider directly;
  go through `AuthProvider` (PR 3) so OIDC providers drop in later.
- **Flat MVP model: Tenant → Project → Resource.** A Tenant is the billing/SSO
  boundary; a Project is a team/app namespace that owns repositories and
  pipelines directly (no teams layer). Each project maps 1:1 to a Forgejo org.
  Cross-cutting views filter by the current project (sidebar switcher, cookie
  `quill_current_project`).
- **One shared design system.** Style with the classes in
  `frontend/app/globals.css` (ported from Forge). Don't add Tailwind or inline
  ad-hoc styles; extend the system with new classes instead.

## Backend (`backend/`)

- Module: `github.com/nielsuitterdijk22/quill`. Go 1.24, stdlib `log/slog`.
- Stack: `chi` router, `pgx` + `sqlc` (typed queries), `golang-migrate`.
- Layout: `cmd/api` entrypoint; `internal/{config,logging,server,httpx,...}`.
- Before pushing: `make be-fmt be-vet be-test` (CI enforces `gofmt`, `go vet`,
  `go build`, `go test`).

## Frontend (`frontend/`)

- Next.js 14 app router, TypeScript, no Tailwind.
- Server components call the backend via `app/lib/api.ts`
  (`QUILL_API_BASE_URL`); browser calls use the `/api/backend/*` rewrite.
- The authenticated shell lives in the `app/(app)` route group; auth-less pages
  (e.g. `/login`) sit outside it.
- Before pushing: `make fe-lint fe-build`.

## Workflow

- Foundation-first roadmap in `README.md`. One focused PR per item, each with a
  task-list checklist; keep `main` green.
- Local stack: `make up` (Postgres + Forgejo in Docker; api, dispatch & web
  hot-reload on the host) or `make stack` (full containerised stack). See
  `deploy/compose/README.md`.

## Working rules for agents

These rules apply to every coding agent in this repository. The foundry loop enforces the
ones marked **(enforced)**: breaking them throws the work away.

### Read order, every session
1. `FEEDBACK.md`: the owner's latest feedback. It overrides everything except the mission.
2. `SUITE.md` (if present: the product family this repo belongs to), `MISSION.md`,
   then this file, then `DECISIONS.md`.
3. Your task (in `.foundry/task.md` when run by foundry, otherwise the top of `BACKLOG.md`).

### Owner documents
- `SUITE.md`, `MISSION.md`, `FEATURES.md`, `USER_STORIES.md` belong to the owner. Never edit them. **(enforced)**
- `BACKLOG.md` and `JOURNAL.md` are maintained by the foundry loop; don't edit them during a task.
- `FEEDBACK.md` is written by the owner; read it, don't edit it.

### How to work
- One task per session. Keep the diff small and on-topic.
- Tests first: encode the task's acceptance check as tests, see them fail, then implement.
- `./check` is the only definition of done. It must exit 0 before you finish.
- Never delete, skip or weaken existing tests or assertions unless the task says
  "Tests may change: yes". **(enforced)**
- Never change `./check` or linter/type-checker configuration. **(enforced)** CI workflow
  edits are allowed when a deploy needs them (owner, 2026-11-08) — keep them minimal and
  YAML-validate before pushing.
- Record non-obvious design decisions in `DECISIONS.md`: date, decision, why, and the alternatives you rejected.
- Prefer the standard library and existing dependencies. Add a dependency only if it saves real work,
  and note it in `DECISIONS.md`.
- Git: the owner authorized agents to commit and push directly to `main` (2026-11-08);
  CI (`.github/workflows/deploy-scaleway.yml`) deploys from main. This overrides the old
  "the loop commits" rule above.
- When something is ambiguous, pick the option most consistent with MISSION.md and
  write the question in your summary instead of stalling.

### Finishing a session
Write `.foundry/summary.md` with `## Done`, `## Questions`, and `## Follow-ups` sections.

## Operations handbook (Scaleway production, decided by the owner 2026-11-08)

The owner granted agents **full operator access** to the production deployment: reading
state, `terraform apply` with the direnv credentials, SSH to the VM, container redeploys —
as long as changes are reversible and recorded in `DECISIONS.md`. Priorities when
trade-offs arise: **features > cost > safety**; there are no users yet and we want a
working product; Scaleway budget is **€20/month**. The MISSION.md "must never happen"
rules (tenant isolation, no credential leaks) remain hard constraints regardless.

### Access
- `source .envrc` in the repo root (direnv; gitignored) for `SCW_*` credentials.
  scw CLI v2.65 is installed; region comes from `SCW_DEFAULT_REGION=fr-par` (v2.65 dropped
  the per-command `region=` arg).
- Hosts: `app.black-tulip.nl` / `api.black-tulip.nl` are Serverless Containers (custom
  domains, TLS at the Scaleway edge); `git`/`ssh`/`auth.black-tulip.nl` are A records to
  the Forgejo VM `163.172.7.225` (Caddy terminates TLS, proxies to forgejo/zitadel).
  DNS records live at the Cloudflare registrar (apex `black-tulip.nl`), NOT in a
  Scaleway zone.
- VM SSH: `cd deploy/scaleway/terraform && terraform output -raw vm_ssh_private_key` —
  write to a temp file, `chmod 600`, use `ubuntu@163.172.7.225`, **delete the file after
  and never print the key**. VM stack: `sudo docker compose -f /opt/quill/docker-compose.yml ps`.
- Secrets: Secret Manager (`scw secret secret list`, `scw secret version access <id>
  revision=<n>`) holds the operator copy. Never print or commit secret values;
  `terraform.tfvars` / `.envrc` are gitignored for this reason.

### Deploy / redeploy gotchas (learned the hard way)
- Push to `main` → CI builds images (`<sha>` + `latest` tags) and runs `terraform apply`.
- Serverless containers do **not** reliably auto-redeploy on tag push: after CI finishes,
  run `scw container container redeploy <container-id>` (ids from `scw container container list`)
  and wait ~2 min before testing.
- `latest` can race (an older commit's CI run finishing last overwrites it) — compare the
  `latest` digest with the `<sha>` tag digest of HEAD before trusting it.
- Local image builds MUST use `--platform=linux/amd64` — Scaleway rejects arm64 with
  "Invalid Image architecture" and the container goes to `error` state (check
  `error_message` in `scw container container get`).
- The deploy workflow pins the domain to `black-tulip.nl` literally — do not reintroduce
  `vars.QUILL_DOMAIN` for hosts (it still holds the pre-migration value).

### Debugging production 500s
Container logs are not reachable via the scw CLI. Fastest path: run the api **locally**
against the live SDB + live Forgejo — env recipe (never commit):
`QUILL_DATABASE_URL` from `terraform output sdb_endpoints` + `sdb_user`/`sdb_password`
from tfvars, `FORGEJO_BASE_URL=https://git.black-tulip.nl`, `FORGEJO_ADMIN_TOKEN` from
Secret Manager (`/quill/app/app-forgejo-admin-token`), `ZITADEL_ISSUER=https://auth.black-tulip.nl`,
`QUILL_ENV=production`, plus any `QUILL_JWT_SECRET`/`QUILL_SECRET_ENCRYPTION_KEY`.
Then point tests at `QUILL_E2E_API_URL=http://localhost:18080` and read real errors in
the local log.

### Live e2e tests
`backend/tests/live/` (Go, stdlib only) drives the deployed platform: headless Zitadel
login (PKCE; the login UI is stepwise — POST `/ui/login/loginname` with the **email** as
loginName, then `/ui/login/password`, then `/ui/login/mfa/prompt` with `skip=true`, each
with the page's rotating `gorilla.csrf.Token` + `authRequestID`), then the Quill API,
git clone/push, PRs, pipelines. Run:
`QUILL_LIVE_TEST=1 QUILL_E2E_CLIENT_ID=<zitadel client id> QUILL_E2E_PROJECT_ID=<project id> QUILL_E2E_USER=<email> QUILL_E2E_PASSWORD=<pw> go test ./tests/live/ -v`
Skipped unless `QUILL_LIVE_TEST=1`, so `./check`/CI never run them. Test users are
created via the Zitadel mgmt API on the VM (`/v2/users/human`, Host header must be
`auth.black-tulip.nl`).

### Cost watch
Current drivers (billing via `scw billing consumption list`): DEV1-S VM (fixed, required
for git/CI), `quill` Serverless SQL DB CPU (billed only while the api container is scaled
up — api has `min_scale=0`), containers mostly inside the free tier. The stale pending
Scaleway zone `quill.black-tulip.nl` costs ~€0.36/mo and cannot be deleted via API
(root-zone protection) — inert. Don't leave cron watch-loops running (they kept the api
container hot and burned SDB CPU); cancel them (`cron list` / `cron cancel` in the agent
tooling) once a check is green.

### Known open items
- GitHub repo import: OAuth creds are live (the api 302s to github.com) and the
  cross-host cookie bug is fixed (2026-11-08): the flow cookies carry
  `Domain=black-tulip.nl` via `QUILL_COOKIE_DOMAIN` (terraform sets it from
  `var.domain`) because the flow starts on the `app` host via the frontend
  rewrite while the OAuth app callback lands on the `api` host. The GitHub OAuth
  app callback must stay `https://api.black-tulip.nl/api/v1/auth/github/callback`.
  Still unverified with a real GitHub authorization end-to-end (repo list +
  import); check as soon as a GitHub account is available. If you ever own the
  GitHub OAuth app settings, the cleaner design is registering the callback on
  the app host (`app.black-tulip.nl/api/backend/auth/github/callback`) and
  dropping the cookie-domain workaround.
- `/projects/[project]/repos` has no index page (404) — repos live under `/repositories`
  and `/{owner}/{repo}`.
- The `Docs` workflow fails on pre-existing markdownlint violations and the
  `check` workflow fails at `go install golangci-lint@latest` (both red since
  before 2026-11-08); `./check`'s gofmt step swallows its own failures
  (`(...) || true` shell bug) — CI catches what it misses.
