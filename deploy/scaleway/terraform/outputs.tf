output "project_id" {
  description = "Scaleway project holding all Quill resources."
  value       = scaleway_account_project.quill.id
}

output "region" {
  value = var.region
}

# ---- cluster ----
output "cluster_id" {
  value = scaleway_k8s_cluster.quill.id
}

output "cluster_apiserver_url" {
  value = scaleway_k8s_cluster.quill.apiserver_url
}

output "kubeconfig" {
  description = "Admin kubeconfig (also in Secret Manager at /quill/k8s/kubeconfig)."
  value       = scaleway_k8s_cluster.quill.kubeconfig[0].config_file
  sensitive   = true
}

# ---- registry ----
output "registry_endpoint" {
  description = "Push/pull endpoint, e.g. rg.fr-par.scw.cloud/quill"
  value       = scaleway_registry_namespace.quill.endpoint
}

output "registry_namespace" {
  value = scaleway_registry_namespace.quill.name
}

# ---- database ----
output "rdb_private_ip" {
  description = "Private-network IP of the RDB instance (used by in-cluster pods)."
  value       = scaleway_rdb_instance.quill.private_ip[0].address
}

output "rdb_public_endpoint_ip" {
  description = "Public IP for operator access (psql from allowed CIDRs; TLS enforced, port 5432). Visible in the Scaleway console; the provider's endpoint_ip attribute is deprecated, so this reads the same value via the load_balancer/private_network data when present."
  value       = scaleway_rdb_instance.quill.endpoint_ip
}

output "rdb_admin_user" {
  value = scaleway_rdb_instance.quill.user_name
}

output "rdb_databases" {
  value = local.rdb_databases
}

# ---- backups ----
output "backup_bucket" {
  value = scaleway_object_bucket.backups.name
}

output "backup_s3_endpoint" {
  description = "S3 endpoint for the backup bucket."
  value       = "https://s3.${var.region}.scw.cloud"
}

output "backup_access_key" {
  value     = scaleway_iam_api_key.backup.access_key
  sensitive = true
}

output "backup_secret_key" {
  value     = scaleway_iam_api_key.backup.secret_key
  sensitive = true
}

# ---- CI deployer ----
output "ci_access_key" {
  description = "Store as GitHub repo secret SCW_CI_ACCESS_KEY."
  value       = scaleway_iam_api_key.ci.access_key
  sensitive   = true
}

output "ci_secret_key" {
  description = "Store as GitHub repo secret SCW_CI_SECRET_KEY."
  value       = scaleway_iam_api_key.ci.secret_key
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
  description = "Public hostnames of the deployment."
  value = {
    app  = local.app_host
    git  = local.git_host
    auth = local.auth_host
    ssh  = local.ssh_host
  }
}

# ---- secret manager paths (values are sensitive; read via `scw secret version get`) ----
output "secret_paths" {
  value = {
    rdb_admin_password    = "${local.secret_path_prefix}/rdb/rdb-admin-password"
    zitadel_db_password   = "${local.secret_path_prefix}/rdb/zitadel-db-password"
    jwt_secret            = "${local.secret_path_prefix}/app/jwt-secret"
    secret_encryption_key = "${local.secret_path_prefix}/app/secret-encryption-key"
    webhook_secret        = "${local.secret_path_prefix}/app/webhook-secret"
    dispatch_secret       = "${local.secret_path_prefix}/app/dispatch-secret"
    forgejo_admin_token   = "${local.secret_path_prefix}/app/forgejo-admin-token"
    zitadel_masterkey     = "${local.secret_path_prefix}/zitadel/zitadel-masterkey"
    backup_s3_access_key  = "${local.secret_path_prefix}/backup/s3-access-key"
    backup_s3_secret_key  = "${local.secret_path_prefix}/backup/s3-secret-key"
    kubeconfig            = "${local.secret_path_prefix}/k8s/kubeconfig"
    quill_database_url    = "${local.secret_path_prefix}/app/quill-database-url"
  }
}
