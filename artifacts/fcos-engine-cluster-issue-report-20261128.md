# Fedora CoreOS kubeops-engine cluster — issue report

**Date:** 2026-11-28T08:38Z  
**Source env:** uat-ubuntu (`192.168.222.142`)  
**Kubeconfig:** `/root/kubeops-engine/kube/config`  
**API VIP:** `https://192.168.200.5:6443`  
**Cluster:** 3× control-plane only (no workers), K8s **v1.31.4**, CRI-O **1.32.0**, OS **Fedora CoreOS 45.20261030.3.1**  
**Deploy run:** `20261128-demo-ha` (Flannel, platform toggle on)  
**Audience:** kubeops-engine / kubeops-platform teams (investigate & fix; no further ops changes from this report)

---

## Executive summary

Control-plane HA join succeeded (3/3 Ready). Platform install on primary marked **complete**, but several **engine + platform integration** defects left controllers/UI broken or flaky:

| # | Severity | Area | One-line |
|---|----------|------|----------|
| 1 | **P0** | CNI (engine) | control01 `cni0` stuck on CRI-O default `10.85.0.0/16`; Flannel expects `10.244.0.1/24` → sandbox create fails |
| 2 | **P0** | Platform images (engine) | Secondary CPs never got `/etc/kubeops/install-platform` → no image preload → `ImagePullBackOff` on `localhost/kubeops-*:1.3.0` |
| 3 | **P1** | Scheduling (engine/platform) | CP-only cluster kept control-plane taints; Dex/dashboard/metrics lacked CP tolerations → long Pending |
| 4 | **P1** | Dep images (platform/engine) | Dashboard/Dex/oauth2/nginx/metrics-server only present on control01; not distributed to all CPs |
| 5 | **P2** | Rollout (platform install) | Install script marked success after deploy rollout **timeout**; stuck RS / ProgressDeadlineExceeded |
| 6 | **P2** | FCOS static pods (engine) | `flexvolume-dir` FailedMount on kube-controller-manager (`/usr/libexec/kubernetes` read-only) |
| 7 | **P3** | Image drift | control01 kubeops image digests differ from artifacts loaded on control02/03 |

Addon Operators (Ceph/NFS/MetalLB/Monitoring/Virt) show **Unavailable — not installed** (expected until Addons UI deploy). Console/OIDC CRs report Ready, but **Dex + kube-dashboard pods were not actually runnable** on control01 due to #1.

---

## Cluster snapshot (at report time)

### Nodes
| Node | IP | Status | Role |
|------|-----|--------|------|
| control01.kubeops.com | 192.168.200.100 | Ready | control-plane |
| control02.kubeops.com | 192.168.200.101 | Ready | control-plane |
| control03.kubeops.com | 192.168.200.102 | Ready | control-plane |

PodCIDRs: `.100 → 10.244.0.0/24`, `.102 → 10.244.1.0/24`, `.101 → 10.244.2.0/24`  
CNI ConfigMap: Flannel `Network: 10.244.0.0/16`

### Controllers that came up
- `kubeops-console`, `kubeops-authentication` managers: Running (after image availability)
- Addon controllers (ceph/metallb/monitoring/nfs/virt): Running
- CoreDNS (kubeops-dns), kubeops-cni DS, kube-system static pods: Running

### Still broken / not Ready
| Namespace | Pod / Deploy | Status | Node |
|-----------|--------------|--------|------|
| kubeops-console | kube-dashboard | ContainerCreating | control01 |
| kubeops-console | dashboard-metrics-scraper | ContainerCreating | control01 |
| kubeops-authentication | dex | Init:0/1 | control01 |
| kube-system | metrics-server | ContainerCreating | control01 |
| kubeops-addon-controller | virt controller (one replica) | ContainerCreating | control01 |

**Common error on control01:**
```text
FailedCreatePodSandBox: flannel failed (add): failed to set bridge addr:
"cni0" already has an IP address different from 10.244.0.1/24
```

Observed `cni0` on control01: **`10.85.0.1/16`** (CRI-O default), not `10.244.0.1/24`.

---

## Issue details

### 1. P0 — CRI-O default bridge fights Flannel (control01)

**Evidence**
- `/etc/cni/net.d/` on all nodes contains **both**:
  - `10-flannel.conflist` (kubeops Flannel)
  - `100-crio-bridge.conflist` (CRI-O default → bridge `cni0`, subnet `10.85.0.0/16`)
- control01: `cni0 = 10.85.0.1/16`; CoreDNS was seen on `10.85.0.x`
- control02/03: `cni0 = 10.244.{2,1}.1/24` (Flannel won first) — healthy
- New sandboxes on control01 fail with bridge addr mismatch vs Flannel’s `10.244.0.1/24`

**Impact:** Any new pod scheduled to control01 cannot create a sandbox (Dex, dashboard, metrics-server, etc.).

**Likely owner:** **kubeops-engine** bootstrap / CNI bring-up on FCOS+CRI-O  
**Suggested direction (for platform team):** On Flannel (and Calico) installs, remove/disable `100-crio-bridge.conflist` before first pod; ensure flannel owns `cni0`; document/reset procedure if cni0 already wrong.

---

### 2. P0 — Platform image preload only on primary CP

**Evidence**
- Generated Butane for `20261128-demo-ha`:
  - `control01.kubeops.com.bu` contains `/etc/kubeops/install-platform`
  - `control02` / `control03` **do not**
- On live nodes:
  - control01: `HAS_INSTALL_PLATFORM`, `HAS_IMAGES_MARKER`, `crictl` shows all `localhost/kubeops-*:1.3.0`
  - control02/03 (as deployed): **no** `install-platform`, **no** images initially → pods scheduled there hit:
    ```text
    Failed to pull image "localhost/kubeops-nfs:1.3.0": RegistryUnavailable
    dial tcp [::1]:80: connect: connection refused
    ```
- Current `generator.py` comments say “all CPs preload images”, but **this demo-ha ISO set did not** emit the flag on secondaries.

**Impact:** ImagePullBackOff / rollout timeout for all localhost operator images on non-primary CPs.

**Likely owner:** **kubeops-engine** Ignition generation + `install-kubeops-platform.sh`  
**Suggested direction:** Emit `install-platform` + registries.conf + platform unit on **every** control-plane; secondaries preload only; primary deploys after all CPs Ready (as script intends). Regenerate ISOs and verify `.bu` for all CPs.

---

### 3. P1 — CP-only cluster + control-plane taint vs workload tolerations

**Evidence (before any untaint)**
- All 3 nodes had `node-role.kubernetes.io/control-plane:NoSchedule`
- Operator manager Deployments had CP tolerations (after install patch)
- **Child workloads did not:**
  - Dex: no CP toleration → FailedScheduling
  - kube-dashboard: no CP toleration
  - metrics-scraper: only legacy `master` key, not `control-plane`
- `untaint_control_planes` was **off** in this demo-ha style run (CP-only lab)

**Impact:** Dex/dashboard stuck Pending until taints removed or tolerations added.

**Likely owner:** **kubeops-engine** (default untaint for CP-only) **and/or kubeops-platform** (always tolerate CP on managed Deployments: Dex, dashboard, scraper, metrics-server)

---

### 4. P1 — Dependency images not on all nodes

**Images required for console/OIDC path**
- `nginx:1.27-alpine`
- `kubernetesui/dashboard:v2.7.0`
- `kubernetesui/metrics-scraper:v1.0.8`
- `quay.io/oauth2-proxy/oauth2-proxy:v7.7.1`
- `ghcr.io/dexidp/dex:v2.41.1`
- `registry.k8s.io/metrics-server/metrics-server:v0.7.2`

**Evidence:** Present under `crictl images` on **control01 only**; absent on control02/03 at check time.  
Platform install script tags some of these on the node that runs preload; secondaries never ran preload (#2).

**Likely owner:** **kubeops-engine** platform bundle preload list on all CPs; optionally **kubeops-platform** document airgap image set.

---

### 5. P2 — Platform install reports success after rollout failure

**Evidence** (`/var/log/kubeops-platform.log` on control01):
```text
error: timed out waiting for the condition   # kubeops-console
error: deployment "kubeops-authentication" exceeded its progress deadline
...
kubeops-platform install complete
```
- Deployments left with `ProgressDeadlineExceeded` and **two ReplicaSets desired** (old without tolerations Pending + new ImagePullBackOff).

**Likely owner:** **kubeops-engine** `install-kubeops-platform.sh` (must fail / not write installed marker if rollouts fail).

---

### 6. P2 — kube-controller-manager flexvolume on FCOS

**Events:**
```text
MountVolume.SetUp failed for volume "flexvolume-dir":
mkdir /usr/libexec/kubernetes: read-only file system
```
Controllers still Running; noisy / may break flexvolume users.

**Likely owner:** **kubeops-engine** kubeadm/Ignition volume mounts for FCOS immutable `/usr`.

---

### 7. P3 — Image digest drift across nodes

| Node | `localhost/kubeops-console:1.3.0` id (crictl) |
|------|-----------------------------------------------|
| control01 | `d4d19faa3c07a` (from original node preload) |
| control02/03 | `81c01f21e9af4` (from uat-ubuntu `artifacts/images/*.tar`) |

Same tag, different content — risk of inconsistent operator behavior after reschedule.

**Likely owner:** **kubeops-engine** artifact single-source + preload; rebuild/redistribute one canonical 1.3.0 set.

---

## Operator / product status (not bugs by themselves)

| Operator | Phase | Note |
|----------|-------|------|
| kubeops-console | Ready (status) | UI pods on control01 still broken due to CNI #1 |
| kubeops-authentication | Ready (status) | Dex pods stuck Init/sandbox on control01 |
| kubeops-ceph / nfs / metallb / monitoring / virtualization | Unavailable | “not installed; deploy from Addons” — expected |

Console URL claimed: `https://192.168.200.100:30443/`  
Dex URL claimed: `http://192.168.200.100:32056/dex`

---

## Note on investigation-side changes (for awareness)

During triage (before this “report-only” request), some **non-code** cluster mutations already happened on the live lab. Platform team should treat the cluster as **partially touched**:

1. Operator image tarballs were loaded onto control02/03 via podman/`crictl` from uat-ubuntu `kubeops-platform/artifacts/images/`.
2. Control-plane `NoSchedule` taints were removed on all three nodes.
3. Platform Deployments were `rollout restart`ed once.

**Not done (user declined further resolve):** disabling `100-crio-bridge.conflist`, resetting control01 `cni0`, engine/platform code fixes, ISO regenerate.

---

## Recommended ownership checklist

**kubeops-engine**
- [ ] Disable CRI-O default bridge CNI whenever Flannel/Calico is selected (all nodes, before first workload).
- [ ] Ensure `/etc/kubeops/install-platform` + image preload on **every** control-plane ISO/Ignition.
- [ ] For CP-only topologies, default `untaint_control_planes=on` **or** fail fast with a clear error.
- [ ] Do not write `kubeops-platform-installed` if console/auth rollouts fail.
- [ ] Fix FCOS flexvolume hostPath for kube-controller-manager.
- [ ] Re-verify `20261128-demo-ha` (and future) `.bu` files for control02/03.

**kubeops-platform**
- [ ] Add control-plane tolerations to Dex, kube-dashboard, metrics-scraper, metrics-server (and any other managed workloads).
- [ ] Publish/complete airgap image list (dashboard, dex, oauth2-proxy, nginx, metrics-server) in platform bundle consumed by engine.
- [ ] Align shipped `1.3.0` tar digests with what primary preload uses.

---

## Quick reproduce / verify commands (from uat-ubuntu)

```bash
export KUBECONFIG=/root/kubeops-engine/kube/config
kubectl get nodes -o wide
kubectl get pods -A | grep -vE 'Running|Completed'
kubectl get operators.dashboard.ocp -o wide
# on each node:
#   ip -br a show cni0
#   ls /etc/cni/net.d/
#   sudo crictl images | grep -E 'kubeops|dashboard|dex'
#   test -f /etc/kubeops/install-platform && echo yes
```

---

*Report generated for handoff; no further remediation requested.*
