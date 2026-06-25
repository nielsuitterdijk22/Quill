# Quill pipeline extensions

Quill pipelines are compatible with GitHub Actions syntax. On top of that, Quill adds a set of native extensions for environment promotion, policy gates, and deployment tracking.

## Environment gates

Deploy to an environment with Quill's native environment gate. The step blocks until the environment's promotion policy is satisfied — required approvals, source branch rules, and any configured wait period.

```yaml
jobs:
  deploy-staging:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Deploy to staging
        uses: quill/deploy@v1
        with:
          environment: staging

  deploy-production:
    runs-on: ubuntu-latest
    needs: deploy-staging
    steps:
      - uses: actions/checkout@v4
      - name: Deploy to production
        uses: quill/deploy@v1
        with:
          environment: production
          requires-previous: staging   # blocks until staging deploy is recorded
```

If the environment has a required approval policy, the step pauses and notifies the configured approvers. The run resumes automatically when approval is granted.

## Policy-aware status reports

Quill automatically reports pipeline results back to open PRs. If your branch policy requires status checks to pass, you don't need any extra configuration — the pipeline result is the check.

To require a specific workflow to pass before merging, reference it by name in your branch policy:

```
Required status checks: CI / test
```

The format is `{workflow name} / {job name}`.

## Environment variables

Quill injects these variables into every run:

| Variable | Value |
|----------|-------|
| `QUILL_ORG` | The org slug |
| `QUILL_REPO` | The repo slug |
| `QUILL_REF` | The branch or tag that triggered the run |
| `QUILL_SHA` | The full commit SHA |
| `QUILL_RUN_ID` | The unique run ID |
| `QUILL_EVENT` | The trigger event (`push`, `pull_request`, `manual`) |

These mirror the `GITHUB_*` equivalents so most GitHub Actions work without modification.

## Secrets

Store sensitive values in **repo Settings → Secrets** or **project Settings → Secrets** (available to all repos in the project).

Reference them in workflows:

```yaml
steps:
  - name: Deploy
    run: ./deploy.sh
    env:
      DEPLOY_KEY: ${{ secrets.DEPLOY_KEY }}
```

Secrets are never printed in logs. If a step accidentally echoes one, Quill redacts it.

## Caching

```yaml
- uses: actions/cache@v4
  with:
    path: ~/.npm
    key: ${{ runner.os }}-node-${{ hashFiles('**/package-lock.json') }}
    restore-keys: |
      ${{ runner.os }}-node-
```

Caching is compatible with the standard `actions/cache` action.
