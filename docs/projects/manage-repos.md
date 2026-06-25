# Manage repos

## Add a repo to a project

**Create new** — from the project page, click **New repository**. The repo is automatically assigned to this project.

**Move an existing repo** — go to the repo's **Settings → General → Project** and select the project from the dropdown. A repo can only belong to one project.

## Remove a repo from a project

Go to the repo's **Settings → General → Project → Unassign**. The repo still exists but is no longer scoped to this project — it won't appear in the project's repo list and the project's policies no longer apply.

An unassigned repo is still accessible directly at its URL. Org-level policies still apply.

## Transfer a repo to another org

Go to repo **Settings → General → Danger zone → Transfer**. The repo moves to the new org's namespace. All issues, PRs, and pipeline history move with it. The old URL redirects automatically for 30 days.

You need to be an owner in both the source and destination orgs to transfer.

## Archive a repo

Archiving makes a repo read-only. No new pushes, no new PRs, no pipeline runs. The repo stays visible and all its history is preserved.

Go to repo **Settings → General → Archive this repository**.

Archived repos still count against your org's repo limit. You can unarchive at any time.

## Delete a repo

Go to repo **Settings → General → Danger zone → Delete this repository**.

Deletion is permanent. All code, issues, PRs, and pipeline history are gone. There is no undo.

You must type the repo name to confirm.

## Repo limits

Each project can have any number of repos. There's no hard limit on repos per org on the current plan.
