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
- **Email notifications.** PR review requests, CI failures, and @-mentions.
  Without these, users churn silently.

## SaaS — important before growth

- **Landing + pricing page.** Can't sell without this.
- **Audit log.** Paying customers in regulated industries will ask on day one.
- **Repo / storage quotas.** Prevent one tenant from filling the Forgejo disk.
- **Webhooks.** Users expect to pipe events to Slack, external CI, etc.
- **Backup strategy.** Postgres snapshots + Forgejo git data. Losing customer
  code ends the company.

## Smaller polish

- Confirm the `/markup` endpoint's 512 KiB input cap is right for large READMEs
  and document it.
