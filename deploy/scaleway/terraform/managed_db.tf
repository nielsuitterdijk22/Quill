# ---- Managed PostgreSQL (replaces the Serverless SQL databases) ---------------
#
# 2026-11-16: the Serverless SQL databases had billed €26.9 in the first 16 days of
# November (≈€50 a month at that rate, inflated by heavy agent testing). They are
# billed per vCPU-second of activity, and the instances appear to have been busy
# around the clock (Zitadel's background queries are the likely cause — not
# verified). One always-on db-dev-s (2 vCPU, 2 GB, local SSD, €0.0156/h incl.
# management ≈ €11.4/month) hosts both databases at a flat, predictable price.
#
# Reachability: containers and jobs have no fixed egress IPs (and jobs can't join a
# private network), so the instance keeps a public endpoint with an open ACL —
# the same exposure class as the serverless database it replaces: TLS required
# (sslmode=require), a 40-character random password, nothing else on the box.
# Tightening this (private network for the containers) is a follow-up.

resource "random_password" "rdb" {
  length           = 40
  min_upper        = 2
  min_lower        = 2
  min_numeric      = 2
  min_special      = 2
  override_special = "-_" # URL-safe: the password is embedded in postgresql:// URLs
}

resource "scaleway_rdb_instance" "main" {
  name       = "quill-pg"
  project_id = scaleway_account_project.quill.id
  region     = var.region

  engine        = "PostgreSQL-${var.sdb_version}"
  node_type     = var.rdb_node_type
  is_ha_cluster = false
  volume_type   = "lssd"

  user_name = "quill"
  password  = random_password.rdb.result

  # Daily managed backups on top of the nightly pg_backup job (which dumps to
  # object storage and so also covers an instance loss).
  disable_backup            = false
  backup_schedule_frequency = 24
  backup_schedule_retention = 7

  load_balancer {}

  tags = ["quill"]
}

resource "scaleway_rdb_database" "dbs" {
  for_each = toset(local.sdb_names)

  instance_id = scaleway_rdb_instance.main.id
  name        = each.value
}

resource "scaleway_rdb_privilege" "dbs" {
  for_each = toset(local.sdb_names)

  instance_id   = scaleway_rdb_instance.main.id
  user_name     = scaleway_rdb_instance.main.user_name
  database_name = scaleway_rdb_database.dbs[each.value].name
  permission    = "all"
}

resource "scaleway_rdb_acl" "open" {
  instance_id = scaleway_rdb_instance.main.id

  acl_rules {
    ip          = "0.0.0.0/0"
    description = "serverless containers/jobs have no fixed egress IPs; TLS + password required"
  }
}
