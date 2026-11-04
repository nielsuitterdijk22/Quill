# DNS zone for var.domain.
#   api/app/auth → CNAME to the serverless container domains (managed by TF)
#   git/ssh     → A records to the Forgejo VM IP (set via scripts/dns-sync.sh or
#                  -var vm_ip=... once the instance exists)

resource "scaleway_domain_zone" "quill" {
  count      = var.create_dns_zone ? 1 : 0
  domain     = local.root_domain
  subdomain  = local.subdomain
  project_id = scaleway_account_project.quill.id
}

locals {
  dns_zone_id = var.create_dns_zone ? scaleway_domain_zone.quill[0].id : var.existing_dns_zone_id
}

resource "scaleway_domain_record" "api" {
  count      = local.dns_zone_id != "" ? 1 : 0
  dns_zone   = local.dns_zone_id
  name       = "api"
  type       = "CNAME"
  data       = local.container_cname["api"]
  ttl        = 300
  project_id = scaleway_account_project.quill.id
}

resource "scaleway_domain_record" "app" {
  count      = local.dns_zone_id != "" ? 1 : 0
  dns_zone   = local.dns_zone_id
  name       = "app"
  type       = "CNAME"
  data       = local.container_cname["app"]
  ttl        = 300
  project_id = scaleway_account_project.quill.id
}

resource "scaleway_domain_record" "auth" {
  count      = local.dns_zone_id != "" ? 1 : 0
  dns_zone   = local.dns_zone_id
  name       = "auth"
  type       = "CNAME"
  data       = local.container_cname["auth"]
  ttl        = 300
  project_id = scaleway_account_project.quill.id
}

resource "scaleway_domain_record" "git" {
  count      = var.vm_ip != "" && local.dns_zone_id != "" ? 1 : 0
  dns_zone   = local.dns_zone_id
  name       = "git"
  type       = "A"
  data       = var.vm_ip
  ttl        = 300
  project_id = scaleway_account_project.quill.id
}

resource "scaleway_domain_record" "ssh" {
  count      = var.vm_ip != "" && local.dns_zone_id != "" ? 1 : 0
  dns_zone   = local.dns_zone_id
  name       = "ssh"
  type       = "A"
  data       = var.vm_ip
  ttl        = 300
  project_id = scaleway_account_project.quill.id
}
