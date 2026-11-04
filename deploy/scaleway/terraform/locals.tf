locals {
  name_prefix = replace(var.project_name, "_", "-")

  # Public hostnames (subdomains of var.domain).
  app_host  = "app.${var.domain}"
  git_host  = "git.${var.domain}"
  auth_host = "auth.${var.domain}"
  ssh_host  = "ssh.${var.domain}"

  # Database names on the managed RDB instance.
  rdb_databases = ["quill", "forgejo", "zitadel"]

  # Split var.domain into the registrable root and the subdomain label(s) that
  # the Scaleway DNS zone represents (e.g. quill.example.com -> root
  # example.com, subdomain quill; apex example.com -> subdomain "").
  domain_parts = split(".", var.domain)
  root_domain  = "${local.domain_parts[length(local.domain_parts) - 2]}.${local.domain_parts[length(local.domain_parts) - 1]}"
  subdomain    = join(".", slice(local.domain_parts, 0, length(local.domain_parts) - 2))

  # Most Scaleway resources take tags as a list of strings; object buckets take
  # a map — keep both shapes.
  tags     = ["project:quill", "env:mvp", "managed:terraform"]
  tags_map = { project = "quill", env = "mvp", managed = "terraform" }
}
