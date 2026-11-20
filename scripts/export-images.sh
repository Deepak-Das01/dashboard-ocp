#!/usr/bin/env bash
# Maintainer helper: export required images from local containerd as amd64 tar archives.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMG_DIR="${IMG_DIR:-$ROOT/artifacts/images}"
PLATFORM="${PLATFORM:-linux/amd64}"

command -v ctr >/dev/null || { echo "ctr is required" >&2; exit 1; }
mkdir -p "$IMG_DIR"

export_one() {
  local ref="$1" file="$2"
  echo "==> pull/export ${ref}"
  ctr -n k8s.io images pull --platform "$PLATFORM" "$ref"
  ctr -n k8s.io images export --platform "$PLATFORM" "$IMG_DIR/$file" "$ref"
  ls -lh "$IMG_DIR/$file"
}

export_one docker.io/library/nginx:1.27-alpine nginx-1.27-alpine-amd64.tar
export_one docker.io/kubernetesui/dashboard:v2.7.0 kubernetesui-dashboard-v2.7.0-amd64.tar
export_one docker.io/kubernetesui/metrics-scraper:v1.0.8 kubernetesui-metrics-scraper-v1.0.8-amd64.tar
export_one registry.k8s.io/metrics-server/metrics-server:v0.7.2 metrics-server-v0.7.2-amd64.tar

(cd "$IMG_DIR" && sha256sum *.tar > "$ROOT/deploy/images/SHA256SUMS")
echo "==> Wrote checksums to deploy/images/SHA256SUMS"
cat "$ROOT/deploy/images/SHA256SUMS"
