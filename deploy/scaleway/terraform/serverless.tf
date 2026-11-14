# Serverless core: the quill scale-to-zero PostgreSQL database (min_cpu = 0 —
# no charge when idle) + the api/web containers (min_scale = 0) + a cron backup
# job.
#
# Since 2026-11-14 there is no VM at all: git lives in the object-storage
# engine (bucket + Postgres referee) and CI runs on ephemeral instances
# started by the broker (ci_broker.tf).

# ---- Serverless SQL databases ----------------------------------------------------

resource "scaleway_sdb_sql_database" "dbs" {
  for_each = toset(local.sdb_names)

  name       = each.value
  project_id = scaleway_account_project.quill.id
  region     = var.region
  version    = var.sdb_version
  min_cpu    = var.sdb_min_cpu # 0 = scale to zero when idle
  max_cpu    = var.sdb_max_cpu
}

# ---- Container namespace -----------------------------------------------------------

resource "scaleway_container_namespace" "quill" {
  name        = "quill"
  project_id  = scaleway_account_project.quill.id
  region      = var.region
  description = "Quill serverless containers (api, web, zitadel)"
}

# ---- api (Quill Go backend) ---------------------------------------------------------

resource "scaleway_container" "api" {
  lifecycle {
    # registry_image is MANAGED again: deploys pin image_tag to the git SHA, so
    # every apply updates the image and forces a new container revision (that
    # is what actually redeploys). The old ignore_changes - justified by 412
    # races when per-sha updates raced a "latest" auto-redeploy - left the
    # image frozen, which is how builds kept silently not going live
    # ("changes didn't land", 2026-11-11). CI pushes the sha tag before this
    # apply runs, so the image always exists.
    ignore_changes = [deploy]
  }

  namespace_id   = scaleway_container_namespace.quill.id
  name           = "api"
  region         = var.region
  registry_image = "${local.registry}/quill-backend:${var.image_tag}"
  deploy         = true

  port                   = 8080
  cpu_limit              = var.container_cpu
  memory_limit           = var.container_memory_mb
  min_scale              = 0 # scale to zero — free when idle
  max_scale              = var.container_max_scale
  privacy                = "public"
  protocol               = "http1"
  https_connections_only = true
  timeout                = 300

  environment_variables = {
    QUILL_ENV                  = "production"
    QUILL_HTTP_ADDR            = ":8080"
    QUILL_LOG_FORMAT           = "json"
    QUILL_CORS_ALLOWED_ORIGINS = "https://${local.app_host}"
    # The GitHub import OAuth cookies must be shared between the app host (where
    # the flow starts via the frontend rewrite) and the API host (where the
    # GitHub OAuth callback lands), so they get the parent domain.
    QUILL_COOKIE_DOMAIN = var.domain
    QUILL_AUTH_PROVIDER = "zitadel"
    ZITADEL_ISSUER      = "https://${local.auth_host}"
    # Pipelines run in queue mode: broker + ephemeral CI instances
    # (ci_broker.tf). The Forgejo VM and its inline dispatcher are gone
    # (2026-11-14).
    QUILL_PIPELINE_QUEUE_MODE = var.ci_queue_mode ? "true" : "false"
    QUILL_BROKER_URL          = "https://${scaleway_container.broker.domain_name}"
    # The own git engine serves every repository. Forgejo is off: no
    # FORGEJO_* settings means the api never calls it (2026-11-14).
    QUILL_GIT_ENGINE_MODE = var.git_engine_mode
    QUILL_GIT_S3_ENDPOINT = "https://s3.${var.region}.scw.cloud"
    QUILL_GIT_S3_REGION   = var.region
    QUILL_GIT_S3_BUCKET   = scaleway_object_bucket.repos.name
    QUILL_GIT_PUBLIC_URL  = local.git_public_url
    QUILL_GIT_HOST        = local.git_host
  }
  secret_environment_variables = {
    QUILL_DATABASE_URL             = local.db_url["quill"]
    QUILL_JWT_SECRET               = random_password.jwt_secret.result
    QUILL_SECRET_ENCRYPTION_KEY    = base64encode(random_password.secret_encryption_key.result)
    QUILL_WEBHOOK_SECRET           = random_password.webhook_secret.result
    QUILL_PIPELINE_DISPATCH_SECRET = local.dispatch_secret
    QUILL_GIT_S3_ACCESS_KEY        = scaleway_iam_api_key.git_engine.access_key
    QUILL_GIT_S3_SECRET_KEY        = scaleway_iam_api_key.git_engine.secret_key
    ZITADEL_MANAGEMENT_TOKEN       = var.zitadel_management_token
    GITHUB_CLIENT_ID               = var.github_client_id
    GITHUB_CLIENT_SECRET           = var.github_client_secret
    CREEM_API_KEY                  = var.creem_api_key
    CREEM_WEBHOOK_SECRET           = var.creem_webhook_secret
    CREEM_TEAM_PRODUCT_ID          = var.creem_team_product_id
    CREEM_PRO_PRODUCT_ID           = var.creem_pro_product_id
  }

  liveness_probe {
    interval          = "10s"
    failure_threshold = 3
    timeout           = "5s"
    http {
      path = "/readyz"
    }
  }
}

# ---- web (Next.js frontend) -----------------------------------------------------------

resource "scaleway_container" "web" {
  lifecycle {
    # registry_image is MANAGED again: deploys pin image_tag to the git SHA, so
    # every apply updates the image and forces a new container revision (that
    # is what actually redeploys). The old ignore_changes - justified by 412
    # races when per-sha updates raced a "latest" auto-redeploy - left the
    # image frozen, which is how builds kept silently not going live
    # ("changes didn't land", 2026-11-11). CI pushes the sha tag before this
    # apply runs, so the image always exists.
    ignore_changes = [deploy]
  }

  namespace_id   = scaleway_container_namespace.quill.id
  name           = "web"
  region         = var.region
  registry_image = "${local.registry}/quill-web:${var.image_tag}"
  deploy         = true

  port                   = 3001
  cpu_limit              = var.container_cpu
  memory_limit           = var.container_memory_mb
  min_scale              = 0
  max_scale              = var.container_max_scale
  privacy                = "public"
  protocol               = "http1"
  https_connections_only = true
  timeout                = 60

  environment_variables = {
    NODE_ENV           = "production"
    HOSTNAME           = "0.0.0.0"
    QUILL_API_BASE_URL = "https://${local.api_host}" # server components call the api container
    AUTH_URL           = "https://${local.app_host}"
    # Runtime override of the build-time NEXT_PUBLIC_ZITADEL_ISSUER build-arg:
    # server-side NextAuth code (auth.ts) reads process.env at runtime, so this
    # fixes sign-in immediately on container update without an image rebuild.
    # The client bundle (zitadel-bridge) still uses the baked value until the
    # CI var NEXT_PUBLIC_ZITADEL_ISSUER is updated and the image rebuilt.
    NEXT_PUBLIC_ZITADEL_ISSUER = "https://${local.auth_host}"
  }
  secret_environment_variables = {
    AUTH_SECRET = random_password.jwt_secret.result
    # Zitadel client secret for the confidential OIDC app: enables Auth.js
    # refresh-token rotation so sessions renew instead of expiring hourly.
    # Empty keeps the app a public PKCE client (development).
    ZITADEL_CLIENT_SECRET = var.zitadel_client_secret
  }

  liveness_probe {
    interval          = "10s"
    failure_threshold = 3
    tcp               = true
    timeout           = "5s"
  }
}

# ---- custom domains ---------------------------------------------------------------------

resource "scaleway_container_domain" "api" {
  container_id = scaleway_container.api.id
  hostname     = local.api_host
  region       = var.region
}

# git.<domain> on the api container (D2). Only once the Cloudflare `git`
# record is a CNAME to the api container — Scaleway issues the certificate at
# create time and a binding made while DNS still answers the VM goes to error
# (recreate with -replace, the auth lesson of 2026-11-13).
resource "scaleway_container_domain" "git" {
  count        = var.git_host_on_api ? 1 : 0
  container_id = scaleway_container.api.id
  hostname     = local.git_host
}

resource "scaleway_container_domain" "app" {
  container_id = scaleway_container.web.id
  hostname     = local.app_host
  region       = var.region
}

# ---- backup job (nightly pg_dump of the quill SDB -> Object Storage) --------------------
# The forgejo database is not dumped here anymore: it lives on the VM and is
# covered by the forgejo-backup sidecar (`forgejo dump` includes the DB dump).

resource "scaleway_job_definition" "pg_backup" {
  name       = "pg-backup"
  project_id = scaleway_account_project.quill.id
  region     = var.region

  image_uri              = "${local.registry}/quill-backup:${var.image_tag}"
  command                = "/usr/local/bin/backup-pg.sh"
  cpu_limit              = 500
  memory_limit           = 512
  local_storage_capacity = 1024

  env = {
    S3_ENDPOINT           = "https://s3.${var.region}.scw.cloud"
    BACKUP_BUCKET         = scaleway_object_bucket.backups.name
    AWS_ACCESS_KEY_ID     = scaleway_iam_api_key.backup.access_key
    AWS_SECRET_ACCESS_KEY = scaleway_iam_api_key.backup.secret_key
    BACKUP_RETENTION_DAYS = tostring(var.backup_retention_days)
    QUILL_DATABASE_URL    = local.db_url["quill"]
    # C1: also dump the zitadel SDB nightly (the VM's zitadel-backup sidecar
    # covers only the old VM-local database and dies with the VM).
    ZITADEL_DATABASE_URL = local.db_url["zitadel"]
  }

  cron {
    schedule = var.backup_schedule
    timezone = "UTC"
  }

  retry_policy {
    max_retries = 2
  }
}
