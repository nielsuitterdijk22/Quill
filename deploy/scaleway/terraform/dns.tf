# DNS zone for var.domain + A records pointing at the load balancers.
#
# The ingress (Traefik) and SSH load-balancer IPs are only known after the
# cluster exists, so records are created when `ingress_lb_ip` / `ssh_lb_ip`
# are set — scripts/dns-sync.sh fetches the IPs from the cluster and re-applies
# with those variables, or creates the records directly via the `scw` CLI.

variable "existing_dns_zone_id" {
  description = "Use an existing Scaleway DNS zone instead of creating one (set create_dns_zone=false)."
  type        = string
  default     = ""
}

resource "scaleway_domain_zone" "quill" {
  count      = var.create_dns_zone ? 1 : 0
  domain     = local.root_domain
  subdomain  = local.subdomain
  project_id = scaleway_account_project.quill.id
}

locals {
  dns_zone_id = var.create_dns_zone ? scaleway_domain_zone.quill[0].id : var.existing_dns_zone_id
}

resource "scaleway_domain_record" "app" {
  count      = var.ingress_lb_ip != "" && local.dns_zone_id != "" ? 1 : 0
  dns_zone   = local.dns_zone_id
  name       = "app"
  type       = "A"
  data       = var.ingress_lb_ip
  ttl        = 300
  project_id = scaleway_account_project.quill.id
}

resource "scaleway_domain_record" "git" {
  count      = var.ingress_lb_ip != "" && local.dns_zone_id != "" ? 1 : 0
  dns_zone   = local.dns_zone_id
  name       = "git"
  type       = "A"
  data       = var.ingress_lb_ip
  ttl        = 300
  project_id = scaleway_account_project.quill.id
}

resource "scaleway_domain_record" "auth" {
  count      = var.ingress_lb_ip != "" && local.dns_zone_id != "" ? 1 : 0
  dns_zone   = local.dns_zone_id
  name       = "auth"
  type       = "A"
  data       = var.ingress_lb_ip
  ttl        = 300
  project_id = scaleway_account_project.quill.id
}

# Forgejo SSH (git clone over ssh://git@ssh.<domain>:2222) rides the dedicated
# LoadBalancer service (k8s/forgejo.yaml), whose IP differs from the ingress.
resource "scaleway_domain_record" "ssh" {
  count      = var.ssh_lb_ip != "" && local.dns_zone_id != "" ? 1 : 0
  dns_zone   = local.dns_zone_id
  name       = "ssh"
  type       = "A"
  data       = var.ssh_lb_ip
  ttl        = 300
  project_id = scaleway_account_project.quill.id
}
