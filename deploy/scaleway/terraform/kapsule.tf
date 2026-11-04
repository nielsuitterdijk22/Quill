# Kapsule (managed Kubernetes) — the cloud-native runtime for the Quill stack.
#
# The cluster is attached to the private network so pods reach the RDB
# instance over private IPs. Ingress is Kapsule's built-in Traefik controller;
# TLS is terminated by cert-manager (Let's Encrypt, DNS-01 via Scaleway DNS)
# installed by deploy/scaleway/scripts/bootstrap-cluster.sh.

resource "scaleway_k8s_cluster" "quill" {
  name        = "${local.name_prefix}-kapsule"
  description = "Quill MVP workload cluster"
  project_id  = scaleway_account_project.quill.id
  region      = var.region

  cni                         = "cilium"
  version                     = var.k8s_version
  type                        = "kapsule"
  private_network_id          = scaleway_vpc_private_network.quill.id
  delete_additional_resources = true

  auto_upgrade {
    enable                        = true
    maintenance_window_day        = "sunday"
    maintenance_window_start_hour = 3
  }

  tags = local.tags

  depends_on = [scaleway_vpc_private_network.quill]
}

# Public API-server access for CI (GitHub runners have dynamic egress IPs).
# Harden by replacing 0.0.0.0/0 with your CI egress ranges when known.
resource "scaleway_k8s_acl" "quill" {
  cluster_id = scaleway_k8s_cluster.quill.id
  region     = var.region
  acl_rules {
    ip              = "0.0.0.0/0"
    description     = "CI + operator access (tighten to known egress CIDRs)"
    scaleway_ranges = true
  }
}

resource "scaleway_k8s_pool" "default" {
  cluster_id  = scaleway_k8s_cluster.quill.id
  region      = var.region
  zone        = var.zone
  name        = "default"
  node_type   = var.node_type
  size        = var.node_count
  min_size    = var.node_min_size
  max_size    = var.node_max_size
  autoscaling = var.node_autoscaling
  autohealing = true
  version     = scaleway_k8s_cluster.quill.version

  root_volume_type       = "b_ssd"
  root_volume_size_in_gb = 50

  wait_for_pool_ready = true

  tags = local.tags
}

# Write the kubeconfig locally when kubeconfig_path is set (used by CI and the
# bootstrap script). The same value is also stored in Secret Manager (secrets.tf).
resource "local_file" "kubeconfig" {
  count           = var.kubeconfig_path != "" ? 1 : 0
  filename        = var.kubeconfig_path
  content         = scaleway_k8s_cluster.quill.kubeconfig[0].config_file
  file_permission = "0600"
}
