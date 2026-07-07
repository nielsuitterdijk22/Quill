# Policies overview

Policies are how you control how code moves through your system. They live at three levels — org, project, and repo — and they compose.

## What a policy is

A policy targets a branch pattern and defines a list of gates. A gate is a condition that must pass before a merge goes through. All gates must pass. There's no "any one of these" — it's always all of them.

```text
Policy: protect main
  Branch pattern: main
  Gates:
    - required_reviews: 2
    - pipeline_checks: CI / test
    - time_window: weekdays 09:00–18:00 UTC
    - external_check: https://itsm.acme.com/quill-gate
```

## Gate types

| Gate | What it checks |
|------|---------------|
| [`required_reviews`](branch-rules.md) | Minimum number of approvals from project members |
| [`branch_rule`](branch-rules.md) | Source branch must match a pattern (e.g. `feature/*` only) |
| [`pipeline_checks`](branch-rules.md) | Named pipeline jobs must be passing |
| [`time_window`](time-windows.md) | Current time must be within an allowed window |
| [`change_freeze`](change-freeze.md) | No active freeze declared on the target branch |
| [`external_check`](external-checks.md) | An external system must return a pass verdict |

## Where policies live

- **Org policies** — apply to all repos in the org. Set in org **Settings → Policies**.
- **Project policies** — apply to all repos in the project. Set in project **Settings → Policies**.
- **Repo policies** — apply only to this repo. Set in repo **Settings → Branch policies**.

## Inheritance

When multiple levels define a policy for the same branch pattern, Quill merges them. The strictest rule wins:

- If org requires 1 approval and project requires 2, the effective requirement is 2.
- If org allows merges anytime but project has a time window, the time window applies.
- If org has a change_freeze gate and repo doesn't, the repo still has the gate.

## Locking

A policy can be locked. A locked policy can't be overridden by any narrower scope.

If you lock a policy at the org level, no project or repo can change or remove it — even org owners acting on a specific repo. This is the strongest enforcement guarantee Quill provides.

Locked policies are shown with a lock icon in the UI. The lock can only be removed from the level that set it.

## Who can set policies

- **Org owners** — can set and lock policies at any level
- **Project owners** — can set project and repo policies (within any org-level locks)
- **Repo admins** — can set repo policies (within any org- or project-level locks)

## Related

- [Branch rules](branch-rules.md)
- [Time windows](time-windows.md)
- [Change freeze](change-freeze.md)
- [External checks](external-checks.md)
- [Policy hierarchy](../concepts/policy-hierarchy.md)
