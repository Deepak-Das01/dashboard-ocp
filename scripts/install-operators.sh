#!/usr/bin/env bash
# Single-script install: CRDs + kubeops-* operators + persistent OIDC console wiring.
#
# Operators (name = namespace):
#   kubeops-console          role/component: console
#   kubeops-authentication   role/component: oidc
#
# Default (persistent product path):
#   - ClusterOIDC → Dex IdP (kubeadmin)
#   - KubeDashboard auth.mode=oidc → oauth2-proxy + Dex login page
#   - CLI stays on existing kubeconfig (cluster-admin)
#
# Escape hatch (basic auth only, no Dex):
#   AUTH_MODE=secret ./scripts/install-operators.sh
#
# Usage:
#   ./scripts/install-operators.sh
#   LOAD_IMAGES=false ./scripts/install-operators.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export PATH="/usr/local/go/bin:${PATH:-}"

VERSION="${VERSION:-1.3.0}"
CONSOLE_NAME="kubeops-console"
AUTH_NAME="kubeops-authentication"
CONSOLE_NS="${CONSOLE_NS:-$CONSOLE_NAME}"
AUTH_NS="${AUTH_NS:-$AUTH_NAME}"
CONSOLE_IMG="${CONSOLE_IMG:-${CONSOLE_NAME}:${VERSION}}"
AUTH_IMG="${AUTH_IMG:-${AUTH_NAME}:${VERSION}}"
# Back-compat aliases
OIDC_IMG="${OIDC_IMG:-$AUTH_IMG}"

LOAD_IMAGES="${LOAD_IMAGES:-true}"
BUILD_OPERATORS="${BUILD_OPERATORS:-true}"
PACK_IMAGES="${PACK_IMAGES:-true}"
PULL_OAUTH2_PROXY="${PULL_OAUTH2_PROXY:-true}"
OAUTH2_PROXY_IMG="${OAUTH2_PROXY_IMG:-quay.io/oauth2-proxy/oauth2-proxy:v7.7.1}"
AUTH_MODE="${AUTH_MODE:-oidc}" # oidc | secret | token
INSTALL_OIDC_CR="${INSTALL_OIDC_CR:-}"
CONSOLE_SAMPLE="${CONSOLE_SAMPLE:-}"
OIDC_SAMPLE="${OIDC_SAMPLE:-$ROOT/operator/config/samples/clusteroidc.yaml}"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-300}"

case "$AUTH_MODE" in
  oidc)
    INSTALL_OIDC_CR="${INSTALL_OIDC_CR:-true}"
    CONSOLE_SAMPLE="${CONSOLE_SAMPLE:-$ROOT/operator/config/samples/kubedashboard_oidc.yaml}"
    ;;
  secret)
    INSTALL_OIDC_CR="${INSTALL_OIDC_CR:-false}"
    CONSOLE_SAMPLE="${CONSOLE_SAMPLE:-$ROOT/operator/config/samples/kubedashboard_secret.yaml}"
    ;;
  token)
    INSTALL_OIDC_CR="${INSTALL_OIDC_CR:-false}"
    CONSOLE_SAMPLE="${CONSOLE_SAMPLE:-$ROOT/operator/config/samples/kubedashboard_token.yaml}"
    ;;
  *)
    echo "ERROR: AUTH_MODE must be oidc|secret|token (got: $AUTH_MODE)" >&2
    exit 1
    ;;
esac

log() { printf '==> %s\n' "$*"; }
warn() { printf 'WARNING: %s\n' "$*" >&2; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

command -v kubectl >/dev/null || die "kubectl is required"
command -v go >/dev/null || die "go is required (to build operators)"
command -v python3 >/dev/null || die "python3 is required (to pack operator images)"
command -v ctr >/dev/null || die "ctr (containerd) is required to import operator images"
kubectl cluster-info >/dev/null 2>&1 || die "kubectl cannot reach the cluster (check kubeconfig)"

NODE_IP="$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null || true)"

log "kubeops operators install (v${VERSION})"
echo "    node:              ${NODE_IP:-unknown}"
echo "    auth mode:         ${AUTH_MODE}"
echo "    ${CONSOLE_NAME}:   ns=${CONSOLE_NS} image=${CONSOLE_IMG} role=console"
echo "    ${AUTH_NAME}:      ns=${AUTH_NS} image=${AUTH_IMG} role=oidc"
echo "    console sample:    ${CONSOLE_SAMPLE}"
echo "    install ClusterOIDC:${INSTALL_OIDC_CR}"

# --- 1) Workload images ---
if [[ "$LOAD_IMAGES" == "true" ]]; then
  log "Loading dashboard/metrics images"
  "$ROOT/scripts/load-images.sh" || warn "image load failed; continuing if images already present"
fi

if [[ "$PULL_OAUTH2_PROXY" == "true" && "$AUTH_MODE" == "oidc" ]]; then
  log "Ensuring oauth2-proxy image ${OAUTH2_PROXY_IMG}"
  ctr -n k8s.io images pull "$OAUTH2_PROXY_IMG" >/dev/null || warn "could not pull ${OAUTH2_PROXY_IMG}; ensure it exists on nodes"
fi

# --- 2) Build operator binaries ---
mkdir -p "$ROOT/operator/bin" "$ROOT/artifacts/images"
if [[ "$BUILD_OPERATORS" == "true" ]]; then
  log "Building kubeops operator binaries"
  (
    cd "$ROOT/operator"
    go mod tidy
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o "bin/${CONSOLE_NAME}" ./cmd/console
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o "bin/${AUTH_NAME}" ./cmd/oidc
  )
fi
[[ -x "$ROOT/operator/bin/${CONSOLE_NAME}" ]] || die "missing $ROOT/operator/bin/${CONSOLE_NAME}"
[[ -x "$ROOT/operator/bin/${AUTH_NAME}" ]] || die "missing $ROOT/operator/bin/${AUTH_NAME}"

# --- 3) Pack + import operator images ---
if [[ "$PACK_IMAGES" == "true" ]]; then
  log "Packing kubeops operator images into containerd"
  CONSOLE_TAR="$ROOT/artifacts/images/${CONSOLE_NAME}-${VERSION}-amd64.tar"
  AUTH_TAR="$ROOT/artifacts/images/${AUTH_NAME}-${VERSION}-amd64.tar"
  python3 "$ROOT/scripts/pack-static-image.py" \
    "$ROOT/operator/bin/${CONSOLE_NAME}" "$CONSOLE_IMG" "$CONSOLE_TAR" --entrypoint /manager
  python3 "$ROOT/scripts/pack-static-image.py" \
    "$ROOT/operator/bin/${AUTH_NAME}" "$AUTH_IMG" "$AUTH_TAR" --entrypoint /manager
  ctr -n k8s.io images import "$CONSOLE_TAR"
  ctr -n k8s.io images import "$AUTH_TAR"
fi

# --- 4) CRDs + RBAC + Deployments ---
log "Applying CRDs"
kubectl apply -f "$ROOT/operator/config/crd/"

log "Applying kubeops RBAC + namespaces"
# Clean legacy names from earlier installs
kubectl delete ns kube-dashboard-system kubernetes-dashboard kube-oidc --ignore-not-found=true --wait=false >/dev/null 2>&1 || true
kubectl delete clusterrolebinding console-operator oidc-operator --ignore-not-found=true >/dev/null 2>&1 || true
# roleRef is immutable — recreate kubeops bindings cleanly
kubectl delete clusterrolebinding kubeops-console kubeops-authentication --ignore-not-found=true >/dev/null 2>&1 || true
kubectl delete operators console oidc --ignore-not-found=true >/dev/null 2>&1 || true
kubectl apply -f "$ROOT/operator/config/rbac/role.yaml"

log "Applying ${CONSOLE_NAME} + ${AUTH_NAME} Deployments"
kubectl apply -f "$ROOT/operator/config/manager/operators.yaml"
kubectl -n "$CONSOLE_NS" set image "deploy/${CONSOLE_NAME}" manager="${CONSOLE_IMG}" >/dev/null
kubectl -n "$AUTH_NS" set image "deploy/${AUTH_NAME}" manager="${AUTH_IMG}" >/dev/null

log "Waiting for kubeops operator pods"
kubectl -n "$CONSOLE_NS" rollout status "deploy/${CONSOLE_NAME}" --timeout="${WAIT_TIMEOUT}s"
kubectl -n "$AUTH_NS" rollout status "deploy/${AUTH_NAME}" --timeout="${WAIT_TIMEOUT}s"

kubectl apply -f "$ROOT/operator/config/samples/operators.yaml"

# --- 5) ClusterOIDC first ---
if [[ "$INSTALL_OIDC_CR" == "true" ]]; then
  log "Applying ClusterOIDC (Dex + oauth2 client secrets)"
  kubectl apply -f "$OIDC_SAMPLE"

  log "Waiting for Dex + oauth2-proxy-oidc secret"
  deadline=$((SECONDS + WAIT_TIMEOUT))
  while (( SECONDS < deadline )); do
    dex_ready="$(kubectl -n "$AUTH_NS" get deploy dex -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)"
    [[ "$dex_ready" =~ ^[0-9]+$ ]] || dex_ready=0
    if (( dex_ready >= 1 )) && kubectl -n "$CONSOLE_NS" get secret oauth2-proxy-oidc >/dev/null 2>&1; then
      log "Dex Ready and oauth2 secret mirrored into ${CONSOLE_NS}"
      break
    fi
    sleep 3
  done
  if ! kubectl -n "$CONSOLE_NS" get secret oauth2-proxy-oidc >/dev/null 2>&1; then
    if kubectl -n "$AUTH_NS" get secret oauth2-proxy-oidc >/dev/null 2>&1; then
      warn "oauth2 secret in ${AUTH_NS} only; ${CONSOLE_NAME} will mirror/retry into ${CONSOLE_NS}"
    else
      warn "oauth2-proxy-oidc not ready — check: kubectl get clusteroidc; kubectl -n ${AUTH_NS} logs deploy/${AUTH_NAME}"
    fi
  fi
fi

# --- 6) Console CR ---
log "Applying KubeDashboard (auth.mode=${AUTH_MODE})"
kubectl apply -f "$CONSOLE_SAMPLE"

# --- 7) Wait for console Ready ---
log "Waiting for KubeDashboard Ready (up to ${WAIT_TIMEOUT}s)"
deadline=$((SECONDS + WAIT_TIMEOUT))
phase=""
auth=""
while (( SECONDS < deadline )); do
  phase="$(kubectl get kubedashboard kube-dashboard -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  auth="$(kubectl get kubedashboard kube-dashboard -o jsonpath='{.status.auth.mode}' 2>/dev/null || true)"
  if [[ "$phase" == "Ready" ]]; then
    if [[ "$AUTH_MODE" != "oidc" || "$auth" == "oidc" ]]; then
      break
    fi
  fi
  sleep 5
done
if [[ "$phase" != "Ready" ]]; then
  warn "KubeDashboard phase is '${phase:-unknown}' — check: kubectl get kubedashboard -o yaml"
  warn "Operator logs: kubectl -n ${CONSOLE_NS} logs deploy/${CONSOLE_NAME}"
else
  log "KubeDashboard is Ready (auth=${auth:-unknown})"
fi

if [[ "$AUTH_MODE" == "oidc" ]]; then
  if kubectl -n "$CONSOLE_NS" get cm kube-dashboard-nginx -o jsonpath='{.data.nginx\.conf}' 2>/dev/null | grep -q 'auth_request /oauth2/auth'; then
    log "NGINX OIDC auth_request gate is active"
  else
    warn "NGINX does not yet show auth_request — wait for rollout"
  fi
  kubectl -n "$CONSOLE_NS" rollout status deploy/kube-dashboard --timeout="${WAIT_TIMEOUT}s" || true
fi

URL="$(kubectl get kubedashboard kube-dashboard -o jsonpath='{.status.url}' 2>/dev/null || true)"
if [[ -z "$URL" && -n "$NODE_IP" ]]; then
  URL="https://${NODE_IP}:30443/"
fi

PASS_HINT="kubectl -n ${CONSOLE_NS} get secret kubeadmin-bootstrap -o jsonpath='{.data.password}' | base64 -d; echo"
if [[ "$AUTH_MODE" == "oidc" ]]; then
  PASS_HINT="kubectl -n ${AUTH_NS} get secret kubeadmin-oidc-bootstrap -o jsonpath='{.data.password}' | base64 -d; echo"
fi

cat <<EOF

────────────────────────────────────────────────────────
Installed (persistent kubeops wiring).
  kubectl get operators
  kubectl get kubedashboard,clusteroidc
  kubectl -n ${CONSOLE_NS} get pods
  kubectl -n ${AUTH_NS} get pods

Operators:
  ${CONSOLE_NAME}          role=console   ns=${CONSOLE_NS}
  ${AUTH_NAME}   role=oidc      ns=${AUTH_NS}

Console (browser):
  ${URL:-https://<node-ip>:30443/}
  Auth:   ${AUTH_MODE}
  User:   kubeadmin
  Pass:   ${PASS_HINT}

CLI: keep using your existing kubeconfig (cluster-admin).

Re-run anytime:
  ./scripts/install-operators.sh

Basic-auth only (no Dex):
  AUTH_MODE=secret ./scripts/install-operators.sh
────────────────────────────────────────────────────────
EOF
