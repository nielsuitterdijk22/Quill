# Change freeze

A change freeze blocks all merges to a branch until it's lifted. It's for exceptional situations — a production incident, a major release window, a compliance audit period — when you need to stop all changes immediately and resume them explicitly.

## Declare a freeze

Go to the branch's policy settings (repo, project, or org level) → **Change freeze → Freeze now**.

Or from the PR itself: if you have owner permissions, a **Declare freeze** option appears in the PR action menu.

You'll be prompted for:
- **Reason** — shown to anyone who tries to merge while the freeze is active
- **Scope** — which branches are frozen (defaults to the current policy's branch pattern)
- **Auto-lift** — optional. Set a time when the freeze automatically lifts.

The freeze takes effect immediately. All PRs targeting frozen branches show a **Frozen** banner. The merge button is disabled for everyone.

## Active freeze banner

When a freeze is active, every PR targeting the frozen branch shows:

> **Merge blocked: change freeze active**
> Frozen by Jane Doe — "Incident response: payment service degraded"
> Declared 14 minutes ago

## Lift a freeze

Any org owner or project owner can lift the freeze. Go to **Settings → Policies → Active freezes** and click **Lift freeze**. You'll be asked for a confirmation note.

Alternatively, if an auto-lift time was set, the freeze lifts automatically at that time.

## Scheduled freezes

You can schedule a recurring change freeze in advance — useful for release windows when no merges are allowed while a build is being validated and deployed:

```
Gate: change_freeze
Schedule:
  - cron: "0 18 * * 5"     # every Friday at 18:00 UTC
    duration: 64h            # through Monday 10:00 UTC
    reason: "Weekend release freeze"
```

Scheduled freezes are declared automatically at the configured time and lift when the duration expires.

## Freeze scope

A freeze can be scoped to:
- A specific branch (e.g. only `main`)
- A branch pattern (e.g. `release/*`)
- All branches in a project or org

When declared from a repo policy, it only affects that repo's matching branches. When declared from an org policy, it affects all matching branches across all repos.

## Override

Org owners can override a freeze on a specific PR by clicking **Override freeze** and providing a reason. This is recorded in the audit log with the reason and the overriding user. The freeze remains active for all other PRs.

## Audit trail

Every freeze declaration, lift, and override is recorded in the audit log:
- Who declared the freeze and when
- The stated reason
- Who lifted it (or "auto-lifted") and when
- Any overrides, with reasons

See [Audit log](../security/audit-log.md).
