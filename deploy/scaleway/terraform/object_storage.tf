# Object Storage (S3-compatible) — the off-cluster backup target.
# The in-cluster backup CronJob (deploy/scaleway/k8s/backup-cronjob.yaml)
# pg_dump's all three databases and uploads Forgejo dumps here; objects expire
# after `backup_retention_days` via the lifecycle rule.

resource "scaleway_object_bucket" "backups" {
  name       = var.backup_bucket_name
  project_id = scaleway_account_project.quill.id
  region     = var.region

  versioning {
    enabled = true
  }

  lifecycle_rule {
    id      = "expire-old-backups"
    enabled = true
    prefix  = ""
    expiration {
      days = var.backup_retention_days
    }
  }

  tags = local.tags_map
}

resource "scaleway_object_bucket_acl" "backups" {
  bucket     = scaleway_object_bucket.backups.name
  region     = var.region
  project_id = scaleway_account_project.quill.id
  acl        = "private"
}

# ---- Git engine bucket (serverless epic, PR D1) -----------------------------------
# The own-git-engine's truth: packs + manifests, tenant-scoped keys. No
# lifecycle rule — packfiles are permanent data, not backups. Objects only
# ever change through the engine's write path (referee CAS first), so plain
# versioning (not the backups bucket's delete-protection semantics) is enough.

resource "scaleway_object_bucket" "repos" {
  name       = var.repos_bucket_name
  project_id = scaleway_account_project.quill.id
  region     = var.region

  versioning {
    enabled = true
  }

  tags = local.tags_map
}
