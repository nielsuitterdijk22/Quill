# Quill on Scaleway — serverless (scale-to-zero) runbook

Designed for near-zero traffic (hobby/2-user MVP) at **< €10/month**: everything
that can be serverless is serverless; only Forgejo (git data needs a durable
disk) and the CI dispatcher (needs a Docker daemon) run on one tiny VM — and
Forgejo's Postgres runs on that VM too (its serverless SDB never scaled to
zero, see DECISIONS.md).

## Architecture

```text
  app.<domain> ──CNAME──▶ Serverless Container: web (Next.js)     min_scale=0
  api.<domain> ──CNAME──▶ Serverless Container: api (Go)          min_scale=0
                                        │
  git.<domain> ──A──▶ DEV1-S VM: forgejo + forgejo-db (Postgres) + caddy(TLS)
  ssh.<domain> ──A──▶   + zitadel (+ zitadel-db) + dispatch + backup sidecars
  auth.<domain> ──A──▶   (git SSH :2222, git HTTP via caddy :443, auth via caddy h2c)

  Serverless SQL DB (PostgreSQL, min_cpu=0 → free when idle):
     quill                              (7 days of managed backups included free)
     (forgejo + zitadel Postgres run as containers on the VM, named volumes)

  Object Storage: backups bucket (nightly pg-backup Serverless Job + forgejo dump)
  Container Registry: images · DNS zone · Secret Manager · IAM keys (ci, backup, db)
```

| Component | Runs as | Why |
| --- | --- | --- |
| Quill api / web | Serverless Containers, `min_scale=0` | stateless — free when idle |
| quill Postgres | Serverless SQL DB, `min_cpu=0` | free compute when idle; storage only |
| Forgejo | 1× DEV1-S VM (Docker) | git repos need a persistent disk |
| forgejo-db (Forgejo Postgres) | postgres:16-alpine container on the VM, named volume | a serverless DB can't scale to zero while Forgejo holds a persistent connection pool (~EUR 22/mo CPU) — local Postgres on the VM is free |
| zitadel + its Postgres | containers on the same VM | IdP state + machine keys; already VM-local |
| dispatch (CI) | same VM | needs the Docker socket |
| backups | Serverless Job (cron, quill SDB only) + VM sidecars (forgejo dump, zitadel pg_dump) | pg_dump/forgejo dump → S3 |
| TLS | Caddy on the VM (Let's Encrypt HTTP-01) for git; Scaleway edge for container domains | no LB needed |

## Cost estimate (2026-11 actuals, excl. VAT, fr-par)

The 2026-11 bill showed "Serverless SQL Database - CPU consumption" at EUR 4.29
in 5.9 days (~EUR 22/mo projected) because Forgejo's persistent connection pool
kept the `forgejo` SDB at ~0.22 vCPU 24/7. With the forgejo SDB destroyed (its
data now lives in the VM-local forgejo-db container), the remaining bill is:

| Resource | /month |
| --- | --- |
| DEV1-S VM (2 vCPU / 2 GiB) + 20 GB root volume + flexible IP | ≈ €0.85 (2026-11 actuals: 0.29 + 0.09 + 0.16 in 5.9 days) |
| Serverless SQL DB `quill` — compute | €0 when idle (min_cpu=0); the nightly pg-backup job causes a brief daily blip |
| Serverless SQL DB `quill` — storage (incl. 7d backups) | ≈ €0.02–0.10 |
| Serverless Containers api/web | €0 when idle (free tier covers the tiny usage), cents when used |
| Backup job (nightly) + bucket + registry + secret manager | ≈ €0.10 |
| **Total** | **≈ €1–5/mo** (2026-11 projected without the forgejo SDB CPU: ~EUR 4.3/mo) |

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

Repo **secrets**: `SCW_ACCESS_KEY` / `SCW_SECRET_KEY` / `SCW_DEFAULT_ORGANIZATION_ID`
(your own key — CI runs Terraform and scw with it; it carries the scoped `quill-ci`
IAM policy), `VM_SSH_KEY` (output `vm_ssh_private_key`), `FORGEJO_ADMIN_TOKEN` and
`ZITADEL_MANAGEMENT_TOKEN` (filled by provision.sh, then re-apply), `SDB_USER` /
`SDB_PASSWORD` (your tfvars values).
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

## DNS records (add these at your registrar for black-tulip.nl)

The external domain validates via a TXT record (see below); the app records can
be set directly at your current registrar — no need to delegate NS to Scaleway
(unless you prefer that; then `create_dns_zone=true` and TF manages the zone):

| Record | Type | Value |
| --- | --- | --- |
| `_scaleway-challenge.quill` | TXT | the `validation_token` shown in console → Domains → quill.black-tulip.nl |
| `api.quill` | CNAME | the api container's native domain (`terraform output` / console → Containers → api) |
| `app.quill` | CNAME | the web container's native domain |
| `git.quill` | A | `terraform output -raw vm_public_ip` |
| `ssh.quill` | A | same VM IP |
| `auth.quill` | A | same VM IP |

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
