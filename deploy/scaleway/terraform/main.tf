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

# Zitadel masterkey must be exactly 32 characters.
resource "random_password" "zitadel_masterkey" {
  length  = 32
  special = false
}

# ---- IAM: CI deployer ----------------------------------------------------------

resource "scaleway_iam_application" "ci" {
  name        = "quill-ci-deployer"
  description = "GitHub Actions deployer: terraform apply, registry push, DNS sync, container logs."
}

resource "scaleway_iam_policy" "ci" {
  name           = "quill-ci-deployer-policy"
  description    = "Rights needed by the deploy workflow (scoped to the Quill project)."
  application_id = scaleway_iam_application.ci.id

  rule {
    project_ids = [scaleway_account_project.quill.id]
    permission_set_names = [
      "ContainerRegistryFullAccess",
      "ServerlessContainersFullAccess",
      "ServerlessJobsFullAccess",
      "ServerlessSQLFullAccess",
      "DNSFullAccess",
      "SecretManagerReadOnly",
      "InstancesReadOnly",
      "ObjectStorageReadWriteAccess",
    ]
  }
}

resource "scaleway_iam_api_key" "ci" {
  application_id     = scaleway_iam_application.ci.id
  description        = "GitHub Actions key — store as SCW_CI_ACCESS_KEY / SCW_CI_SECRET_KEY repo secrets"
  default_project_id = scaleway_account_project.quill.id
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
    permission_set_names = ["ObjectStorageReadWriteAccess"]
  }
}

resource "scaleway_iam_api_key" "backup" {
  application_id     = scaleway_iam_application.backup.id
  description        = "S3 credentials for the backup job + Forgejo VM backup sidecar"
  default_project_id = scaleway_account_project.quill.id
}

# ---- IAM: database user -----------------------------------------------------------
# Serverless SQL databases authenticate with an IAM identity: the username is the
# IAM user id and the password is one of its API keys' secret key.

resource "scaleway_iam_user" "db" {
  username = "quill-db"
  email    = "db@${var.domain}"
}

resource "scaleway_iam_api_key" "db" {
  user_id            = scaleway_iam_user.db.id
  description        = "Credentials for the serverless SQL databases (username = user id, password = secret key)"
  default_project_id = scaleway_account_project.quill.id
}

# ---- SSH key for the Forgejo VM -----------------------------------------------------
# Injected via cloud-init authorized_keys (and used by CI/provision over SSH).

resource "tls_private_key" "vm" {
  algorithm = "ED25519"
}
