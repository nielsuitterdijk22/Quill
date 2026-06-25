# Organizations

An organization is the top-level boundary in Quill. It's your company, team, or project namespace.

When you create an org, you get:
- A shared namespace for all your repos (`quill.so/your-org/repo-name`)
- A member roster with role-based access
- Projects for organizing repos and scoping policies
- An org-level audit log

## Orgs vs. personal accounts

Every Quill user has a personal namespace at `quill.so/your-username`. It's yours alone — great for side projects, experiments, or anything that doesn't need a team. You don't need to create an org to get started.

When you need to collaborate, create an org. Repos under an org are owned by the org, not by any individual member. If someone leaves, the repos stay.

## Org roles

| Role | What they can do |
|------|-----------------|
| **Owner** | Everything — billing, settings, member management, delete the org |
| **Member** | Access repos and projects they've been added to |

Owners can promote any member to owner and demote other owners. There must always be at least one owner.

## Creating an org

Go to your avatar menu → **New organization**. Pick a slug — this becomes your namespace and can't be changed later, so choose carefully.

## Related
- [Projects](projects.md) — how repos and policies are organized inside an org
- [Roles & permissions](../security/rbac.md) — the full permission model
- [Invite members](../projects/invite-members.md) — adding people to your org
