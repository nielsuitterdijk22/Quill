# Review & comment

## Browse the diff

On a PR page, the **Files changed** tab shows a unified diff of every file touched by the PR. Use the file tree on the left to jump between files.

Lines in green were added. Lines in red were removed.

## Leave a comment

Click any line number in the diff to open an inline comment. Write your comment and click **Start review** to queue it, or **Add single comment** to post it immediately.

Use **Start review** when you have multiple comments to leave — it batches them into a single review notification instead of spamming the author.

## Submit a review

After leaving your inline comments, click **Finish your review** (top right of the diff view) and choose one of:

- **Comment** — leave feedback without a verdict
- **Approve** — you're happy with the changes
- **Request changes** — you have concerns that must be addressed before this merges

Click **Submit review**.

## Dismiss a review

If a reviewer's **Request changes** has been addressed, an author or project owner can dismiss it. Go to the review on the PR timeline → **Dismiss review**. Add a note explaining why.

If the branch policy has **dismiss stale approvals** enabled, approvals are automatically dismissed when new commits are pushed. This prevents a PR from being merged on an old approval after significant changes.

## Resolve threads

After a discussion thread has been addressed, click **Resolve conversation** to collapse it. This keeps the PR page clean as reviews progress. Resolved threads are still accessible by clicking **Show resolved**.

## Re-request a review

After making changes, click the refresh icon next to a reviewer's name in the **Reviewers** panel to re-request their review.

## Who can review?

Anyone with read access to the repo can comment. Approvals only count from project members (or collaborators, for personal repos). See [Branch rules](../policies/branch-rules.md) for how required review counts are configured.
