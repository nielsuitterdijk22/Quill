# Secrets

Secrets are encrypted values you make available to workflow runs without
committing them to your repository. A workflow reads one as
`${{ secrets.NAME }}`, exactly as it would on GitHub Actions.

Typical uses: API tokens, registry credentials, deploy keys, signing keys.

## Where secrets live: three scopes

A secret belongs to exactly one of three scopes. Each scope is managed in a
different place and applies to a different set of runs.

| Scope | Applies to | Managed on |
| --- | --- | --- |
| **Project** | Every repository in the project | Project → Settings → **Secrets** |
| **Repository** | One repository's runs | Repo → Settings → **Secrets** |
| **Environment** | Runs that target that environment | Project → Settings → **Environment secrets** |

You need to be a **project admin** to view or manage secrets at any scope.

## How scopes combine

When a run starts, Quill gathers the secrets that apply to it and merges them in
this order:

```
project  →  repository  →  environment
(broadest)                 (most specific)
```

Later scopes win. So if a project secret and a repository secret share the same
name, the run sees the **repository** value. If the run also targets an
environment that defines that name, the **environment** value wins over both.

This lets you set a sensible default once at the project level and override it
for a specific repo or a specific environment (for example, a `DEPLOY_TOKEN`
that differs between `staging` and `production`).

### Targeting an environment

Environment secrets only reach a run that actually targets that environment. For
a manual run, choose it from the **Environment** dropdown on the **Run workflow**
form (the dropdown only appears when the project has environments). Push- and
pull-request-triggered runs do not target an environment, so they receive
project and repository secrets only.

## The inherited view

On a repository's **Secrets** settings, the secrets that reach the repo's runs
from *other* scopes are listed above the repo's own secrets, read-only and
labelled by scope. This is just a window into what a run will receive — you still
manage those values at their own scope (the project or the environment). A repo
secret of the same name overrides an inherited one.

## Adding or rotating a secret

1. Open the **Secrets** card for the scope you want.
2. Click **+ Add secret**, enter a `NAME` and value, and save. To change an
   existing value, click **Rotate** on its row and enter the new value.

Names follow GitHub Actions rules: they are upper-cased and may contain only
letters, digits, and underscores, and must start with a letter or underscore.
Names beginning with `GITHUB_` are reserved and rejected.

## Secrets are write-only

Once saved, a value is **never shown again** — not in the UI, not through the
API. Listings return only the name, scope, and last-updated time. If you lose a
value, rotate it to a new one rather than trying to read the old one back. This
is why the form warns that values can't be retrieved after saving.

## Log masking

Quill redacts known secret values from pipeline logs — both the live stream and
the stored logs — replacing any occurrence with `***`. A workflow that
accidentally echoes a secret will not leak it through Quill's UI or database.

A couple of consequences worth knowing:

- Very short values (fewer than 3 characters) are **not** masked, to avoid
  shredding unrelated log text. Don't rely on secrets that short.
- Masking is a literal substring match. A value that the workflow transforms
  (base64-encodes, URL-escapes, splits) before printing may no longer match and
  could appear in logs. Treat masking as a safety net, not a guarantee.

## Encryption at rest

Values are encrypted with AES-256-GCM before they are stored; the database holds
only ciphertext and a per-secret nonce, never plaintext. They are decrypted only
in memory when a run is dispatched.

## Limits

- Up to **100 secrets per scope**.
- Up to **64 KiB** per secret value.

## Operator note: the encryption key

Self-hosted instances set `QUILL_SECRET_ENCRYPTION_KEY` (see
[Self-hosting](../../SELF_HOSTING.md)). It is **required in production**; local
development falls back to a built-in insecure key. Treat the key as durable:
changing it makes every existing secret undecryptable, and affected runs will
fail rather than silently drop the secret — rotate the secrets themselves after
any key change.
