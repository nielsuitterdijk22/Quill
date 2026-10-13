# Backlog

Ordered: agents take the top task under **Todo**, one per run. Foundry moves tasks
to **Parked** or **Done** itself — keep the `### T-NNN: Title` headings and fields intact.

## Todo

### T-027: Update CreateOrgTenant query and add test
- Story: (CTO)
- Accept: go test ./backend/internal/store/... -run TestCreateOrgTenant passes.
- Tests may change: no
- Needs: Quill/T-024a

1. In `backend/internal/store/queries/tenants.sql`, update the `CreateOrgTenant` query to include `plan` in the INSERT column list (set to 'internal' or param) and in the RETURNING clause. 2. Regenerate or manually update `backend/internal/store/db/tenants.sql.go` to match the new SQL signature. 3. In `backend/internal/store/tenants_test.go`, add a test `TestCreateOrgTenant` that calls `store.CreateOrgTenant` and asserts the returned `Tenant.Plan` is 'internal' and `Kind` is 'org'. Skip if `QUILL_TEST_DATABASE_URL` is not set.

### T-024: Implement admin create-tenant CLI command
- Story: (CTO)
- Accept: go test ./backend/cmd/quill/... passes; `go run ./cmd/quill admin create-tenant --name test --plan internal` prints a valid UUID to stdout.
- Tests may change: no
- Needs: Quill/T-023

1. Create `backend/cmd/quill/main.go` using standard `flag` package with `admin` subcommand.
2. Implement `create-tenant` subcommand accepting `--name` and `--plan`.
3. Call `store.CreateOrgTenant` and print the returned `tenant_id`.
4. Add a unit test that mocks the store and verifies the output format.

### T-025: Add integration test for admin create-tenant
- Story: (CTO)
- Accept: go test -tags=integration ./backend/cmd/quill/... passes when QUILL_TEST_DATABASE_URL is set.
- Tests may change: no
- Needs: Quill/T-024

1. Add `backend/cmd/quill/admin_integration_test.go`.
2. Skip test if `QUILL_TEST_DATABASE_URL` is not set.
3. Run the CLI command and verify a row exists in `tenants` with `kind='org'` and the provided `plan`.
4. Ensure `./check` passes without requiring Docker.

### T-022: Implement admin seed-file CLI command
- Story: (CTO)
- Accept: Running `go run ./cmd/quill admin seed-file --tenant <id> --repo suite-meta --path README.md --content 'Hello'` creates/updates the file in the repo via Forgejo API; `./check` passes.
- Tests may change: no
- Needs: Quill/T-000

1. Add `seed-file` subcommand to the `quill admin` CLI.
2. Accept `--tenant`, `--repo`, `--path`, and `--content` (or `--file` for stdin).
3. Use Forgejo API to create or update the file at the specified path.
4. Commit with a message indicating it was seeded by admin.
5. Add a unit test mocking the Forgejo client.

### T-016: Implement Quota Calculation and Middleware
- Story: (CTO)
- Accept: Run `go test ./internal/quota/... -v` to verify `TestCalculateUsage` and `TestMiddlewareRejects` pass.
- Tests may change: no

Create the `internal/quota` package. Implement logic to calculate current tenant usage (repo count, storage bytes) from the database. Create a middleware that checks `tenant.tier` and current usage against free-tier limits (50 repos, 1GB), returning 403/413 if exceeded. Do not wire it to routes yet.

### T-017: Enforce Quotas on Project Creation
- Story: (CTO)
- Accept: Run `go test ./internal/api/... -v` to verify `TestCreateProjectQuota` passes, confirming a 403 response when creating the 51st repo on a free tier.
- Tests may change: no
- Needs: Quill/T-001

Update the `POST /projects` handler to include the quota middleware. Ensure the project creation logic calls the quota check before writing to the database. Verify that the middleware correctly identifies the tenant and returns the appropriate error code.

### T-018: Enforce Quotas on Webhook Pushes
- Story: (CTO)
- Accept: Run `go test ./internal/webhook/... -v` to verify `TestPushQuota` passes, confirming a 413 response when a push exceeds the 1GB storage limit.
- Tests may change: no
- Needs: Quill/T-001

Update the push webhook handler to include the quota middleware. Ensure the storage usage check is performed against the incoming payload size. Verify that the webhook returns a 413 response if the push would exceed the storage limit for the tenant's tier.

### T-015: Define Canonical PRMergedPayload and Tenant Header Contract
- Story: (CTO)
- Accept: Go struct `PRMergedPayload` exists in `internal/events`; `X-Tenant-Id` constant defined in `internal/http`; `go test ./...` passes.
- Tests may change: no

Create a shared types package defining `PRMergedPayload` (fields: pr_id, repo_id, tenant_id, merged_by, merged_at, sha). Define the `X-Tenant-Id` header constant. This serves as the reference contract for Tempo and Forge to import.

### T-002: Implement Nested Group Hierarchy Creation
- Story: US-005
- Accept: Run `go test ./internal/group/... -v` to verify tests `TestCreateNestedGroup` and `TestMaxDepthExceeded` pass, ensuring depth validation and parent linkage.
- Tests may change: no

Extend `internal/group` to support `parent_id` and `depth`. Add `POST /api/v1/groups` endpoint. Enforce max depth 5. Update DB schema if `groups` table lacks `parent_id`/`depth` columns.

### T-003: Enforce Tier-Based Group Depth Limits
- Story: US-008
- Accept: Run `go test ./internal/group/... -v` to verify `TestFreeTierDepthLimit` passes, ensuring free users cannot create groups deeper than level 1.
- Tests may change: no

In group creation logic, check user's `tier`. If 'individual' and `parent_id` is provided (implying depth > 1), return 403 'Nested groups require Enterprise tier'. Enterprise users bypass this.

### T-004: Implement Permission Inheritance Logic
- Story: US-006
- Accept: Run `go test ./internal/permission/... -v` to verify `TestPermissionInheritance` passes, confirming effective role calculation from group memberships.
- Tests may change: no

Create `internal/permission` service. Function `GetEffectiveRole(userID, repoID)` traverses group hierarchy. Returns highest privilege (reader < writer < admin). Update repo access checks to use this.

### T-005: Implement Access Granting via REST API
- Story: US-007
- Accept: Run `go test ./internal/api/... -v` to verify `TestGrantAccessAPI` passes, confirming 201 on success and audit log entry creation.
- Tests may change: no

Add `POST /api/v1/projects/{id}/members` endpoint. Accepts `{user_email, role}`. Validates admin scope. Updates `project_members` table. Calls audit logger. Handles 404 for invalid project IDs.

### T-006: Implement Sequential Pipeline Step Execution
- Story: US-016
- Accept: Run `go test ./internal/pipeline/... -v` to verify `TestSequentialExecution` passes, ensuring steps run in order and stop on failure.
- Tests may change: no

Update `internal/pipeline` runner logic. Iterate through `steps` array. Execute shell commands sequentially. If a step exits non-zero, mark pipeline 'Failed' and skip remaining steps. Log output per step.

### T-007: Implement Secret Masking in Pipeline Logs
- Story: US-015
- Accept: Run `go test ./internal/pipeline/... -v` to verify `TestSecretMasking` passes, confirming sensitive env vars are replaced with '***' in logs.
- Tests may change: no

In log capture logic, identify env vars marked as `secret: true` in pipeline config. Replace their values with `***` in stored logs and API responses before returning to frontend.

### T-008: Implement Real-Time Pipeline Log Streaming
- Story: US-014
- Accept: Run `curl -H 'Authorization: Bearer <token>' http://localhost:8080/api/v1/pipelines/{id}/logs` to verify it returns streaming log output with correct status badges.
- Tests may change: no

Implement `GET /api/v1/pipelines/{id}/logs` endpoint. Use Server-Sent Events (SSE) or chunked transfer to stream logs from Forge/runner. Frontend updates scrollable pane in real-time.

### T-009: Implement Tenant Isolation Enforcement
- Story: US-026
- Accept: Run `go test ./internal/middleware/... -v` to verify `TestTenantIsolation` passes, ensuring all queries filter by `tenant_id` and cross-tenant access returns 404.
- Tests may change: no

Create `internal/middleware` tenant filter. Inject `tenant_id` from JWT into context. Modify all repository/PR/pipeline queries to include `WHERE tenant_id = $1`. Ensure 404 (not 403) for cross-tenant resource IDs.

### T-010: Implement Audit Logging for Key Events
- Story: US-025
- Accept: Run `go test ./internal/audit/... -v` to verify `TestAuditLogCreation` passes, confirming records for PR merges, permission changes, and billing events.
- Tests may change: no

Create `internal/audit` package. Define `AuditEvent` struct. Add `LogEvent` function called from PR merge, permission grant, and billing handlers. Store in `audit_logs` table with actor, target, timestamp.

### T-011: Implement Audit Log Viewing UI & API
- Story: US-028
- Accept: Run `npm run test` in frontend to verify `AuditLogPage.test.tsx` passes, ensuring only admins see the page and filters work.
- Tests may change: no

Add `GET /api/v1/audit-logs` endpoint (admin-only). Create `frontend/app/admin/audit/page.tsx`. Implement filtering by action type. Display table of recent events. Redirect non-admins to home.

### T-012: Implement GDPR Account Deletion
- Story: US-027
- Accept: Run `go test ./internal/user/... -v` to verify `TestAccountDeletion` passes, ensuring user data is purged from Quill DB and Forgejo.
- Tests may change: no

Add `DELETE /api/v1/users/me` endpoint. Mark user as `deleted`. Trigger async job to purge user from Quill DB and call Forgejo API to remove user. Ensure login returns 401 after deletion.

### T-013: Implement Email Notifications for PR Events
- Story: US-029
- Accept: Run `go test ./internal/notification/... -v` to verify `TestPRReviewEmail` passes, confirming email dispatch on PR open/review/CI failure.
- Tests may change: no

Create `internal/notification` package using SMTP client. Add handlers for PR opened (notify reviewers), PR reviewed (notify author), and CI failed (notify author). Use template engine for email content.

### T-014: Implement Outgoing Webhook Configuration
- Story: US-030
- Accept: Run `go test ./internal/webhook/... -v` to verify `TestWebhookDelivery` passes, ensuring POST to configured URL with retry logic on failure.
- Tests may change: no

Add `webhooks` table. Create `POST /api/v1/projects/{id}/webhooks` endpoint. Implement async dispatcher to send JSON payloads on PR opened/merged/failed. Add retry logic with exponential backoff (3 attempts).

## Parked

### T-001: Implement Individual Tier Quota Enforcement (Repos & Storage)
- Story: US-017
- Accept: Run `go test ./internal/quota/... -v` to verify tests `TestRepoQuotaExceeded` and `TestStorageQuotaExceeded` pass, confirming 403/413 responses for free-tier limits.
- Tests may change: no

Create `internal/quota` package. Add middleware/handlers to check `tenant.tier` and `tenant.usage` against free-tier limits (50 repos, 1GB). Update `POST /projects` and push webhooks to reject over-limit actions for free users.
- Parked: 2026-10-08 after 3 attempts — stopped: timeout cap hit. Tried: attempt 1: stopped: timeout cap hit | attempt 2: stopped: timeout cap hit | attempt 3: stopped: timeout cap hit
- CTO (2026-10-09): Remove this task. It has been replaced by the three focused tasks above which cover the same scope but are sized for a single agent session.
- CTO (2026-10-12): superseded by T-016, T-017, T-018 (repaired: the split had left it queued)

### T-019: Create internal 'blacktulip' tenant and bootstrap repo
- Story: (owner thought)
- Accept: A tenant named 'blacktulip' exists in the database with a valid tenant_id, and a repository 'suite-meta' is created under it containing a README.md that links to the Tempo board and Forge pipeline.
- Tests may change: no
- Needs: Quill/T-000

1. Add a CLI command `quill admin create-tenant --name blacktulip --plan internal`.
2. Run the command to generate the tenant_id.
3. Create the 'suite-meta' repository via API for this tenant.
4. Commit a README.md that includes links to the Tempo board and Forge pipeline for the suite itself.
- CTO (2026-10-12): superseded by T-020, T-021, T-022

### T-020: Implement admin create-tenant CLI command
- Story: (CTO)
- Accept: Running `go run ./cmd/quill admin create-tenant --name test-tenant --plan internal` creates a row in the tenants table with kind='org' and outputs the generated tenant_id; `./check` passes.
- Tests may change: no
- Needs: Quill/T-000

1. Create `backend/cmd/quill/main.go` with a `cobra` (or standard `flag`) subcommand `admin`.
2. Implement `create-tenant` which accepts `--name` and `--plan`.
3. Call `store.CreateOrgTenant` with the provided name and plan.
4. Print the new tenant_id to stdout in a machine-readable format.
5. Add a unit test for the command logic (mocking store).
- CTO (2026-10-12): dropped — Too broad for one session; replaced by T-023, T-024, and T-025 which split the work into store, CLI, and integration steps.
- CTO (2026-10-12): superseded by T-023, T-024, T-025

### T-023: Add plan column and CreateOrgTenant store method
- Story: (CTO)
- Accept: go test ./backend/internal/store/... passes; migration 000019 exists and is applied in tests.
- Tests may change: no
- Needs: Quill/T-000

1. Create migration 000019_tenant_plan.sql adding `plan TEXT NOT NULL DEFAULT 'internal'` to `tenants`.
2. Update `db.Tenant` struct to include `Plan string`.
3. Add `store.CreateOrgTenant(ctx, params)` that inserts a row with `kind='org'`.
4. Add a unit test using a mock or in-memory store if Postgres is unavailable, or skip if `QUILL_TEST_DATABASE_URL` is not set.
- Parked: 2026-10-13 after 3 attempts — stopped: explored only — no file changed in 20 min (the task needs a smaller, more concrete first step). Tried: attempt 1: stopped: looping — the model kept repeating one call (bash {"command": "grep -rn 'tenant' /work/backend/internal/platform/projects.go | grep -v 'tenantID\\|TenantID\\|tenant_id\\|//' | head; grep -rn 'tenant' /work/backend/internal/server/projects.go | grep -v 'TenantID\\|tenant_id\\|//' | head"}); do something different next time | attempt 2: stopped: looping — the model kept repeating one call (bash {"command": "grep -rn \"kind\\|plan\" /work/backend/internal/forgejo/ 2>/dev/null | head -20"}); do something different next time. What it found:
## Objective
- Implement T-023: Add a `plan` column to the `tenants` table and ensure the `CreateOrgTenant` store method exists, with tests and a working migration.

## Important Details
- Task file `.foundry/task.md` specifies 4 steps: (1) create migration `000019_tenant_plan.sql`, (2) update `db.Tenant` struct with `Plan string`, (3) add `store.CreateOrgTenant(ctx, params)` with `kind='org'`, (4) add unit test (skip if `QUILL_TEST_DATABASE_URL` unset).
- Acceptance check: `go test ./backend/internal/store/...` passes; migration 000019 exists and is applied in tests.
- `CreateOrgTenant` SQL query **already exists** in `backend/internal/store/queries/tenants.sql` (lines 12-15) and generated Go in `backend/internal/store/db/tenants.sql.go` (lines 39-62).
- `kind` column was added in migration `000016_organizations.up.sql` with `DEFAULT 'personal'`.
- `db.Tenant` struct (in `backend/internal/store/db/models.go:179`) currently has: `ID`, `Slug`, `Name`, `ExternalOrgID`, `CreatedAt`, `UpdatedAt` — **no `Plan` field yet**.
- `Store` embeds `*db.Queries` (`backend/internal/store/store.go`), so `CreateOrgTenant` is already accessible as `store.CreateOrgTenant`.
- FEEDBACK.md (2026-10-12): Forge migrated `tenants` from `installation_id` to `tenant_id UUID`; Quill must send `tenant_id` UUID.
- Hard rules: never edit SUITE.md/MISSION.md/FEATURES.md/USER_STORIES.md; never delete/skip/weaken tests; no git mutation commands; work inside `/work`; PostgreSQL 16 at `$TEST_DATABASE_URL` / `$Q | attempt 3: stopped: explored only — no file changed in 20 min (the task needs a smaller, more concrete first step). What it found:
## Objective
- Implement T-023: Add a `plan` column to the `tenants` table (migration 000019), update the `db.Tenant` struct, and ensure the `CreateOrgTenant` store method exists with tests.

## Important Details
- Acceptance check: `go test ./backend/internal/store/...` passes; migration 000019 exists and is applied in tests.
- Migration 000019 must add `plan TEXT NOT NULL DEFAULT 'internal'` to `tenants`.
- `db.Tenant` struct (in `backend/internal/store/db/models.go`) currently lacks a `Plan` field; needs `Plan string`.
- `CreateOrgTenant` SQL query **already exists** in `backend/internal/store/queries/tenants.sql` (line 12) and generated code in `backend/internal/store/db/tenants.sql.go` (line 39). It inserts with `kind='org'` but does NOT include `plan` yet.
- The `CreateOrgTenant` RETURNING clause currently lists `id, slug, name, external_org_id, created_at, updated_at` — will need `plan` added after the column is added.
- `GetTenantBySlug` and `GetTenantByID` use `SELECT *` so they will automatically pick up the new `plan` column once the struct is updated.
- The `CreateTenant` query (for personal tenants) also uses `SELECT *` — adding a NOT NULL column with a DEFAULT is safe.
- Migration files are embedded via `//go:embed *.sql` in `backend/internal/store/migrations/embed.go`; golang-migrate applies them in order.
- Two earlier attempts failed due to tool-looping (repeated identical grep calls). Avoid repeating the same command.
- FEEDBACK.md notes: Forge migrated `ten
- CTO (2026-10-13): superseded by T-026, T-027

## Done

### T-021: Implement admin create-repo CLI command
- Story: (CTO)
- Accept: Running `go run ./cmd/quill admin create-repo --tenant <id> --name suite-meta` calls the Forgejo API to create the repository and outputs the repo URL; `./check` passes.
- Tests may change: no
- Needs: Quill/T-000

1. Add `create-repo` subcommand to the `quill admin` CLI.
2. Accept `--tenant` (UUID) and `--name` (string).
3. Resolve the tenant's Forgejo org/user context.
4. Call Forgejo REST API to create the repository.
5. Handle errors if repo already exists (idempotent).
6. Add a unit test mocking the Forgejo client.
- Done: 2026-10-13 in dbaea7e (attempt 2)

### T-026: Add plan column to tenants schema
- Story: (CTO)
- Accept: go test ./backend/internal/store/... passes; migration 000019_tenant_plan.sql exists and is applied in tests.
- Tests may change: no
- Needs: Quill/T-000

1. Create `backend/internal/store/migrations/000019_tenant_plan.sql` containing `ALTER TABLE tenants ADD COLUMN plan TEXT NOT NULL DEFAULT 'internal';`. 2. Update `backend/internal/store/db/models.go` in the `Tenant` struct to add `Plan string` after `ExternalOrgID`. 3. Run `go generate ./...` if sqlc is used, or manually update `backend/internal/store/db/tenants.sql.go` to include `plan` in the `Tenant` struct and any `SELECT *` queries that return it. 4. Ensure `go build ./...` passes.
- Done: 2026-10-13 in ba9436a (attempt 2)

### T-000: Make ./check pass on main
- Story: (infrastructure)
- Accept: `./check` exits 0
- Tests may change: yes

Fix the code (preferred) or the check script so every step passes. Do not
delete tests to get green; if a test is genuinely obsolete, explain it in summary.md.
Last output:
```
 downloading go.opentelemetry.io/otel v1.43.0
go: downloading github.com/cloudflare/circl v1.6.3
go: downloading github.com/kevinburke/ssh_config v1.2.0
go: downloading github.com/skeema/knownhosts v1.3.1
go: downloading github.com/xanzy/ssh-agent v0.3.3
go: downloading golang.org/x/net v0.55.0
go: downloading gopkg.in/warnings.v0 v0.1.2
go: downloading github.com/klauspost/compress v1.18.5
go: downloading github.com/xeipuuv/gojsonschema v1.2.0
go: downloading github.com/agnivade/levenshtein v1.2.1
go: downloading github.com/xeipuuv/gojsonpointer v0.0.0-20190905194746-02993c407bfb
go: downloading github.com/felixge/httpsnoop v1.0.4
go: downloading go.opentelemetry.io/otel/metric v1.43.0
go: downloading github.com/go-logr/logr v1.4.3
go: downloading github.com/go-logr/stdr v1.2.2
go: downloading go.opentelemetry.io/auto/sdk v1.2.1
npm notice
npm notice New major version of npm available! 11.19.0 -> 12.2.0
npm notice Changelog: https://github.com/npm/cli/releases/tag/v12.2.0
npm notice To update run: npm install -g npm@12.2.0
npm notice
node:internal/process/promises:394
    triggerUncaughtException(err, true /* fromPromise */);
    ^

[TypeError: fetch failed] {
  [cause]: Error: getaddrinfo EAI_AGAIN registry.npmjs.org
      at GetAddrInfoReqWrap.onlookupall [as oncomplete] (node:dns:123:26) {
    errno: -3001,
    code: 'EAI_AGAIN',
    syscall: 'getaddrinfo',
    hostname: 'registry.npmjs.org'
  }
}

Node.js v24.21.0
Next.js build worker exited with code: 1 and signal: null
```
- Done: 2026-10-07 in 482e855 (attempt 1)
