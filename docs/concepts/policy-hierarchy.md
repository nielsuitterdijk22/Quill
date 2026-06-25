# Policy hierarchy

Policies in Quill apply at three levels: **org**, **project**, and **repo**. They inherit downward and can be locked so narrower scopes can't weaken them.

## The three levels

```
Org
 └── Project
      └── Repo
```

A policy set at the org level applies to every repo in that org. A policy at the project level applies to every repo in that project. A policy at the repo level applies only to that repo.

When multiple levels define a rule for the same branch pattern, the **most restrictive** rule wins. If your org requires 1 approval and your project requires 2, the effective requirement is 2.

## Locking

Any level can lock a policy. A locked policy can't be overridden or weakened by a narrower scope.

For example: your org sets a policy requiring all merges to `main` go through a change management gate, and locks it. No project or repo can remove or bypass that gate, even if an owner of that project tries.

This is the control compliance teams care about. If a policy is locked at the org level, it's enforced everywhere, period.

## Policy types

| Type | What it controls |
|------|-----------------|
| [Branch rules](../policies/branch-rules.md) | Required reviews, PR requirements, force push protection |
| [Time windows](../policies/time-windows.md) | When merges are allowed (business hours, day of week) |
| [Change freeze](../policies/change-freeze.md) | Manual or scheduled freeze periods — no merges until lifted |
| [External checks](../policies/external-checks.md) | Async gates that call out to your own systems and wait for approval |

Policies are composable. A branch can have multiple gates — all must pass before a merge goes through.

## Where policies live in the UI

- **Org policies** — org Settings → Policies
- **Project policies** — project Settings → Policies
- **Repo policies** — repo Settings → Branch policies

## Related
- [Branch rules](../policies/branch-rules.md)
- [Time windows](../policies/time-windows.md)
- [Change freeze](../policies/change-freeze.md)
- [External checks](../policies/external-checks.md)
- [Roles & permissions](../security/rbac.md)
