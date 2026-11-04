# ---- Scaleway location (fr-par = EU, Paris — pinned per SUITE.md EU-residency) ----
variable "region" {
  description = "Scaleway region for all resources."
  type        = string
  default     = "fr-par"
}

variable "zone" {
  description = "Scaleway zone for zonal resources (node pools, RDB)."
  type        = string
  default     = "fr-par-1"
}

variable "project_name" {
  description = "Name of the Scaleway project that holds the Quill infra."
  type        = string
  default     = "quill-mvp"
}

# ---- DNS ---------------------------------------------------------------------
variable "domain" {
  description = "Root domain for the deployment (e.g. quill.example.com). A DNS zone for this domain is created in Scaleway DNS. Subdomains: app (web UI), git (Forgejo), auth (Zitadel)."
  type        = string
}

variable "create_dns_zone" {
  description = "Create and manage the Scaleway DNS zone for `domain`. Set false if the zone already exists (e.g. domain hosted elsewhere)."
  type        = bool
  default     = true
}

# Optional: after the first apply, set these to the load-balancer IPs so
# Terraform manages the A records (see scripts/dns-sync.sh for the automated path).
variable "ingress_lb_ip" {
  description = "Public IP of the Kapsule ingress load balancer (Traefik). When set, A records for app/git/auth are created."
  type        = string
  default     = ""
}

variable "ssh_lb_ip" {
  description = "Public IP of the Forgejo SSH load balancer (Service type=LoadBalancer). When set, the ssh.<domain> A record is created."
  type        = string
  default     = ""
}

# ---- Kapsule (Kubernetes) -----------------------------------------------------
variable "k8s_version" {
  description = "Kubernetes version for the Kapsule cluster."
  type        = string
  default     = "1.33"
}

variable "node_type" {
  description = "Instance type for the Kapsule node pool. DEV1-M (3 vCPU / 4 GB) fits the MVP stack."
  type        = string
  default     = "DEV1-M"
}

variable "node_count" {
  description = "Number of nodes in the default pool (minimum for the MVP)."
  type        = number
  default     = 2
}

variable "node_autoscaling" {
  description = "Enable cluster autoscaler on the default pool."
  type        = bool
  default     = true
}

variable "node_min_size" {
  type    = number
  default = 2
}

variable "node_max_size" {
  type    = number
  default = 5
}

# ---- RDB (managed PostgreSQL) --------------------------------------------------
variable "rdb_node_type" {
  description = "RDB instance node type. db-dev1-m is the smallest production-acceptable size."
  type        = string
  default     = "db-dev1-m"
}

variable "rdb_is_ha" {
  description = "Enable high availability for the RDB instance (doubles cost; off for MVP)."
  type        = bool
  default     = false
}

variable "rdb_backup_schedule" {
  description = "Managed RDB backup schedule (Scaleway-side backups, in addition to the S3 CronJob dumps)."
  type        = string
  default     = "0 2 * * *" # daily 02:00 UTC
}

# ---- Object Storage (backups) ---------------------------------------------------
variable "backup_bucket_name" {
  description = "S3-compatible bucket for Postgres dumps and Forgejo dumps. Must be globally unique within the region."
  type        = string
}

variable "backup_retention_days" {
  description = "Days to keep backups in the bucket (enforced by the backup CronJob prune)."
  type        = number
  default     = 30
}

# ---- Container registry ----------------------------------------------------------
variable "registry_namespace" {
  description = "Container registry namespace for Quill images (rg.<region>.scw.cloud/<namespace>)."
  type        = string
  default     = "quill"
}

# ---- Kubernetes access ------------------------------------------------------------
variable "kubeconfig_path" {
  description = "Local path to write the Kapsule kubeconfig to (also stored in Secret Manager). Empty = do not write a local file."
  type        = string
  default     = ""
}

variable "deploy_cidrs" {
  description = "CIDRs allowed to reach the RDB public endpoint (TLS enforced). Restrict to your office/CI egress IPs when known; the Kapsule nodes use dynamic egress IPs, so 0.0.0.0/0 is the MVP default."
  type        = list(string)
  default     = ["0.0.0.0/0"]
}
