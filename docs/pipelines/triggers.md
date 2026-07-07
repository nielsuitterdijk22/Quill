# Triggers

A trigger defines when a workflow runs. Set it with the `on:` key in your workflow file.

## Supported triggers

### `push`

Runs when commits are pushed to matching branches or tags.

```yaml
on:
  push:
    branches:
      - main
      - 'release/**'
    tags:
      - 'v*'
```

Leave `branches` out to run on all pushes:

```yaml
on: push
```

### `pull_request`

Runs when a PR is opened, synchronized (new commits pushed), or reopened.

```yaml
on:
  pull_request:
    branches:
      - main        # only PRs targeting main
```

Pipeline results from `pull_request` runs show up as status checks on the PR. If a branch policy requires status checks to pass, this is how they get reported.

### `manual`

Runs when triggered manually from the Pipelines tab or via the API.

```yaml
on:
  workflow_dispatch:
```

Manual runs appear in the Pipelines tab with a **Run workflow** button. You can pass inputs to a manual run:

```yaml
on:
  workflow_dispatch:
    inputs:
      environment:
        description: 'Target environment'
        required: true
        default: 'staging'
        type: choice
        options:
          - staging
          - production
```

## Combining triggers

A single workflow can have multiple triggers:

```yaml
on:
  push:
    branches: [main]
  pull_request:
  workflow_dispatch:
```

## Filtering by path

Run a workflow only when specific files change:

```yaml
on:
  push:
    paths:
      - 'src/**'
      - 'package.json'
```

Useful for monorepos — only run the backend pipeline when backend files change.

## Skipping a run

Add `[skip ci]` to your commit message to skip all pipelines for that push:

```bash
git commit -m "update docs [skip ci]"
```
