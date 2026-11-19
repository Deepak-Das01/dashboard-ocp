#!/usr/bin/env bash
# Install Kubernetes Dashboard (upstream) + Glass UI v1 on any cluster.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NS="${NS:-kubernetes-dashboard}"
DASHBOARD_VERSION="${DASHBOARD_VERSION:-v2.7.0}"
DASHBOARD_URL="${DASHBOARD_URL:-https://raw.githubusercontent.com/kubernetes/dashboard/${DASHBOARD_VERSION}/aio/deploy/recommended.yaml}"
SKIP_UPSTREAM="${SKIP_UPSTREAM:-false}"
EXTRA_SAN="${EXTRA_SAN:-}"

echo "==> kubernetes-dashboard Glass UI v1 (1.0.0)"
echo "    namespace: ${NS}"
echo "    upstream: ${DASHBOARD_URL}"

command -v kubectl >/dev/null || { echo "kubectl is required"; exit 1; }
command -v openssl >/dev/null || { echo "openssl is required"; exit 1; }

# Resolve node IP hint for TLS SAN (best-effort)
NODE_IP="$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null || true)"
if [[ -n "$NODE_IP" && -z "$EXTRA_SAN" ]]; then
  EXTRA_SAN="IP:${NODE_IP}"
fi
export EXTRA_SAN

if [[ "$SKIP_UPSTREAM" != "true" ]]; then
  echo "==> Applying official Kubernetes Dashboard ${DASHBOARD_VERSION}"
  kubectl apply -f "$DASHBOARD_URL"
  echo "==> Waiting for upstream dashboard rollout"
  kubectl -n "$NS" rollout status deploy/kubernetes-dashboard --timeout=180s
  kubectl -n "$NS" rollout status deploy/dashboard-metrics-scraper --timeout=180s || true
else
  echo "==> Skipping upstream dashboard (SKIP_UPSTREAM=true)"
  kubectl -n "$NS" get deploy kubernetes-dashboard >/dev/null
fi

# Optional: allow token login when UI is reached over HTTP (lab only)
if [[ "${ENABLE_INSECURE_LOGIN:-true}" == "true" ]]; then
  echo "==> Ensuring --enable-insecure-login on upstream dashboard (lab convenience)"
  ARGS="$(kubectl -n "$NS" get deploy kubernetes-dashboard -o jsonpath='{.spec.template.spec.containers[0].args}' 2>/dev/null || true)"
  if [[ "$ARGS" != *enable-insecure-login* ]]; then
    kubectl -n "$NS" patch deploy kubernetes-dashboard --type='json' -p='[
      {"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--enable-insecure-login"}
    ]'
    kubectl -n "$NS" rollout status deploy/kubernetes-dashboard --timeout=120s
  fi
fi

echo "==> Creating TLS secret for Glass UI"
NS="$NS" EXTRA_SAN="$EXTRA_SAN" "$ROOT/scripts/create-tls-secret.sh"

echo "==> Applying Glass UI v1 manifests (kustomize)"
kubectl apply -k "$ROOT/deploy"

echo "==> Waiting for Glass UI rollout"
kubectl -n "$NS" rollout status deploy/dashboard-glass --timeout=120s

HTTP_PORT="$(kubectl -n "$NS" get svc dashboard-glass -o jsonpath='{.spec.ports[?(@.name=="http")].nodePort}')"
HTTPS_PORT="$(kubectl -n "$NS" get svc dashboard-glass -o jsonpath='{.spec.ports[?(@.name=="https")].nodePort}')"
echo
echo "Glass UI v1 is ready."
echo "  HTTP : http://<node-ip>:${HTTP_PORT}/"
echo "  HTTPS: https://<node-ip>:${HTTPS_PORT}/   (self-signed; accept browser warning)"
if [[ -n "$NODE_IP" ]]; then
  echo "  Example: http://${NODE_IP}:${HTTP_PORT}/"
  echo "           https://${NODE_IP}:${HTTPS_PORT}/"
fi
echo
echo "Get a login token:"
echo "  ${ROOT}/scripts/get-token.sh"
echo
echo "Port-forward (recommended for local login without TLS warnings):"
echo "  kubectl -n ${NS} port-forward svc/dashboard-glass 8443:443"
echo "  open https://127.0.0.1:8443/"
