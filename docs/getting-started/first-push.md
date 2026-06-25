# Push your first commit

Once you've created a repo, here's how to get your code into it.

## Starting from scratch

If you created a new repo without initializing it, Quill shows you the exact commands. Copy them and run them locally:

```bash
echo "# my-project" >> README.md
git init
git add README.md
git commit -m "first commit"
git branch -M main
git remote add origin https://quill.so/your-username/my-project.git
git push -u origin main
```

## Pushing an existing local repo

If you already have a repo locally:

```bash
git remote add origin https://quill.so/your-username/my-project.git
git branch -M main
git push -u origin main
```

## Authentication

**HTTPS** — when you push over HTTPS, Quill will ask for your username and a personal access token. You can generate one in **Settings → Access tokens**. Your account password won't work here — tokens only.

**SSH** — if you've [set up an SSH key](ssh-keys.md), pushes just work without any credential prompt. Recommended for day-to-day use.

## Verify it worked

After pushing, go to `quill.so/your-username/my-project` in your browser. Your files should be there.

## Next steps

- [Set up SSH](ssh-keys.md) — stop typing tokens on every push
- [Branches](../repositories/branches.md) — working with feature branches
- [Open a pull request](../pull-requests/open.md) — when you're ready to merge
