#!/usr/bin/env bash
# make-ci-kubeconfig.sh — build a kubeconfig for the quill-ci-deployer IAM key
# and print it base64-encoded (paste into the GitHub repo secret KUBE_CONFIG).
#
# The Kapsule API server accepts the IAM secret key as a bearer token.
# Requires: terraform state in ../terraform (for apiserver_url + CA) OR env:
#   APISERVER_URL, CLUSTER_CA (base64), SCW_CI_SECRET_KEY
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
TF_DIR="$HERE/../terraform"

APISERVER_URL="${APISERVER_URL:-$(cd "$TF_DIR" && terraform output -raw cluster_apiserver_url 2>/dev/null || true)}"
: "${APISERVER_URL:?APISERVER_URL required}"
: "${SCW_CI_SECRET_KEY:?SCW_CI_SECRET_KEY required (terraform output ci_secret_key)}"

if [ -z "${CLUSTER_CA:-}" ]; then
  CLUSTER_CA="$(cd "$TF_DIR" && terraform output -raw kubeconfig 2>/dev/null | python3 -c '
import sys, yaml, base64, json
try:
    kc = yaml.safe_load(sys.stdin)
except Exception:
    kc = None
if kc:
    print(kc["clusters"][0]["cluster"]["certificate-authority-data"])
' 2>/dev/null || true)"
fi
: "${CLUSTER_CA:?CLUSTER_CA required (base64 CA from the cluster kubeconfig)}"

cat <<EOF | base64 -w0; echo
apiVersion: v1
kind: Config
clusters:
- name: quill-kapsule
  cluster:
    server: ${APISERVER_URL}
    certificate-authority-data: ${CLUSTER_CA}
contexts:
- name: quill-kapsule
  context: { cluster: quill-kapsule, user: quill-ci }
current-context: quill-kapsule
users:
- name: quill-ci
  user:
    token: ${SCW_CI_SECRET_KEY}
EOF
