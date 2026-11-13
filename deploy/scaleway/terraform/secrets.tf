# Scaleway Secret Manager — operator-facing copy of all credentials (the running
# services get theirs from TF variables/env, which is where these values originate;
# nothing secret is ever committed to git).

locals {
  secret_path_prefix = "/quill"
}

resource "scaleway_secret" "s" {
  # The Brevo SMTP Secret Manager copies were retired (2026-11-13): the values
  # live in GitHub secrets and are injected straight into the Forgejo mailer
  # env by the deploy workflow; the API-side secrets had already been deleted,
  # which made every CI apply fail on their scheduled-for-deletion versions
  # ("cannot act on deleted resource"). SMTP creds are GH-secrets-only now.
  for_each = toset([
    "app/jwt-secret",
    "app/secret-encryption-key",
    "app/webhook-secret",
    "app/dispatch-secret",
    "app/forgejo-admin-token",
    "app/zitadel-management-token",
    "zitadel/masterkey",
    "forgejo/db-password",
    "db/user",
    "db/password",
    "db/quill-url",
    "backup/s3-access-key",
    "backup/s3-secret-key",
    "git-engine/s3-access-key",
    "git-engine/s3-secret-key",
    "vm/ssh-private-key",
  ])

  name       = replace(each.value, "/", "-")
  path       = "${local.secret_path_prefix}/${split("/", each.value)[0]}"
  project_id = scaleway_account_project.quill.id
  region     = var.region
  protected  = false # readable by operators/CI for rotation + disaster recovery
}

resource "scaleway_secret_version" "v" {
  for_each = scaleway_secret.s

  secret_id   = each.value.id
  region      = var.region
  description = "managed by Terraform"
  data = lookup({
    "app/jwt-secret"               = random_password.jwt_secret.result
    "app/secret-encryption-key"    = base64encode(random_password.secret_encryption_key.result)
    "app/webhook-secret"           = random_password.webhook_secret.result
    "app/dispatch-secret"          = local.dispatch_secret
    "app/forgejo-admin-token"      = var.forgejo_admin_token
    "app/zitadel-management-token" = var.zitadel_management_token
    "zitadel/masterkey"            = random_password.zitadel_masterkey.result
    "forgejo/db-password"          = random_password.forgejo_db.result
    "db/user"                      = local.db_user
    "db/password"                  = local.db_password
    "db/quill-url"                 = local.db_url["quill"]
    "backup/s3-access-key"         = scaleway_iam_api_key.backup.access_key
    "backup/s3-secret-key"         = scaleway_iam_api_key.backup.secret_key
    "git-engine/s3-access-key"     = scaleway_iam_api_key.git_engine.access_key
    "git-engine/s3-secret-key"     = scaleway_iam_api_key.git_engine.secret_key
    "vm/ssh-private-key"           = tls_private_key.vm.private_key_openssh,
  }, each.key, "")
}
