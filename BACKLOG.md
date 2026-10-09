# Backlog

Ordered: agents take the top task under **Todo**, one per run. Foundry moves tasks
to **Parked** or **Done** itself — keep the `### T-NNN: Title` headings and fields intact.

## Todo

### T-001: Implement Individual Tier Quota Enforcement (Repos & Storage)
- Story: US-017
- Accept: Run `go test ./internal/quota/... -v` to verify tests `TestRepoQuotaExceeded` and `TestStorageQuotaExceeded` pass, confirming 403/413 responses for free-tier limits.
- Tests may change: no

Create `internal/quota` package. Add middleware/handlers to check `tenant.tier` and `tenant.usage` against free-tier limits (50 repos, 1GB). Update `POST /projects` and push webhooks to reject over-limit actions for free users.
- Parked: 2026-10-08 after 3 attempts — stopped: timeout cap hit. Tried: attempt 1: stopped: timeout cap hit | attempt 2: stopped: timeout cap hit | attempt 3: stopped: timeout cap hit
- CTO (2026-10-09): Remove this task. It has been replaced by the three focused tasks above which cover the same scope but are sized for a single agent session.

### T-018: Enforce Quotas on Webhook Pushes
- Story: (CTO)
- Accept: Run `go test ./internal/webhook/... -v` to verify `TestPushQuota` passes, confirming a 413 response when a push exceeds the 1GB storage limit.
- Tests may change: no
- Needs: Quill/T-001

Update the push webhook handler to include the quota middleware. Ensure the storage usage check is performed against the incoming payload size. Verify that the webhook returns a 413 response if the push would exceed the storage limit for the tenant's tier.

### T-017: Enforce Quotas on Project Creation
- Story: (CTO)
- Accept: Run `go test ./internal/api/... -v` to verify `TestCreateProjectQuota` passes, confirming a 403 response when creating the 51st repo on a free tier.
- Tests may change: no
- Needs: Quill/T-001

Update the `POST /projects` handler to include the quota middleware. Ensure the project creation logic calls the quota check before writing to the database. Verify that the middleware correctly identifies the tenant and returns the appropriate error code.

### T-016: Implement Quota Calculation and Middleware
- Story: (CTO)
- Accept: Run `go test ./internal/quota/... -v` to verify `TestCalculateUsage` and `TestMiddlewareRejects` pass.
- Tests may change: no

Create the `internal/quota` package. Implement logic to calculate current tenant usage (repo count, storage bytes) from the database. Create a middleware that checks `tenant.tier` and current usage against free-tier limits (50 repos, 1GB), returning 403/413 if exceeded. Do not wire it to routes yet.

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

## Done

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
