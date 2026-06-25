# Roles & permissions

Quill uses a layered role model. Roles exist at three levels: org, project, and repo. A role at a higher level grants access to everything beneath it.

## Org roles

| Role | Description |
|------|-------------|
| **Owner** | Full control over the org — settings, billing, members, policies, all repos |
| **Member** | Access is controlled by project membership |

Every org must have at least one owner. Owners can promote members to owners and demote other owners (as long as one owner remains).

## Project roles

| Role | Description |
|------|-------------|
| **Owner** | Manage project settings, policies, membership, and all repos in the project |
| **Member** | Read and write access to all repos in the project |

Project membership doesn't grant any org-level access. A project member can't see other projects they haven't been added to.

## Personal repo roles

| Role | Description |
|------|-------------|
| **Owner** | You. Full control. |
| **Collaborator** | Read and write access (no settings or admin) |

## Permission matrix

| Action | Org Owner | Project Owner | Project Member | Collaborator |
|--------|-----------|---------------|----------------|--------------|
| View repo | ✓ | ✓ | ✓ | ✓ |
| Clone repo | ✓ | ✓ | ✓ | ✓ |
| Push branch | ✓ | ✓ | ✓ | ✓ |
| Open PR | ✓ | ✓ | ✓ | ✓ |
| Merge PR (gates pass) | ✓ | ✓ | ✓ | ✓ |
| Approve PR | ✓ | ✓ | ✓ | ✓ |
| Override policy gate | ✓ | ✓ | — | — |
| Manage repo settings | ✓ | ✓ | — | — |
| Set repo policies | ✓ | ✓ | — | — |
| Set project policies | ✓ | ✓ | — | — |
| Lock policies | ✓ | — | — | — |
| Set org policies | ✓ | — | — | — |
| Invite org members | ✓ | — | — | — |
| Manage org settings | ✓ | — | — | — |
| Delete org | ✓ | — | — | — |

## Policy overrides

Org owners and project owners can override certain policy gates on specific PRs — time windows, change freezes, and external checks. Every override is recorded in the audit log with the actor, timestamp, and stated reason.

**Locked policies cannot be overridden, even by org owners.** This is the key guarantee for compliance: a locked gate is absolute.

## Admins

Platform admins (Quill staff) have elevated access for support and infrastructure purposes. Admin actions are recorded in the audit log and are never used to read your code.

## Related
- [Organizations](../concepts/organizations.md)
- [Projects](../concepts/projects.md)
- [Audit log](audit-log.md)
- [Policy hierarchy](../concepts/policy-hierarchy.md)
