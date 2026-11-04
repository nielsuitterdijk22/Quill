# The one non-serverless piece: a single small VM running Forgejo (git data needs
# a durable disk), the pipeline dispatcher (needs a Docker daemon), Caddy (TLS
# for git.<domain> via Let's Encrypt HTTP-01), and a Forgejo-dump backup sidecar.
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
    volume_type           = "b_ssd"
    delete_on_termination = false # never destroy git data on VM replacement
  }

  cloud_init = templatefile("${path.module}/../vm/cloud-init.yml", {
    domain              = var.domain
    git_host            = local.git_host
    api_host            = local.api_host
    forgejo_db_url      = local.db_url["forgejo"]
    forgejo_db_host     = local.sdb_host["forgejo"]
    forgejo_db_port     = local.sdb_port["forgejo"]
    db_user             = local.db_user
    db_password         = local.db_password
    webhook_secret      = random_password.webhook_secret.result
    dispatch_secret     = random_password.dispatch_secret.result
    jwt_secret          = random_password.jwt_secret.result
    enc_key             = base64encode(random_password.secret_encryption_key.result)
    forgejo_admin_token = var.forgejo_admin_token
    backup_image        = "${local.registry}/quill-backup:${var.image_tag}"
    backend_image       = "${local.registry}/quill-backend:${var.image_tag}"
    s3_endpoint         = "https://s3.${var.region}.scw.cloud"
    backup_bucket       = scaleway_object_bucket.backups.name
    aws_access_key      = scaleway_iam_api_key.backup.access_key
    aws_secret_key      = scaleway_iam_api_key.backup.secret_key
    backup_retention    = var.backup_retention_days
    ssh_public_key      = tls_private_key.vm.public_key_openssh
    letsencrypt_email   = var.letsencrypt_email
  })

  tags = local.tags
}
