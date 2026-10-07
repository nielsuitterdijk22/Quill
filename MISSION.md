# Mission

Quill is a vendor-hosted, multi-tenant SaaS version-control platform offering nested group hierarchies, SSO/SCIM auth, branch policies, CI pipelines via Forge, live Tempo work-tracking, and Stripe usage-based billing. It runs as a single Docker Compose stack on the vendor's infrastructure, giving enterprises a GitHub/Bitbucket alternative with proper tenant isolation and free individuals a credible personal-repo home.

> Owner document. Agents read it first and treat it as read-only.

## Problem
Enterprises need a vendor-hosted multi-tenant VCS with nested group hierarchies, SSO/SCIM, branch policies, CI, and work-tracking; individuals need a free tier for personal repos. Existing SaaS options don't offer the nested permission inheritance, suite-native integrations, or EU-resident single-tenant-per-isolation posture this vendor is selling.

## Users
Primary: Platform/DevOps engineers at 50-500 person enterprises (banks, insurance) managing 50+ repos across nested org structures, using Entra SSO/SCIM, branch policies, Forge CI, and Tempo tickets. Secondary: Individual developers on the free tier (50 repos, 1GB, 100 pipeline min/mo) with local email/password auth. Tertiary: Tenant admins managing groups, members, billing, and audit logs.

## Core workflows
- Vendor manually provisions enterprise tenant; admin configures Entra OIDC + SCIM in a setup wizard; creates nested groups (Bank→Lending→Fraud, max depth 5); SCIM syncs users nightly
- Developer in Fraud signs in via Entra SSO, navigates group tree, browses repos, clones via HTTPS/SSH, opens a PR linked to TEMPO-42
- PR merge gate evaluates branch policy (2 required reviews) + Forge pipeline status + Tempo ticket state; on merge, Quill auto-transitions TEMPO-42 to Done
- Group admin grants a developer read access to a sibling project via UI or external ITSM calls Quill REST API
- Individual developer self-signs-up with local auth, creates personal group, adds repos up to 50, uses 100 pipeline min/mo free; overage billed via Stripe
- Tenant admin views usage dashboard (repos, storage, pipeline minutes), receives Stripe invoice monthly, reviews audit log

## Examples
- Sara (Fraud) needs read access to Bank→Investment→Trading: Group Admin calls POST /api/v1/projects/bank/investment/trading/members {user: sarajones@bank.com, role: reader} via ITSM; Sara sees the repo next login
- PR #42 to Lending→Fraud→fraud-detection: requires 2 approvals + Forge pipeline pass. Pipeline runs `go build ./... && go test ./...` in ephemeral container. PR page shows 1/2 approvals, pipeline running, TEMPO-42 (In Progress). Merge disabled until all pass. On merge, TEMPO-42 → Done.
- Enterprise: 200 devs, 40 groups, 150 repos. Entra SCIM syncs nightly. Vendor invoices €4,800/mo via Stripe (12GB storage, 3000 pipeline min, 150 repos).
- Individual dev: registers with email/password, creates group 'my-projects', adds 3 repos, pipeline `npm test` on PR, hits 100 min limit, Stripe charges €0.05/min overage.

## Done looks like (first version)
- User registers (local email/password) or signs in via any OIDC (Entra primary); SCIM syncs enterprise users and group memberships
- Vendor manually provisions enterprise tenant; admin configures SSO + SCIM; creates nested group hierarchy (max depth 5)
- Developer browses repo file tree, branches, commits; opens/views/reviews/merges PRs with diff view and inline Forge pipeline logs
- Branch policies enforce required reviews + pipeline status as merge gate; per-repo toggle for required vs informational CI
- Forge integration: Quill POSTs clone URL + commit SHA + steps to Forge API; Forge runs ephemeral container, calls Quill webhook on completion; logs viewable on PR page and pipeline list
- Tempo integration: validate ticket ID on PR creation, display title/status inline, auto-transition to Done on merge, show open tickets on repo page
- Stripe billing: free individual tier (50 repos/1GB/100min), overage pay-as-you-go; enterprise usage-based monthly invoicing; usage dashboard for admins
- Email notifications for PR review requests, CI failures, @-mentions; org member invite flow via email link
- Audit log: every permission change, admin action, merge, and billing event recorded with actor, timestamp, target
- REST API for external ITSM tools: grant/revoke project access, trigger pipelines, query audit log
- Outgoing webhooks to user-configured endpoints on PR opened, pipeline failed, merge completed
- Tenant isolation: every DB query filtered by tenant_id; dedicated audit pass confirms zero cross-tenant leakage
- Landing + pricing page for logged-out visitors; GDPR account deletion purges Quill + Forgejo data
- Single Docker Compose stack: Postgres + Forgejo + Quill API (:8080) + Quill web (:3001) + Forge; Caddy for TLS

## Out of scope
- Yaly (software catalog/Backstage) live integration — ownership stored in Quill, Yaly sync deferred to a separate workstream
- Tempo, Forge, and Yaly codebases — Quill integrates against their existing APIs; building those tools is out of scope for this mission
- Multi-region deployment, HA clustering, or cloud-provider migration — single Docker Compose on vendor's device for now
- Self-hosting by customers — this is a SaaS the vendor hosts
- Custom pipeline DSL or complex DAG workflows — v1 supports sequential shell steps via Forge
- Mobile app or PWA offline mode
- Real-time collaboration (live editing, presence)
- Migration tools from GitHub/GitLab/Bitbucket — users clone repos manually
- Plugin/marketplace system for third-party extensions
- Non-English localization — v1 is English only
- Hard repo/storage quotas for enterprises — usage-based billing replaces caps; individuals have fixed free-tier limits

## Must never happen
- A tenant's repos, PRs, metadata, or pipeline logs must never be visible to another tenant — every query is tenant-scoped with no admin override
- User credentials (passwords, tokens, SSO assertions) must never be logged or stored in plaintext
- Pipeline runner secrets (env vars, tokens) must never appear in logs, API responses, or the frontend
- Deleting a tenant must not delete other tenants' data; deleting a user must not delete their commits/history
- The system must never accept a request that bypasses the tenant_id filter
- Forgejo admin token must never be exposed in API responses, logs, or the frontend
- Session cookies must always be HttpOnly + Secure + SameSite=Strict in production
- Individual-tier users must never access enterprise-tier features (SSO, SCIM, nested groups beyond depth 1, unlimited repos) without upgrading
- Billing data (Stripe customer IDs, payment methods) must never be visible to non-admin users or other tenants

## Constraints
- Backend: Go 1.24, chi router, pgx + sqlc, golang-migrate; Postgres for metadata; Forgejo REST API for all git/PR operations (wrapped, never forked)
- Frontend: Next.js 14 app router, TypeScript, no Tailwind — shared dark design system from globals.css (purple #7c5cff accent, 212px sidebar)
- Auth: provider-agnostic AuthProvider interface; v1 ships local username/password + any OIDC (Entra primary) + SCIM; JWT sessions with HttpOnly/Secure/SameSite=Strict cookies
- Deployment: single Docker Compose stack on vendor's device; Caddy for TLS; Postgres + Forgejo + Quill API (:8080) + Quill web (:3001) + Forge; no cloud provider yet
- Nested group hierarchy: Tenant/Individual (top) → Group (nested, max depth 5) → Project (leaf, 1:1 with Forgejo org) → Repo (single location)
- Forge integration: Quill POSTs clone URL + commit SHA + steps (shell commands + env vars) to Forge REST API; Forge calls Quill webhook on completion; Quill polls for logs
- Tempo integration: live API calls to validate/display/auto-transition tickets; no stubs
- Stripe for billing; SMTP for email notifications; Entra OIDC + SCIM for enterprise auth; no other external dependencies
- Individual tier: free, 50 repos, 1GB git storage, 100 pipeline min/mo, local auth only, max group depth 1; overage via Stripe pay-as-you-go
- Enterprise tier: manually provisioned by vendor, SSO/SCIM required, usage-based Stripe invoicing, no hard quotas, nested groups up to depth 5
- GDPR: account deletion purges all user data from Quill DB and Forgejo; data resides on vendor's device in EU
