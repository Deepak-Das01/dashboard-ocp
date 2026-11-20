#!/usr/bin/env bash
# Remove kube-dashboard, optional support manifests, optional metrics-server.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NS="${NS:-kubernetes-dashboard}"
REMOVE_SUPPORT="${REMOVE_SUPPORT:-false}"
REMOVE_UPSTREAM="${REMOVE_UPSTREAM:-false}"  # alias for REMOVE_SUPPORT
REMOVE_METRICS_SERVER="${REMOVE_METRICS_SERVER:-false}"
DASHBOARD_VERSION="${DASHBOARD_VERSION:-v2.7.0}"
SUPPORT_MANIFEST="${SUPPORT_MANIFEST:-$ROOT/deploy/upstream/dashboard-support-${DASHBOARD_VERSION}.yaml}"
LEGACY_MANIFEST="${LEGACY_MANIFEST:-$ROOT/deploy/upstream/dashboard-recommended-${DASHBOARD_VERSION}.yaml}"

if [[ "$REMOVE_UPSTREAM" == "true" ]]; then
  REMOVE_SUPPORT=true
fi

echo "==> Removing kube-dashboard from namespace ${NS}"
kubectl delete -k "$ROOT/deploy" --ignore-not-found=true
kubectl -n "$NS" delete secret kube-dashboard-tls --ignore-not-found=true
# legacy names
kubectl -n "$NS" delete secret dashboard-glass-tls --ignore-not-found=true
kubectl -n "$NS" delete deploy,svc dashboard-glass --ignore-not-found=true

if [[ "$REMOVE_SUPPORT" == "true" ]]; then
  echo "==> Removing Dashboard support resources"
  if [[ -f "$SUPPORT_MANIFEST" ]]; then
    kubectl delete -f "$SUPPORT_MANIFEST" --ignore-not-found=true
  fi
  if [[ -f "$LEGACY_MANIFEST" ]]; then
    kubectl delete -f "$LEGACY_MANIFEST" --ignore-not-found=true
  fi
fi

if [[ "$REMOVE_METRICS_SERVER" == "true" ]]; then
  echo "==> Removing metrics-server"
  kubectl delete -f "$ROOT/deploy/metrics-server/components.yaml" --ignore-not-found=true
fi

echo "Done."
