# Feedback

The owner writes here at check-ins. Agents read this first, every session. Newest at the top.
Delete items once they are handled. The loop commits your edits automatically.

## 2026-10-13 — decided by the CTO
The acceptance criteria for T-028 are satisfied. The test exists at `backend/cmd/quill` (module root `backend/`), not `backend/cmd/admin`. Agents must use `go test ./cmd/quill -run TestSeedFileSelfHeal` from the `backend/` directory. Do not create a `cmd/admin` package; the CLI entrypoint is `cmd/quill`.

> Answering: Quill T-028 (Persist repositories row on seed-file self-heal) asks:

## 2026-10-13 — decided by the CTO
Canonical PR Identity Contract: For all cross-product communication (NATS events, REST APIs, and shared schemas), a PR is uniquely identified by `repo_slug` (string) and `quill_pr_number` (integer). Do not use UUIDs or other internal IDs for PR references in the event bus or public APIs.

> Answering: Tempo T-015 (Implement ticket_pr_links schema and Store operations) asks:

## 2026-10-13 — decided by the CTO
CLI Architecture: The `admin` command group must be designed for additive extension. When implementing subcommands like `create-repo` (T-021), do not block on the full scaffolding (T-024). If T-024 is not merged, include the minimal `admin` dispatch logic in your PR. If T-024 is merged first, adapt your subcommand registration to use its existing pattern. The goal is that T-021 and T-024 can merge in either order without significant rework.

> Answering: Quill T-021 (Implement admin create-repo CLI command) asks:

## 2026-10-12 — decided by the CTO
Forge has migrated the `tenants` table from `installation_id` to `tenant_id UUID`. Any code in Quill that creates or looks up Forge tenants must now generate and use a UUID for `tenant_id`. Ensure that Quill's integration with Forge (e.g., webhook handlers or API calls) sends the `tenant_id` UUID rather than relying on `installation_id`.

> Answering: Forge T-015 (Implement admin create-tenant CLI command) asks:

## 2026-10-12 — from the owner, via the CTO (med priority)
Prioritize the creation of the internal 'blacktulip' tenant (T-019) and bootstrap repository. This is the foundational step for the suite's dogfooding mandate. Ensure the tenant is configured to support the BlackTulip team's identity and initial project structure.

> The owner's words: would be fun if you could use the suite as well for your work :D or if you use it as your own knowledge / persitency layer
