# Audit log

The audit log is a tamper-evident, append-only record of every significant action in your org. It's the primary tool for compliance investigations, access reviews, and incident response.

## Access the audit log

Org owners: go to **org Settings → Audit log**.

You can filter by actor, event type, resource, date range, or any combination.

## What's logged

Every log entry includes:
- **Timestamp** — UTC, millisecond precision
- **Actor** — the user who performed the action (username + user ID)
- **IP address** — the IP the request came from
- **Event type** — what happened (see below)
- **Target resource** — what was acted on (repo, user, policy, etc.)
- **Before/after** — for mutations, the state before and after the change

### Authentication events

| Event | Description |
|-------|-------------|
| `auth.sign_in` | User signed in |
| `auth.sign_out` | User signed out |
| `auth.sign_in_failed` | Failed login attempt (wrong password, locked account) |
| `auth.password_changed` | User changed their password |

### Org events

| Event | Description |
|-------|-------------|
| `org.created` | Org was created |
| `org.deleted` | Org was deleted |
| `org.member_invited` | Invite sent to an email address |
| `org.member_joined` | Invited user accepted and joined |
| `org.member_removed` | Member was removed from the org |
| `org.member_role_changed` | Member's org role was changed |

### Project events

| Event | Description |
|-------|-------------|
| `project.created` | Project was created |
| `project.deleted` | Project was deleted |
| `project.member_added` | Org member was added to the project |
| `project.member_removed` | Member was removed from the project |
| `project.member_role_changed` | Member's project role was changed |

### Repository events

| Event | Description |
|-------|-------------|
| `repo.created` | Repository was created |
| `repo.deleted` | Repository was deleted |
| `repo.archived` | Repository was archived |
| `repo.unarchived` | Repository was unarchived |
| `repo.visibility_changed` | Visibility was changed (before/after recorded) |
| `repo.forked` | Repository was forked |
| `repo.transferred` | Repository was transferred to another org |

### Git events

| Event | Description |
|-------|-------------|
| `git.branch_created` | Branch was created |
| `git.branch_deleted` | Branch was deleted |
| `git.force_push_blocked` | Force push was blocked by policy |

### Pull request events

| Event | Description |
|-------|-------------|
| `pr.opened` | PR was opened |
| `pr.merged` | PR was merged |
| `pr.closed` | PR was closed without merging |
| `pr.review_submitted` | Review was submitted (with state: approved/changes_requested/comment) |
| `pr.approval_dismissed` | Approval was dismissed (manually or by stale-approval policy) |

### Policy events

| Event | Description |
|-------|-------------|
| `policy.created` | Policy was created (full config recorded) |
| `policy.updated` | Policy was updated (before/after recorded) |
| `policy.deleted` | Policy was deleted |
| `policy.locked` | Policy was locked |
| `policy.unlocked` | Policy was unlocked |
| `policy.gate_triggered` | External check gate fired for a PR |
| `policy.gate_passed` | Gate returned a pass verdict |
| `policy.gate_failed` | Gate returned a fail verdict or timed out |
| `policy.gate_overridden` | Gate was overridden by an owner (reason recorded) |
| `policy.freeze_declared` | Change freeze was declared (reason recorded) |
| `policy.freeze_lifted` | Change freeze was lifted |
| `policy.freeze_overridden` | Freeze was overridden for a specific PR (reason recorded) |

### Pipeline events

| Event | Description |
|-------|-------------|
| `pipeline.run_triggered` | Pipeline run was triggered (event and ref recorded) |
| `pipeline.run_completed` | Run finished (status: success/failure/cancelled) |
| `pipeline.run_cancelled` | Run was manually cancelled |

### Admin events

| Event | Description |
|-------|-------------|
| `admin.user_activated` | User account was activated |
| `admin.user_deactivated` | User account was deactivated |
| `admin.audit_log_exported` | Audit log was exported |

## Export

Audit logs can be exported as JSON or CSV. Go to **org Settings → Audit log → Export**.

Exports include the full log for the selected date range. Exports themselves are logged (`admin.audit_log_exported`).

## Retention

Audit logs are retained for 90 days by default. Extended retention (1 year, 7 years) is available on higher plans.

After the retention period, log entries are permanently deleted. Export regularly if you need longer-term records.

## Immutability

Audit log entries cannot be edited or deleted — not by org owners, not by Quill staff. The log is append-only.

## Related
- [Roles & permissions](rbac.md)
- [Access control model](access-control.md)
- [Change freeze](../policies/change-freeze.md)
