# Fedora CoreOS deploy — full issue audit (kubeops-platform)

**Date:** 2026-11-28  
**Cluster under review:** kubeops-engine FCOS HA (`control01/02/03`, CRI-O 1.32, K8s 1.31.4, VIP `192.168.200.5`)  
**Scope:** Remaining / related CoreOS issues beyond the short-name + CP-toleration fixes already landed in platform.

---

## Status legend

| Tag | Meaning |
|-----|---------|
| **FIXED (platform)** | Code/scripts changed in this repo (needs rebuild/redeploy to take effect on cluster) |
| **OPEN (engine)** | Belongs to kubeops-engine Ignition/bootstrap — not fixable in platform alone |
| **OPEN (ops)** | Live cluster / airgap packing / redeploy step |
| **WATCH** | Works today but fragile on FCOS/HA |

---

## A. Issues already fixed in platform (prior + this pass)

| # | Issue | Fix |
|---|--------|-----|
| A1 | Short names (`nginx:…`, `kubernetesui/…`, `kubeops-*:…`) → CRI-O `ImageInspectError` / no TTY short-name prompt | `images.Qualify`, FQ defaults, manager YAMLs `localhost/…`, install script FQ pulls |
| A2 | Operator reconcile reverts image patches to short names | Console/OIDC always qualify on reconcile; scraper managed in Go |
| A3 | CP-only cluster: platform pods Pending on `control-plane` taint | `placement.ControlPlaneTolerations` on dashboard, Dex, scraper, metrics-server, NFS, monitoring, virt UI, manager Deployments |
| A4 | `install-operators.sh` / `load-images.sh` hard-require **containerd `ctr`** — fails on FCOS/CRI-O | **FIXED now:** prefer `podman load/pull`, fall back to `ctr` |
| A5 | OAuth cookie/whitelist + Dex redirect bound to **one** node IP — breaks HA / multi-node NodePort access | **FIXED now:** all Ready node IPs (+ VIP host) in cookie/whitelist; Dex `redirectURIs` for every public IP |

**Live cluster still runs old operator images** — Deployments still show `nginx:1.27-alpine` / `kubernetesui/…` until operators are rebuilt and rolled out.

---

## B. Still open — engine / cluster (not platform-only)

| # | Sev | Issue | Evidence | Owner |
|---|-----|--------|----------|--------|
| B1 | P0 | Secondary CPs historically missing `/etc/kubeops/install-platform` → no image preload | Live: control01 `INSTALL_PLATFORM=yes`; control02/03 `=no`. demo-ha `.bu` only on control01 | **engine** |
| B2 | P0 | CRI-O default bridge `100-crio-bridge.conflist` fights Flannel (`cni0` `10.85` vs `10.244`) | Earlier: control01 sandbox failures; now cni0 is `10.244.0.1/24` and crio-bridge removed on nodes (someone/engine fixed live) | **engine** (must disable crio-bridge on *all* nodes before first pod) |
| B3 | P1 | Workload images (nginx/dashboard/scraper) not on every CP | control02/03 lacked those images at audit; only operator `localhost/kubeops-*` + dex/oauth2 present | **engine** preload list + **ops** sync after pack |
| B4 | P2 | kube-controller-manager `flexvolume-dir` → `mkdir /usr/libexec/kubernetes: read-only file system` | FCOS immutable `/usr` | **engine** kubeadm/Ignition volume |
| B5 | P2 | Platform install marker written after rollout timeout | control01 `/var/log/kubeops-platform.log` | **engine** `install-kubeops-platform.sh` |
| B6 | P3 | Same tag `1.3.0`, different digests across nodes | control01 vs 02/03 image IDs differ | **engine** single artifact source |

---

## C. Platform / product watch items on FCOS

| # | Sev | Issue | Notes |
|---|-----|--------|--------|
| C1 | P1 | **Dex/console NodePorts vs API VIP** | VIP `192.168.200.5` typically fronts **:6443 only**. Console `:30443` and Dex `:32056` are per-node. Issuer/redirect correctly use a node InternalIP; set `ClusterOIDC.spec.issuerURL` explicitly if you front Dex on a VIP/LB. |
| C2 | P1 | **KubeVirt on FCOS VMs** | Needs nested virtualization (`/dev/kvm`) on the hypervisor; without it VMs stay scheduling/failed. Platform sets `useEmulation` optionally — confirm for lab. |
| C3 | P1 | **NFS client helpers** | NFS provisioner uses in-pod NFS mounts; FCOS nodes may need `nfs-utils` (rpm-ostree) for kernel NFS client behavior depending on CRI-O/kernel path. Validate before NFS addon UAT. |
| C4 | P2 | **SELinux + privileged addons** | Ceph / KubeVirt namespaces use PSA `privileged`. Usually OK on FCOS; watch AVC denials if custom policies. |
| C5 | P2 | **Airgap / non-interactive pulls** | Any remaining short name in vendored upstream YAML (large KubeVirt/CDI/MetalLB blobs) can still fail under CRI-O short-name enforcement. Prefer preloading those images on all nodes. |
| C6 | P3 | **oauth2-proxy PKCE warning** | Log: provider supports PKCE but not enabled — cosmetic today; enable `--code-challenge-method=S256` later. |

---

## D. Live cluster snapshot (2026-11-28 ~09:02Z)

| Check | Result |
|-------|--------|
| Nodes | 3/3 Ready, CRI-O 1.32, FCOS 45 |
| Non-Running pods | none (at snapshot) |
| CNI `cni0` | control01/02/03 all `10.244.x.1/24`; `100-crio-bridge` absent |
| Console HTTPS | `302` → `/oauth2/start` on `:30443` |
| Dex discovery | `200` on `:32056` |
| kube-dashboard images **on cluster** | still **short** (`nginx:1.27-alpine`, `kubernetesui/dashboard:v2.7.0`) — old console operator |
| Dex / scraper tolerations | still missing CP key on live objects — old images |
| Manager operators | `localhost/kubeops-*:1.3.0` + CP toleration (patched earlier) |

---

## E. Platform changes in this audit pass

1. `scripts/load-images.sh` — podman-first import for CRI-O/FCOS  
2. `scripts/install-operators.sh` — podman-first pack/import/pull; no hard `ctr` dependency  
3. `operator/internal/endpoint` — VIP + all node IPs helper  
4. Console oauth2-proxy — multi-IP cookie domains + whitelist  
5. Dex — multi-IP `redirectURIs` for HA NodePort access  

---

## F. Recommended next steps

**Platform team**
1. Rebuild/pack `localhost/kubeops-console:1.3.0` + `kubeops-authentication:1.3.0` (+ addons) with these commits  
2. Import on **all three** FCOS nodes via `podman load`  
3. Rollout restart console + auth controllers; confirm Deployments show FQ images + CP tolerations  
4. Preload FQ workload images on every CP:  
   `docker.io/library/nginx:1.27-alpine`, `docker.io/kubernetesui/dashboard:v2.7.0`, `docker.io/kubernetesui/metrics-scraper:v1.0.8`, `ghcr.io/dexidp/dex:v2.41.1`, `quay.io/oauth2-proxy/oauth2-proxy:v7.7.1`, `registry.k8s.io/metrics-server/metrics-server:v0.7.2`

**Engine team**
1. Emit `install-platform` on every control-plane ISO  
2. Disable `100-crio-bridge.conflist` whenever Flannel/Calico is selected  
3. Fail install if console/auth rollouts time out (don’t write installed marker)  
4. Fix flexvolume hostPath for FCOS immutable `/usr`  
5. For CP-only labs, default `untaint_control_planes=on` **or** rely on platform tolerations (now present after redeploy)

---

*Audit complete. Engine items B1–B6 remain outside this repo.*
