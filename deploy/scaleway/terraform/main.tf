# Project, random credentials and IAM principals for the Quill MVP on Scaleway.
#
# Credentials contract (never committed — export as env vars or use
# terraform.tfvars copied from terraform.tfvars.example):
#   SCW_ACCESS_KEY, SCW_SECRET_KEY        — API key with rights on the org
#   SCW_DEFAULT_ORGANIZATION_ID           — organization ID
#   SCW_DEFAULT_PROJECT_ID                — optional; a project is created below
#   SCW_DEFAULT_REGION / SCW_DEFAULT_ZONE — optional overrides

resource "scaleway_account_project" "quill" {
  name        = var.project_name
  description = "Quill MVP — cloud-native deployment (Kapsule + RDB + Object Storage)"
}

# ---- generated secrets -------------------------------------------------------

resource "random_password" "rdb_admin" {
  length           = 32
  special          = true
  override_special = "!#$%&*()-_=+[]{}<>?"
}

resource "random_password" "zitadel_db" {
  length           = 32
  special          = true
  override_special = "!#$%&*()-_=+[]{}<>?"
}

resource "random_password" "jwt_secret" {
  length  = 64
  special = false
}

# 32 random bytes for AES-256-GCM pipeline-secret encryption; the app expects a
# base64-encoded key (QUILL_SECRET_ENCRYPTION_KEY), so encode at use site.
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

# Placeholder for the Forgejo admin token: minted on first boot by
# scripts/k8s-provision.sh, which updates both the Kubernetes secret and the
# Secret Manager entry. The API refuses repo/PR operations until this is set.
resource "random_password" "forgejo_admin_token_placeholder" {
  length  = 40
  special = false
  keepers = {
    note = "placeholder — replaced by k8s-provision.sh after first boot"
  }
}

# ---- IAM: CI deployer ----------------------------------------------------------

resource "scaleway_iam_application" "ci" {
  name        = "quill-ci-deployer"
  description = "GitHub Actions deployer: push images to the registry, manage Kapsule, sync DNS records."
}

resource "scaleway_iam_policy" "ci" {
  name            = "quill-ci-deployer-policy"
  description     = "Least-privilege set needed by the deploy workflow."
  application_id  = scaleway_iam_application.ci.id
  no_principal    = false
  organization_id = scaleway_iam_application.ci.organization_id

  rule {
    project_ids = [scaleway_account_project.quill.id]
    permission_set_names = [
      "KapsuleFullAccess",
      "ContainerRegistryFullAccess",
      "DNSFullAccess",
      "SecretManagerReadOnly",
    ]
  }
}

resource "scaleway_iam_api_key" "ci" {
  application_id     = scaleway_iam_application.ci.id
  description        = "GitHub Actions deploy key — store as SCW_CI_ACCESS_KEY / SCW_CI_SECRET_KEY repo secrets"
  default_project_id = scaleway_account_project.quill.id
}

# ---- IAM: backup writer ----------------------------------------------------------

resource "scaleway_iam_application" "backup" {
  name        = "quill-backup-writer"
  description = "In-cluster backup CronJob identity: read/write the backup bucket only."
}

resource "scaleway_iam_policy" "backup" {
  name            = "quill-backup-writer-policy"
  description     = "Object Storage access scoped to the Quill project (bucket-level restriction is done via bucket policy)."
  application_id  = scaleway_iam_application.backup.id
  no_principal    = false
  organization_id = scaleway_iam_application.backup.organization_id

  rule {
    project_ids          = [scaleway_account_project.quill.id]
    permission_set_names = ["ObjectStorageReadWriteAccess"]
  }
}

resource "scaleway_iam_api_key" "backup" {
  application_id     = scaleway_iam_application.backup.id
  description        = "S3 credentials for the backup CronJob — stored in Secret Manager and the k8s secret"
  default_project_id = scaleway_account_project.quill.id
}
