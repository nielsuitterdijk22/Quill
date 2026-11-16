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

  # Public clone base of the own git engine: the dedicated git host once DNS
  # points it at the api container, the api host's /git mount until then.
  git_public_url = var.git_host_on_api ? "https://${local.git_host}" : "https://${local.api_host}/git"

  # Databases on the managed instance (managed_db.tf): quill (platform metadata)
  # and zitadel. (They ran on Serverless SQL until 2026-11-16 — see managed_db.tf.)
  sdb_names = ["quill", "zitadel"]

  db_host     = scaleway_rdb_instance.main.load_balancer[0].ip
  db_port     = scaleway_rdb_instance.main.load_balancer[0].port
  db_user     = scaleway_rdb_instance.main.user_name
  db_password = random_password.rdb.result

  db_url = {
    for n in local.sdb_names :
    n => "postgresql://${local.db_user}:${local.db_password}@${local.db_host}:${local.db_port}/${n}?sslmode=require"
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
