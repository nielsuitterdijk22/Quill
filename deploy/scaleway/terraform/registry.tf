# Container Registry namespace — Quill images are pushed here by CI and pulled
# by the Kapsule cluster: rg.<region>.scw.cloud/<namespace>/<image>.

resource "scaleway_registry_namespace" "quill" {
  name        = var.registry_namespace
  project_id  = scaleway_account_project.quill.id
  region      = var.region
  is_public   = false
  description = "Quill MVP images (api, web, dispatch, backup)"
}
