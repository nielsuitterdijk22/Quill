# pipeline-showcase

Every push to `main` runs:

| workflow | shows |
| --- | --- |
| `ci.yml` | a **local reusable** workflow (`./.github/workflows/test.yml`), a **cross-repo template** (`pipeline-templates/.../lint.yml@main`) and a **docker build** that `needs:` the tests |
| `secrets.yml` | a repository **secret** injected into a step and masked in the logs |
| `deploy.yml` | **build → push to the Scaleway registry → deploy a serverless container**, then curls the live URL until it serves this commit |
