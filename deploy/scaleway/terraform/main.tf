# Project, generated credentials and IAM principals for the serverless Quill MVP
# on Scaleway.
#
# Credentials contract (never committed — export as env vars):
#   SCW_ACCESS_KEY, SCW_SECRET_KEY, SCW_DEFAULT_ORGANIZATION_ID
#   SCW_DEFAULT_REGION / SCW_DEFAULT_ZONE

resource "scaleway_account_project" "quill" {
  name        = var.project_name
  description = "Quill MVP — serverless Scaleway deployment (SDB + Serverless Containers + 1 Forgejo VM)"
}

# ---- generated secrets -------------------------------------------------------

resource "random_password" "jwt_secret" {
  length  = 64
  special = false
}

# 32 random bytes for AES-256-GCM pipeline-secret encryption, base64-encoded.
resource "random_password" "secret_encryption_key" {
  length  = 32
  special = false
}

resource "random_password" "webhook_secret" {
  length  = 48
  special = false
}

resource "random_password" "dispatch_secret" {
  length  = 48
  special = false
}

resource "random_password" "zitadel_db" {
  length           = 32
  special          = true
  override_special = "!#$%&*()-_=+[]{}<>?"
}

# Password for the VM-local Forgejo Postgres (forgejo-db container). The
# serverless "forgejo" SDB was removed: Forgejo's persistent connection pool
# kept it at ~0.22 vCPU 24/7 (~EUR 22/mo CPU); see DECISIONS.md.
resource "random_password" "forgejo_db" {
  length           = 32
  special          = true
  override_special = "!#$%&*()-_=+[]{}<>?"
}

# Zitadel masterkey must be exactly 32 characters.
resource "random_password" "zitadel_masterkey" {
  length  = 32
  special = false
}

# ---- IAM: backup writer ----------------------------------------------------------

resource "scaleway_iam_application" "backup" {
  name        = "quill-backup-writer"
  description = "Backup job identity: read/write the backup bucket only."
}

resource "scaleway_iam_policy" "backup" {
  name           = "quill-backup-writer-policy"
  description    = "Object Storage access scoped to the Quill project."
  application_id = scaleway_iam_application.backup.id

  rule {
    project_ids          = [scaleway_account_project.quill.id]
    permission_set_names = ["ObjectStorageFullAccess"]
  }
}

resource "scaleway_iam_api_key" "backup" {
  application_id     = scaleway_iam_application.backup.id
  description        = "S3 credentials for the backup job + Forgejo VM backup sidecar"
  default_project_id = scaleway_account_project.quill.id
}

# ---- SSH key for the Forgejo VM -----------------------------------------------------
# Injected via cloud-init authorized_keys (and used by CI/provision over SSH).

resource "tls_private_key" "vm" {
  algorithm = "ED25519"
}
