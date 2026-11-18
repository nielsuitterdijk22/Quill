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

Quill has **no free tier and no trial**. Every account pays, except hand-selected
beta users. Each tenant has a **plan**:

| Plan | Who | Price (incl. VAT) | Caps |
| --- | --- | --- | --- |
| `starter` | individuals | €4 / month | 5 GB git storage, 100 CI min/mo |
| `pro` | individuals | €9 / month | 10 GB git storage, 500 CI min/mo |
| `team` ("Team Pro") | organizations | €11 / user / month | none (usage-based) |
| `beta` | hand-picked users | free | same as `pro` |
| `enterprise`, `internal` | vendor-provisioned | custom / none | none |
| `inactive` | every new account or org | — | **locked** |
| `lapsed` | canceled subscription | — | **locked** |

A **locked** tenant keeps read access to its data, but creating repositories,
running pipelines, inviting members and SCIM/SSO changes are refused until it
subscribes. Repository counts are never capped; storage and CI minutes are.

Checkouts go through Creem: `POST /api/v1/me/billing/checkout` with
`{"plan": "starter" | "pro"}` for personal accounts and
`POST /api/v1/orgs/{org}/billing/checkout` for Team Pro. The Creem product ids are
`CREEM_STARTER_PRODUCT_ID`, `CREEM_PRO_PRODUCT_ID` and `CREEM_TEAM_PRODUCT_ID`
(terraform: `creem_*_product_id`). A canceled or expired subscription moves the
tenant to `lapsed`.

The platform operator can set any plan via `PUT /api/v1/orgs/{org}/billing/plan`
(platform admin only; audit-logged as `billing.plan_changed`).

### Beta invites

Free beta access is granted by hand with a temporary, capped link:

- `quill admin create-beta-invite --note "Sam" --uses 1 --days 14` (prints
  `…/beta/<code>`), or **Admin → Beta invites** in the app.
- Whoever opens the link while signed in has their personal account moved to
  `beta`. Codes are stored hashed, expire, have a use cap, and can be revoked
  (`DELETE /api/v1/admin/beta-invites/{id}`); accounts already redeemed keep
  `beta`. A code never replaces a paid or vendor plan.
- Organizations get `beta` through the admin plan switch.

### Usage meters

Org admins see plan + metered usage at **Organization settings → Billing & usage**
(`GET /api/v1/orgs/{org}/billing`); personal accounts at **Settings → Billing**
(`GET /api/v1/me/billing`).

| Meter | Source |
| --- | --- |
| Repositories | count of repos across the org's projects |
| Git storage | engine repo sizes (bytes) |
| Pipeline minutes | sum of finished run durations this month |
