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
  # count must depend only on variables (not on computed resource attrs)
  have_zone = var.create_dns_zone || var.existing_dns_zone_id != ""
}

resource "scaleway_domain_record" "api" {
  count      = local.have_zone ? 1 : 0
  dns_zone   = local.dns_zone_id
  name       = "api"
  type       = "CNAME"
  data       = local.container_cname["api"]
  ttl        = 300
  project_id = scaleway_account_project.quill.id
}

resource "scaleway_domain_record" "app" {
  count      = local.have_zone ? 1 : 0
  dns_zone   = local.dns_zone_id
  name       = "app"
  type       = "CNAME"
  data       = local.container_cname["app"]
  ttl        = 300
  project_id = scaleway_account_project.quill.id
}

# auth.<domain> is served by Zitadel on the Forgejo VM (Caddy h2c vhost).
resource "scaleway_domain_record" "auth" {
  count      = local.have_zone && var.vm_ip != "" ? 1 : 0
  dns_zone   = local.dns_zone_id
  name       = "auth"
  type       = "A"
  data       = var.vm_ip
  ttl        = 300
  project_id = scaleway_account_project.quill.id
}

resource "scaleway_domain_record" "git" {
  count      = local.have_zone && var.vm_ip != "" ? 1 : 0
  dns_zone   = local.dns_zone_id
  name       = "git"
  type       = "A"
  data       = var.vm_ip
  ttl        = 300
  project_id = scaleway_account_project.quill.id
}

resource "scaleway_domain_record" "ssh" {
  count      = local.have_zone && var.vm_ip != "" ? 1 : 0
  dns_zone   = local.dns_zone_id
  name       = "ssh"
  type       = "A"
  data       = var.vm_ip
  ttl        = 300
  project_id = scaleway_account_project.quill.id
}
