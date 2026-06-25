# Clone & push

## Clone a repo

Every repo page has a **Clone** button in the top right. Click it to get the clone URL.

**HTTPS:**
```bash
git clone https://quill.so/your-org/my-repo.git
```

**SSH** (recommended — no token prompt on push):
```bash
git clone git@quill.so:your-org/my-repo.git
```

See [Set up SSH](../getting-started/ssh-keys.md) if you haven't added your key yet.

## Push to a branch

```bash
git checkout -b feature/my-change
git add .
git commit -m "describe the change"
git push origin feature/my-change
```

After pushing, Quill shows a banner in the repo with a link to open a PR.

## HTTPS authentication

If you're using HTTPS, you need a personal access token as your password — not your account password.

Generate one at **Settings → Access tokens → New token**. Give it a name, set an expiry, and check the `write:repository` scope. Copy it immediately — it won't be shown again.

Then push:
```bash
# Git will prompt for username and password
# Username: your-username
# Password: paste the access token
git push origin main
```

To avoid the prompt every time, store the token in your Git credential helper:
```bash
git config --global credential.helper store
```

## Force push

Force pushing is blocked on protected branches by default. If you need to rewrite history on a feature branch, force push is fine:

```bash
git push --force-with-lease origin feature/my-change
```

`--force-with-lease` is safer than `--force` — it fails if someone else pushed to the branch since you last fetched.

## Related
- [Set up SSH](../getting-started/ssh-keys.md)
- [Branches](branches.md)
- [Open a pull request](../pull-requests/open.md)
