# Create a project

Projects organize repos and people inside an org. You need at least one project before you can create org repos.

## Create

Inside your org, go to **Projects → New project**.

- **Name** — shown in the UI throughout Quill. Something clear like "Platform Engineering" or "Frontend."
- **Slug** — used internally for API calls and policy references. Lowercase, no spaces.
- **Description** — optional. Helps new members understand what this project is for.

Click **Create project**. You're automatically the owner.

## Add repos

From the project page, click **New repository** to create a repo directly in this project, or go to an existing repo's **Settings → Project** to move it here.

A repo can only belong to one project at a time.

## Set project policies

Go to **project Settings → Policies** to define branch rules that apply to all repos in this project. Project policies stack on top of org policies — the more restrictive rule always wins.

See [Policy hierarchy](../concepts/policy-hierarchy.md) for how inheritance works.

## Project roles

| Role | Permissions |
|------|------------|
| **Owner** | Manage members, settings, policies, delete the project |
| **Member** | Read and write access to all repos in the project |

Project owners are set by org owners. Project membership doesn't grant any org-level permissions.

## Related

- [Invite members](invite-members.md)
- [Manage repos](manage-repos.md)
- [Policy hierarchy](../concepts/policy-hierarchy.md)
