# Access control model

This page answers the questions a risk manager or security auditor typically asks about Quill. It describes what controls exist, who they apply to, and what can and cannot be bypassed.

## Who can access what

Access in Quill is always role-based. There are no per-resource ACLs — a user's access to a repo flows entirely from their membership in the project that repo belongs to, plus their org role.

The question "can user X read repo Y?" resolves as:

1. Is X an org owner? → Yes
2. Is X a member of the project that Y belongs to? → Yes
3. Is Y public or internal, and is X a member of the org? → Yes if internal; Yes for public regardless
4. Otherwise → No

## What can never be bypassed

These controls are absolute, regardless of role:

- **Locked policies** — a policy locked at any scope cannot be weakened or removed by any actor at a narrower scope, including org owners acting on a specific repo. Locking is the only way to provide a hard compliance guarantee in Quill.
- **Audit log** — no one can delete or modify audit log entries, including Quill staff.
- **Force push on protected branches** — when the `block_force_push` gate is set, git rejects force pushes at the protocol level, not just the UI. There is no API workaround.

## What can be overridden

Some gates can be overridden by sufficiently privileged users:

| Gate | Who can override | Is it logged? |
|------|-----------------|---------------|
| Time window | Org owner, project owner | Yes — actor + reason |
| Change freeze | Org owner | Yes — actor + reason |
| External check | Org owner, project owner | Yes — actor + reason |
| Required reviews | Cannot be overridden (must be met) | N/A |
| Pipeline checks | Cannot be overridden (must be met) | N/A |

Required reviews and pipeline checks cannot be bypassed by any UI action. If you need a break-glass procedure, it must be handled by temporarily modifying the policy (which is itself logged).

## Authentication

Quill supports authentication via:

- **SSO (SAML/OIDC)** — configured per org. When SSO is required, all members must authenticate through your identity provider. Password login is disabled for SSO-enforced orgs.
- **Username + password** — for orgs without SSO. Passwords are bcrypt-hashed, never stored in plaintext.
- **Personal access tokens** — for API and git HTTPS access. Tokens are scoped, expirable, and individually revocable. All token actions are attributed to the token owner.

## Data isolation

Each org's data is logically isolated. Org members cannot query or access data belonging to other orgs. Quill staff cannot access your code repositories through normal application paths.

## Secrets

Pipeline secrets are encrypted at rest. They are only decrypted in the runner environment during a pipeline run. Quill redacts secret values from pipeline logs automatically.

## Questions for your audit

Here are the controls you'll most likely need to demonstrate:

**"Can a developer merge without approval?"**
Set `required_reviews: 1` (or more) on your branch policy. Lock it at the org or project level. It cannot be bypassed.

**"Can someone change a policy without it being noticed?"**
Every policy change is logged in the audit log with a before/after diff, the actor, and the timestamp.

**"Can we prevent merges outside business hours?"**
Yes — use a `time_window` gate. Lock it if you need it to be non-negotiable.

**"What happens if our change management process isn't followed?"**
Use an `external_check` gate to require a pass signal from your ITSM tool. Without the signal, the merge button stays disabled.

**"Can we prove a specific policy was active when a PR merged?"**
Yes — the audit log records the full policy configuration at the time of each merge. The `pr.merged` event captures which gates passed and their verdicts.

## Related

- [Roles & permissions](rbac.md)
- [Audit log](audit-log.md)
- [Policies overview](../policies/overview.md)
