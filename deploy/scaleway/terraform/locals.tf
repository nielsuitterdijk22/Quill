locals {
  name_prefix = replace(var.project_name, "_", "-")

  # Public hostnames (subdomains of var.domain).
  app_host  = "app.${var.domain}"
  api_host  = "api.${var.domain}"
  git_host  = "git.${var.domain}"
  auth_host = "auth.${var.domain}"
  ssh_host  = "ssh.${var.domain}"

  # Serverless SQL databases: quill (platform metadata), forgejo (git backend),
  # zitadel (IdP).
  sdb_names = ["quill", "forgejo", "zitadel"]

  # SDB endpoint is host[:port]; normalize to host + port 5432.
  sdb_host = { for n, db in scaleway_sdb_sql_database.dbs : n => split(":", db.endpoint)[0] }
  sdb_port = { for n, db in scaleway_sdb_sql_database.dbs : n => length(split(":", db.endpoint)) > 1 ? split(":", db.endpoint)[1] : "5432" }

  # SDB credentials are the IAM DB user's id (username) + its API key secret
  # (password) — see Scaleway SDB docs.
  db_user     = scaleway_iam_user.db.id
  db_password = scaleway_iam_api_key.db.secret_key

  db_url = {
    for n in local.sdb_names :
    n => "postgres://${local.db_user}:${local.db_password}@${local.sdb_host[n]}:${local.sdb_port[n]}/${n}?sslmode=require"
  }

  registry = scaleway_registry_namespace.quill.endpoint

  # CNAME targets for the serverless container domains (strip https://).
  container_cname = {
    api  = scaleway_container.api.domain_name
    app  = scaleway_container.web.domain_name
    auth = scaleway_container.zitadel.domain_name
  }

  # Split var.domain into the registrable root and the subdomain label(s) that
  # the Scaleway DNS zone represents.
  domain_parts = split(".", var.domain)
  root_domain  = "${local.domain_parts[length(local.domain_parts) - 2]}.${local.domain_parts[length(local.domain_parts) - 1]}"
  subdomain    = join(".", slice(local.domain_parts, 0, length(local.domain_parts) - 2))

  tags     = ["project:quill", "env:mvp", "managed:terraform"]
  tags_map = { project = "quill", env = "mvp", managed = "terraform" }
}
