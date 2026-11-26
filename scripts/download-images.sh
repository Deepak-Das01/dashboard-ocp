#!/usr/bin/env bash
# Download packaged image archives from the GitHub Release that hosts them.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMG_DIR="${IMG_DIR:-$ROOT/artifacts/images}"
IMAGE_RELEASE_TAG="${IMAGE_RELEASE_TAG:-images-v1.2.2}"
REPO="${REPO:-Deepak-Das01/kubeops-platform}"
BASE="https://github.com/${REPO}/releases/download/${IMAGE_RELEASE_TAG}"

FILES=(
  nginx-1.27-alpine-amd64.tar
  kubernetesui-dashboard-v2.7.0-amd64.tar
  kubernetesui-metrics-scraper-v1.0.8-amd64.tar
  metrics-server-v0.7.2-amd64.tar
)

mkdir -p "$IMG_DIR"
command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }

echo "==> Downloading image archives from ${BASE}"
for f in "${FILES[@]}"; do
  dest="$IMG_DIR/$f"
  if [[ -f "$dest" && "${FORCE_DOWNLOAD:-false}" != "true" ]]; then
    echo "    skip (exists): $f"
    continue
  fi
  echo "    get $f"
  curl -fL --retry 3 -o "$dest" "${BASE}/${f}"
done

if [[ -f "$ROOT/deploy/images/SHA256SUMS" ]]; then
  echo "==> Verifying checksums"
  (cd "$IMG_DIR" && sha256sum -c "$ROOT/deploy/images/SHA256SUMS")
fi

echo "Done. Archives in ${IMG_DIR}"
