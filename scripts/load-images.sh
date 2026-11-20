#!/usr/bin/env bash
# Load packaged container images into containerd (k8s.io namespace).
# Prefer local artifacts/images; optionally download from GitHub Releases first.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMG_DIR="${IMG_DIR:-$ROOT/artifacts/images}"
IMAGE_RELEASE_TAG="${IMAGE_RELEASE_TAG:-images-v1.2.2}"
REPO="${REPO:-Deepak-Das01/dashboard-ocp}"
DOWNLOAD_IF_MISSING="${DOWNLOAD_IF_MISSING:-true}"

FILES=(
  nginx-1.27-alpine-amd64.tar
  kubernetesui-dashboard-v2.7.0-amd64.tar
  kubernetesui-metrics-scraper-v1.0.8-amd64.tar
  metrics-server-v0.7.2-amd64.tar
)

command -v ctr >/dev/null || { echo "ctr (containerd) is required to load images" >&2; exit 1; }

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

echo "==> Loading images from ${IMG_DIR}"
for f in "${FILES[@]}"; do
  path="$IMG_DIR/$f"
  if [[ ! -f "$path" ]]; then
    echo "Missing image archive: $path" >&2
    echo "Run: ./scripts/download-images.sh   or place amd64 *.tar files in artifacts/images/" >&2
    exit 1
  fi
  echo "    import $f"
  ctr -n k8s.io images import "$path"
done

echo "==> Image load complete"
ctr -n k8s.io images ls | awk 'NR==1 || /nginx:1.27-alpine|kubernetesui\/dashboard:v2.7.0|kubernetesui\/metrics-scraper:v1.0.8|metrics-server:v0.7.2/'
