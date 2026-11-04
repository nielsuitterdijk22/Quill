# VPC + private network shared by the Kapsule cluster and the RDB instance, so
# Postgres traffic between the app and the database stays on Scaleway's private
# backbone instead of the public endpoint.

resource "scaleway_vpc" "quill" {
  name       = "${local.name_prefix}-vpc"
  project_id = scaleway_account_project.quill.id
  region     = var.region
  tags       = local.tags
}

resource "scaleway_vpc_private_network" "quill" {
  name   = "${local.name_prefix}-pn"
  vpc_id = scaleway_vpc.quill.id
  region = var.region
  # ipv4_subnet is auto-assigned by Scaleway when omitted.
  tags = local.tags
}
