#!/usr/bin/env bash
# Remove Glass UI v1 (and optionally the upstream dashboard).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NS="${NS:-kubernetes-dashboard}"
REMOVE_UPSTREAM="${REMOVE_UPSTREAM:-false}"
DASHBOARD_VERSION="${DASHBOARD_VERSION:-v2.7.0}"
DASHBOARD_URL="${DASHBOARD_URL:-https://raw.githubusercontent.com/kubernetes/dashboard/${DASHBOARD_VERSION}/aio/deploy/recommended.yaml}"

echo "==> Removing Glass UI v1 from namespace ${NS}"
kubectl delete -k "$ROOT/deploy" --ignore-not-found=true
kubectl -n "$NS" delete secret dashboard-glass-tls --ignore-not-found=true

if [[ "$REMOVE_UPSTREAM" == "true" ]]; then
  echo "==> Removing upstream Kubernetes Dashboard"
  kubectl delete -f "$DASHBOARD_URL" --ignore-not-found=true
fi

echo "Done."
