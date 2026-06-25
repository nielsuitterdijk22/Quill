# Branches

## View branches

Go to your repo and click the **Branches** tab. You'll see all branches, their last commit, how far ahead/behind they are from the default branch, and whether they're protected.

## Create a branch

Locally:
```bash
git checkout -b feature/my-feature
git push -u origin feature/my-feature
```

## Default branch

The default branch is what new PRs target and what visitors see when they land on the repo. It's `main` by default. Change it in **Settings → General → Default branch**.

Before changing the default branch, make sure the branch you're switching to is up to date with your current default. Git history doesn't move automatically.

## Protected branches

A protected branch has policies applied to it — required reviews, merge checks, force push blocks, and so on. The **Branches** tab shows a lock icon next to protected branches.

To see what policies apply to a branch, go to **Settings → Branch policies** and look for patterns that match the branch name. Policies use glob patterns, so `main` matches exactly `main`, and `release/*` matches any branch starting with `release/`.

See [Branch rules](../policies/branch-rules.md) for the full policy reference.

## Delete a branch

After a PR merges, Quill can automatically delete the source branch. Turn this on in **Settings → General → Automatically delete head branches**.

To delete manually from the Branches tab, click the trash icon next to any branch. You can't delete the default branch.

Locally:
```bash
git branch -d feature/my-feature          # delete if merged
git push origin --delete feature/my-feature  # delete from remote
```

## Rename a branch

```bash
git branch -m old-name new-name
git push origin --delete old-name
git push origin new-name
git push --set-upstream origin new-name
```

If you're renaming the default branch, do it from **Settings → General** instead — Quill will update all open PRs targeting it.
