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
- Git: the owner authorized agents to commit and push directly to `main` (2026-11-08).
  Since 2026-11-14 **Quill hosts and deploys itself**: the primary remote is
  `quill` = `https://git.black-tulip.nl/nuitterdijk/quill` (since 2026-11-15; `https://api.black-tulip.nl/git/...` still works) (Quill CI runs
  `.github/workflows/quill-deploy.yml` on every push to main); GitHub (`origin`) is a
  mirror — push `main` to both (`git push quill main && git push origin main`).
  **Since 2026-11-15 `main` on Quill is PR-only** (branch policy: require pull request; the
  `production` environment only deploys `main`). Agents push a branch (`git push quill agent/<topic>`)
  and open its PR with the `quill` client: from `backend/`, `QUILL_TOKEN_FILE=<file> go run ./cmd/quill pr open
  --head agent/<topic> --title ... --body-file <md>`, then `pr merge <n> --method rebase` once `./check` is green
  (the owner wants agents to merge their own work, 2026-11-15), `run watch <n> --workflow
  .github/workflows/quill-deploy.yml` for the deploy. The token comes from `quill admin issue-api-token
  --username nuitterdijk --name <label> --out <file>` (Bearer `qat_…`, acts as the owner); revoke it (delete
  its git_tokens row) after use. `quill admin open-pull` remains for when no API is reachable. After a
  merge, sync the mirror: `git fetch quill && git push origin quill/main:main`.
  Agent git credentials: `quill admin issue-git-token --username nuitterdijk --out <file>`
  (0600; use via a credential helper, delete the file and revoke the token after).
  This overrides the old "the loop commits" rule above.
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
- Hosts: `app`, `api` and (since 2026-11-15) `git.black-tulip.nl` are Serverless Containers
  (custom domains, TLS at the Scaleway edge; `git` routes to the api's git engine). The Forgejo
  VM is **gone** — its old IP `163.172.7.225` now belongs to other instances (CI workers): do not
  SSH there or trust old host keys. The VM SSH/compose notes below are historical.
  DNS records live at the Cloudflare registrar (apex `black-tulip.nl`), NOT in a
  Scaleway zone.
- VM SSH: `cd deploy/scaleway/terraform && terraform output -raw vm_ssh_private_key` —
  write to a temp file, `chmod 600`, use `ubuntu@163.172.7.225`, **delete the file after
  and never print the key**. VM stack: `sudo docker compose -f /opt/quill/docker-compose.yml ps`.
- Secrets: Secret Manager (`scw secret secret list`, `scw secret version access <id>
  revision=<n>`) holds the operator copy. Never print or commit secret values;
  `terraform.tfvars` / `.envrc` are gitignored for this reason.

### Deploy / redeploy gotchas (learned the hard way)

- Push to `main` on Quill → Quill CI (`quill-deploy.yml`, an ephemeral DEV1-S worker)
  builds images (`<sha>` + `latest` tags) and runs `terraform apply` with the
  `quill-deploy-ci` IAM key (Quill repo secrets SCW_ACCESS_KEY/SCW_SECRET_KEY); every
  other secret input comes from Secret Manager `/quill/deploy/*`
  (`deploy/scaleway/scripts/deploy-tfvars.py`). The terraform state bucket (default
  "Quill" project) has a bucket policy allowing the owner user + that application —
  add any new deploy identity there. GitHub's `deploy-scaleway.yml` is manual
  break-glass only (`gh workflow run deploy-scaleway.yml`); never run both at once
  (the state has no lock). Watch Quill deploys on the repo's Pipelines page.
- Deploys pin `image_tag` to the git SHA (see the workflow apply step): every apply changes
  `registry_image`, forcing a new container revision — changes always land. Do NOT revert to
  a pinned `latest`: apply becomes a no-op and nothing redeploys (agents kept finding
  "changes didn't land" for this reason; 2026-11-11). A manual
  `scw container container redeploy <id>` (ids via `scw container container list`) is only
  needed when editing a container straight from a local `terraform apply` that left the tag
  unchanged.
- VM compose maintenance: NEVER run bare `docker compose up -d <svc>` for a single service —
  compose honors `depends_on` and can recreate/restart OTHER services with it (a bare
  `up -d dispatch` restarted forgejo and took git.black-tulip.nl down for ~2 min on
  2026-11-11). Use `docker compose up -d --no-deps <svc>` for one service, or
  `docker compose up -d` only when you genuinely intend to reconcile the whole stack.
  The compose file interpolates `$STAMP` inside a shell command — expect an interpolation
  warning from `docker compose config`; it is benign.
- Local image builds MUST use `--platform=linux/amd64` — Scaleway rejects arm64 with
  "Invalid Image architecture" and the container goes to `error` state (check
  `error_message` in `scw container container get`).
- The deploy workflow pins the domain to `black-tulip.nl` literally — do not reintroduce
  `vars.QUILL_DOMAIN` for hosts (it still holds the pre-migration value).

### Debugging production 500s

Container logs are not reachable via the scw CLI. Fastest path: run the api **locally**
against the live database + live Forgejo — env recipe (never commit):
`QUILL_DATABASE_URL` from `terraform output -json db_urls` (key `quill`; the managed instance
`quill-pg`, since 2026-11-16 — it was Serverless SQL before; `terraform output rdb_endpoint`)
from tfvars, `FORGEJO_BASE_URL=https://git.black-tulip.nl`, `FORGEJO_ADMIN_TOKEN` from
Secret Manager (`/quill/app/app-forgejo-admin-token`), `ZITADEL_ISSUER=https://auth.black-tulip.nl`,
`QUILL_ENV=production`, plus any `QUILL_JWT_SECRET`/`QUILL_SECRET_ENCRYPTION_KEY`.
Then point tests at `QUILL_E2E_API_URL=http://localhost:18080` and read real errors in
the local log.
**Careful: a local build applies its migrations to the live database at startup.** If it carries a
migration production doesn't have yet, deployed api instances used to crash on every cold start
("no migration found for version N") — a ~15-minute outage on 2026-11-16. Since then the api
tolerates a schema that is ahead, but still: only run a local build with new migrations against
production when you're about to deploy that same change, and check `/readyz` afterwards.
Serverless logs ARE reachable now: `SCW_DEFAULT_PROJECT_ID=<namespace project>
scw container container logs <container-id> time-span=30m` (stream; kill it after a few seconds).

### Login look and feel

Zitadel serves the login pages, so they cannot share Quill's CSS. Its label policy (palette, logo,
Geist font, dark theme) and login texts are set by an idempotent script:
`ZITADEL_MANAGEMENT_TOKEN=<PAT> python3 deploy/scaleway/scripts/zitadel-branding.py` (PAT = Secret
Manager `app-zitadel-management-token`; Cloudflare rejects Python's default User-Agent, the script
sets its own). Run it after changing Quill's palette (`frontend/app/globals.css`) or restoring a
Zitadel database into a fresh instance. The flow for a new user is: `/sign-in` auto-starts the
login (no button screen) → Zitadel login name → password → one skippable 2-factor offer (skip
lifetime 10 years) → Quill onboarding (no username step; the handle is derived and editable in
Settings). `/sign-in?error=…` shows a card with a button instead of auto-starting, so a failing
login cannot loop. SSO identity orgs inherit the instance label policy.

### Live e2e tests

`backend/tests/live/` (Go, stdlib only) drives the deployed platform: headless Zitadel
login (PKCE; the login UI is stepwise — POST `/ui/login/loginname` with the **email** as
loginName, then `/ui/login/password`, then `/ui/login/mfa/prompt` with `skip=true`, each
with the page's rotating `gorilla.csrf.Token` + `authRequestID`; Zitadel may serve the
login page inline with 200 or redirect to it — the helper accepts both), then the Quill
API, git clone/push, PRs, pipelines. Run:
`QUILL_LIVE_TEST=1 QUILL_E2E_CLIENT_ID=<zitadel client id> QUILL_E2E_PROJECT_ID=<project id> QUILL_E2E_USER=<email> QUILL_E2E_PASSWORD=<pw> go test ./tests/live/ -v`
Skipped unless `QUILL_LIVE_TEST=1`, so `./check`/CI never run them. **After each live round, clean up the test debris:**
`QUILL_DATABASE_URL=<db dsn> FORGEJO_BASE_URL=https://git.black-tulip.nl FORGEJO_ADMIN_TOKEN=<token> ZITADEL_ISSUER=https://auth.black-tulip.nl QUILL_SECRET_ENCRYPTION_KEY=<key> QUILL_ENV=production go run ./cmd/quill admin purge-e2e --confirm`
(re-run until counts are zero; it deletes `e2e-`-prefixed orgs/projects/repos only —
never users, personal projects, or real content). Add `ZITADEL_MANAGEMENT_TOKEN` (Secret Manager
`app-zitadel-management-token`) so test orgs' SSO identity orgs in Zitadel are deleted too.
SSO live test: `QUILL_E2E_SSO_UPSTREAM` = Secret Manager `app-e2e-sso-upstream` (JSON fixture).

**Agent credentials recipe** (agents can and should run the live suite + signed-in
web-session verification themselves):

- `QUILL_E2E_USER` / `QUILL_E2E_PASSWORD`: the fixture account
  `e2e-tester@black-tulip.nl`; its password lives in Secret Manager as
  `/quill/app/app-e2e-user-password` (`scw secret version access <id> revision=latest`
  returns base64 — decode with `base64 -d`). If it ever needs resetting: the Zitadel
  mgmt PAT is in Secret Manager `app-zitadel-management-token` (also base64 on access),
  then `POST /management/v1/users/<userId>/password` with the PAT as bearer
  (`userId` from `POST /v2/users` search), then complete the forced password change by
  logging in once through the UI flow (Zitadel then requires a fresh sign-in).
- `QUILL_E2E_CLIENT_ID`: the public-PKCE probe app `quill-e2e-probe`
  (`399023859392643080`); the web app's own client is confidential and its secret
  is unreadable (GH secret / hashed container env). `QUILL_E2E_PROJECT_ID`: the
  GitHub repo variable `NEXT_PUBLIC_ZITADEL_PROJECT_ID` (`gh variable list`).
- Signed-in web verification without a browser: start the app's own Auth.js OIDC flow
  (`GET /api/auth/csrf` → `POST /api/auth/signin/zitadel` with the csrfToken), complete
  the Zitadel stepwise login inside that flow, land the callback on the app, and the
  session cookie jar holds a signed-in browser session — GET any page (e.g. `/pulls`)
  and inspect the real HTML. Reference implementation used 2026-11-09: the raw API
  dance in `tests/live/live_test.go` overlayed on the app's `/api/auth/signin` chain.
- Secret Manager `version access` returns values base64-encoded (double-encoded for
  values written via `data=-`); the console/TF state carry the raw values. The
  `app-forgejo-admin-token` and `app-zitadel-management-token` Secret Manager copies
  were re-stored cleanly on 2026-11-09.

### Cost watch

Current drivers (billing via `scw billing consumption list`): the managed PostgreSQL `quill-pg`
(db-dev-s, flat ≈€11.4/month since 2026-11-16 — it replaced Serverless SQL databases that billed
≈€50/month at the November rate — most likely because Zitadel's background queries kept them awake, not verified; heavy agent testing contributed), Serverless Containers (≈€15 CPU + €3.4 memory month-to-date on 2026-11-16 — the next biggest
line; the cause is not yet investigated), and the DEV1-S CI worker instances (billed per run).
The stale pending
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
