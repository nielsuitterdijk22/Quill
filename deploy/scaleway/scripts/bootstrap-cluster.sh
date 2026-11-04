#!/usr/bin/env bash
# bootstrap-cluster.sh — one-time cluster add-ons after `terraform apply`:
#   1. cert-manager (Let's Encrypt) + the Scaleway DNS-01 webhook
#   2. the ClusterIssuer + DNS credentials secret
#   3. the zitadel-steps ConfigMap (from deploy/compose/zitadel/steps.yaml)
#
# Requires: kubectl configured for the Kapsule cluster (kubeconfig_path output),
# helm, and SCW_CI_ACCESS_KEY / SCW_CI_SECRET_KEY in the env (or SCW_* owner keys).
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$HERE/../../.."
NS="${KUBE_NAMESPACE:-quill}"
CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:-v1.16.2}"
LETSENCRYPT_EMAIL="${LETSENCRYPT_EMAIL:?set LETSENCRYPT_EMAIL}"
SCW_REGION="${SCW_DEFAULT_REGION:-fr-par}"
SCW_PROJECT_ID="${SCW_PROJECT_ID:?set SCW_PROJECT_ID (terraform output project_id)}"

echo "==> Installing cert-manager $CERT_MANAGER_VERSION"
kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
kubectl -n cert-manager wait --for=condition=available deployment/cert-manager --timeout=300s

echo "==> Installing Scaleway DNS-01 webhook for cert-manager"
helm repo add scaleway https://helm.scaleway.com 2>/dev/null || true
helm repo update
helm upgrade --install cert-manager-webhook-scaleway scaleway/cert-manager-webhook-scaleway \
  --namespace cert-manager \
  --set groupName=acme.scaleway.com \
  --set env.SCW_DEFAULT_REGION="$SCW_REGION" \
  --set env.SCW_DEFAULT_PROJECT_ID="$SCW_PROJECT_ID" \
  --wait --timeout 300s

echo "==> DNS credentials secret for the webhook (cert-manager namespace)"
kubectl -n cert-manager create secret generic scaleway-dns-creds \
  --from-literal=SCW_ACCESS_KEY="${SCW_CI_ACCESS_KEY:?set SCW_CI_ACCESS_KEY}" \
  --from-literal=SCW_SECRET_KEY="${SCW_CI_SECRET_KEY:?set SCW_CI_SECRET_KEY}" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "==> ClusterIssuer + zitadel-steps ConfigMap"
kubectl create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$NS" create configmap zitadel-steps \
  --from-file=steps.yaml="$REPO_ROOT/deploy/compose/zitadel/steps.yaml" \
  --dry-run=client -o yaml | kubectl apply -f -

# ClusterIssuer is rendered with the ACME email and applied with the manifests.
export LETSENCRYPT_EMAIL
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
if command -v envsubst >/dev/null 2>&1; then
  envsubst < "$HERE/../k8s/cert-manager.yaml" > "$tmp"
else
  perl -pe 's/\$\{(\w+)\}/exists $ENV{$1} ? $ENV{$1} : "\${$1}"/ge' "$HERE/../k8s/cert-manager.yaml" > "$tmp"
fi
kubectl apply -f "$tmp"

echo "==> bootstrap done"
