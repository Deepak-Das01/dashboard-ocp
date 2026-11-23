# Kube Dashboard

Themed Kubernetes console based on Dashboard v2.7.0, managed by **`kubeops-*` operators**, with Dex OIDC login by default.

[![Release](https://img.shields.io/badge/release-v1.3.0-0B5FFF?style=flat-square)](https://github.com/Deepak-Das01/dashboard-ocp/releases/tag/v1.3.0)
[![Upstream Dashboard](https://img.shields.io/badge/Kubernetes%20Dashboard-v2.7.0-326CE5?style=flat-square&logo=kubernetes&logoColor=white)](https://github.com/kubernetes/dashboard/releases/tag/v2.7.0)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-1.34%20lab--tested-326CE5?style=flat-square&logo=kubernetes&logoColor=white)](#prerequisites)
[![NGINX](https://img.shields.io/badge/NGINX-1.27--alpine-009639?style=flat-square&logo=nginx&logoColor=white)](deploy/kube-dashboard/deployment.yaml)
[![Theme](https://img.shields.io/badge/UI-kube--dashboard-7DD3C0?style=flat-square)](deploy/kube-dashboard/theme.css)
[![Contributions](https://img.shields.io/badge/contributions-welcome-2EA44F?style=flat-square)](#contributing)
[![License](https://img.shields.io/badge/license-see%20acknowledgements-lightgrey?style=flat-square)](#license-and-acknowledgements)

> **Independent project.** kube-dashboard is a community packaging and cosmetic customization of the official [Kubernetes Dashboard](https://github.com/kubernetes/dashboard). It is **not** an official Kubernetes, CNCF, or Apple product.

| Item | Value |
|------|--------|
| Product | Kube Dashboard |
| Release | **v1.3.0** (stable packaging) + **kubeops operators** on `main` |
| Version | **1.3.0** |
| Upstream Dashboard | `kubernetesui/dashboard:v2.7.0` |
| Metrics scraper | `kubernetesui/metrics-scraper:v1.0.8` |
| Console image (proxy) | `nginx:1.27-alpine` |
| Auth (default) | **OIDC via Dex** (`kubeadmin`) |
| Default HTTPS NodePort | **30443** |
| Default HTTP NodePort | **30080** (redirects to HTTPS) |
| Dex NodePort | **32056** |
| Virtualization UI NodePort | **30444** (kubevirt-manager; also embedded in console) |
| Monitoring Grafana NodePort | **30445** (also embedded in console at `/kubeops/mon-ui/`) |
| Prometheus NodePort | **30446** |
| Alertmanager NodePort | **30447** |
| Ceph dashboard NodePort | **30448** (also embedded in console at `/kubeops/ceph-ui/`) |
| MetalLB | L2 LoadBalancer IPs via Addons → MetalLB (`metallb-system`, vendored **v0.16.2**) |
| Namespaces | `kubeops-console`, `kubeops-authentication`, `kubeops-addon-controller`, `kubeops-virtualization`, `kubeops-nfs`, `kubeops-monitoring`, `kubeops-ceph-cluster`, `metallb-system` |
| Repository | [github.com/Deepak-Das01/dashboard-ocp](https://github.com/Deepak-Das01/dashboard-ocp) |

---

## Quick start (recommended)

Requires: `kubectl`, `go`, `python3`, `ctr` (containerd), cluster-admin kubeconfig.

```bash
git clone https://github.com/Deepak-Das01/dashboard-ocp.git
cd dashboard-ocp
git checkout main

# Interactive (progress bar + status, dated log, kubeadmin-password file):
./scripts/kubeops-installer.sh
# or: ./scripts/install-operators.sh --interactive

# Non-interactive:
./scripts/install-operators.sh
```

```bash
kubectl get ops
kubectl get kubedashboard,clusteroidc,nfsprovisioner,virtualization,monitoring,rookceph,metallb
kubectl get pods -n kubeops-console
kubectl get pods -n kubeops-authentication
kubectl get pods -n kubeops-addon-controller
kubectl get pods -n kubeops-monitoring   # after Addons → Monitoring Deploy
kubectl get pods -n kubeops-ceph-cluster # after Addons → Ceph Deploy
kubectl get pods -n metallb-system       # after Addons → MetalLB Deploy
```

**Browser:** `https://<node-ip>:30443/` → Dex → sign in as `kubeadmin`

```bash
kubectl -n kubeops-authentication get secret kubeadmin-oidc-bootstrap \
  -o jsonpath='{.data.password}' | base64 -d; echo
# Interactive installer also writes: ./kubeops-installer/kubeadmin-password
```

Use a private window if old cookies interfere. CLI stays on your existing kubeconfig.

| Flag | Default | Meaning |
|------|---------|---------|
| `AUTH_MODE` | `oidc` | `oidc` \| `secret` \| `token` |
| `LOAD_IMAGES` | `true` | Load dashboard/metrics image tars |
| `LOAD_IMAGES=false` | — | Skip image download/import if already present |

```bash
LOAD_IMAGES=false ./scripts/install-operators.sh
AUTH_MODE=secret ./scripts/install-operators.sh   # NGINX basic auth instead of Dex
./scripts/kubeops-installer.sh --yes --no-load-images   # non-interactive UI wrapper
```

Installer output (interactive):

| Path | Contents |
|------|----------|
| `./kubeops-installer/<YYYY-MM-DD-HHMMSS>-log` | Full install transcript |
| `./kubeops-installer/kubeadmin-password` | Username, password, console URL |

Full operator docs: [docs/OPERATOR_AND_AUTH.md](docs/OPERATOR_AND_AUTH.md).

---

## Operators (`kubeops-*`)

Core operators use **name = namespace**. Addon controllers share `kubeops-addon-controller`.

| Operator / controller | Namespace | Role (`spec.component`) | Owns |
|-----------------------|-----------|-------------------------|------|
| **kubeops-console** | `kubeops-console` | `console` | Operator + `kube-dashboard` UI + metrics-scraper |
| **kubeops-authentication** | `kubeops-authentication` | `oidc` | Operator + Dex IdP + OIDC secrets |
| **kubeops-nfs-controller** | `kubeops-addon-controller` | `nfs` | `NFSProvisioner` CR → NFS provisioner + StorageClass |
| **kubeops-virtualization-controller** | `kubeops-addon-controller` | `virtualization` | `Virtualization` CR → KubeVirt + CDI + kubevirt-manager |
| **kubeops-monitoring-controller** | `kubeops-addon-controller` | `monitoring` | `Monitoring` CR → Prometheus + Grafana + Alertmanager + exporters |
| **kubeops-ceph-controller** | `kubeops-addon-controller` | `ceph` | `RookCeph` CR → Rook + CephCluster + RBD/CephFS StorageClasses |
| **kubeops-metallb-controller** | `kubeops-addon-controller` | `metallb` | `MetalLB` CR → MetalLB L2 + `IPAddressPool` + `L2Advertisement` |

```text
kubeops-console/
  kubeops-console          # operator
  kube-dashboard           # NGINX + Dashboard + oauth2-proxy
  dashboard-metrics-scraper

kubeops-authentication/
  kubeops-authentication   # operator
  dex                      # OIDC login (NodePort 32056)

kubeops-addon-controller/
  kubeops-nfs-controller
  kubeops-virtualization-controller
  kubeops-monitoring-controller
  kubeops-ceph-controller
  kubeops-metallb-controller

kubeops-nfs/                 # default NFSProvisioner workload ns
kubeops-virtualization/      # KubeVirt + CDI + kubevirt-manager UI
kubeops-monitoring/          # Prometheus + Grafana + Alertmanager (+ exporters)
kubeops-ceph-cluster/        # Rook + CephCluster + toolbox + CSI
metallb-system/              # MetalLB controller + speaker + IP pool (after Deploy)
```

```bash
kubectl get ops
# NAME                         COMPONENT        PHASE
# kubeops-console              console          Ready
# kubeops-authentication       oidc             Ready
# kubeops-nfs                  nfs              Ready
# kubeops-virtualization       virtualization   Ready
# kubeops-monitoring           monitoring       Ready
# kubeops-ceph                 ceph             Unavailable  # until Addons Deploy
# kubeops-metallb              metallb          Unavailable  # until Addons Deploy
```

Install/uninstall **addon workloads** from the console **Addons** page (creates/deletes CRs). Controllers stay running. Details: [docs/ADDON_CONTROLLERS.md](docs/ADDON_CONTROLLERS.md).

Hardware sizing per operator/addon: [Prerequisites → Hardware requirements](#hardware-requirements-operator--addon).

---

## Table of contents

- [Quick start (recommended)](#quick-start-recommended)
- [Operators (kubeops-)](#operators-kubeops-)
- [Product overview](#product-overview)
- [Screenshots](#screenshots)
- [Features](#features)
- [Technology stack](#technology-stack)
- [Architecture](#architecture)
- [Prerequisites](#prerequisites)
  - [Hardware requirements (operator / addon)](#hardware-requirements-operator--addon)
- [Classic install (scripts/install.sh)](#classic-install-scriptsinstallsh)
- [Detailed manual installation](#detailed-manual-installation)
- [Accessing the dashboard](#accessing-the-dashboard)
- [Authentication and authorization](#authentication-and-authorization)
- [Deployment verification](#deployment-verification)
- [Theme customization](#theme-customization)
- [Configuration reference](#configuration-reference)
- [Troubleshooting](#troubleshooting)
- [Upgrading](#upgrading)
- [Rollback](#rollback)
- [Uninstallation](#uninstallation)
- [Repository structure](#repository-structure)
- [Security and known limitations](#security-and-known-limitations)
- [Roadmap](#roadmap)
- [Releases and changelog](#releases-and-changelog)
- [Contributing](#contributing)
- [License and acknowledgements](#license-and-acknowledgements)

---

## Product overview

**Kube Dashboard** delivers a refined visual theme for the official Kubernetes Dashboard **without rebuilding** the upstream Angular application.

The Dashboard binary runs as a **sidecar** in the `kube-dashboard` pod. NGINX:

1. Terminates TLS for a convenient NodePort HTTPS endpoint.
2. Proxies to the Dashboard sidecar on loopback (`127.0.0.1:9090`).
3. Injects `theme.css`, `nav-accordion.js`, and Operators/Addons/Virtualization/Monitoring/Ceph scripts into HTML responses.
4. Proxies `/kubeops/virt-ui/` to kubevirt-manager and injects `virt-theme.css`.
5. Proxies `/kubeops/mon-ui/` to Grafana and injects `mon-theme.css` (kubeops palette, readable panels).
6. Proxies `/kubeops/ceph-ui/` to the Rook mgr dashboard and injects `ceph-theme.css`.

### Why this project exists

Operators often want a clearer, more modern cluster UI while keeping familiar Dashboard workflows. This packaging focuses on **cosmetic modernization** and a single-pod install, while preserving upstream routes, APIs, authentication, and RBAC behaviour.

### What you get

- A sapphire / charcoal / teal ambient visual identity.
- Translucent panels, reflective borders, and layered depth.
- Polished header, namespace selector, tables, forms, and overlays.
- The same workload, networking, storage, and RBAC management surfaces provided by Dashboard v2.7.0 (subject to your cluster permissions and services).
- In-console **Operators** and **Addons** pages for kubeops status and one-click addon install.
- **Virtualization** sidebar view embedding kubevirt-manager (KubeVirt + CDI) with the same glass theme.
- **Monitoring** sidebar view embedding Grafana (Prometheus stack) with a matching dark palette and clear charts.

### What this is not

- Not a fork that recompiles the Dashboard frontend for day-to-day installs.
- Not a security modernization of Dashboard v2.7.0.
- Not production-certified or comprehensively browser-validated.

---

## Screenshots

Screenshots of the live **v1.3.0** Kube Dashboard interface should live under `docs/screenshots/` and be linked with relative Markdown paths.

> **Documentation note:** PNG assets are not yet committed in this repository. Capture them from a running v1.3.0 deployment (hard-refresh the browser first) and save the following filenames before linking them in this section:

| Suggested file | Subject |
|----------------|---------|
| `docs/screenshots/workloads-overview.png` | Workloads overview / status charts |
| `docs/screenshots/pods.png` | Pods resource table |
| `docs/screenshots/deployments.png` | Deployments |
| `docs/screenshots/namespaces.png` | Namespaces |
| `docs/screenshots/resource-table.png` | Generic resource table + pagination |
| `docs/screenshots/operators.png` | In-console Operators status cards |
| `docs/screenshots/addons.png` | Addons page (NFS + Virtualization + Monitoring + Ceph + MetalLB tiles) |
| `docs/screenshots/virtualization.png` | Embedded kubevirt-manager (glass theme) |
| `docs/screenshots/monitoring.png` | Embedded Grafana enterprise observability dashboard |
| `docs/screenshots/settings.png` | Settings |
| `docs/screenshots/yaml-editor.png` | Create from input / YAML editor |
| `docs/screenshots/create-form.png` | Create from form |
| `docs/screenshots/namespace-dropdown.png` | Namespace selector dropdown |
| `docs/screenshots/notifications.png` | Notification panel |
| `docs/screenshots/login.png` | Login page |

Do **not** include authentication tokens, kubeconfig material, or private cluster identifiers in published screenshots.

<!-- Example (enable after assets exist):
![Workloads overview](docs/screenshots/workloads-overview.png)
-->

---

## Features

### Visual enhancements

Cosmetic improvements delivered by `deploy/kube-dashboard/theme.css`:

- Premium macOS-inspired Kube Dashboard interface
- Sapphire-blue, charcoal, and teal ambient gradients
- Translucent glass surfaces with backdrop blur where supported
- Reflective borders and layered visual depth
- Frosted dropdown menus and elevated overlays
- Polished navigation sidebar with consistent active selection
- Improved namespace selector (closed control and open search surface)
- Refined search and header controls
- Consistent resource tables and glass-styled pagination footers
- Corrected notification overlay stacking and compact empty state
- Refined settings, floating labels, and form alignment
- Coordinated YAML/JSON editor appearance
- Emerald workload status indicators
- Smooth hover and focus transitions
- `prefers-reduced-motion` support
- Improved Events table text containment (in-cell wrapping; no table layout changes)

### In-console Operators, Addons, Virtualization, Monitoring, Ceph, and MetalLB

Delivered by kubeops operators + injected UI assets (`operators.js`, `addons.js`, `virtualization.js`, `monitoring.js`, `ceph.js`, `virt-theme.css`, `mon-theme.css`, `ceph-theme.css`):

- **Operators** nav item — live status cards for console, authentication, NFS, virtualization, monitoring, Ceph, and MetalLB
- **Addons** page — deploy/uninstall `NFSProvisioner`, `Virtualization`, `Monitoring`, `RookCeph`, and `MetalLB` CRs without leaving the console
- **Virtualization** nav item (before CRDs) — same-origin iframe of kubevirt-manager at `/kubeops/virt-ui/`
- **Monitoring** nav item — same-origin iframe of Grafana at `/kubeops/mon-ui/` (default home dashboard: Kubernetes enterprise observability)
- **Ceph** nav item — same-origin iframe of the Rook mgr dashboard at `/kubeops/ceph-ui/` (NodePort **30448**)
- **MetalLB** (Addons only) — L2 LoadBalancer IP pool; no separate sidebar embed (use Services with `type: LoadBalancer`)
- Glass theme overlay for kubevirt-manager; opaque kubeops-palette theme for Grafana so graphs stay readable
- Console OIDC already gates access — Grafana runs anonymous + auth-proxy in the embed (no Grafana login)
- Brand/nav leave-guards so embeds do not nest the main dashboard or stick on leave

### Existing Kubernetes management capabilities

Inherited from **upstream Kubernetes Dashboard v2.7.0** (not introduced by the theme). Availability depends on Kubernetes RBAC and cluster services:

- Workload overview and status charts
- Pods, Deployments, StatefulSets, DaemonSets, ReplicaSets, Replication Controllers
- Jobs and CronJobs
- Services, Ingresses, and Ingress Classes
- ConfigMaps and Secrets
- PersistentVolumes, PersistentVolumeClaims, and StorageClasses
- Namespaces, Nodes, and Events
- NetworkPolicies
- ServiceAccounts, Roles, RoleBindings, ClusterRoles, ClusterRoleBindings
- Custom Resource Definitions
- Resource filtering and search
- YAML / JSON create and edit flows
- Pod logs and terminal access (when permitted)
- Metrics visualization when the metrics scraper and cluster metrics stack are available

Addon controllers (`NFSProvisioner`, `Virtualization`, `Monitoring`) are **additional** kubeops APIs beyond upstream Dashboard.

---

## Technology stack

| Component | Technology |
|-----------|------------|
| Base application | Kubernetes Dashboard **v2.7.0** (`kubernetesui/dashboard:v2.7.0`) |
| UI framework | Upstream Angular + Angular Material (bundled in Dashboard image) |
| Custom theme | CSS (`theme.css`, `virt-theme.css`, `mon-theme.css`) |
| Reverse proxy | NGINX **1.27-alpine** (`nginx:1.27-alpine`) |
| Deployment | kubeops operators (`install-operators.sh`) + classic Kustomize (`deploy/`) |
| Configuration | ConfigMaps `kube-dashboard-ui`, `kube-dashboard-nginx` |
| Metrics | Dashboard metrics scraper `kubernetesui/metrics-scraper:v1.0.8` |
| Authentication | OIDC (Dex + oauth2-proxy) by default; secret/token modes available |
| Authorization | Kubernetes RBAC |
| TLS for console | Secret `kube-dashboard-tls` (self-signed via `scripts/create-tls-secret.sh`) |
| Virtualization | KubeVirt **1.10.0** + CDI **1.66.1** + kubevirt-manager **1.5.4** |
| NFS addon | Dynamic NFS provisioner via `NFSProvisioner` CR |
| Monitoring addon | Prometheus **2.54** + Grafana **11.2** + Alertmanager **0.27** + kube-state-metrics + node-exporter |

---

## Architecture

### Operator path (default)

```text
Browser
  │
  ├─ http://<node>:30080  ──301──► https://<node>:30443/
  └─ https://<node>:30443/
           │
           ▼  auth_request (unauthenticated → /oauth2/start)
     kube-dashboard (kubeops-console ns)
     ┌─────────────────────────────────────┐
     │ NGINX → oauth2-proxy → Dex (:32056) │
     │ On success: inject SA bearer        │
     │ → Dashboard sidecar :9090           │
     └─────────────────────────────────────┘
           │
           ▼
     Kubernetes API
```

| Piece | Namespace | Job |
|-------|-----------|-----|
| `kubeops-console` | `kubeops-console` | Reconciles `KubeDashboard` |
| `kube-dashboard` | `kubeops-console` | UI (NGINX + Dashboard + oauth2-proxy) |
| `kubeops-authentication` | `kubeops-authentication` | Reconciles `ClusterOIDC` |
| `dex` | `kubeops-authentication` | OIDC IdP / login form |
| `kubeops-nfs-controller` | `kubeops-addon-controller` | Reconciles `NFSProvisioner` |
| `kubeops-virtualization-controller` | `kubeops-addon-controller` | Reconciles `Virtualization` |
| `kubeops-monitoring-controller` | `kubeops-addon-controller` | Reconciles `Monitoring` |
| `kubeops-ceph-controller` | `kubeops-addon-controller` | Reconciles `RookCeph` |
| `kubeops-metallb-controller` | `kubeops-addon-controller` | Reconciles `MetalLB` |
| KubeVirt + CDI + kubevirt-manager | `kubeops-virtualization` | VM stack + UI (NodePort **30444**) |
| Prometheus + Grafana + Alertmanager | `kubeops-monitoring` | Observability stack (NodePorts **30445–30447**) |
| Rook + CephCluster + RBD/CephFS | `kubeops-ceph-cluster` | Storage (dashboard NodePort **30448**) |
| MetalLB controller + speaker + pool | `metallb-system` | L2 LoadBalancer IPs (after MetalLB Deploy) |

NGINX also proxies `/kubeops/virt-ui/` → kubevirt-manager (`virt-theme.css`), `/kubeops/mon-ui/` → Grafana (`mon-theme.css`), and `/kubeops/ceph-ui/` → Rook mgr dashboard (`ceph-theme.css`) so the sidebar embeds match the console palette. Addons APIs include `/kubeops/api/metallbs`.

### Mermaid diagram

```mermaid
flowchart LR
  Browser -->|HTTPS :30443| NGINX["NGINX + oauth2-proxy"]
  NGINX -->|login| Dex["Dex :32056"]
  NGINX --> Dash["Dashboard sidecar"]
  NGINX -->|/kubeops/virt-ui/| KVM["kubevirt-manager"]
  NGINX -->|/kubeops/mon-ui/| Graf["Grafana"]
  NGINX -->|/kubeops/ceph-ui/| CephUI["Rook mgr dashboard"]
  Dash --> API["Kubernetes API"]
  OpC["kubeops-console"] -.->|owns| NGINX
  OpA["kubeops-authentication"] -.->|owns| Dex
  OpN["kubeops-nfs-controller"] -.->|NFSProvisioner| NFS["kubeops-nfs"]
  OpV["kubeops-virtualization-controller"] -.->|Virtualization| Virt["kubeops-virtualization"]
  OpM["kubeops-monitoring-controller"] -.->|Monitoring| Mon["kubeops-monitoring"]
  OpCeph["kubeops-ceph-controller"] -.->|RookCeph| CephNS["kubeops-ceph-cluster"]
  OpMLB["kubeops-metallb-controller"] -.->|MetalLB| MLB["metallb-system"]
  Virt --> KVM
  Mon --> Graf
```

---

## Prerequisites

### Mandatory (software / cluster)

- A working Kubernetes cluster with **containerd** (lab uses `containerd://2.x`)
- `kubectl` configured for the target context (cluster-admin for install)
- For `./scripts/install-operators.sh`: `go`, `python3`, `ctr` (containerd CLI)
- Permission to create namespaces, CRDs, ClusterRoles, Deployments, and NodePorts
- Network access to pull (or locally preload) console images:
  - `kubernetesui/dashboard:v2.7.0`
  - `kubernetesui/metrics-scraper:v1.0.8`
  - `nginx:1.27-alpine`
  - Plus Dex / oauth2-proxy and any **addon** images you Deploy from Addons
- `openssl` (TLS secret helpers), `bash`, `git`, and a modern browser
- Swap disabled on nodes (kubeadm requirement)

### Hardware requirements (operator / addon)

Figures below are **practical lab / UAT guidance** (not hard product limits). Controllers themselves are small; **workloads** created by Addons → Deploy dominate RAM, CPU, and disk.

#### Node profiles (whole cluster)

| Profile | What you run | vCPU | RAM | OS disk | Extra disks / notes |
|---------|--------------|------|-----|---------|---------------------|
| **Core only** | `kubeops-console` + `kubeops-authentication` + addon **controllers** (no Addons Deploy yet) | 2–4 | **8 GB** | 40+ GB | NodePort **30443** reachable |
| **Core + NFS + Monitoring** | Above + NFS provisioner + Prometheus/Grafana stack | 4–8 | **16 GB** | 80+ GB | Prefer a StorageClass for Prometheus PVC; else emptyDir (ephemeral) |
| **Full lab (recommended UAT)** | Core + Monitoring + **Ceph** (+ optional Virt) | **8** | **16–24 GB** | **80–120 GB** | **≥3 raw disks** for Ceph OSDs (lab uses 3× ~50 GB); Virt needs KVM or emulation |
| **Virt-heavy** | Full lab + several VMs | 8+ | **32 GB** | 100+ GB | Nested virt (`/dev/kvm`) strongly preferred |

**Lab reference node:** Ubuntu 24.04, **8 vCPU / ~23 GB RAM / 120 GB OS + 3×50 GB Ceph disks**, Kubernetes **v1.34.13** — console + Ceph + monitoring fit comfortably (~6 GB RAM used at idle).

#### Per operator / controller

| Operator / controller | Namespace | Idle footprint (approx.) | Notes |
|-----------------------|-----------|--------------------------|-------|
| **kubeops-console** | `kubeops-console` | ~256–512 MiB RAM, low CPU | Operator + `kube-dashboard` (NGINX + Dashboard + oauth2-proxy) + metrics-scraper. Always required for the UI. |
| **kubeops-authentication** | `kubeops-authentication` | ~128–256 MiB RAM | Operator + Dex. Required for default OIDC (`kubeadmin`). |
| **kubeops-nfs-controller** | `kubeops-addon-controller` | Controller ~64–128 MiB | Controller only until Addons → NFS Deploy. |
| **kubeops-virtualization-controller** | `kubeops-addon-controller` | Controller ~64–128 MiB | Controller only until Addons → Virtualization Deploy. |
| **kubeops-monitoring-controller** | `kubeops-addon-controller` | Controller ~64–128 MiB | Controller only until Addons → Monitoring Deploy. |
| **kubeops-ceph-controller** | `kubeops-addon-controller` | Controller ~64–128 MiB | Controller only until Addons → Ceph Deploy. Disk probe API is lightweight. |
| **kubeops-metallb-controller** | `kubeops-addon-controller` | Controller ~64–128 MiB | Controller only until Addons → MetalLB Deploy. |

Shared addon-controller namespace holds all five controllers; budget **~0.5–1 GB RAM** for controllers together.

#### Per addon workload (after Addons → Deploy)

| Addon CR | Workload namespace | Minimum practical HW | Recommended | Special requirements |
|----------|-------------------|----------------------|-------------|----------------------|
| **NFSProvisioner** (`nfs`) | `kubeops-nfs` | +0.5–1 GB RAM, small PVC/hostPath for export | 1 GB RAM | Host path or volume for NFS export data; creates StorageClass `kubeops-nfs-storage` (used by Monitoring when present). |
| **Monitoring** (`monitoring`) | `kubeops-monitoring` | +2–4 GB RAM, +10–20 GB disk for TSDB | **4 GB RAM**, PVC on NFS/Ceph SC | Prometheus **30446**, Grafana **30445**, Alertmanager **30447**. Without a StorageClass, metrics use emptyDir (lost on restart). |
| **Virtualization** (`virtualization`) | `kubeops-virtualization` | +2–4 GB RAM idle (KubeVirt + CDI + UI) | **8+ GB** free for guest VMs | KubeVirt **v1.10** + CDI + kubevirt-manager (**30444**). Prefer `/dev/kvm`; set `spec.useEmulation: true` only for labs without KVM (slower). First Deploy pulls many images. |
| **RookCeph** (`ceph`) | `kubeops-ceph-cluster` | +4–8 GB RAM; **≥3 unused disks** (SNO lab) | **8 GB RAM**, 3× 20–50 GB+ raw disks | Rook **v1.16.7** / Ceph **v19.2.3**. Disks must **not** be the OS disk (`Discover disks` filters boot/nbd/loop). Multi-node: dedicated storage nodes, mon≥3, pool size 3. Dashboard **30448** / embed `/kubeops/ceph-ui/`. Uninstall wipe needs `kubeops.io/wipe-disks=confirmed`. |
| **MetalLB** (`metallb`) | `metallb-system` | +128–256 MiB RAM | Free LAN range for L2 | Vendored MetalLB **v0.16.2** (L2 only). Form fields: address pool (CIDR/range), pool name. Pick IPs that do **not** overlap node or DHCP addresses. `Service type: LoadBalancer` gets an EXTERNAL-IP from the pool. No BGP in v1. |

#### Combined UAT checklist (2 machines)

Use the same **Kubernetes patch** you intend to validate (lab: **v1.34.13**). Example split:

| Machine | OS | Suggested role | HW |
|---------|-----|----------------|-----|
| Ubuntu | 22.04 / 24.04 | Match lab / full stack (console + Ceph + monitoring) | 8 vCPU, 16–24 GB RAM, 120 GB OS, **3× raw disks** |
| RHEL | 8 / 9 | Core + monitoring (or second k8s minor) | 4–8 vCPU, 16 GB RAM, 80+ GB OS |

Both: containerd, swap off, firewall allows NodePorts above (at least **30443** for console).

### Network (NodePorts)

| Port | Service |
|------|---------|
| **30443** | Console HTTPS (primary) |
| **30080** | Console HTTP → HTTPS redirect |
| **32056** | Dex OIDC |
| **30444** | kubevirt-manager (after Virtualization Deploy) |
| **30445–30447** | Grafana / Prometheus / Alertmanager (after Monitoring Deploy) |
| **30448** | Ceph mgr dashboard (after Ceph Deploy) |

### Optional

- NodePort reachability from your workstation (or use port-forward)
- Cluster metrics-server if you want populated upstream Dashboard resource charts
- A custom TLS certificate to replace the generated self-signed secret
- Extra Kubernetes minors for multi-version UAT (**1.35.x / 1.36.x**); keep one cluster matching lab **1.34.13**

### Lab-tested Kubernetes version

This packaging was exercised in a lab on **Kubernetes v1.34.13** (Ubuntu 24.04). Compatibility with every distribution or version is **not** claimed.

### Pre-flight checks

```bash
kubectl config current-context
kubectl cluster-info
kubectl get nodes -o wide
# RAM / disk sanity on each node (example)
free -h
lsblk -d -o NAME,SIZE,TYPE,MOUNTPOINT
# Confirm unused disks before Ceph Deploy (console Addons → Discover disks)
```

---

## Classic install (`scripts/install.sh`)

Script/kustomize path **without** operators (namespace `kubernetes-dashboard`, token login). Prefer [Quick start](#quick-start-recommended) for the kubeops + OIDC path.

```bash
kubectl config current-context
./scripts/install.sh

kubectl -n kubernetes-dashboard get svc kube-dashboard
./scripts/get-token.sh
# Open https://<NODE_IP>:30443/
```

`install.sh` downloads image archives from GitHub Release **`images-v1.2.2`** when `artifacts/images/*.tar` are missing. See [docs/METRICS_AND_IMAGES.md](docs/METRICS_AND_IMAGES.md).

> [!WARNING]
> Dashboard runs with `--enable-insecure-login` on loopback only; metrics-server includes `--kubelet-insecure-tls` for lab convenience. Prefer proper TLS and review both for shared/production environments.

---

## Detailed manual installation

Use this when you want explicit control over each step. Namespace: **`kubernetes-dashboard`**.

### 0. Images (optional if already on the node)

```bash
./scripts/download-images.sh   # GitHub Release images-v1.2.2
./scripts/load-images.sh
```

### 1. metrics-server + Dashboard support resources (vendored)

```bash
kubectl apply -f deploy/metrics-server/components.yaml
kubectl -n kube-system rollout status deploy/metrics-server --timeout=180s

# RBAC, secrets, metrics-scraper only — no separate kubernetes-dashboard Deployment
kubectl apply -f deploy/upstream/dashboard-support-v2.7.0.yaml
kubectl -n kubernetes-dashboard rollout status deploy/dashboard-metrics-scraper --timeout=180s
```

### 2. TLS secret for kube-dashboard (namespace-scoped)

```bash
./scripts/create-tls-secret.sh
# Creates/updates Secret kubernetes-dashboard/kube-dashboard-tls
```

Optional SAN override:

```bash
EXTRA_SAN="IP:10.0.0.10,DNS:dashboard.example.com" ./scripts/create-tls-secret.sh
```

### 3. kube-dashboard + optional login ServiceAccount (Kustomize)

```bash
kubectl apply -k deploy/
kubectl -n kubernetes-dashboard rollout status deploy/kube-dashboard --timeout=120s
```

This applies:

| Resource | Scope | Purpose |
|----------|-------|---------|
| ConfigMap `kube-dashboard-ui` | Namespace | Theme CSS + nav accordion JS |
| ConfigMap `kube-dashboard-nginx` | Namespace | NGINX config |
| Deployment/Service `kube-dashboard` | Namespace | Console (NGINX + Dashboard sidecar) |
| ServiceAccount `dashboard-admin` | Namespace | Optional token login helper |
| Secret `dashboard-admin-token` | Namespace | SA token secret (compatibility) |
| ClusterRoleBinding `dashboard-admin` | Cluster | Binds SA to **`cluster-admin`** |

> [!IMPORTANT]
> The bundled `dashboard-admin` binding uses **`cluster-admin`** for lab convenience. Prefer a least-privilege Role/ClusterRole for real environments. Installing the app and using the app are separate permission concerns.

### Verify after each major step

```bash
kubectl -n kubernetes-dashboard get deploy,svc,cm,sa
kubectl get clusterrolebinding dashboard-admin
```

---

## Accessing the dashboard

### NodePort (default)

Committed defaults in `deploy/kube-dashboard/service.yaml`:

| Port name | Service port | NodePort |
|-----------|--------------|----------|
| `https` | 443 → container 8443 | **30443** |
| `http` | 80 → container 8080 | **30080** (301 redirect to HTTPS `:30443`) |

Virtualization UI (when installed): kubevirt-manager Service NodePort **30444**, also embedded at `https://<NODE_IP>:30443/` → **Virtualization** sidebar (proxied `/kubeops/virt-ui/`).

Monitoring (when installed): Grafana **30445**, Prometheus **30446**, Alertmanager **30447**. Grafana is also embedded at `https://<NODE_IP>:30443/` → **Monitoring** sidebar (proxied `/kubeops/mon-ui/`).

Discover values and a node address:

```bash
kubectl -n kubernetes-dashboard get svc kube-dashboard -o wide
kubectl get nodes -o wide
```

URL format:

```text
https://<NODE_IP>:30443/
```

Do not treat any single lab node IP as a public product URL.

### Port-forward

```bash
kubectl -n kubernetes-dashboard port-forward svc/kube-dashboard 8443:443
```

Then open `https://127.0.0.1:8443/`.

### TLS certificates

HTTPS uses Secret **`kube-dashboard-tls`**. The helper script generates a **self-signed** certificate. Browsers will show a trust warning until you replace the secret with a certificate your environment trusts.

Do not globally disable TLS verification as standard guidance.

### Ingress / LoadBalancer

Not provided as built-in product manifests. Operators may publish `kube-dashboard` through their own Ingress or LoadBalancer configuration.

---

## Authentication and authorization

### Operator path (default OIDC)

1. Open `https://<node-ip>:30443/` (accept self-signed cert if prompted).
2. You are redirected to **Dex** (`:32056`).
3. Sign in as **`kubeadmin`** with the password from `kubeops-authentication/kubeadmin-oidc-bootstrap`.
4. After login, NGINX injects a `dashboard-admin` SA bearer so the UI has cluster-admin API access.

CLI access continues to use your existing kubeconfig (not the browser OIDC session).

Optional: wire apiserver OIDC for kubectl-as-kubeadmin — see [docs/OPERATOR_AND_AUTH.md](docs/OPERATOR_AND_AUTH.md).

### Classic / token path

```bash
./scripts/get-token.sh
# Defaults: NS=kubernetes-dashboard SA=dashboard-admin DURATION=24h
```

Never commit, paste into issues, or publish real tokens.

### Least privilege

- Create a dedicated ServiceAccount with only the Role/ClusterRole your operators need.
- Point `SA=...` when calling `get-token.sh`, or create tokens with `kubectl create token`.
- Treat `dashboard-admin` → `cluster-admin` as a lab shortcut, not a production default.

### Common auth failures

| Symptom | Likely cause |
|---------|----------------|
| HTTP 401 | Missing/invalid/expired token |
| HTTP 403 | Token valid but RBAC denies the API request |
| Login page loops / errors | Wrong URL scheme/port; use the HTTPS NodePort endpoint |

---

## Deployment verification

### Operator path (recommended)

```bash
kubectl get ops
kubectl get kubedashboard,clusteroidc
kubectl get nfsprovisioner,virtualization,monitoring
kubectl get pods -n kubeops-console
kubectl get pods -n kubeops-authentication
kubectl get pods -n kubeops-addon-controller
kubectl get pods -n kubeops-virtualization   # after Addons → Virtualization Deploy
kubectl get pods -n kubeops-monitoring       # after Addons → Monitoring Deploy
kubectl -n kubeops-console rollout status deploy/kube-dashboard
```

Expected: Operators `Ready`, console HTTPS on **30443**, virt UI NodePort **30444** when Virtualization is installed, Grafana **30445** / Prometheus **30446** when Monitoring is installed.

### Classic path

```bash
kubectl -n kubernetes-dashboard get deploy,pods,svc
kubectl -n kubernetes-dashboard rollout status deploy/kube-dashboard
```

### Confirm CSS assets

```bash
curl -skI https://127.0.0.1:30443/theme.css
# After Virtualization is Ready (authenticated session / in-cluster):
# /virt-theme.css is injected into /kubeops/virt-ui/ HTML
```

Expect HTTP `200`/`302` (OIDC) and hard-refresh the browser after CSS updates.

---

## Theme customization

Primary stylesheets (operator path embeds from `operator/internal/assets/kube-dashboard/`):

```text
theme.css       # main console glass theme
virt-theme.css  # kubevirt-manager embed palette (AdminLTE overlay)
```

Classic packaging still uses:

```text
deploy/kube-dashboard/theme.css
```

### Useful CSS variables (`:root`)

| Variable | Role |
|----------|------|
| `--masthead-glass` | Header / toolbar ambient gradient |
| `--glass-surface` / `--glass-surface-flat` / `--glass-surface-elevated` | Panel fills |
| `--glass-surface-recessed` | Recessed inputs |
| `--glass-blur` / `--glass-blur-strong` | Backdrop blur radii |
| `--glass-saturation` | Backdrop saturation |
| `--glass-border` / `--glass-highlight` | Borders and reflections |
| `--text-primary` / `--text-secondary` / `--text-muted` | Typography |
| `--link-color` | Resource links |
| `--status-success` / `--status-warning` / `--status-error` / `--status-info` | Status colours |
| `--glass-shadow` / `--glass-shadow-elevated` | Depth |

Safe example (edit locally, then redeploy the ConfigMap):

```css
:root {
  --glass-blur-strong: 40px;
  --link-color: #8DC4FF;
  --status-success: #43D6A0;
}
```

### Apply stylesheet updates

Using Kustomize (regenerates ConfigMaps from files):

```bash
kubectl apply -k deploy/
kubectl -n kubernetes-dashboard rollout restart deploy/kube-dashboard
kubectl -n kubernetes-dashboard rollout status deploy/kube-dashboard
```

Or update only the CSS ConfigMap:

```bash
kubectl create configmap kube-dashboard-css \
  --from-file=theme.css=deploy/kube-dashboard/theme.css \
  -n kubernetes-dashboard --dry-run=client -o yaml | kubectl apply -f -
kubectl -n kubernetes-dashboard rollout restart deploy/kube-dashboard
```

Then hard-refresh the browser (CSS is served with `Cache-Control: no-cache`, but clients may still cache aggressively).

---

## Configuration reference

| Parameter | Default | Where configured |
|-----------|---------|------------------|
| Namespace | `kubernetes-dashboard` | `deploy/kustomization.yaml`, scripts `NS` |
| Upstream Dashboard version | `v2.7.0` | `scripts/install.sh` `DASHBOARD_VERSION` |
| Upstream manifest | `deploy/upstream/dashboard-recommended-v2.7.0.yaml` | `DASHBOARD_MANIFEST` |
| Skip upstream install | `false` | `SKIP_UPSTREAM=true` |
| Install metrics-server | `true` | `INSTALL_METRICS_SERVER` |
| Load/download images | `true` | `LOAD_IMAGES` / `scripts/download-images.sh` |
| Image release tag | `images-v1.2.2` | `IMAGE_RELEASE_TAG` |
| Enable insecure login patch | `true` | `ENABLE_INSECURE_LOGIN` in `install.sh` |
| Console proxy image | `nginx:1.27-alpine` | `deploy/kube-dashboard/deployment.yaml` |
| Dashboard image | `kubernetesui/dashboard:v2.7.0` | Vendored upstream manifest |
| Metrics scraper image | `kubernetesui/metrics-scraper:v1.0.8` | Vendored upstream manifest |
| metrics-server image | `registry.k8s.io/metrics-server/metrics-server:v0.7.2` | `deploy/metrics-server/components.yaml` |
| Service type | `NodePort` | `deploy/kube-dashboard/service.yaml` |
| HTTPS NodePort | `30443` | `deploy/kube-dashboard/service.yaml` |
| HTTP NodePort | `30080` | `deploy/kube-dashboard/service.yaml` |
| TLS secret name | `kube-dashboard-tls` | `scripts/create-tls-secret.sh` / Deployment volume |
| Theme stylesheet | `deploy/kube-dashboard/theme.css` | ConfigMap `kube-dashboard-css` |
| NGINX config | `deploy/kube-dashboard/nginx.conf` | ConfigMap `kube-dashboard-nginx` |
| Proxy CPU/memory | requests `20m`/`32Mi`, limits `200m`/`128Mi` | `deploy/kube-dashboard/deployment.yaml` |
| Login helper SA | `dashboard-admin` | `deploy/rbac/dashboard-admin.yaml` |
| Product labels | `app.kubernetes.io/version: "1.3.0"`, `dashboard.ocp/release: v1.3.0` | kube-dashboard + RBAC manifests |

This repository does **not** ship a Helm chart or invented values files.

---

## Troubleshooting

| Problem | Checks |
|---------|--------|
| Pods `Pending` | `kubectl -n kubernetes-dashboard describe pod <pod>`; node resources / taints |
| `ImagePullBackOff` | Registry access; preload images; `describe pod` events |
| `CrashLoopBackOff` (kube-dashboard) | Missing `kube-dashboard-tls`; NGINX config errors — check kube-dashboard logs |
| URL not reachable | NodePort firewall/security groups; try port-forward |
| TLS warning | Expected for self-signed cert; replace secret or trust explicitly |
| HTTP 401 | Regenerate token: `./scripts/get-token.sh` |
| HTTP 403 | Inspect RBAC for the authenticated identity |
| Expired token | Create a new token (`DURATION` / `kubectl create token`) |
| Theme missing | Confirm HTML includes `/theme.css`; `curl -skI` the stylesheet |
| Old CSS after update | Rollout restart kube-dashboard Deployment; hard-refresh browser |
| Virtualization blank / nested UI | Confirm `/kubeops/virt-ui/` proxy + `virt-theme.css`; hard-refresh `?nocache=` |
| Addons tile stuck Progressing | `kubectl get virtualization,nfsprovisioner,monitoring -o yaml`; check addon-controller logs |
| Monitoring panels empty | `kubectl -n kubeops-monitoring get pods`; Prometheus targets `http://<node>:30446/targets`; prefer NFS StorageClass for PVC mode |
| NGINX proxy errors | `logs deploy/kube-dashboard`; confirm upstream Service `kubernetes-dashboard` |
| Metrics empty / charts waiting | `kubectl top nodes`; metrics-server + scraper logs; wait ~2–5 min for samples |
| Overlay/dropdown styling | Hard-refresh; confirm served CSS matches repo file |
| Missing permissions | Dashboard features require RBAC; install rights ≠ view/edit rights |

Prefer diagnose → targeted fix. Avoid destructive cluster cleanup as a first step.

---

## Upgrading

Publishing a Git tag does **not** update a live cluster automatically.

```bash
cd dashboard-ocp
git pull origin main
# Preserve local theme edits if you customized theme.css
./scripts/install.sh
# or, if upstream + metrics-server are already present:
# SKIP_UPSTREAM=true INSTALL_METRICS_SERVER=false ./scripts/install.sh
# or: kubectl apply -k deploy/ && kubectl -n kubernetes-dashboard rollout restart deploy/kube-dashboard
```

Then verify pods and hard-refresh the browser. Compare stylesheets if needed:

```bash
curl -sk https://127.0.0.1:30443/theme.css | sha256sum
sha256sum deploy/kube-dashboard/theme.css
```

---

## Rollback

Git tags available include `v1.2.1`, `v1.2.0`, `v1.1.0`, `v1.0.0`.

```bash
git fetch --tags
git checkout v1.2.1
kubectl apply -k deploy/
kubectl -n kubernetes-dashboard rollout restart deploy/kube-dashboard
kubectl -n kubernetes-dashboard rollout status deploy/kube-dashboard
```

Checking out an older Git revision alone does **not** roll back running workloads until manifests/ConfigMaps are re-applied.

Do not use `git reset --hard` on shared `main` or force-push as a rollback procedure.

---

## Uninstallation

### Operator path

Delete addon workloads first (while controllers are still running) so finalizers can GC:

```bash
kubectl delete monitoring,nfsprovisioner,virtualization --all --all-namespaces
kubectl delete kubedashboard --all
kubectl delete clusteroidc --all
kubectl delete operators --all
kubectl delete ns \
  kubeops-console kubeops-authentication kubeops-addon-controller \
  kubeops-nfs kubeops-virtualization kubeops-monitoring
kubectl delete crd \
  kubedashboards.dashboard.ocp clusteroidcs.dashboard.ocp operators.dashboard.ocp \
  nfsprovisioners.dashboard.ocp virtualizations.dashboard.ocp monitorings.dashboard.ocp
kubectl delete clusterrolebinding \
  kubeops-console kubeops-authentication \
  kubeops-nfs-controller kubeops-virtualization-controller kubeops-monitoring-controller \
  dashboard-admin oidc-kubeadmin --ignore-not-found
```

### Classic path

```bash
./scripts/uninstall.sh
```

Removes classic `deploy/` resources and Secret `kube-dashboard-tls`.

Optional:

```bash
REMOVE_UPSTREAM=true ./scripts/uninstall.sh
REMOVE_METRICS_SERVER=true ./scripts/uninstall.sh
REMOVE_UPSTREAM=true REMOVE_METRICS_SERVER=true ./scripts/uninstall.sh
```

> [!WARNING]
> Do not delete the `kubernetes-dashboard` namespace if it contains unrelated workloads you still need. The uninstall script does not blindly wipe arbitrary cluster objects beyond the kube-dashboard kustomization (and optional upstream / metrics-server manifests when enabled).

---

## Repository structure

```text
dashboard-ocp/
├── README.md
├── CHANGELOG.md
├── VERSION / RELEASE
├── operator/                         # kubeops operators + addon controllers
│   ├── cmd/console|oidc|nfs|virtualization|monitoring|ceph|metallb/
│   ├── config/
│   │   ├── addons/controllers.list   # addon registry for install-operators.sh
│   │   ├── crd|rbac|manager|samples/
│   │   └── manager|rbac/addons/      # NFS + virt + mon + ceph + metallb controllers
│   └── internal/
│       ├── assets/kube-dashboard/    # theme.css, virt-theme.css, mon-theme.css, *.js
│       ├── controller/               # console reconciler + nginx templates
│       ├── oidc/ nfs/ virtualization/ monitoring/ ceph/ metallb/
├── deploy/                           # classic kustomize packaging (install.sh)
├── scripts/
│   ├── kubeops-installer.sh          # interactive CLI (progress bar + logs + password file)
│   ├── install-operators.sh          # recommended (kubeops + OIDC + addons); --interactive
│   ├── pack-static-image.py
│   ├── install-phase-c-oidc.sh / configure-apiserver-oidc.sh
│   ├── install.sh / uninstall.sh
│   └── download-images.sh / load-images.sh / get-token.sh
├── artifacts/images/                 # local *.tar (gitignored)
└── docs/
    ├── OPERATOR_AND_AUTH.md
    ├── ADDON_CONTROLLERS.md
    └── METRICS_AND_IMAGES.md
```

---

## Security and known limitations

- Base application is **Kubernetes Dashboard v2.7.0** (legacy upstream release).
- Kube Dashboard is a **cosmetic** customization, not a security modernization.
- Upstream maintenance, CVEs, and dependency age must be evaluated before any sensitive use.
- **Production security readiness has not been established.**
- Comprehensive cross-browser validation has not been completed.
- CSS customization can break when upstream DOM/Material classes change.
- `backdrop-filter` behaviour varies by browser; fallbacks exist when unsupported.
- Resource visibility and actions are governed entirely by Kubernetes RBAC.
- Bundled `dashboard-admin` ClusterRoleBinding uses `cluster-admin` for labs.
- HTTP→HTTPS redirect NodePort and `--enable-insecure-login` are lab-oriented conveniences.

---

## Roadmap

### Delivered

- Kube Dashboard cosmetic modernization (v1.3.0)
- Packaging via NGINX + ConfigMap CSS injection
- **kubeops-console** / **kubeops-authentication** operators
- Single-script OIDC install (`./scripts/install-operators.sh`)
- Interactive installer (`./scripts/kubeops-installer.sh`) with progress bar, dated logs, and `kubeadmin-password`
- Dex login + oauth2-proxy gate for the console
- Shared **kubeops-addon-controller** namespace + Addons page
- **NFSProvisioner** addon (`kubeops-nfs-controller`)
- **Virtualization** addon (KubeVirt + CDI + kubevirt-manager embed + glass theme)
- **Monitoring** addon (Prometheus + Grafana + Alertmanager embed + kubeops palette theme)
- **Ceph** addon (Rook + RBD/CephFS + embedded mgr dashboard)
- **MetalLB** addon (L2 IPAddressPool + L2Advertisement; vendored MetalLB v0.16.2)

### Possible future work (not implemented)

- Published operator container images on a registry
- MetalLB BGP mode / multi-pool UI
- Additional theme options
- Improved responsive layouts
- Broader accessibility testing
- Security / dependency modernization of the base stack
- Loki / Tempo / Thanos for the monitoring stack

No delivery dates are promised.

---

## Releases and changelog

- Tags / releases: [github.com/Deepak-Das01/dashboard-ocp/tags](https://github.com/Deepak-Das01/dashboard-ocp/tags)
- Detailed notes: [`CHANGELOG.md`](CHANGELOG.md)

### v1.3.0 highlights

Stable single-pod **kube-dashboard** console (NGINX + Dashboard sidecar), themed UI, metrics-server packaging, and offline image helpers. No separate upstream dashboard or glass proxy Deployment.

**On `main` beyond the v1.3.0 tag:** kubeops Operators/Addons UI, interactive `kubeops-installer.sh`, NFS provisioner addon, Virtualization stack (KubeVirt/CDI + embedded kubevirt-manager), Monitoring stack (Prometheus/Grafana/Alertmanager + embedded Grafana with kubeops palette), Ceph addon (Rook + RBD/CephFS + embedded mgr dashboard), and MetalLB addon (L2 LoadBalancer pools via Addons).

Prior verified tags: `v1.2.1`, `v1.2.0`, `v1.1.0`, `v1.0.0`.

---

## Contributing

1. Fork the repository.
2. Create a feature branch from `main` or a release tag.
3. Prefer small, reviewable cosmetic or packaging changes.
4. For CSS work, edit `operator/internal/assets/kube-dashboard/theme.css` (plus `virt-theme.css` / `mon-theme.css` for embeds), rebuild/pack `kubeops-console`, and hard-refresh the browser.
5. Open a pull request with a clear summary and test notes.
6. File bugs with: Kubernetes version, kube-dashboard version, browser, steps to reproduce, and redacted screenshots.

Do **not** include tokens, kubeconfigs, or secrets in issues or PRs.

---

## License and acknowledgements

- This repository currently has **no committed `LICENSE` file**. Treat distribution terms carefully and consult repository owners before redistribution.
- Upstream [Kubernetes Dashboard](https://github.com/kubernetes/dashboard) is maintained by the Kubernetes / Dashboard contributors and is licensed under **Apache License 2.0**.
- Acknowledgements: Kubernetes Dashboard contributors, the Kubernetes project, Angular and Angular Material, NGINX, [KubeVirt](https://kubevirt.io/) / CDI, [kubevirt-manager](https://kubevirt-manager.io/), [MetalLB](https://metallb.universe.tf/), [Rook](https://rook.io/) / Ceph, and Apple **macOS / visionOS** solely as **visual design inspiration**.

No affiliation, endorsement, or partnership with Apple, the Kubernetes project, MetalLB, Rook, or kubevirt-manager is implied.
