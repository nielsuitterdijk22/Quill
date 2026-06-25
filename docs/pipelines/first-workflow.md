# Write your first workflow

Quill pipelines use GitHub Actions syntax. If you've written a GitHub Actions workflow before, you already know the format. Create a YAML file in `.github/workflows/` and Quill picks it up automatically.

## A minimal workflow

Create `.github/workflows/ci.yml`:

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Run tests
        run: npm test
```

Push this file and Quill will run it immediately.

## What the fields mean

**`on`** — what triggers this workflow. `push` runs it on pushes to specified branches. `pull_request` runs it whenever a PR is opened or updated. See [Triggers](triggers.md) for the full list.

**`jobs`** — one or more jobs that run in parallel by default. Each job runs in a fresh environment.

**`runs-on`** — the runner environment. `ubuntu-latest` is the standard choice.

**`steps`** — a sequence of tasks. `uses:` runs a pre-built action. `run:` runs a shell command.

## A more realistic example

```yaml
name: CI

on:
  push:
    branches: [main, 'release/**']
  pull_request:

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: '20'
      - run: npm ci
      - run: npm run lint

  test:
    runs-on: ubuntu-latest
    needs: lint
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: '20'
      - run: npm ci
      - run: npm test

  build:
    runs-on: ubuntu-latest
    needs: [lint, test]
    steps:
      - uses: actions/checkout@v4
      - run: npm ci && npm run build
```

`needs:` makes a job wait for another to succeed before it starts.

## Commit and push

```bash
git add .github/workflows/ci.yml
git commit -m "add CI workflow"
git push
```

Go to the repo's **Pipelines** tab. You'll see the run appear, usually within a few seconds.

## Related
- [Triggers](triggers.md)
- [Quill pipeline extensions](quill-extensions.md)
- [View logs](logs.md)
