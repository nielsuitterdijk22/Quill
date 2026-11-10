# The one non-serverless piece: a single small VM running Forgejo (git data needs
# a durable disk), its own Postgres (forgejo-db container — the serverless
# forgejo SDB never scaled to zero and cost ~EUR 22/mo in CPU), the pipeline
# dispatcher (needs a Docker daemon), Caddy (TLS for git.<domain> via Let's
# Encrypt HTTP-01), and a Forgejo-dump backup sidecar.
#
# Deployed/updated over SSH by CI (see .github/workflows/deploy-scaleway.yml);
# the cloud-init below only runs on first boot.

resource "scaleway_instance_security_group" "vm" {
  name                    = "quill-forgejo-vm"
  project_id              = scaleway_account_project.quill.id
  zone                    = var.zone
  inbound_default_policy  = "drop"
  outbound_default_policy = "accept"
  stateful                = true

  # SSH for deploys — restrict via var.ssh_cidrs.
  dynamic "inbound_rule" {
    for_each = var.ssh_cidrs
    content {
      action   = "accept"
      protocol = "TCP"
      port     = 22
      ip_range = inbound_rule.value
    }
  }
  # HTTP (Let's Encrypt HTTP-01) + HTTPS (git.<domain>) + git SSH + dispatch API.
  dynamic "inbound_rule" {
    for_each = [80, 443, 2222, 8090]
    content {
      action   = "accept"
      protocol = "TCP"
      port     = inbound_rule.value
      ip_range = "0.0.0.0/0"
    }
  }
}

resource "scaleway_instance_server" "forgejo" {
  name              = "quill-forgejo"
  type              = var.vm_type
  image             = var.vm_image
  zone              = var.zone
  project_id        = scaleway_account_project.quill.id
  security_group_id = scaleway_instance_security_group.vm.id

  enable_dynamic_ip      = true # free dynamic public IP
  replace_on_type_change = false
  boot_type              = "local"

  root_volume {
    size_in_gb            = var.vm_root_volume_gb
    delete_on_termination = false # never destroy git data on VM replacement
    # volume_type omitted: b_ssd is retired for instance volumes; the default applies
  }

  cloud_init = templatefile("${path.module}/../vm/cloud-init.yml", {
    domain     = var.domain
    git_host   = local.git_host
    docker_gid = var.vm_docker_gid
    # Forgejo's Postgres is the forgejo-db container on this VM (compose
    # service name), not a serverless SDB.
    forgejo_db_host     = "forgejo-db"
    forgejo_db_port     = "5432"
    forgejo_db_user     = "forgejo"
    forgejo_db_password = random_password.forgejo_db.result
    dispatch_secret     = local.dispatch_secret
    backup_image        = "${local.registry}/quill-backup:latest"
    backend_image       = "${local.registry}/quill-backend:latest"
    zitadel_image       = "${local.registry}/quill-zitadel:latest"
    zitadel_masterkey   = random_password.zitadel_masterkey.result
    zitadel_db_password = random_password.zitadel_db.result
    s3_endpoint         = "https://s3.${var.region}.scw.cloud"
    backup_bucket       = scaleway_object_bucket.backups.name
    aws_access_key      = scaleway_iam_api_key.backup.access_key
    aws_secret_key      = scaleway_iam_api_key.backup.secret_key
    backup_retention    = var.backup_retention_days
    ssh_public_key      = tls_private_key.vm.public_key_openssh
    letsencrypt_email   = var.letsencrypt_email
    # Brevo SMTP (transactional email). Rendered as conditional env blocks; on a
    # running Zitadel instance the admin API (scripts/zitadel-smtp.sh, run by CI)
    # is what applies the settings — env vars only apply at instance creation.
    brevo_enabled       = local.brevo_enabled
    brevo_host_port     = local.brevo_host_port
    brevo_smtp_host     = var.brevo_smtp_host
    brevo_smtp_port     = tostring(var.brevo_smtp_port)
    brevo_smtp_user     = var.brevo_smtp_user
    brevo_smtp_password = var.brevo_smtp_password
    brevo_from          = local.brevo_from
    brevo_from_name     = var.brevo_smtp_from_name
  })

  tags = local.tags
}
