# ---- pipeline showcase deploy target (2026-11-13) ----------------------------------
#
# A user-facing demo of "pipelines that deploy to Scaleway": the showcase repo's
# workflow builds an image on the ephemeral CI worker, pushes it to its own
# registry namespace and (re)deploys a min_scale-0 container in its own
# container namespace. The workflow authenticates with a dedicated IAM key that
# lives ONLY in that repo's pipeline secrets — scoped to containers + registry,
# never instances/storage/IAM. Everything here is free while idle.

resource "scaleway_iam_application" "showcase_deployer" {
  name        = "quill-showcase-deployer"
  description = "Pipeline showcase: push images + deploy serverless containers."
}

resource "scaleway_iam_policy" "showcase_deployer" {
  name           = "quill-showcase-deployer-policy"
  description    = "Containers + registry only, scoped to the Quill project."
  application_id = scaleway_iam_application.showcase_deployer.id

  rule {
    project_ids          = [scaleway_account_project.quill.id]
    permission_set_names = ["ContainersFullAccess", "ContainerRegistryFullAccess"]
  }
}

resource "scaleway_iam_api_key" "showcase_deployer" {
  application_id     = scaleway_iam_application.showcase_deployer.id
  description        = "Showcase repo pipeline secret (SCW_ACCESS_KEY/SCW_SECRET_KEY)"
  default_project_id = scaleway_account_project.quill.id

  lifecycle {
    ignore_changes = [expires_at]
  }
}

resource "scaleway_registry_namespace" "showcase" {
  name        = "quill-showcase"
  project_id  = scaleway_account_project.quill.id
  region      = var.region
  is_public   = false
  description = "Images pushed by the pipeline showcase."
}

resource "scaleway_container_namespace" "showcase" {
  name        = "showcase"
  project_id  = scaleway_account_project.quill.id
  region      = var.region
  description = "Containers deployed by the pipeline showcase workflow (min_scale 0)."
}

output "showcase" {
  value = {
    registry           = scaleway_registry_namespace.showcase.endpoint
    container_ns_id    = split("/", scaleway_container_namespace.showcase.id)[1]
    deployer_access_id = scaleway_iam_api_key.showcase_deployer.access_key
  }
}
