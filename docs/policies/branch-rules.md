# Branch rules

Branch rules are the core of Quill's policy system. They define what must be true before a PR can merge into a protected branch.

## Set up a branch rule

Go to **Settings → Branch policies** (repo, project, or org level). Click **New policy**. Enter a branch pattern and configure the gates you want.

Branch patterns use glob syntax:

- `main` — matches exactly `main`
- `release/*` — matches any branch starting with `release/`
- `**` — matches all branches

## Available gates

### Required reviews

Require a minimum number of approvals before merging.

```text
Required approvals: 2
```

Approvals must come from project members (or collaborators, for personal repos). The PR author's own approval doesn't count.

**Dismiss stale approvals** — if enabled, approvals are automatically dismissed when new commits are pushed to the PR. This prevents merging on an old approval after the code has changed. Recommended for any branch you care about.

### Source branch restrictions

Restrict which branches can be merged into this branch.

```text
Allowed source branches: feature/*, bugfix/*
```

This blocks direct merges from `main` into `main` (preventing accidental self-merges), and ensures all changes come through proper feature branches. Useful for trunk-based development workflows.

### Require pull request

Block direct pushes to the branch — all changes must go through a PR. Prevents commits being pushed directly to `main` without review.

```text
Require pull request: true
```

### Block force push

Prevent `git push --force` to the branch. Protects commit history from being rewritten.

```text
Block force push: true
```

Recommended for any shared branch. Even with force push allowed, `--force-with-lease` is still safer than `--force`.

### Required status checks

Require specific pipeline jobs to pass before merging. Reference them as `{workflow name} / {job name}`:

```text
Required status checks:
  - CI / test
  - CI / lint
```

**Require branch to be up to date** — if enabled, the PR branch must have the latest commits from the target branch before the pipeline results are considered valid. Prevents a "passing tests on stale code" scenario.

## Example: protecting main

A reasonable starting policy for most teams:

```text
Branch: main
Gates:
  - Require pull request: true
  - Required approvals: 1
  - Dismiss stale approvals: true
  - Block force push: true
  - Required status checks: CI / test
```

## Example: strict release branch

For release branches with a higher bar:

```text
Branch: release/*
Gates:
  - Require pull request: true
  - Required approvals: 2
  - Dismiss stale approvals: true
  - Block force push: true
  - Required status checks: CI / test, CI / integration
  - Allowed source branches: main, hotfix/*
  - Time window: weekdays 10:00–16:00 UTC
```

## Related

- [Time windows](time-windows.md)
- [Change freeze](change-freeze.md)
- [External checks](external-checks.md)
- [Policy hierarchy](../concepts/policy-hierarchy.md)
