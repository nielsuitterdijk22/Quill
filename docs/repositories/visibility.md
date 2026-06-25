# Visibility & access

## Visibility levels

| Level | Who can see the repo |
|-------|---------------------|
| **Private** | Only you (personal) or project members (org) |
| **Internal** | All members of the org |
| **Public** | Anyone, including visitors without an account |

**Read** means: browse files, view commits, clone the repo, view PRs and pipelines.

**Write** means: push branches, open PRs, trigger pipelines.

**Admin** means: change settings, manage policies, delete the repo.

## Change visibility

Repo **Settings → General → Visibility**. You'll be asked to confirm because changing to a more open visibility is irreversible without setting it back.

Org owners can change visibility on any repo in the org regardless of project membership.

## Personal repos

A private personal repo is visible only to you. You can add collaborators — go to **Settings → Collaborators** and invite by username or email.

## Org repos

Access to an org repo is controlled by project membership. If a user is a member of the project the repo belongs to, they have read/write access to that repo.

Org **owners** have admin access to all repos in the org, regardless of project membership.

## Public repos

Public repos are readable by everyone. To push to a public repo, you still need to be a project member (for org repos) or the repo owner (for personal repos).

Public repos show up in search results and can be forked by any logged-in user.

## Archived repos

An archived repo is read-only. No pushes, no new PRs, no pipeline runs. It stays visible to everyone who had access before archiving, but nothing can change.

Archive from **Settings → General → Archive this repository**. You can unarchive it later.
