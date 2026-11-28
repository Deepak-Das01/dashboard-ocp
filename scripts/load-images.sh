#!/usr/bin/env bash
# Load packaged container images into the node runtime store.
# Supports:
#   - CRI-O / Fedora CoreOS (podman load → containers/storage shared with cri-o)
#   - containerd (ctr -n k8s.io images import)
# Prefer local artifacts/images; optionally download from GitHub Releases first.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMG_DIR="${IMG_DIR:-$ROOT/artifacts/images}"
IMAGE_RELEASE_TAG="${IMAGE_RELEASE_TAG:-images-v1.2.2}"
REPO="${REPO:-Deepak-Das01/kubeops-platform}"
DOWNLOAD_IF_MISSING="${DOWNLOAD_IF_MISSING:-true}"

FILES=(
  nginx-1.27-alpine-amd64.tar
  kubernetesui-dashboard-v2.7.0-amd64.tar
  kubernetesui-metrics-scraper-v1.0.8-amd64.tar
  metrics-server-v0.7.2-amd64.tar
)

have_podman=false
have_ctr=false
command -v podman >/dev/null 2>&1 && have_podman=true
command -v ctr >/dev/null 2>&1 && have_ctr=true
if ! $have_podman && ! $have_ctr; then
  echo "ERROR: need podman (CRI-O/FCOS) or ctr (containerd) to import images" >&2
  exit 1
fi

import_tar() {
  local path="$1"
  if $have_podman; then
    podman load -i "$path"
    return
  fi
  ctr -n k8s.io images import "$path"
}

mkdir -p "$IMG_DIR"

need_download=false
for f in "${FILES[@]}"; do
  if [[ ! -f "$IMG_DIR/$f" ]]; then
    need_download=true
    break
  fi
done

if [[ "$need_download" == "true" && "$DOWNLOAD_IF_MISSING" == "true" ]]; then
  echo "==> Missing local image archives; downloading from GitHub Release ${IMAGE_RELEASE_TAG}"
  "$ROOT/scripts/download-images.sh"
fi

echo "==> Loading images from ${IMG_DIR} (runtime: $( $have_podman && echo podman/cri-o || echo containerd ))"
for f in "${FILES[@]}"; do
  path="$IMG_DIR/$f"
  if [[ ! -f "$path" ]]; then
    echo "Missing image archive: $path" >&2
    echo "Run: ./scripts/download-images.sh   or place amd64 *.tar files in artifacts/images/" >&2
    exit 1
  fi
  echo "    import $f"
  import_tar "$path"
done

# Ensure FQ tags exist for CRI-O short-name enforcement (retag when only short/localhost present).
if $have_podman; then
  podman tag docker.io/library/nginx:1.27-alpine nginx:1.27-alpine 2>/dev/null || true
  podman tag docker.io/kubernetesui/dashboard:v2.7.0 kubernetesui/dashboard:v2.7.0 2>/dev/null || true
  podman tag docker.io/kubernetesui/metrics-scraper:v1.0.8 kubernetesui/metrics-scraper:v1.0.8 2>/dev/null || true
  # Prefer FQ as primary names for inspect
  podman tag nginx:1.27-alpine docker.io/library/nginx:1.27-alpine 2>/dev/null || true
  podman tag kubernetesui/dashboard:v2.7.0 docker.io/kubernetesui/dashboard:v2.7.0 2>/dev/null || true
  podman tag kubernetesui/metrics-scraper:v1.0.8 docker.io/kubernetesui/metrics-scraper:v1.0.8 2>/dev/null || true
fi

echo "==> Image load complete"
if $have_podman; then
  podman images | awk 'NR==1 || /nginx|kubernetesui|metrics-server/'
elif $have_ctr; then
  ctr -n k8s.io images ls | awk 'NR==1 || /nginx|kubernetesui|metrics-server/'
fi
