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

