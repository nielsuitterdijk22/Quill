# Scaleway credentials come from the environment (never committed):
#   SCW_ACCESS_KEY, SCW_SECRET_KEY, SCW_DEFAULT_PROJECT_ID, SCW_DEFAULT_ORGANIZATION_ID
# or via `scw` CLI config. See README.md.
provider "scaleway" {
  # region, zone, access_key, secret_key, project_id, organization_id
  # are all read from SCW_* env vars by default.
}

provider "random" {}

provider "tls" {}
