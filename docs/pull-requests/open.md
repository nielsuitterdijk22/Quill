# Open a pull request

A pull request is a proposal to merge one branch into another. It's where code review, policy checks, and automated gates all come together before a change lands.

## From the UI

After pushing a branch, Quill shows a banner on the repo page: **"Your branch was recently pushed — open a PR."** Click it.

Or go to the repo's **Pull requests** tab → **New pull request**. Choose your source branch and the target branch you want to merge into.

Fill in:

- **Title** — one line, present tense. "Add payment retry logic" not "Added payment retry logic."
- **Description** — what changed and why. Link to an issue or ticket if relevant.

Click **Open pull request**.

## From the command line

After pushing your branch:

```bash
git push origin feature/my-change
```

Quill prints a URL to open a PR directly. Most terminals let you Cmd/Ctrl+click it.

## Draft PRs

If your work isn't ready for review yet, open a **draft PR**. It signals to reviewers that feedback isn't needed yet. Pipelines still run on draft PRs — useful for catching issues early.

Click the arrow next to **Open pull request** → **Open as draft**.

When you're ready for review, click **Mark as ready for review** on the PR page.

## Cross-fork PRs

To propose changes from a fork back to the original repo, go to the PR creation page and click **Compare across forks**. Select your fork as the head repository and choose the branch.

## What happens after you open a PR

- Required reviewers are notified
- Pipelines defined with the `pull_request` trigger run automatically
- Policy gates are evaluated — the PR shows which gates are passing or pending
- Reviewers can comment, request changes, or approve

## Related

- [Review & comment](review.md)
- [Merge](merge.md)
- [Branch rules](../policies/branch-rules.md)
