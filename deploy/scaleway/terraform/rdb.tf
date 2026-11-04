# Managed PostgreSQL (RDB) — one instance, three databases:
#   quill    — Quill platform metadata
#   forgejo  — Forgejo git backend
#   zitadel  — Zitadel identity provider (SSO/SCIM)
#
# The instance is attached to the same private network as the Kapsule cluster,
# so the app and the backup CronJob use the private IP. The public endpoint is
# TLS-enforced and ACL-gated to `deploy_cidrs` for operator access (psql, TF).
# On top of the dumps the backup CronJob uploads to Object Storage, RDB's own
# managed backups run on `rdb_backup_schedule`.

resource "scaleway_rdb_instance" "quill" {
  name          = "${local.name_prefix}-pg"
  project_id    = scaleway_account_project.quill.id
  region        = var.region
  node_type     = var.rdb_node_type
  engine        = "PostgreSQL-16"
  is_ha_cluster = var.rdb_is_ha

  user_name = "quill"
  password  = random_password.rdb_admin.result

  volume_type        = "bssd"
  volume_size_in_gb  = 20
  encryption_at_rest = true

  disable_backup            = false
  backup_schedule_frequency = var.rdb_backup_schedule
  backup_schedule_retention = var.backup_retention_days
  backup_same_region        = true

  private_network {
    pn_id = scaleway_vpc_private_network.quill.id
  }

  tags = local.tags

  depends_on = [scaleway_vpc_private_network.quill]
}

# Public endpoint ACL: TLS is always required; restrict to deploy_cidrs.
resource "scaleway_rdb_acl" "quill" {
  instance_id = scaleway_rdb_instance.quill.id
  region      = var.region

  dynamic "acl_rules" {
    for_each = var.deploy_cidrs
    content {
      ip          = acl_rules.value
      description = "allowed deployer/operator CIDR"
    }
  }
}

# Databases (owner/managed are assigned automatically to the admin user).
resource "scaleway_rdb_database" "quill" {
  for_each    = toset(local.rdb_databases)
  instance_id = scaleway_rdb_instance.quill.id
  region      = var.region
  name        = each.value
}

# Dedicated Zitadel DB user (least privilege: owns only the zitadel database).
resource "scaleway_rdb_user" "zitadel" {
  instance_id = scaleway_rdb_instance.quill.id
  region      = var.region
  name        = "zitadel"
  password    = random_password.zitadel_db.result
  is_admin    = false
}

resource "scaleway_rdb_privilege" "zitadel" {
  instance_id   = scaleway_rdb_instance.quill.id
  region        = var.region
  user_name     = scaleway_rdb_user.zitadel.name
  database_name = scaleway_rdb_database.quill["zitadel"].name
  permission    = "all"
}
