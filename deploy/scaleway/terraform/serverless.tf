# Serverless core: the quill scale-to-zero PostgreSQL database (min_cpu = 0 —
# no charge when idle) + the api/web containers (min_scale = 0) + a cron backup
# job.
#
# Forgejo itself is NOT serverless: git data needs a durable disk and CI needs a
# Docker daemon, so it runs on one small VM (forgejo_vm.tf) — and its Postgres
# runs on that VM too (forgejo-db container in cloud-init.yml). The serverless
# "forgejo" SDB was removed because Forgejo's persistent connection pool kept
# it at ~0.22 vCPU 24/7 (~EUR 22/mo CPU); see DECISIONS.md.

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
    # Image updates flow through the registry: CI pushes the "latest" tag and
    # Scaleway auto-redeploys. Letting terraform update registry_image races
    # Scaleway's rolling redeploy and intermittently fails with 412.
    ignore_changes = [registry_image, deploy]
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
    FORGEJO_BASE_URL           = "https://${local.git_host}"
    FORGEJO_PUBLIC_URL         = "https://${local.git_host}"
    QUILL_CORS_ALLOWED_ORIGINS = "https://${local.app_host}"
    QUILL_AUTH_PROVIDER        = "zitadel"
    ZITADEL_ISSUER             = "https://${local.auth_host}"
    # The api triggers pipelines by calling the dispatcher on the Forgejo VM
    # (HMAC-protected with QUILL_PIPELINE_DISPATCH_SECRET).
    QUILL_PIPELINE_DISPATCH_URL = "http://${scaleway_instance_server.forgejo.public_ips[0].address}:8090"
  }
  secret_environment_variables = {
    QUILL_DATABASE_URL             = local.db_url["quill"]
    QUILL_JWT_SECRET               = random_password.jwt_secret.result
    QUILL_SECRET_ENCRYPTION_KEY    = base64encode(random_password.secret_encryption_key.result)
    QUILL_WEBHOOK_SECRET           = random_password.webhook_secret.result
    QUILL_PIPELINE_DISPATCH_SECRET = random_password.dispatch_secret.result
    FORGEJO_ADMIN_TOKEN            = var.forgejo_admin_token
    ZITADEL_MANAGEMENT_TOKEN       = var.zitadel_management_token
    GITHUB_CLIENT_ID               = var.github_client_id
    GITHUB_CLIENT_SECRET           = var.github_client_secret
  }

  liveness_probe {
    interval          = "10s"
    failure_threshold = 3
    tcp               = true
    timeout           = "5s"
  }
}

# ---- web (Next.js frontend) -----------------------------------------------------------

resource "scaleway_container" "web" {
  lifecycle {
    # Image updates flow through the registry: CI pushes the "latest" tag and
    # Scaleway auto-redeploys. Letting terraform update registry_image races
    # Scaleway's rolling redeploy and intermittently fails with 412.
    ignore_changes = [registry_image, deploy]
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
  }

  cron {
    schedule = var.backup_schedule
    timezone = "UTC"
  }

  retry_policy {
    max_retries = 2
  }
}
