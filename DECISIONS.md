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
