# Repositories

A repository is where your code lives. It's a standard Git repo — you clone it, push to it, open pull requests against it.

Every repo lives at `quill.so/{owner}/{repo-name}` where `owner` is either your username (personal repo) or an org name (org repo).

## Personal vs. org repos

**Personal repos** live under your username: `quill.so/jane/my-project`. Only you can manage them by default, though you can add collaborators. Good for side projects, experiments, or anything that doesn't need an organizational home.

**Org repos** live under the org: `quill.so/acme/payments-service`. They're owned by the org, assigned to a project, and governed by that project's policies. Good for anything a team collaborates on.

## Visibility

| Visibility | Who can see it |
|------------|---------------|
| **Private** | Only org members with access (or you, for personal repos) |
| **Internal** | All members of the org |
| **Public** | Anyone, including unauthenticated visitors |

Default is private. You can change visibility in the repo's Settings tab.

## What's inside a repo

- **Code** — browse files and directories, view raw content
- **Commits** — full history with diffs
- **Branches** — all branches, with protection status
- **Pull requests** — open, merged, and closed PRs
- **Pipelines** — CI/CD workflows and their run history
- **Settings** — visibility, default branch, branch policies, danger zone

## Default branch

Every repo has a default branch (usually `main`). This is the branch PRs target by default and the one that shows up when you visit the repo. You can change it in Settings.

## Related

- [Create a repository](../repositories/create.md)
- [Clone & push](../repositories/clone-and-push.md)
- [Visibility & access](../repositories/visibility.md)
- [Projects](projects.md)
