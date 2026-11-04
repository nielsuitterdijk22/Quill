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

# Restrict the bucket to the backup writer principal (defense in depth on top
# of the IAM policy's ObjectStorageReadWriteAccess).
resource "scaleway_object_bucket_policy" "backups" {
  bucket = scaleway_object_bucket.backups.name
  region = var.region
  policy = jsonencode({
    Version = "2023-04-17"
    Statement = [
      {
        Sid       = "AllowBackupWriter"
        Effect    = "Allow"
        Principal = { AWS = [scaleway_iam_application.backup.srn] }
        Action = [
          "s3:GetObject",
          "s3:PutObject",
          "s3:DeleteObject",
          "s3:ListBucket",
          "s3:GetObjectVersion",
          "s3:DeleteObjectVersion",
        ]
        Resource = [
          "arn:aws:s3:::${scaleway_object_bucket.backups.name}",
          "arn:aws:s3:::${scaleway_object_bucket.backups.name}/*",
        ]
      },
    ]
  })
}
