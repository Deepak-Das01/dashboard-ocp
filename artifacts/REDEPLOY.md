# KubeOps operators — redeploy (v1.3.0)

Packed 2026-11-22T21:33:00Z (includes kubeops-ceph)

## Artifacts
- artifacts/images/kubeops-console-1.3.0-amd64.tar
- artifacts/images/kubeops-authentication-1.3.0-amd64.tar
- artifacts/images/kubeops-nfs-1.3.0-amd64.tar
- artifacts/images/kubeops-virtualization-1.3.0-amd64.tar
- artifacts/images/kubeops-monitoring-1.3.0-amd64.tar
- artifacts/images/kubeops-ceph-1.3.0-amd64.tar

Images are already imported into local containerd (namespace k8s.io).

## Full reinstall (recommended)
```bash
cd /root/dashboard-ocp
LOAD_IMAGES=false BUILD_OPERATORS=false PACK_IMAGES=false ./scripts/install-operators.sh
```

Or rebuild+pack+install in one shot:
```bash
./scripts/install-operators.sh
```

## Ceph wipe retest evidence
See `artifacts/ceph-retest-20261122/FULL_REPORT.md`.
