# Scaleway credentials come from the environment (never committed):
#   SCW_ACCESS_KEY, SCW_SECRET_KEY, SCW_DEFAULT_PROJECT_ID, SCW_DEFAULT_ORGANIZATION_ID
# or via `scw` CLI config / terraform.tfvars. See README.md.
provider "scaleway" {
  # region, zone, access_key, secret_key, project_id, organization_id
  # are all read from SCW_* env vars by default.
}

provider "random" {}

# The Kubernetes provider is configured from the Kapsule kubeconfig that this
# configuration writes to Secret Manager / local file (see kapsule.tf). It is
# only used for the cert-manager ClusterIssuer and is optional: set
# kubernetes_config_path when applying with cluster access.
provider "kubernetes" {
  config_path = var.kubeconfig_path != "" ? var.kubeconfig_path : null
}
