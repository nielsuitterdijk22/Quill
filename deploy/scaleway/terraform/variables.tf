# ---- Scaleway location (fr-par = EU, Paris — pinned per SUITE.md EU-residency) ----
variable "region" {
  description = "Scaleway region for all resources."
  type        = string
  default     = "fr-par"
}

variable "zone" {
  description = "Scaleway zone for zonal resources (the Forgejo VM)."
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
  description = "Root domain for the deployment (e.g. quill.example.com). Subdomains: app (web), api (backend), git (Forgejo HTTP), auth (Zitadel), ssh (git SSH)."
  type        = string
}

variable "create_dns_zone" {
  description = "Create and manage the Scaleway DNS zone for `domain`. Set false if the zone already exists."
  type        = bool
  default     = true
}

variable "existing_dns_zone_id" {
  description = "Use an existing Scaleway DNS zone instead of creating one (set create_dns_zone=false)."
  type        = string
  default     = ""
}

# ---- DNS for the Forgejo VM ----------------------------------------------------
variable "vm_ip" {
  description = "Public IP of the Forgejo VM. Empty on first apply; scripts/dns-sync.sh fills the git/ssh A records from the instance after creation (or re-apply with this var set)."
  type        = string
  default     = ""
}

# ---- Backup --------------------------------------------------------------------
variable "backup_bucket_name" {
  description = "S3-compatible bucket for backups. Must be unique within the region."
  type        = string
}

variable "repos_bucket_name" {
  description = "S3-compatible bucket for the own git engine (packs + manifests). Must be unique within the region."
  type        = string
}

variable "backup_retention_days" {
  description = "Days to keep backups in the bucket (pruned by the backup job) and SDB's own 7-day managed backups are included free."
  type        = number
  default     = 30
}

variable "backup_schedule" {
  description = "Cron schedule (UTC) for the pg_dump backup job."
  type        = string
  default     = "0 3 * * *"
}

# ---- Registry / images -----------------------------------------------------------
variable "registry_namespace" {
  description = "Container registry namespace (rg.<region>.scw.cloud/<namespace>)."
  type        = string
  default     = "quill"
}

variable "image_tag" {
  description = "Image tag for the Quill containers/job. CI sets this to the git SHA on every deploy."
  type        = string
  default     = "latest"
}

# ---- Serverless SQL database credentials -------------------------------------------
# SDB authenticates IAM principals: username = the IAM user ID, password = an API
# key secret of that user. Create the key for your own user (console: IAM & API
# keys -> generate for yourself) and pass both here / via -var (CI: GH secrets
# SDB_USER / SDB_PASSWORD).
variable "sdb_user" {
  description = "IAM user ID used as the username for the serverless SQL databases."
  type        = string
}

variable "sdb_password" {
  description = "API key secret of sdb_user, used as the SDB password."
  type        = string
  sensitive   = true
}

variable "vm_docker_gid" {
  description = "Numeric group id of the docker group on the VM host; the dispatch container is added to it so the act runner can dial the engine socket."
  type        = string
  default     = "988"
}

variable "dispatch_secret" {
  description = "Shared HMAC secret between the api and the pipeline dispatch container. Set it explicitly so api <-> dispatch cannot drift across state regenerations; empty falls back to the generated random_password."
  type        = string
  default     = ""
  sensitive   = true
}

# ---- Serverless SQL databases -----------------------------------------------------
variable "sdb_version" {
  description = "PostgreSQL major version for the serverless databases."
  type        = string
  default     = "16"
}

variable "sdb_min_cpu" {
  description = "Min vCPU for each serverless DB. 0 = scales to zero when idle (no compute charge)."
  type        = number
  default     = 0
}

variable "sdb_max_cpu" {
  description = "Max vCPU for each serverless DB (bounds the bill under load)."
  type        = number
  default     = 4
}

# ---- Serverless containers (api, web, zitadel) ------------------------------------
variable "container_cpu" {
  description = "vCPU (mCPU) limit per container instance."
  type        = number
  default     = 1000
}

variable "container_memory_mb" {
  description = "Memory limit per container instance, in MB."
  type        = number
  default     = 1024
}

variable "container_max_scale" {
  description = "Max instances per container. min_scale is always 0 (scale to zero)."
  type        = number
  default     = 2
}

# ---- Forgejo VM --------------------------------------------------------------------
variable "vm_type" {
  description = "Instance type for the single Forgejo/dispatch VM. DEV1-S (2 vCPU / 2 GB) is enough for a hobby instance; git data needs a real disk, so Forgejo can't be serverless."
  type        = string
  default     = "DEV1-S"
}

variable "vm_image" {
  description = "Instance image label (Ubuntu 24.04)."
  type        = string
  default     = "ubuntu_noble"
}

variable "vm_root_volume_gb" {
  description = "Root volume size for the Forgejo VM (holds git data)."
  type        = number
  default     = 20
}

variable "ssh_cidrs" {
  description = "CIDRs allowed to SSH (port 22) into the Forgejo VM. Tighten to your egress IPs."
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

# ---- App secrets provided out-of-band (post-provision) ------------------------------
variable "forgejo_admin_token" {
  description = "Forgejo admin token. Placeholder on first apply; scripts/provision.sh mints it on the VM and you re-apply with -var forgejo_admin_token=... (also stored in Secret Manager)."
  type        = string
  default     = "pending-provision"
  sensitive   = true
}

variable "zitadel_client_secret" {
  description = "Zitadel client secret for the web app's confidential OIDC app (empty = public PKCE client)."
  type        = string
  sensitive   = true
  default     = ""
}

variable "zitadel_management_token" {
  description = "Zitadel machine-user PAT for the Management API. Retrieved from the zitadel container logs on first boot (scripts/provision.sh)."
  type        = string
  default     = "pending-provision"
  sensitive   = true
}

variable "github_client_id" {
  type      = string
  default   = "unset"
  sensitive = true

  # Reject obvious placeholders (terraform's "unset" default, or a dummy like
  # "x" typed into tfvars) so a broken OAuth flow can't deploy silently — but
  # accept ANY non-placeholder value: client-ID prefixes differ between OAuth
  # apps (Iv1.…), GitHub Apps (IvCB…/Iv23…), and may gain new formats, so
  # pinning a prefix rejected a real credential and failed every CI apply.
  validation {
    condition     = var.github_client_id == "unset" || !contains(["", "x", "changeme", "change-me", "placeholder", "todo", "redacted"], lower(trimspace(var.github_client_id)))
    error_message = "github_client_id looks like a placeholder. Set the real client ID from the GitHub app (github.com/settings/developers; callback https://api.<domain>/api/v1/auth/github/callback) or leave \"unset\" to disable GitHub import with a clear error."
  }
}

variable "github_client_secret" {
  type      = string
  default   = "unset"
  sensitive = true

  validation {
    condition     = var.github_client_secret == "unset" || !contains(["x", "changeme", "change-me", "placeholder", "todo", "redacted"], lower(trimspace(var.github_client_secret)))
    error_message = "github_client_secret looks like a placeholder. Set the real secret from the GitHub OAuth app, or leave \"unset\" to disable GitHub import with a clear error."
  }
}

variable "letsencrypt_email" {
  description = "ACME account email (Caddy on the VM issues Let's Encrypt certs for git.<domain> via HTTP-01)."
  type        = string
}

# ---- Transactional email (Brevo SMTP relay) --------------------------------------
# Zitadel (invites, activation, password reset) and Forgejo (PR notifications)
# send through Brevo. Leave user/password empty to disable. CI passes these from
# GH secrets BREVO_SMTP_NAME / BREVO_SMTP_KEY (+ optional var BREVO_SMTP_FROM).
variable "brevo_smtp_host" {
  description = "Brevo SMTP relay host (without port)."
  type        = string
  default     = "smtp-relay.brevo.com"
}

variable "brevo_smtp_port" {
  description = "Brevo SMTP port. 2525 = STARTTLS (587/465 are blocked on Scaleway egress — verified: connection timeout from the VM, 2525 open)."
  type        = number
  default     = 2525
}

variable "brevo_smtp_user" {
  description = "Brevo SMTP login (the xxx@smtp-brevo.com login shown in the Brevo dashboard)."
  type        = string
  default     = ""
}

variable "brevo_smtp_password" {
  description = "Brevo SMTP key (xsmtpib-...), from Transactional -> SMTP -> generate key."
  type        = string
  default     = ""
  sensitive   = true
}

variable "brevo_smtp_from" {
  description = "Sender address. Defaults to info@<parent of domain> (the parent is the Brevo-authenticated domain). The domain must be authenticated in Brevo."
  type        = string
  default     = ""
}

variable "brevo_smtp_from_name" {
  description = "Sender display name."
  type        = string
  default     = "Quill"
}

variable "creem_api_key" {
  description = "Creem merchant-of-record API key. Placeholder on first apply; set a real key (and webhook secret + team product id) to activate plan upgrades."
  type        = string
  default     = "unset"
}

variable "creem_webhook_secret" {
  description = "Creem webhook signing secret (creem-signature header verification)."
  type        = string
  default     = "unset"
}

variable "creem_team_product_id" {
  description = "Creem product id for Quill's Team plan (checkout sessions sell this)."
  type        = string
  default     = "unset"
}

variable "creem_pro_product_id" {
  description = "Creem product id for Quill's individual Pro plan (personal-account upgrades)."
  type        = string
  default     = "unset"
}

variable "ci_queue_mode" {
  description = "Phase A switch: true routes pipeline runs through the Postgres queue + broker + ephemeral CI instances; false keeps the inline VM dispatch."
  type        = bool
  default     = false
}

variable "ci_instance_type" {
  description = "Commercial type of the ephemeral CI worker instance."
  type        = string
  default     = "DEV1-S"
}
