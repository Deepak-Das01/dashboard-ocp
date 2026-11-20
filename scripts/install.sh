#!/usr/bin/env bash
# Install kube-dashboard (single pod) + metrics-scraper support + metrics-server.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NS="${NS:-kubernetes-dashboard}"
DASHBOARD_VERSION="${DASHBOARD_VERSION:-v2.7.0}"
SUPPORT_MANIFEST="${SUPPORT_MANIFEST:-$ROOT/deploy/upstream/dashboard-support-${DASHBOARD_VERSION}.yaml}"
SKIP_SUPPORT="${SKIP_SUPPORT:-false}"
INSTALL_METRICS_SERVER="${INSTALL_METRICS_SERVER:-true}"
LOAD_IMAGES="${LOAD_IMAGES:-true}"
EXTRA_SAN="${EXTRA_SAN:-}"

echo "==> kube-dashboard + metrics-server"
echo "    namespace: ${NS}"
echo "    support manifest: ${SUPPORT_MANIFEST}"
echo "    metrics-server: ${INSTALL_METRICS_SERVER}"

command -v kubectl >/dev/null || { echo "kubectl is required"; exit 1; }
command -v openssl >/dev/null || { echo "openssl is required"; exit 1; }

NODE_IP="$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null || true)"
if [[ -n "$NODE_IP" && -z "$EXTRA_SAN" ]]; then
  EXTRA_SAN="IP:${NODE_IP}"
fi
export EXTRA_SAN

if [[ "$LOAD_IMAGES" == "true" ]]; then
  echo "==> Loading container images (local archives or GitHub Release assets)"
  "$ROOT/scripts/load-images.sh" || {
    echo "WARNING: image load failed; continuing (cluster must already have required images)" >&2
  }
fi

if [[ "$INSTALL_METRICS_SERVER" == "true" ]]; then
  echo "==> Applying metrics-server"
  kubectl apply -f "$ROOT/deploy/metrics-server/components.yaml"
  kubectl -n kube-system rollout status deploy/metrics-server --timeout=180s
fi

if [[ "$SKIP_SUPPORT" != "true" ]]; then
  if [[ ! -f "$SUPPORT_MANIFEST" ]]; then
    echo "Missing support manifest: $SUPPORT_MANIFEST" >&2
    exit 1
  fi
  echo "==> Applying Dashboard support resources (RBAC, scraper; no separate dashboard Deployment)"
  kubectl apply -f "$SUPPORT_MANIFEST"
  kubectl -n "$NS" rollout status deploy/dashboard-metrics-scraper --timeout=180s || true
fi

# Remove legacy separate upstream dashboard + old glass proxy if present
echo "==> Removing legacy separate dashboard / glass resources (if any)"
kubectl -n "$NS" delete deploy kubernetes-dashboard --ignore-not-found=true
kubectl -n "$NS" delete svc kubernetes-dashboard --ignore-not-found=true
kubectl -n "$NS" delete deploy dashboard-glass --ignore-not-found=true
kubectl -n "$NS" delete svc dashboard-glass --ignore-not-found=true
kubectl -n "$NS" delete cm dashboard-glass-css dashboard-glass-nginx --ignore-not-found=true
kubectl -n "$NS" delete secret dashboard-glass-tls --ignore-not-found=true

echo "==> Creating TLS secret for kube-dashboard"
NS="$NS" EXTRA_SAN="$EXTRA_SAN" "$ROOT/scripts/create-tls-secret.sh"

echo "==> Applying kube-dashboard manifests (kustomize)"
kubectl apply -k "$ROOT/deploy"

echo "==> Waiting for kube-dashboard rollout"
kubectl -n "$NS" rollout status deploy/kube-dashboard --timeout=180s

if [[ "$INSTALL_METRICS_SERVER" == "true" ]]; then
  echo "==> Checking metrics API (kubectl top nodes)"
  for i in 1 2 3 4 5 6; do
    if kubectl top nodes >/dev/null 2>&1; then
      kubectl top nodes
      break
    fi
    echo "    waiting for metrics.k8s.io (${i}/6)..."
    sleep 10
  done
fi

HTTP_PORT="$(kubectl -n "$NS" get svc kube-dashboard -o jsonpath='{.spec.ports[?(@.name=="http")].nodePort}')"
HTTPS_PORT="$(kubectl -n "$NS" get svc kube-dashboard -o jsonpath='{.spec.ports[?(@.name=="https")].nodePort}')"
echo
echo "kube-dashboard is ready."
echo "  HTTP : http://<node-ip>:${HTTP_PORT}/"
echo "  HTTPS: https://<node-ip>:${HTTPS_PORT}/   (self-signed; accept browser warning)"
if [[ -n "$NODE_IP" ]]; then
  echo "  Example: https://${NODE_IP}:${HTTPS_PORT}/"
fi
echo
echo "Get a login token:"
echo "  ${ROOT}/scripts/get-token.sh"
echo
echo "Port-forward:"
echo "  kubectl -n ${NS} port-forward svc/kube-dashboard 8443:443"
echo "  open https://127.0.0.1:8443/"
