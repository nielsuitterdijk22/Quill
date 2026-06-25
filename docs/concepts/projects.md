# Projects

A project is how you organize repos and people inside an organization.

Think of it like a team. You might have a `platform` project, a `frontend` project, and a `data` project — each with its own members, its own repos, and its own policies. A repo belongs to exactly one project. A person can belong to multiple projects.

Projects don't appear in URLs. Your repo is still at `quill.so/acme/my-repo` regardless of which project it's in. Projects are an internal organizing concept, not a URL hierarchy.

## What projects give you

**Scoped membership** — add only the people who need access to a given set of repos. A contractor working on frontend doesn't need access to your data pipelines.

**Policy inheritance** — policies you set at the project level apply to all repos in that project, unless a repo overrides them. Policies at the org level apply to everything. See [Policy hierarchy](policy-hierarchy.md).

**A shared view** — members of a project see all its repos, pipelines, and open PRs in one place.

## Projects vs. organizations

| | Org | Project |
|---|---|---|
| Scope | Billing, namespace, SSO | Team grouping, policy scope |
| Appears in URLs | Yes (`quill.so/org-name/`) | No |
| Has members | Yes | Yes (subset of org members) |
| Has policies | Yes (org-wide) | Yes (project-wide) |

## Creating a project

Inside your org, go to **Projects → New project**. Give it a name and a slug. The slug is used internally but isn't customer-visible in URLs.

Once created, you can add repos and invite org members to it.

## Related
- [Organizations](organizations.md)
- [Policy hierarchy](policy-hierarchy.md)
- [Create a project](../projects/create.md)
