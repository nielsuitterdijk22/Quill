# Groups and billing

## Groups (nested departments & teams)

Inside an organization you can model departments and teams as a **group tree**
(max depth 5). A root group is a department; each child group is a team (or a
sub-department).

- Create groups under **Organization settings → Groups** (org admins only), or
  `POST /api/v1/orgs/{org}/groups` with `{slug, name, parentSlug}`.
- Add members with `POST .../groups/{group}/members` `{userId}`.
- Grant a group a role on a project with
  `PUT .../groups/{group}/projects/{project}` `{role: "owner" | "member"}`.

**Inheritance:** a member of a group is effectively a member of all its
ancestor groups, and the group's project roles apply to all of them. So granting
the *department* group `owner` on a project gives every team beneath it owner
access. Your effective role on a project is the strongest of your direct
membership and your inherited group roles (`owner` > `member`).

Every mutation is written to the audit log (`group.*` actions).

## Billing

Each tenant has a **plan** (`free`, `team`, `enterprise`, `internal`). The
platform operator sets it via `PUT /api/v1/orgs/{org}/billing/plan` (platform
admin only; audit-logged as `billing.plan_changed`).

Org admins see plan + metered usage at **Organization settings → Billing & usage**
(`GET /api/v1/orgs/{org}/billing`):

| Meter | Source |
| --- | --- |
| Repositories | count of repos across the org's projects |
| Git storage | sum of Forgejo repo sizes (bytes) |
| Pipeline minutes | sum of finished run durations |

The `free` plan has hard caps — **50 repos, 1 GiB storage, 100 pipeline
min/mo** — shown as usage badges in the UI. Other plans have no hard caps;
usage is invoiced monthly (Stripe) once a payment method is connected.
