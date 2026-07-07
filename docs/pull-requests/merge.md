# Merge

## Merge a PR

When all required gates pass, the **Merge** button turns green. Click it to merge.

If any gate is pending or failing — required reviews not met, pipeline failing, time window closed, external check pending — the button is disabled and Quill shows which gates are blocking.

## Merge strategies

Choose the merge strategy from the dropdown next to the Merge button:

| Strategy | What it does |
|----------|-------------|
| **Merge commit** | Creates a merge commit. Preserves the full branch history. |
| **Squash and merge** | Squashes all commits into one, then merges. Keeps the target branch history clean. |
| **Rebase and merge** | Replays the PR commits on top of the target branch. Linear history, no merge commit. |

The available strategies are configured per repo in **Settings → General → Merge strategies**.

## What Quill checks before merging

The PR page shows a checklist of all gates. Common ones:

- **Required reviews** — minimum approvals from project members
- **No pending change requests** — all reviewers who requested changes have either approved or been dismissed
- **Pipelines passing** — required status checks have passed
- **Branch up to date** — the PR branch is not behind the target (if required)
- **Time window** — the current time is within the allowed merge window
- **Change freeze** — no active freeze on the target branch
- **External checks** — all configured external gates have returned a pass

If a gate is locked at the org or project level, it can't be bypassed — not even by an org owner.

## Auto-merge

If you want to merge as soon as all gates pass, enable **Auto-merge** on the PR. Quill will merge it automatically once everything clears.

Click **Enable auto-merge** on the PR page, choose the merge strategy, and confirm.

To cancel auto-merge, click **Disable auto-merge**.

## After merging

By default, the source branch is kept. Enable **Automatically delete head branches** in repo Settings to clean them up after merge.

Merged PRs stay in the PR list permanently with a **Merged** label. The diff and discussion are preserved.

## Related

- [Branch rules](../policies/branch-rules.md)
- [External checks](../policies/external-checks.md)
- [Change freeze](../policies/change-freeze.md)
