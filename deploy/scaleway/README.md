# Quill on Scaleway — serverless (scale-to-zero) runbook

Designed for near-zero traffic (hobby/2-user MVP) at **< €20/month**: everything
that can be serverless is serverless; only Forgejo (git data needs a durable
disk) and the CI dispatcher (needs a Docker daemon) run on one tiny VM.

## Architecture

```text
  app.<domain> ──CNAME──▶ Serverless Container: web (Next.js)     min_scale=0
  api.<domain> ──CNAME──▶ Serverless Container: api (Go)          min_scale=0
  auth.<domain> ─CNAME──▶ Serverless Container: zitadel (SSO)     min_scale=0
                                        │
  git.<domain> ──A──▶ DEV1-S VM: forgejo + caddy(TLS) + dispatch + backup sidecar
  ssh.<domain> ──A──▶   (git SSH :2222, git HTTP via caddy :443)

  Serverless SQL DBs (PostgreSQL, min_cpu=0 → free when idle):
     quill · forgejo · zitadel        (7 days of managed backups included free)

  Object Storage: backups bucket (nightly pg-backup Serverless Job + forgejo dump)
  Container Registry: images · DNS zone · Secret Manager · IAM keys (ci, backup, db)
```

| Component | Runs as | Why |
| --- | --- | --- |
| Quill api / web / zitadel | Serverless Containers, `min_scale=0` | stateless — free when idle |
| Postgres ×3 | Serverless SQL DB, `min_cpu=0` | free compute when idle; storage only |
| Forgejo | 1× DEV1-S VM (Docker) | git repos need a persistent disk |
| dispatch (CI) | same VM | needs the Docker socket |
| backups | Serverless Job (cron) + VM sidecar | pg_dump → S3; forgejo dump → S3 |
| TLS | Caddy on the VM (Let's Encrypt HTTP-01) for git; Scaleway edge for container domains | no LB needed |

## Cost estimate (2026 list prices, excl. VAT, fr-par)

| Resource | Qty | /month |
| --- | --- | --- |
| DEV1-S VM (2 vCPU / 2 GiB) | 1 | ≈ €7 |
| VM root volume b_ssd | 20 GB × €0.086 | €1.72 |
| Serverless SQL DBs — compute | idle (min_cpu=0) | €0 |
| Serverless SQL DBs — storage (incl. 7d backups) | ~1–3 GB total | ≈ €0.10–0.30 |
| Serverless Containers api/web/zitadel | scale to zero | €0 when idle, cents when used |
| Backup job (nightly) + bucket | 30 runs + a few GB | ≈ €0.40 |
| Registry / DNS / Secret Manager / IAM / dynamic IP | — | ≈ €0 |
| **Total** | | **≈ €9–10/mo** (well under €20) |

When traffic grows: raise `sdb_max_cpu` / `container_max_scale`, or move Forgejo
back to Kapsule (the previous `deploy/scaleway` revision had full k8s manifests).

## Credentials contract (what I need from you)

```bash
export SCW_ACCESS_KEY="SCW..."
export SCW_SECRET_KEY="..."
export SCW_DEFAULT_ORGANIZATION_ID="..."
export SCW_DEFAULT_REGION="fr-par"
export SCW_DEFAULT_ZONE="fr-par-1"
```

## 1. Provision

```bash
cd deploy/scaleway/terraform
cp terraform.tfvars.example terraform.tfvars   # set domain, letsencrypt_email, backup_bucket_name
terraform init -backend=false                  # bootstrap: local state first (creates the state bucket too)
terraform apply -backend=false -auto-approve   # creates bucket, SDBs, containers, VM, DNS...
# then switch to the shared state bucket (created above) for CI + team use:
terraform init -backend-config="endpoint=https://s3.fr-par.scw.cloud" \
  -backend-config="bucket=<backup_bucket_name>-tfstate" -backend-config="key=quill.tfstate" \
  -backend-config="region=fr-par" -backend-config="access_key=$SCW_ACCESS_KEY" \
  -backend-config="secret_key=$SCW_SECRET_KEY" -backend-config="skip_credentials_validation=true" \
  -backend-config="skip_region_validation=true" -backend-config="skip_metadata_api_check=true" \
  -backend-config="skip_requesting_account_id=true"
terraform apply -auto-approve                  # migrate state into the bucket
```

Delegate `domain` to the output `dns_name_servers` at your registrar.

## 2. Configure GitHub (one-time)

Repo **secrets**: `SCW_CI_ACCESS_KEY` / `SCW_CI_SECRET_KEY` (outputs `ci_access_key` /
`ci_secret_key`), `VM_SSH_KEY` (output `vm_ssh_private_key`), `FORGEJO_ADMIN_TOKEN` and
`ZITADEL_MANAGEMENT_TOKEN` (filled by provision.sh, then re-apply).
Repo **vars**: `QUILL_DOMAIN`, `SCW_REGION`, `SCW_PROJECT_ID`, `BACKUP_BUCKET`,
`REGISTRY`, `LETSENCRYPT_EMAIL`, `VM_IP`, `NEXT_PUBLIC_ZITADEL_ISSUER` (=`https://auth.<domain>`),
`NEXT_PUBLIC_ZITADEL_CLIENT_ID`, `NEXT_PUBLIC_ZITADEL_PROJECT_ID` (from the Zitadel console).

## 3. First deploy + provision

Push to `main` (or run **Deploy to Scaleway (serverless)** manually with
`provision=true` on the first run). The workflow:

1. builds + pushes `quill-backend`, `quill-web`, `quill-backup`, `quill-zitadel` images
2. `terraform apply -var image_tag=<sha> ...` → serverless containers/job redeploy with the new image
3. SSH-deploys the VM compose stack when `deploy/scaleway/vm/**` changes
4. `provision.sh` (first boot): Forgejo admin + token + system webhook → `https://api.<domain>/api/v1/webhooks/forgejo`, Zitadel machine PAT harvested from container logs → Secret Manager; then re-apply TF with the tokens
5. `dns-sync.sh`: git/ssh A records → VM IP; health-checks `https://app.<domain>/login` and `https://git.<domain>/api/healthz`

Then register the first user at `https://app.<domain>` (first account = platform
admin). In the Zitadel console (`https://auth.<domain>`, admin login from
steps.yaml with forced password change), create the OIDC app and set its
client/project id as repo vars; re-run the workflow to rebuild the web image.

## 4. Verify & backups

```bash
curl -fsS https://app.${QUILL_DOMAIN}/login
curl -fsS https://git.${QUILL_DOMAIN}/api/healthz
aws s3 ls s3://$BACKUP_BUCKET/postgres/ --endpoint-url https://s3.fr-par.scw.cloud
aws s3 ls s3://$BACKUP_BUCKET/forgejo/  --endpoint-url https://s3.fr-par.scw.cloud
```

Restore: SDB has 7 days of managed backups built in; for older/point-in-time,
`aws s3 cp s3://$BUCKET/postgres/quill/quill-<ts>.sql.gz - | gunzip | psql "$QUILL_DB_URL"`;
Forgejo git data: download `forgejo/forgejo-dump-<ts>.zip`, `docker compose exec forgejo sh -c 'cd /data && unzip -o /tmp/dump.zip'`.

## Operating

- Deploys: push to `main` (paths `backend/**`, `frontend/**`, `deploy/scaleway/**`) or manual dispatch.
- Cost knobs (TF vars): `vm_type`, `vm_root_volume_gb`, `sdb_max_cpu`, `container_cpu`,
  `container_memory_mb`, `container_max_scale`, `backup_retention_days`.
- Secrets rotation: update in Secret Manager, re-apply TF (containers/job get new env).
- The dispatch API (VM :8090) is internet-exposed but HMAC-protected; the SSH
  port is restricted via `ssh_cidrs`. Tighten both when you have stable egress IPs.
