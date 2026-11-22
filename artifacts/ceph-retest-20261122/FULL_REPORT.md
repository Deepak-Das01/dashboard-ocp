# Ceph operator hard retest — full report

**Date:** 2026-11-22 21:26–21:29 UTC  
**Verdict:** **PASS**  
**Lab:** single-node `kube-node` (192.168.222.141), Rook v1.16.7, Ceph v19.2.3, disks `sdb`/`sdc`/`sdd`

## Summary

| Step | Result | Duration |
|------|--------|----------|
| Hard audit of cleanup/health/Ready gates | PASS (1 WARN) | code review |
| Wipe uninstall (`kubeops.io/wipe-disks=confirmed`) | PASS | **40s** |
| Cleanup verification | PASS | — |
| Reinstall → Ready + HEALTH_OK + 3 OSDs + both SCs | PASS | **112s** |
| Manual ceph CLI / host wipe | **None** | — |

Final: `kubeops-ceph` **Ready / HEALTH_OK**, FSID `bd736c3f-5da0-4323-adaf-1c1c6b49346d`, dashboard http://192.168.222.141:30448/

## Hard audit checklist

| Control | Result | Notes |
|---------|--------|-------|
| Wipe annotation gate | PASS | Refuses uninstall without `kubeops.io/wipe-disks=confirmed` |
| cleanupPolicy sanitize | PASS | `yes-really-destroy-data` + quick zero sanitize |
| FS/pool finalizer force-strip | PASS | Avoids hang when OSDs already gone |
| Device wipe Job | PASS | `ceph-volume lvm zap --destroy` + dd + sgdisk + wipefs; `raw list` must be `{}` |
| Rook cleanup Job wait | PASS | `cluster-cleanup-job` completed before wipe |
| StorageClass GC | PASS | Both SCs removed |
| CephCluster / pools / FS GC | PASS | No `ceph.rook.io` resources after uninstall |
| Node unlabel | PASS | `ceph=storage` removed |
| Disk signature clear | PASS | No FSTYPE/blkid on sdb/sdc/sdd after wipe |
| failureDomain=osd + poolSize auto | PASS | Single-node 3-disk → size 3 across OSDs |
| Health-fix Job | PASS | mon_data_avail_warn=10, crush rule, pool size |
| Ready gate | PASS | HEALTH_OK + ≥1 ready OSD pod required |
| Reinstall without manual CLI | PASS | Fully automatic |
| cleanupPolicy field compat | PASS (fixed) | Dropped unknown `wipeDevicesFromOtherClusters`; wipe Job covers reuse |

## Cleanup verification (after uninstall)

- RookCeph CR: absent  
- CephCluster / CephBlockPool / CephFilesystem: absent  
- StorageClasses: none  
- Node label: cleared  
- Disks: clean (no blueStore)  
- `/var/lib/rook`: cluster dirs removed (exporter stub only)  
- Namespace `kubeops-ceph-cluster`: retained by design (Rook operator reuse)

## Reinstall path (automatic)

1. `Installing Rook operator…`  
2. Wait CephCluster  
3. 3 OSD pods Running (~82s)  
4. Health-fix Job (~10s)  
5. Ready — Ceph HEALTH_OK + rook-ceph-block + rook-cephfs (**112s** total)

## Final `ceph -s` (toolbox)

- health: **HEALTH_OK**  
- mon 1, mgr 1, mds 1/1 + standby, osd **3 up / 3 in**  
- pools: 4 · PGs active+clean  
- capacity: 150 GiB  

## Evidence files

Same directory: `00-baseline.txt` … `05-audit.txt`, `rookceph-reinstall.yaml`
