# ---- Zitadel as a Serverless Container (serverless epic, PR C1) --------------------
#
# Zitadel moves off the VM (cloud-init compose: zitadel + zitadel-db) onto its
# own scale-to-zero Serverless Container, backed by its OWN serverless SQL
# database (`zitadel` — the VM's postgres container had a persistent pool, but
# the container's min_scale=0 drops that pool when idle, so the SDB scales to
# zero like the quill one; the owner picked SDB over a fixed RDB node for the
# idle-cost floor, 2026-11-12).
#
# Boot path discipline (the roadmap's C1 rule): the container runs `zitadel
# start` ONLY — migrations/`setup` never run on a cold start (multi-instance
# concurrency would race them). `scaleway_job_definition.zitadel_setup` runs
# the same image with `zitadel setup`; CI starts it after every apply. Until
# the Cloudflare auth.<domain> record is repointed (manual, registrar-side),
# this stack is INERT — the record still answers the VM, which keeps serving
# auth until the migration is verified and the owner flips DNS.

# The zitadel image's CMD is `start` (run mode). The old `start-from-init`
# first-boot wrapper (instance creation + PAT harvest to /output) only ever
# mattered when the instance was created fresh; production data now migrates
# with the database, and the PAT lives in Secret Manager. See DECISIONS.md.

resource "scaleway_container" "zitadel" {
  lifecycle {
    ignore_changes = [deploy]
  }

  namespace_id   = scaleway_container_namespace.quill.id
  name           = "zitadel"
  region         = var.region
  registry_image = "${local.registry}/quill-zitadel:${var.image_tag}"
  deploy         = true

  port      = 8080
  cpu_limit = var.container_cpu
  # Zitadel cold-start (JVM-sized Go binary + first DB roundtrips) fits in
  # 1024 MB; the concurrent-request budget rides on max_scale + the edge.
  memory_limit = var.container_memory_mb
  min_scale    = 0 # scale to zero — free when idle
  max_scale    = 1 # single instance until multi-instance behavior is verified

  privacy                = "public"
  protocol               = "http1"
  https_connections_only = true
  timeout                = 300

  environment_variables = {
    ZITADEL_EXTERNALDOMAIN = local.auth_host
    ZITADEL_EXTERNALPORT   = "443" # TLS terminates at the Scaleway edge
    ZITADEL_EXTERNALSECURE = "true"
    ZITADEL_TLS_MODE       = "external"

    ZITADEL_DATABASE_POSTGRES_HOST = local.sdb_host["zitadel"]
    ZITADEL_DATABASE_POSTGRES_PORT = local.sdb_port["zitadel"]
    # The SDB IAM user is both the app and the admin (same credentials model
    # as the quill SDB; no per-role users on SDBs).
    ZITADEL_DATABASE_POSTGRES_DATABASE       = "zitadel"
    ZITADEL_DATABASE_POSTGRES_USER_USERNAME  = local.db_user
    ZITADEL_DATABASE_POSTGRES_USER_SSL_MODE  = "require"
    ZITADEL_DATABASE_POSTGRES_ADMIN_USERNAME = local.db_user
    ZITADEL_DATABASE_POSTGRES_ADMIN_SSL_MODE = "require"
  }

  secret_environment_variables = {
    ZITADEL_MASTERKEY                        = random_password.zitadel_masterkey.result
    ZITADEL_DATABASE_POSTGRES_USER_PASSWORD  = local.db_password
    ZITADEL_DATABASE_POSTGRES_ADMIN_PASSWORD = local.db_password
  }

  liveness_probe {
    interval          = "10s"
    failure_threshold = 3
    timeout           = "5s"
    http {
      path = "/debug/healthz"
    }
  }

}

# Custom domain: TLS at the Scaleway edge. The live auth.<domain> A record
# still points at the VM until the owner repoints it at this container's
# domain_name (manual, Cloudflare registrar — see deploy/scaleway/README.md).
resource "scaleway_container_domain" "auth" {
  container_id = scaleway_container.zitadel.id
  hostname     = local.auth_host
  region       = var.region
}

# The setup job: migrations OUTSIDE the serving path, started by CI after the
# apply (see deploy-scaleway.yml "Run zitadel setup job"). Masterkey and the
# DB password are injected from Secret Manager, never from plaintext env.
resource "scaleway_job_definition" "zitadel_setup" {
  name       = "zitadel-setup"
  project_id = scaleway_account_project.quill.id
  region     = var.region

  # The image ENTRYPOINT is [/bin/sh -c], so `command` is a shell snippet —
  # same pattern the VM wrapper used, minus the PAT-harvest loop.
  image_uri = "${local.registry}/quill-zitadel:${var.image_tag}"
  command   = "exec /app/zitadel setup --masterkey \"$ZITADEL_MASTERKEY\" --tlsMode external"

  cpu_limit              = 500
  memory_limit           = 1024
  local_storage_capacity = 1024

  env = {
    ZITADEL_DATABASE_POSTGRES_HOST           = local.sdb_host["zitadel"]
    ZITADEL_DATABASE_POSTGRES_PORT           = local.sdb_port["zitadel"]
    ZITADEL_DATABASE_POSTGRES_DATABASE       = "zitadel"
    ZITADEL_DATABASE_POSTGRES_USER_USERNAME  = local.db_user
    ZITADEL_DATABASE_POSTGRES_USER_SSL_MODE  = "require"
    ZITADEL_DATABASE_POSTGRES_ADMIN_USERNAME = local.db_user
    ZITADEL_DATABASE_POSTGRES_ADMIN_SSL_MODE = "require"
  }

  # secret_reference injects Secret Manager values as env vars in the job
  # run — nothing sensitive lands in the job definition payload.
  secret_reference {
    secret_id   = scaleway_secret.s["zitadel/masterkey"].id
    environment = "ZITADEL_MASTERKEY"
  }

  secret_reference {
    secret_id   = scaleway_secret.s["db/password"].id
    environment = "ZITADEL_DATABASE_POSTGRES_USER_PASSWORD"
  }

  secret_reference {
    secret_id   = scaleway_secret.s["db/password"].id
    environment = "ZITADEL_DATABASE_POSTGRES_ADMIN_PASSWORD"
  }

  retry_policy {
    max_retries = 1
  }

  # No cron: manual/CI-triggered only. Until the DB data is migrated and the
  # job has run once, the container cold-starts into errors — harmless while
  # DNS still answers the VM.
}
