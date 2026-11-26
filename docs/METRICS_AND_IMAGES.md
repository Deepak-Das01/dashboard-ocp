# Metrics Server and offline images

This packaging installs **metrics-server v0.7.2** together with Kube Dashboard so workload charts and `kubectl top` can work out of the box.

## What gets installed

| Component | Source in repo |
|-----------|----------------|
| Support RBAC + metrics-scraper | `deploy/upstream/dashboard-support-v2.7.0.yaml` |
| kube-dashboard console | `deploy/` (kustomize) |
| metrics-server | `deploy/metrics-server/components.yaml` (includes `--kubelet-insecure-tls` for lab kubelets) |

## Images (linux/amd64)

Archives are **not** stored in git (too large). They are published as GitHub Release assets under tag **`images-v1.2.2`**:

- `nginx-1.27-alpine-amd64.tar`
- `kubernetesui-dashboard-v2.7.0-amd64.tar`
- `kubernetesui-metrics-scraper-v1.0.8-amd64.tar`
- `metrics-server-v0.7.2-amd64.tar`

Checksums: `deploy/images/SHA256SUMS`

## Member install (self-contained flow)

```bash
git clone https://github.com/Deepak-Das01/kubeops-platform.git
cd kubeops-platform
git checkout main

./scripts/download-images.sh   # from GitHub Release images-v1.2.2
./scripts/load-images.sh       # import into containerd
./scripts/install.sh           # metrics-server + support + kube-dashboard
```

Skip image download if archives already exist in `artifacts/images/`:

```bash
DOWNLOAD_IF_MISSING=false ./scripts/load-images.sh
LOAD_IMAGES=false ./scripts/install.sh   # if images already on the node
```

Disable metrics-server:

```bash
INSTALL_METRICS_SERVER=false ./scripts/install.sh
```

## Lab note

`--kubelet-insecure-tls` is included for clusters that do not present trusted kubelet serving certificates. Prefer proper kubelet TLS in production and edit `deploy/metrics-server/components.yaml` accordingly.

## Maintainer: refresh image archives

```bash
./scripts/export-images.sh
# then publish artifacts/images/*.tar to GitHub Release tag images-v1.2.2
```
