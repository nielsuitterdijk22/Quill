# Quill on Scaleway (cloud-native) — runbook

This directory takes the Quill MVP from a single VM to cloud-native Scaleway:
managed Kubernetes (Kapsule), managed PostgreSQL (RDB), Object Storage for
backups, Container Registry for images, DNS, IAM, and Secret Manager — all
provisioned by Terraform, with the app deployed as Kubernetes manifests by CI.

## Architecture

```text
                        ┌──────────────────────────────────────────────┐
   app.<domain> ───────▶ │  Kapsule (Kubernetes, Cilium, Traefik ingress) │
   git.<domain> ───────▶ │   namespace quill:                            │
   auth.<domain> ──────▶ │     web (Next.js)        api (Go)             │
   ssh.<domain>:2222 ──▶ │     forgejo (PVC)        dispatch (+dind)     │
                        │     zitadel (SSO)        pg-backup CronJob   │
                        └───────┬──────────────────────────┬───────────┘
              private network   │                          │
                        ┌───────▼──────────┐   ┌───────────▼─────────────┐
                        │  RDB PostgreSQL  │   │  Object Storage (S3)    │
                        │  quill/forgejo/  │   │  backups bucket         │
                        │  zitadel         │   │  postgres/ forgejo/     │
                        └──────────────────┘   └─────────────────────────┘
   DNS zone (Scaleway DNS) · Container Registry · Secret Manager · IAM apps
```

| Compose service | Scaleway replacement |
| --- | --- |
| `postgres` | RDB PostgreSQL-16 on the VPC private network (databases: `quill`, `forgejo`, `zitadel`), TLS + ACL, managed backups |
| `forgejo` | Kapsule Deployment + PVC; HTTP via `git.<domain>` ingress, SSH via a `type: LoadBalancer` service on 2222 |
| `api` | Deployment `api` (1 replica — see note in `k8s/api.yaml`) |
| `dispatch` | Deployment `dispatch` with a DinD sidecar (MVP trade-off, documented in `k8s/dispatch.yaml`) |
| `web` | Deployment `web` (2 replicas), `app.<domain>` ingress |
| `caddy` | Kapsule's built-in Traefik + cert-manager (Let's Encrypt, DNS-01 via Scaleway DNS) |
| `zitadel` | Deployment `zitadel`, DB on RDB, `auth.<domain>` via h2c IngressRoute |
| (manual backups) | `pg-backup` CronJob + forgejo dump sidecar → Object Storage, retention-pruned; plus RDB managed backups |

Region is pinned to `fr-par` (EU/Paris) per SUITE.md EU-residency requirement.

## What I need from you (credentials contract)

Create one IAM API key in the Scaleway console (or with `scw`) with enough
rights to run Terraform (e.g. `ScalewayFullAccess` or an equivalent policy on
the organization), then export:

```bash
export SCW_ACCESS_KEY="SCWXXXXXXXXXXXXXXXX"
export SCW_SECRET_KEY="11111111-1111-1111-1111-111111111111"
export SCW_DEFAULT_ORGANIZATION_ID="11111111-1111-1111-1111-111111111111"
export SCW_DEFAULT_REGION="fr-par"
export SCW_DEFAULT_ZONE="fr-par-1"
```

You only need these once — Terraform creates scoped keys for CI and backups
itself and prints them as outputs.

## 1. Provision the infrastructure

```bash
cd deploy/scaleway/terraform
cp terraform.tfvars.example terraform.tfvars
$EDITOR terraform.tfvars        # set domain= and backup_bucket_name=
terraform init
terraform plan                 # review
terraform apply                # ~10-15 min (Kapsule + RDB)
```

Outputs include `dns_name_servers` — delegate `domain` to them at your
registrar. CI/backup credentials: `terraform output ci_access_key` /
`ci_secret_key` / `backup_access_key` / `backup_secret_key` (sensitive).

## 2. Configure GitHub (one-time)

Repo **secrets**:

| Secret | Value |
| --- | --- |
| `SCW_CI_ACCESS_KEY` | `terraform output -raw ci_access_key` |
| `SCW_CI_SECRET_KEY` | `terraform output -raw ci_secret_key` |
| `KUBE_CONFIG` | `deploy/scaleway/scripts/make-ci-kubeconfig.sh` output (base64) |

Repo **variables**: `QUILL_DOMAIN`, `SCW_REGION` (`fr-par`), `SCW_PROJECT_ID`
(`terraform output -raw project_id`), `RDB_HOST` (`rdb_private_ip`),
`BACKUP_BUCKET` (`backup_bucket`), `REGISTRY` (`registry_endpoint`),
`LETSENCRYPT_EMAIL`, `NEXT_PUBLIC_ZITADEL_ISSUER` (`https://auth.<domain>`),
`NEXT_PUBLIC_ZITADEL_CLIENT_ID`, `NEXT_PUBLIC_ZITADEL_PROJECT_ID` (from the
Zitadel bootstrap, see deploy/compose/ZITADEL-SSO.md).

## 3. Bootstrap the cluster (one-time, needs admin kubeconfig)

```bash
export KUBECONFIG=deploy/scaleway/terraform/kubeconfig   # set kubeconfig_path in tfvars first
deploy/scaleway/scripts/sync-config.sh                   # writes k8s/.env.k8s from TF outputs
set -a; source deploy/scaleway/k8s/.env.k8s; set +a
export SCW_CI_ACCESS_KEY=$(terraform -chdir=deploy/scaleway/terraform output -raw ci_access_key)
export SCW_CI_SECRET_KEY=$(terraform -chdir=deploy/scaleway/terraform output -raw ci_secret_key)
export SCW_PROJECT_ID=$(terraform -chdir=deploy/scaleway/terraform output -raw project_id)
deploy/scaleway/scripts/bootstrap-cluster.sh             # cert-manager + SCW DNS-01 webhook + ClusterIssuer
deploy/scaleway/scripts/sync-secrets.sh                  # push Secret Manager values into the k8s Secret
export IMAGE_TAG=bootstrap
deploy/scaleway/scripts/render.sh
kubectl apply -f deploy/scaleway/k8s/rendered/
```

## 4. First deploy + provision

Push to `main` (or run **Deploy to Scaleway (Kapsule)** manually with
`provision=true` on the first run). The workflow builds and pushes the
images, applies the manifests, runs `k8s-provision.sh` (mints the Forgejo
admin token into the k8s Secret + Secret Manager), syncs DNS A records to the
LB IPs, and health-checks `https://app.<domain>/login`.

Then register the first user at `https://app.<domain>` — the first account
becomes the platform admin.

## 5. Verify

```bash
kubectl -n quill get pods,svc,ingress,certificate,cronjob
curl -fsS https://app.${QUILL_DOMAIN}/login          # web
curl -fsS https://git.${QUILL_DOMAIN}/api/healthz    # forgejo
# backups:
aws s3 ls s3://$BACKUP_BUCKET/postgres/quill/ --endpoint-url https://s3.fr-par.scw.cloud
aws s3 ls s3://$BACKUP_BUCKET/forgejo/ --endpoint-url https://s3.fr-par.scw.cloud
```

## Backup & restore

Three layers:

1. **Object Storage dumps** (portable, off-cluster): nightly `pg-backup`
   CronJob dumps `quill`, `forgejo`, `zitadel` from RDB →
   `s3://<bucket>/postgres/<db>/<db>-<ts>.sql.gz`; the forgejo pod sidecar
   runs `forgejo dump` daily → `s3://<bucket>/forgejo/forgejo-dump-<ts>.zip`.
   Objects older than `backup_retention_days` (default 30) are pruned by the
   same jobs; bucket versioning is on.
2. **RDB managed backups**: Scaleway-side snapshots on `rdb_backup_schedule`
   (daily 02:00 UTC, retention 30d) — restore via console/`scw rdb backup`.
3. **Forgejo PVC**: covered by the dump in layer 1 (git data is in the dump).

Restore (disaster recovery):

```bash
# 1. terraform apply (recreates infra) or reuse the running cluster
# 2. restore Postgres from the newest dump:
aws s3 cp s3://$BACKUP_BUCKET/postgres/quill/quill-<ts>.sql.gz - --endpoint-url https://s3.fr-par.scw.cloud | gunzip | \
  psql "postgres://quill:$RDB_ADMIN_PASSWORD@$RDB_HOST:5432/quill?sslmode=require"
#    (repeat for forgejo, zitadel; drop & recreate the DBs first if they exist)
# 3. restore Forgejo git data: kubectl -n quill exec deploy/forgejo -- \
#      forgejo dump -c /data/forgejo/conf/app.ini   # no — restore via:
#    download the dump and run `forgejo restore` / unzip over /data, or use an
#    RDB-independent path: aws s3 cp s3://$BACKUP_BUCKET/forgejo/forgejo-dump-<ts>.zip /tmp/ &&
#    kubectl -n quill exec deploy/forgejo -c forgejo -- sh -c 'cd /data && unzip -o /tmp/dump.zip'
# 4. redeploy the app (CI workflow), re-run k8s-provision if the token changed
```

## Operating

- **Deploy**: push to `main` (paths `backend/**`, `frontend/**`,
  `deploy/scaleway/**`) or run the workflow manually.
- **Scale**: `terraform apply -var node_max_size=8` (autoscaler on), or bump
  `replicas` in the manifests. `api` stays at 1 replica until webhook-trigger
  idempotency lands.
- **Secrets rotation**: update the value in Secret Manager, re-run
  `sync-secrets.sh`, then `kubectl -n quill rollout restart deploy/api deploy/web`.
- **Cost knobs**: `node_type`/`node_count` (Kapsule), `rdb_node_type`,
  `rdb_is_ha`, PVC size. All are TF variables.

## Security notes

- RDB is on the private network; the public endpoint is TLS + ACL-gated
  (`deploy_cidrs` — tighten to your egress IPs when known).
- The dispatch pod runs a privileged DinD sidecar (MVP trade-off, same as the
  compose stack's socket mount). Only trusted users may trigger pipelines;
  move runners to Forge when available.
- All app secrets live in Secret Manager; the k8s Secret is synced from it
  (`sync-secrets.sh`), never committed.
- TLS everywhere via Let's Encrypt (DNS-01 on Scaleway DNS); `QUILL_ENV=production`
  enforces Secure cookies.
