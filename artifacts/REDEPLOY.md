# KubeOps operators — redeploy (v1.3.0)

Packed 2026-11-23T08:05:48Z

## Artifacts
- artifacts/images/kubeops-console-1.3.0-amd64.tar
- artifacts/images/kubeops-authentication-1.3.0-amd64.tar
- artifacts/images/kubeops-nfs-1.3.0-amd64.tar
- artifacts/images/kubeops-virtualization-1.3.0-amd64.tar
- artifacts/images/kubeops-monitoring-1.3.0-amd64.tar
- artifacts/images/kubeops-ceph-1.3.0-amd64.tar

## Highlights (ceph)
- Hardened disk wipe (blueStore zap + fresh wipe per uninstall)
- Multi-node parallel disk discovery (filters nbd/loop/zero-size)
- Storage-node taint + auto Ceph/CSI tolerations checkbox
- nginx resolver uses CoreDNS ClusterIP (survives reboot)

## Redeploy
```bash
cd /root/kubeops-platform
LOAD_IMAGES=false BUILD_OPERATORS=false PACK_IMAGES=false ./scripts/install-operators.sh
```
