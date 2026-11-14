# ---- CI off the VM (serverless epic, Phase A deploy wiring) -------------------------
#
# The broker is a min_scale-0 Serverless Container (same backend image,
# /quill-broker entrypoint). The api wakes it per enqueue (HMAC, dispatch
# secret) with the number of repositories waiting for a worker; it boots one
# ephemeral DEV1-S instance (tagged quill-ci-worker) per such repo, capped.
#
# Isolation (2026-11-14): each instance serves ONE repository (the api binds
# its worker on the first claim) and carries only its own worker token plus a
# pull-only registry key — no dispatch secret, no instance-management key.
# Jobs get the Docker socket (root on the box), so nothing on the box may
# reach beyond that repo. When the worker is done it retires through the api,
# which has the broker terminate exactly that instance. The broker reaps
# stragglers (TTL, stopped instances, orphan volumes) on every wake.
#
# Gated by var.ci_queue_mode: while false, the api keeps the inline VM dispatch
# path (QUILL_PIPELINE_DISPATCH_URL) and the broker sits idle at zero cost.

resource "scaleway_iam_application" "ci_broker" {
  name        = "quill-ci-broker"
  description = "Ephemeral CI instance orchestration (broker)."
}

resource "scaleway_iam_policy" "ci_broker" {
  name           = "quill-ci-broker-policy"
  description    = "Instances + their block volumes, scoped to the Quill project."
  application_id = scaleway_iam_application.ci_broker.id

  rule {
    project_ids = [scaleway_account_project.quill.id]
    # Scaleway IAM cannot narrow to tagged servers, so this key never leaves
    # the broker (since 2026-11-14 not even onto the CI instances).
    # BlockStorageFullAccess: DEV1-S root volumes are SBS block volumes
    # ("insufficient permissions: write volume" without it, 2026-11-13).
    permission_set_names = ["InstancesFullAccess", "BlockStorageFullAccess"]
  }
}

resource "scaleway_iam_api_key" "ci_broker" {
  application_id     = scaleway_iam_application.ci_broker.id
  description        = "Broker: create/terminate ephemeral CI instances"
  default_project_id = scaleway_account_project.quill.id

  lifecycle {
    ignore_changes = [expires_at]
  }
}

# Pull-only identity for the CI instances' worker image: readable by any job
# on the box, so it can do nothing but pull images.
resource "scaleway_iam_application" "ci_pull" {
  name        = "quill-ci-pull"
  description = "CI instances: pull the worker image from the Container Registry (read-only)."
}

resource "scaleway_iam_policy" "ci_pull" {
  name           = "quill-ci-pull-policy"
  description    = "Container Registry read-only, scoped to the Quill project."
  application_id = scaleway_iam_application.ci_pull.id

  rule {
    project_ids          = [scaleway_account_project.quill.id]
    permission_set_names = ["ContainerRegistryReadOnly"]
  }
}

resource "scaleway_iam_api_key" "ci_pull" {
  application_id     = scaleway_iam_application.ci_pull.id
  description        = "CI instances: worker image pull (read-only)"
  default_project_id = scaleway_account_project.quill.id

  lifecycle {
    ignore_changes = [expires_at]
  }
}

resource "scaleway_container" "broker" {
  lifecycle {
    ignore_changes = [deploy]
  }

  namespace_id   = scaleway_container_namespace.quill.id
  name           = "broker"
  region         = var.region
  registry_image = "${local.registry}/quill-backend:${var.image_tag}"
  command        = ["/quill-broker"]
  deploy         = true

  port         = 8090
  cpu_limit    = 140
  memory_limit = 256
  min_scale    = 0
  max_scale    = 1 # EnsureWorker(s) is single-flighted in-process

  privacy                = "public" # every route but /healthz is HMAC-gated
  protocol               = "http1"
  https_connections_only = true
  timeout                = 300

  environment_variables = {
    QUILL_BROKER_ADDR           = ":8090"
    QUILL_CIB_ZONE              = var.zone
    QUILL_CIB_PROJECT_ID        = scaleway_account_project.quill.id
    QUILL_CIB_INSTANCE_TYPE     = var.ci_instance_type
    QUILL_CIB_API_URL           = "https://${local.api_host}"
    QUILL_CIB_WORKER_IMAGE      = "${local.registry}/quill-backend:${var.image_tag}"
    QUILL_CIB_REGISTRY_USER     = "nologin"
    QUILL_CIB_MAX_INSTANCES     = "3"
    QUILL_PIPELINE_UBUNTU_IMAGE = "node:22-bookworm"
  }
  secret_environment_variables = {
    QUILL_PIPELINE_DISPATCH_SECRET = local.dispatch_secret
    SCW_ACCESS_KEY                 = scaleway_iam_api_key.ci_broker.access_key
    SCW_SECRET_KEY                 = scaleway_iam_api_key.ci_broker.secret_key
    SCW_DEFAULT_PROJECT_ID         = scaleway_account_project.quill.id
    QUILL_CIB_REGISTRY_PASSWORD    = scaleway_iam_api_key.ci_pull.secret_key
  }

  liveness_probe {
    interval          = "10s"
    failure_threshold = 3
    timeout           = "5s"
    http {
      path = "/healthz"
    }
  }
}

# ---- async git maintenance (PR B7) ----------------------------------------------------
# Daily reconcile/GC walk over own-engine repos. A no-op (one SDB query) until
# repos migrate in D1. /mnt is the disk-backed job volume (/tmp is RAM-tmpfs).

resource "scaleway_job_definition" "git_maintenance" {
  name       = "git-maintenance"
  project_id = scaleway_account_project.quill.id
  region     = var.region

  image_uri = "${local.registry}/quill-backend:${var.image_tag}"
  startup_command = ["/bin/sh", "-c",
  "mkdir -p /mnt/gm 2>/dev/null && [ -w /mnt/gm ] && export TMPDIR=/mnt/gm; exec /quill admin git-maintenance --repack-threshold 16"]

  cpu_limit              = 1000
  memory_limit           = 1024
  local_storage_capacity = 4096
  timeout                = "1h"

  # Plaintext env: secret_reference silently vanishes at the API (2026-11-12).
  env = {
    QUILL_DATABASE_URL      = local.db_url["quill"]
    QUILL_GIT_ENGINE_MODE   = "own"
    QUILL_GIT_S3_ENDPOINT   = "https://s3.${var.region}.scw.cloud"
    QUILL_GIT_S3_REGION     = var.region
    QUILL_GIT_S3_BUCKET     = scaleway_object_bucket.repos.name
    QUILL_GIT_S3_ACCESS_KEY = scaleway_iam_api_key.git_engine.access_key
    QUILL_GIT_S3_SECRET_KEY = scaleway_iam_api_key.git_engine.secret_key
  }

  cron {
    schedule = "17 3 * * *"
    timezone = "Europe/Paris"
  }
}
