locals {
  name_prefix = replace(var.project_name, "_", "-")

  # Pipeline dispatch HMAC secret shared by the serverless api and the VM
  # dispatch container. var.dispatch_secret pins it explicitly (in tfvars,
  # gitignored) so the two ends can never drift apart the way the generated
  # random_password did (state regeneration handed the api a new value while
  # the VM container kept its old one - every pipeline run 401'd instantly).
  dispatch_secret = coalesce(var.dispatch_secret, random_password.dispatch_secret.result)

  # Public hostnames (subdomains of var.domain).
  app_host  = "app.${var.domain}"
  api_host  = "api.${var.domain}"
  git_host  = "git.${var.domain}"
  auth_host = "auth.${var.domain}"
  ssh_host  = "ssh.${var.domain}"

  # Serverless SQL databases: quill (platform metadata) + zitadel (C1: the
  # zitadel SERVERLESS CONTAINER drops its connection pool when scaled to
  # zero, so its SDB scales to zero too — unlike Forgejo, whose always-on pool
  # kept the old "forgejo" SDB at ~0.22 vCPU 24/7). Forgejo's database still
  # runs on the VM in the forgejo-db container.
  sdb_names = ["quill", "zitadel"]

  # SDB endpoint is a full postgres:// URI (postgres://host:port/db?sslmode=require).
  sdb_addr = { for n, db in scaleway_sdb_sql_database.dbs : n => split("/", db.endpoint)[2] } # host:port
  sdb_host = { for n, a in local.sdb_addr : n => split(":", a)[0] }
  sdb_port = { for n, a in local.sdb_addr : n => split(":", a)[1] }

  # SDB credentials: IAM user id (username) + one of its API key secrets
  # (password), supplied via var.sdb_user / var.sdb_password.
  db_user     = var.sdb_user
  db_password = var.sdb_password

  db_url = {
    for n in local.sdb_names :
    n => "postgresql://${local.db_user}:${local.db_password}@${local.sdb_host[n]}:${local.sdb_port[n]}/${n}?sslmode=require"
  }

  registry = scaleway_registry_namespace.quill.endpoint

  # CNAME targets for the serverless container domains (strip https://).
  container_cname = {
    api = scaleway_container.api.domain_name
    app = scaleway_container.web.domain_name
    # auth flips to the zitadel container at the registrar when C1 is verified
    auth = scaleway_container.zitadel.domain_name
  }

  # Split var.domain into the registrable root and the subdomain label(s) that
  # the Scaleway DNS zone represents.
  domain_parts = split(".", var.domain)
  root_domain  = "${local.domain_parts[length(local.domain_parts) - 2]}.${local.domain_parts[length(local.domain_parts) - 1]}"
  subdomain    = join(".", slice(local.domain_parts, 0, length(local.domain_parts) - 2))

  tags     = ["project:quill", "env:mvp", "managed:terraform"]
  tags_map = { project = "quill", env = "mvp", managed = "terraform" }

  # Transactional email via Brevo. Enabled only when credentials are supplied
  # (CI: GH secrets BREVO_SMTP_NAME / BREVO_SMTP_KEY).
  # nonsensitive(): the secret-derived bool must not be sensitive, or it can't
  # be used in for_each (secrets.tf) / template conditionals
  brevo_enabled   = nonsensitive(var.brevo_smtp_user != "" && var.brevo_smtp_password != "")
  brevo_from      = var.brevo_smtp_from != "" ? var.brevo_smtp_from : "info@${local.root_domain}"
  brevo_host_port = "${var.brevo_smtp_host}:${var.brevo_smtp_port}"
}
