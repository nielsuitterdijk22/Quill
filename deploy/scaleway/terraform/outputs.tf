output "project_id" {
  description = "Scaleway project holding all Quill resources."
  value       = scaleway_account_project.quill.id
}

output "region" {
  value = var.region
}

# ---- serverless DBs ----
output "sdb_endpoints" {
  description = "Serverless SQL database endpoints (host[:port]) per database."
  value       = { for n, db in scaleway_sdb_sql_database.dbs : n => db.endpoint }
}

output "db_urls" {
  description = "Full postgres:// URLs (also in Secret Manager under /quill/db/*)."
  value       = local.db_url
  sensitive   = true
}

# ---- containers ----
output "container_endpoints" {
  value = {
    api = scaleway_container.api.public_endpoint
    web = scaleway_container.web.public_endpoint
  }
}

output "container_domain_urls" {
  value = {
    api = scaleway_container_domain.api.url
    app = scaleway_container_domain.app.url
  }
}

# ---- registry ----
output "registry_endpoint" {
  value = scaleway_registry_namespace.quill.endpoint
}

# ---- backups ----
output "backup_bucket" {
  value = scaleway_object_bucket.backups.name
}

output "backup_s3_endpoint" {
  value = "https://s3.${var.region}.scw.cloud"
}

output "backup_access_key" {
  value     = scaleway_iam_api_key.backup.access_key
  sensitive = true
}

output "backup_secret_key" {
  value     = scaleway_iam_api_key.backup.secret_key
  sensitive = true
}

# ---- Forgejo VM ----
output "vm_id" {
  value = scaleway_instance_server.forgejo.id
}

output "vm_public_ip" {
  description = "Dynamic public IP of the Forgejo VM (point git/ssh A records here — scripts/dns-sync.sh automates it)."
  value       = scaleway_instance_server.forgejo.public_ips[0].address
}

output "vm_ssh_private_key" {
  description = "SSH private key for the VM (also in Secret Manager at /quill/vm/ssh-private-key)."
  value       = tls_private_key.vm.private_key_openssh
  sensitive   = true
}

# ---- dns ----
output "dns_zone_id" {
  value = local.dns_zone_id
}

output "dns_name_servers" {
  description = "Delegate the domain to these NS records at your registrar when create_dns_zone=true."
  value       = var.create_dns_zone ? scaleway_domain_zone.quill[0].ns : []
}

output "hosts" {
  value = {
    app  = local.app_host
    api  = local.api_host
    git  = local.git_host
    auth = local.auth_host
    ssh  = local.ssh_host
  }
}
