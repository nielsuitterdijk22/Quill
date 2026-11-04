# Decisions

Append-only log of design decisions: date, decision, why, alternatives rejected.

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

- 2026-11-04 — Cloud-native Scaleway deployment lives in `deploy/scaleway/` (Terraform + Kubernetes manifests + scripts + CI workflow `deploy-scaleway.yml`). Architecture: Kapsule (Kubernetes, Cilium, Traefik ingress with cert-manager/Let's Encrypt DNS-01 via Scaleway DNS) runs web/api/dispatch/forgejo/zitadel; RDB PostgreSQL-16 on the same VPC private network holds the `quill`, `forgejo`, `zitadel` databases; Object Storage is the backup target; Container Registry holds the images; Secret Manager holds all credentials (the k8s Secret is synced from it, never committed); IAM apps `quill-ci-deployer` and `quill-backup-writer` carry scoped API keys. Region pinned to fr-par per SUITE.md EU residency. Why: the owner asked to bring the MVP back online cloud-native on Scaleway; the compose stack remains for local dev. Rejected alternatives: keeping the single-VM SSH deploy (the previous state that was lost), Helm charts (raw rendered manifests + envsubst keep the deploy reviewable without a chart toolchain), in-cluster Postgres (managed RDB gives backups/HA for less ops). Backups are three layers: nightly pg_dump CronJob + forgejo dump sidecar uploading to Object Storage with retention pruning, RDB managed backups, and the Forgejo PVC covered by the dump.

- 2026-11-04 — Nested groups (migration 000020): `groups` (tenant-scoped, `parent_id` self-reference, `depth` 1..5 enforced by CHECK and the service), `group_members`, and `group_project_roles`. Effective project role is computed by one recursive-CTE query (`GetEffectiveProjectRole`): direct `project_members` role ∪ roles of all effective groups (direct memberships + all ancestors), precedence owner > member. `authorizeProjectMember`/`authorizeProjectAdmin` now use it, so a department group granted `owner` on a project makes every team subgroup's members owners. API under `/api/v1/orgs/{slug}/groups/...`, UI in org settings. Why: the hard req "Groups (nested for department and teams)" + mission's nested permission inheritance. Deferred: tier-based depth limit (free tier depth 1 — backlog T-003) and SCIM group sync; both are follow-ups.

- 2026-11-04 — Billing MVP: usage metering + plan management, no payment rail yet. `GET /api/v1/orgs/{slug}/billing` (tenant admin) returns plan, metered usage (repo count and finished pipeline-run minutes from Postgres; git storage as the sum of Forgejo repo sizes when the git backend is enabled), and hard limits for the `free` plan (50 repos / 1 GiB / 100 pipeline min). `PUT .../billing/plan` is platform-admin only and audit-logged (`billing.plan_changed`). Why: "Billing" is a hard req for the MVP, but Stripe keys don't exist yet, so the honest MVP slice is metering + plan + audit trail; charging/invoicing via Stripe webhook is the follow-up once keys are provisioned.

- 2026-11-04 — Fixed a latent nil-pointer panic: `platform.Service.resolveRepoForWebhook` dereferenced a nil store when the server is built without a store (handler tests) while Forgejo is enabled (e.g. local shell with FORGEJO_ADMIN_TOKEN set). It now returns `ErrUnavailable`, which the webhook handler acks with 200 — matching the documented contract that a store-less server keeps serving webhooks untouched. Also gofmt'd `cmd/quill/create_repo_test.go` (CI's gofmt gate fails on it; `./check` swallows gofmt failures).
