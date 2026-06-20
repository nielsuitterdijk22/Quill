# Quill — todo

## Open design decision

- **Namespace vocabulary & depth.** Settle the model across Quill / Forge / Yaly.
  Today it's `Org → Repo` with Teams + an owning team; the `namespace` table
  already carries `parent_id` for future nesting. "Org > Team > Project" reads
  awkwardly and doesn't capture "one company, many orgs/groups". Decide the
  canonical hierarchy and naming, then align UI copy + routes. Don't mass-rename
  until the model is agreed.

## SaaS — must-have before first paying customer

- **Billing.** Stripe + usage-gated plans. Right now every sign-up gets
  unlimited everything. Without this the product is free.
- **Tenant isolation audit.** Every data query in the backend needs a
  `tenant_id` filter guard reviewed. A bug that leaks one org's repos or
  issues to another org is a showstopper — do a dedicated pass before launch.
- **Email notifications.** PR review requests, CI failures, and @-mentions.
  Without these, users churn silently.
- **Org member invite flow.** Clerk handles sign-up, but there is no
  "invite a teammate to your org" UX inside Quill itself.
- **Error tracking.** Sentry or equivalent. Need to know when things break in
  prod before users report it.

## SaaS — important before growth

- **Landing + pricing page.** Can't sell without this.
- **Audit log.** Paying customers in regulated industries will ask on day one.
- **Repo / storage quotas.** Prevent one tenant from filling the Forgejo disk.
- **Webhooks.** Users expect to pipe events to Slack, external CI, etc.
- **Backup strategy.** Postgres snapshots + Forgejo git data. Losing customer
  code ends the company.

## Follow-up fixes

- **Git token reconciliation.** If recording a freshly minted token fails *and*
  the compensating Forgejo delete also fails, an orphaned Forgejo token is left
  behind (logged, not user-visible). Add a reconcile path — e.g. list tokens
  from Forgejo and surface/clean any `quill-git-*` tokens with no DB record.
- **Token list metadata.** Store only name + created_at today. Consider surfacing
  scope and last-used (Forgejo exposes these) so users can audit tokens, and
  warn on duplicate display names (only `forgejo_token_name` is unique today).
- **README render caching.** Every repo/tree page render does a POST to the
  backend → Forgejo `/markup`. Cache the rendered HTML (per repo + content hash)
  or render once server-side to avoid repeated round-trips on navigation.
- **Integration coverage for git tokens.** The create/list/revoke flow has no
  test. Add a gated integration test (behind `QUILL_TEST_DATABASE_URL`) covering
  the store queries and the revoke path, mirroring the existing platform tests.

## Smaller polish

- Confirm the `/markup` endpoint's 512 KiB input cap is right for large READMEs
  and document it.
- Empty-state copy for the git-token list (no tokens yet) and for the pipelines
  overview when a repo has workflows but no runs.
- Settings page: hide the password-change form when Clerk auth is active (the
  backend route is removed in Clerk mode; it currently shows and silently 404s).
