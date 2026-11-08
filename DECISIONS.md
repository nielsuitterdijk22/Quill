- 2026-11-06 — Service URLs moved from `*.quill.black-tulip.nl` to `*.black-tulip.nl` (apex of black-tulip.nl): `domain = "black-tulip.nl"` in `terraform.tfvars` / `.envrc`, and the CI repo variables must be set to `QUILL_DOMAIN=black-tulip.nl` and `NEXT_PUBLIC_ZITADEL_ISSUER=https://auth.black-tulip.nl` (the latter is a build-arg baked into the web image — the running image still has the old issuer until the workflow is re-run). Everything derives from `var.domain` (locals: app/api/git/auth/ssh hosts → container domains, serverless env incl. ZITADEL_ISSUER/CORS/FORGEJO URLs, VM cloud-init Caddy/Forgejo/Zitadel), so no other code changes were needed; `deploy/scaleway/README.md` registrar table and the `zitadel-smtp.sh` examples were updated. GitHub OAuth creds are now passed in CI as `-var "github_client_id=${{ secrets.GH_CLIENT_ID }}"` / `-var "github_client_secret=${{ secrets.GH_CLIENT_SECRET }}"` in `deploy-scaleway.yml` (GitHub forbids the `GITHUB_` prefix on secret names, hence `GH_`); the OAuth app callback must be `https://api.black-tulip.nl/api/v1/auth/github/callback`. Manual steps around a domain change: add the `api`/`app` CNAME and `git`/`ssh`/`auth` A records at Cloudflare (black-tulip.nl apex) BEFORE deploying (container-domain certs + Caddy HTTP-01 need them), run the deploy via workflow_dispatch (vm_changed is a path filter on vm files, so a var-only change does not re-render the VM), update the Forgejo system webhook URL and the Zitadel OIDC app redirect URIs to the new hosts, and remove the old `*.quill` records. Accepted breakage: existing git clone URLs (`git.quill.black-tulip.nl`) and Zitadel-issued sessions die with the old hostnames. Rejected: keeping `quill.black-tulip.nl` as an alias (would need extra Caddy vhosts + extra certs for no benefit at MVP scale).
- 2026-11-08 — Owner rules for coding agents (recorded in AGENTS.md's operations handbook): agents have full operator access to the Scaleway deployment (terraform apply, container redeploys, VM SSH — reversible and logged here), may commit and push directly to main (CI deploys from main; CI workflow edits allowed when needed but minimal), and priorities are features > cost > safety with a Scaleway budget of €20/mo — no users yet, goal is a working product. MISSION.md's must-never rules (tenant isolation, no credential leaks) stay hard constraints. Rationale: the owner wants speed to a working MVP over ceremony; the earlier "loop commits / never touch CI" defaults slowed deploys by a day.
- 2026-11-06 — Onboarding bugs on the Scaleway deployment had two root causes, fixed on main. (1) Profile name: the deployed api image predated the rename fixes, and its `GITHUB_CLIENT_ID`/`GITHUB_CLIENT_SECRET` env were the literal placeholder `x` (confirmed via the deployed container's env), so the OAuth start redirected to github.com with `client_id=x` — the GitHub app was never configured, not expired. Fixes: `config.Load` now treats any obvious placeholder ("unset", "x", "changeme", …) as unconfigured for both GitHub vars; the OAuth start/callback redirect back to `/onboarding?github_error=<code>` instead of a raw 501/502 JSON page, and the onboarding page renders a friendly message for that code; terraform now validates `github_client_id` is "unset" or Iv-prefixed and rejects placeholder secrets at plan time, so a dummy value can never deploy silently again; `username_test.go` was updated to the decided post-onboarding rename behavior (its old "locked after first project" assertion contradicted the 2026-11-06 decision below). To actually enable import, the owner sets real `github_client_id`/`github_client_secret` in `terraform.tfvars` (gitignored; see `terraform.tfvars.example` for the required callback URL `https://api.<domain>/api/v1/auth/github/callback`) and re-applies. Rejected: passing the vars from CI (CI workflows are frozen by the working rules) and rejecting non-Iv client IDs at runtime (GitHub App client IDs have different prefixes).
- 2026-11-06 — Username is changeable after onboarding. SetUsername used to refuse
  once the user had any project ("onboarding only"), but the personal project is
  created at first login, so in practice the handle was frozen forever. The guard
  is removed; a rename now cascades to the personal project slug (new
  UpdateProjectSlug query) and the Forgejo user (admin rename). The Forgejo ORG
  cannot be renamed via the API, so existing git clone URLs keep the original
  owner while Quill's /{slug} routing follows the new name. Settings page gained
  a username field (PATCH /api/v1/auth/me/username). Rejected: leaving the guard
  (user-visible dead end) and renaming the Forgejo org (unsupported by the API).
- 2026-11-06 — scw CLI v2.65 dropped the per-command `region=` arg (region comes
  from SCW_DEFAULT_REGION). The pinned v2.42.0 asset 404'd, so CI installs v2.65.2
  and dns-sync.sh/provision.sh no longer pass region= explicitly.

# Decisions

Append-only log of design decisions: date, decision, why, alternatives rejected.

- 2026-11-05 — Transactional email goes through the existing Brevo account
  (`smtp-relay.brevo.com:587`, STARTTLS) for both Zitadel (invites/activation/
  password reset) and Forgejo (PR notifications). Zitadel's
  `ZITADEL_DEFAULTINSTANCE_SMTPCONFIGURATION_*` env vars are only read at instance
  creation, so on the already-running instance CI applies the provider via the
  admin API (`PUT /admin/v1/email/smtp/{id}` auto-activates, else
  `POST /admin/v1/email/smtp` + `POST /admin/v1/email/{id}/_activate`) with the
  IAM-owner machine PAT (`ZITADEL_MANAGEMENT_TOKEN`); `scripts/zitadel-smtp.sh`
  is idempotent. `TLS=true` = implicit TLS with automatic STARTTLS fallback,
  which covers Brevo 587 (and 465). Forgejo gets `[mailer]` env via a
  `docker-compose.override.yml` written by the `vm-deploy` job (cloud-init only
  runs on first boot). Sending domain must be authenticated in Brevo with
  SPF/DKIM(/DMARC) at the registrar. Rejected: Scaleway Transactional Email
  (another account/service to manage; Brevo account already exists), running a
  local MailHog (no real delivery, no use for the 2-user MVP), SES (non-EU).

- 2026-10-07 — T-000 required no code changes: the `./check` failure was
  environmental (sandbox without npm-registry access left a stale, wrong-platform
  `frontend/node_modules`, so the `@next/swc-linux-arm64-gnu` binary was missing
  and `next build` crashed). Fixed by re-running `npm ci` with registry access.
  Rejected alternative: editing `./check` to skip the frontend build — forbidden
  by the working rules and would have hidden real regressions.

- 2026-10-13 — T-026: the new `plan` column is `TEXT NOT NULL DEFAULT 'internal'`
  rather than a Postgres enum or a CHECK-constrained set. Why: the mission only
  names two tiers (free individual, usage-based enterprise) but the vendor will
  provision per-tenant plans by hand; a plain text column with a safe default
  keeps the schema open to new plan names without further migrations, and
  'internal' is the honest label for everything already seeded (personal
  tenants, the 'default' tenant) until billing plans land. Rejected alternatives:
  `CREATE TYPE tenant_plan AS ENUM` (every new plan needs a migration and
  `ALTER TYPE` is awkward in Postgres) and CHECK IN (...) (same rigidity, less
  readable).

- 2026-10-13 — T-021: the `quill admin create-repo` CLI resolves a tenant's
  Forgejo org as the `forgejo_org_name` of the tenant's first project, falling
  back to the tenant slug, then self-heals via `forgejo.GetOrCreateOrg`. Why:
  the `tenants` table has no Forgejo column by design (1:1 tenant→project→org
  mapping lives on `projects`), and `GetOrCreateOrg` mirrors the existing
  `platform.ensureForgejoOrg` recovery path. Rejected alternatives: adding a
  `forgejo_org_name` column to `tenants` (duplicates the project column, breaks
  the 1:1 invariant when a tenant has multiple projects) and a new
   `tenant_organizations` join table (over-engineered for the flat MVP model).
   The tenant→project lookup uses a hand-written `Store.ListProjectsByTenant`
   (raw SQL in `store.go`, like `ListRunsByCommitSHA`) rather than a sqlc
   re-run, to keep the change small.

- 2026-10-13 — T-022: `quill admin seed-file` creates-or-updates a file via the
  Forgejo contents API. The create/update split lives in a new
  `forgejo.Client.SeedFile`: it fetches the current contents first, then PUTs
  (carrying the existing blob SHA, the API's precondition for a replace) when
  the file exists or POSTs when it does not. Why: the contents API has no
  upsert — a POST on an existing path 409s and a PUT without the current SHA
  409s — so the read-then-branch is the only correct idempotent primitive, and
  it lets the same command seed a fresh repo and refresh an existing file.
  The CLI command (`cmd/quill/seed_file.go`) takes `--content` (inline) or
  `--file` (path, or `-` for stdin) and commits as "Seed <path> (admin
  seed-file)". It reuses `create-repo`'s tenant/org resolution (first project's
  `forgejo_org_name`, else tenant slug) and self-heals a missing repo/org the
  same way. The command depends on a small `seedForgejoLike` interface (not the
  concrete client) so the unit test can substitute an in-memory double; the
  real `*forgejo.Client` satisfies it. Rejected alternatives: a separate
  `admin create-file` + `admin update-file` pair (two commands for one intent)
  and a raw `git`-push path (bypasses the Forgejo-API-only golden rule and the
  admin token).


- 2026-10-13 — T-028: `quill admin seed-file` now persists the `repositories`
  row when it self-heals. After the Forgejo repo is resolved (fetched or
  created), `seedFile` calls the existing `recordRepo` helper — the same
  idempotent upsert `createRepo` uses — so a repo that exists in Forgejo but
  was never registered in Postgres gets a row (or its existing row is
  refreshed with the forgejo link). Why: the owner's 2026-10-13 feedback
  confirmed the open question from T-022; persisting here keeps the
  `repositories` table as the source of truth without a separate
  registration step. The row uses `slug` for both `slug` and `name`
  (seed-file has no display name), visibility `private` (matching the
  self-healed org default), and the canonical slug form from
  `normalizeRepoName`, which is the `repo_slug` half of the suite's
  `(repo_slug, quill_pr_number)` PR identity contract. Rejected
  alternatives: a dedicated `UpsertRepository` query (duplicates
  `recordRepo`'s get-then-branch logic) and persisting only when the repo
  was created (leaves the pre-existing-but-unregistered case broken, which
  is exactly the reported bug).

- 2026-11-05 — Forgejo's database moved off the serverless SQL DB onto the VM: a new `forgejo-db` postgres:16-alpine container (named volume `forgejo-pgdata`, user/db `forgejo`, password from a new `random_password.forgejo_db` stored in Secret Manager at `/quill/forgejo/forgejo-db-password`) in the VM compose stack, and the `forgejo` SDB was removed from `sdb_names` so Terraform destroys it. Why: 2026-11 billing showed "Serverless SQL Database - CPU consumption" at EUR 4.29 in 5.9 days (~EUR 22/mo) because Forgejo's persistent connection pool + cron queries kept the `forgejo` SDB at ~0.22 vCPU 24/7 (min_cpu=0 only helps when there are no connections); the `quill` SDB does idle to zero (api container has min_scale=0) and stays. Data was migrated live (pg_dump of the SDB -> restore into forgejo-db, table counts compared) before the SDB was destroyed; Forgejo DB backups are covered by the existing forgejo-backup sidecar (`forgejo dump` includes the database dump), so the pg-backup job now only dumps `quill`. Projected total bill ~EUR 5/mo. Rejected alternatives: keeping Forgejo on the SDB and lowering `sdb_max_cpu` (the charge is for time-at-CPU, not peak — a 24/7 connection pool bills regardless of the cap), a Scaleway RDB dev node (still a fixed ~EUR 10+/mo and more ops than a container on a VM we already pay for), and moving Forgejo itself off the VM (git data needs a durable disk + CI needs a Docker daemon — the reason the VM exists).

- 2026-11-04 — Cloud-native Scaleway deployment lives in `deploy/scaleway/` (Terraform + Kubernetes manifests + scripts + CI workflow `deploy-scaleway.yml`). Architecture: Kapsule (Kubernetes, Cilium, Traefik ingress with cert-manager/Let's Encrypt DNS-01 via Scaleway DNS) runs web/api/dispatch/forgejo/zitadel; RDB PostgreSQL-16 on the same VPC private network holds the `quill`, `forgejo`, `zitadel` databases; Object Storage is the backup target; Container Registry holds the images; Secret Manager holds all credentials (the k8s Secret is synced from it, never committed); IAM apps `quill-ci-deployer` and `quill-backup-writer` carry scoped API keys. Region pinned to fr-par per SUITE.md EU residency. Why: the owner asked to bring the MVP back online cloud-native on Scaleway; the compose stack remains for local dev. Rejected alternatives: keeping the single-VM SSH deploy (the previous state that was lost), Helm charts (raw rendered manifests + envsubst keep the deploy reviewable without a chart toolchain), in-cluster Postgres (managed RDB gives backups/HA for less ops). Backups are three layers: nightly pg_dump CronJob + forgejo dump sidecar uploading to Object Storage with retention pruning, RDB managed backups, and the Forgejo PVC covered by the dump.

- 2026-11-04 — Nested groups (migration 000020): `groups` (tenant-scoped, `parent_id` self-reference, `depth` 1..5 enforced by CHECK and the service), `group_members`, and `group_project_roles`. Effective project role is computed by one recursive-CTE query (`GetEffectiveProjectRole`): direct `project_members` role ∪ roles of all effective groups (direct memberships + all ancestors), precedence owner > member. `authorizeProjectMember`/`authorizeProjectAdmin` now use it, so a department group granted `owner` on a project makes every team subgroup's members owners. API under `/api/v1/orgs/{slug}/groups/...`, UI in org settings. Why: the hard req "Groups (nested for department and teams)" + mission's nested permission inheritance. Deferred: tier-based depth limit (free tier depth 1 — backlog T-003) and SCIM group sync; both are follow-ups.

- 2026-11-04 — Billing MVP: usage metering + plan management, no payment rail yet. `GET /api/v1/orgs/{slug}/billing` (tenant admin) returns plan, metered usage (repo count and finished pipeline-run minutes from Postgres; git storage as the sum of Forgejo repo sizes when the git backend is enabled), and hard limits for the `free` plan (50 repos / 1 GiB / 100 pipeline min). `PUT .../billing/plan` is platform-admin only and audit-logged (`billing.plan_changed`). Why: "Billing" is a hard req for the MVP, but Stripe keys don't exist yet, so the honest MVP slice is metering + plan + audit trail; charging/invoicing via Stripe webhook is the follow-up once keys are provisioned.

- 2026-11-04 — Fixed a latent nil-pointer panic: `platform.Service.resolveRepoForWebhook` dereferenced a nil store when the server is built without a store (handler tests) while Forgejo is enabled (e.g. local shell with FORGEJO_ADMIN_TOKEN set). It now returns `ErrUnavailable`, which the webhook handler acks with 200 — matching the documented contract that a store-less server keeps serving webhooks untouched. Also gofmt'd `cmd/quill/create_repo_test.go` (CI's gofmt gate fails on it; `./check` swallows gofmt failures).

- 2026-11-04 — Serverless rework of deploy/scaleway (replaces the Kapsule/RDB revision, which is in git history): for near-zero traffic the stack runs as 3 scale-to-zero Serverless SQL databases (`scaleway_sdb_sql_database`, min_cpu=0 — free compute when idle, 7 days of managed backups included, storage-only charge), the api/web/zitadel as Serverless Containers with min_scale=0 behind custom domains (api/app/auth.<domain>, TLS at the Scaleway edge), a nightly pg_dump Serverless Job → Object Storage, and ONE DEV1-S VM for Forgejo + dispatch + Caddy (git data needs a durable disk, CI needs a Docker daemon — the two things serverless can't do) with git/ssh A records pointed at its dynamic IP. SDB credentials are an IAM user id + API-key secret (Scaleway's documented SDB auth model), provisioned via `scaleway_iam_user` + `scaleway_iam_api_key`. Estimated ≈ €9–10/mo vs ≈ €90–100/mo for the Kapsule revision. CI deploys images then `terraform apply -var image_tag=<sha>` (state in the backup bucket via the s3 backend); the VM stack deploys over SSH when vm files change. Trade-offs accepted for MVP: dispatch API port 8090 on the VM is public but HMAC-protected; Zitadel machine PAT is harvested from container logs on first boot; SSH open to ssh_cidrs (tighten later).

- 2026-11-05 — `scripts/dns-sync.sh` moved from the removed `scw domain zone/record` commands (dropped in scaleway-cli v2.65, which CI installs — the DNS sync job was failing with usage output) to the `scw dns zone/record` namespace, and now no-ops with a clear message when the Scaleway zone for the domain is not `active`. Why: `create_dns_zone=false` in terraform.tfvars — black-tulip.nl stays on Cloudflare NS and the live records (api/app CNAMEs, git/ssh/auth A) are managed at the registrar, so the Scaleway zone `quill.black-tulip.nl` sits `pending` and is not served; writing git/ssh A records there never affected live DNS. The upsert path (`scw dns record set <zone> name=.. type=A values.0=..`, which is add-or-replace) remains for the day the zone gets delegated. Rejected: syncing Cloudflare via API (no CF token in the repo; VM IP has been stable, and the registrar records are a one-time manual setup documented in deploy/scaleway/README.md).

- 2026-11-06 — `forgejo.Client.RenameUser` now sends the new name as `application/x-www-form-urlencoded` (`new_name=...`) via a new `doForm` helper in `client.go`. Forgejo 15.0.11+gitea-1.22.0 binds `POST /admin/users/{username}/rename` from form values only: a JSON body is rejected with 422 `[NewName]: Required`, which surfaced to users as a generic 500 "could not update username" on `PATCH /api/v1/auth/me/username`. Verified live against git.quill.black-tulip.nl (form body -> 204, JSON body -> 422), with unit tests pinning the Content-Type. Also in the same fix: `config.Load` normalizes the terraform placeholder `unset` for `GITHUB_CLIENT_ID`/`GITHUB_CLIENT_SECRET` to empty, so `GET /api/v1/auth/github` returns a clear 501 "GitHub OAuth is not configured" instead of redirecting to github.com with `client_id=unset`. Root cause of the "broken GH creds": the vars were never in `terraform.tfvars` (only the `"unset"` default), so the deployed api container held the placeholder; the vars are now documented in `terraform.tfvars.example` with the required OAuth callback URL (`https://api.<domain>/api/v1/auth/github/callback`).
