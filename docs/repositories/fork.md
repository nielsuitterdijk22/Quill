# Fork a repository

Forking creates a copy of a repo under a different namespace. The fork is independent — changes in one don't affect the other — but you can open pull requests between them.

## Fork a repo

Go to the repo you want to fork and click **Fork** in the top right.

Choose the destination:
- **Your personal namespace** — `quill.so/your-username/repo-name`
- **An org you belong to** — `quill.so/your-org/repo-name`, assigned to a project you choose

Click **Fork**. Quill copies the full git history. Large repos may take a moment.

## Work on your fork

Clone your fork and work on it as you would any repo:

```bash
git clone git@quill.so:your-username/forked-repo.git
cd forked-repo
git checkout -b feature/my-change
```

## Open a PR back to the original

When you're ready to propose your changes to the original repo, open a PR from your fork's branch to the original repo's default branch. On the PR creation page, use the **compare across forks** option to select your fork as the source.

## Keep your fork up to date

Add the original repo as an upstream remote:

```bash
git remote add upstream git@quill.so:original-org/repo-name.git
git fetch upstream
git merge upstream/main
```

## Fork visibility

A fork inherits the visibility of its source repo at the time of forking. A fork of a private repo starts private. A fork of a public repo starts public.

You can change visibility in the fork's Settings afterward.
