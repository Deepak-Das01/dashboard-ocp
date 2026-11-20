#!/usr/bin/env bash
# Remove Liquid Glass UI, optional upstream Dashboard, and optional metrics-server.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NS="${NS:-kubernetes-dashboard}"
REMOVE_UPSTREAM="${REMOVE_UPSTREAM:-false}"
REMOVE_METRICS_SERVER="${REMOVE_METRICS_SERVER:-false}"
DASHBOARD_VERSION="${DASHBOARD_VERSION:-v2.7.0}"
DASHBOARD_MANIFEST="${DASHBOARD_MANIFEST:-$ROOT/deploy/upstream/dashboard-recommended-${DASHBOARD_VERSION}.yaml}"

echo "==> Removing Glass UI from namespace ${NS}"
kubectl delete -k "$ROOT/deploy" --ignore-not-found=true
kubectl -n "$NS" delete secret dashboard-glass-tls --ignore-not-found=true

if [[ "$REMOVE_UPSTREAM" == "true" ]]; then
  echo "==> Removing upstream Kubernetes Dashboard"
  if [[ -f "$DASHBOARD_MANIFEST" ]]; then
    kubectl delete -f "$DASHBOARD_MANIFEST" --ignore-not-found=true
  fi
fi

if [[ "$REMOVE_METRICS_SERVER" == "true" ]]; then
  echo "==> Removing metrics-server"
  kubectl delete -f "$ROOT/deploy/metrics-server/components.yaml" --ignore-not-found=true
fi

echo "Done."
