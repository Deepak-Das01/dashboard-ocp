# Addon controllers

Addon reconcile controllers live in a shared namespace and **all ship with**
`scripts/install-operators.sh`. Users install/uninstall **addon workloads** from
the console Addons page (create/delete CRs) — they do not install controllers.

## Layout

| Piece | Name | Namespace |
|---|---|---|
| Shared controller home | — | `kubeops-addon-controller` |
| NFS addon controller | `kubeops-nfs-controller` | `kubeops-addon-controller` |
| NFS provisioner workload | release name (default `kubeops-nfs-provisioner`) | CR `spec.namespace` (default `kubeops-nfs`) |
| Operator status row | `kubeops-nfs` (`component: nfs`) | cluster-scoped |
| Virtualization controller | `kubeops-virtualization-controller` | `kubeops-addon-controller` |
| Virtualization UI | kubevirt-manager (NodePort **30444**) | `kubeops-virtualization` |
| Virtualization workload | KubeVirt + CDI | `kubeops-virtualization` |
| Operator status row | `kubeops-virtualization` (`component: virtualization`) | cluster-scoped |
| Monitoring controller | `kubeops-monitoring-controller` | `kubeops-addon-controller` |
| Monitoring UI | Grafana (NodePort **30445**; embed `/kubeops/mon-ui/`) | `kubeops-monitoring` |
| Monitoring workload | Prometheus + Alertmanager + Grafana + exporters | `kubeops-monitoring` |
| Operator status row | `kubeops-monitoring` (`component: monitoring`) | cluster-scoped |

## Registry (source of truth for install)

`operator/config/addons/controllers.list`

```text
# addon_id         go_cmd_pkg        image_name               deploy_name                         component         namespace
nfs                nfs               kubeops-nfs              kubeops-nfs-controller               nfs               kubeops-addon-controller
virtualization     virtualization    kubeops-virtualization   kubeops-virtualization-controller    virtualization    kubeops-addon-controller
monitoring         monitoring        kubeops-monitoring       kubeops-monitoring-controller        monitoring        kubeops-addon-controller
```

`install-operators.sh` loops this file to **build, pack, apply, set-image, and
wait** for every addon controller. Individual addons are not special-cased.

## Add a future controller

1. Implement `operator/cmd/<go_cmd_pkg>/`
2. Add manifests:
   - `operator/config/rbac/addons/kubeops-<id>-controller.yaml` (SA + ClusterRoleBinding)
   - `operator/config/manager/addons/kubeops-<id>-controller.yaml` (Deployment)
3. Append a row to `operator/config/addons/controllers.list`
4. Add an `Operator` sample row in `operator/config/samples/operators.yaml`
5. Re-run `./scripts/install-operators.sh`

## Lifecycle

1. Install script deploys CRDs + every registered `*-controller`.
2. Addons UI → Deploy creates the addon CR (e.g. `NFSProvisioner`, `Virtualization`, `Monitoring`).
3. Controller creates workload objects (Deployment, StorageClass, KubeVirt/CDI, …).
4. Uninstall deletes the CR → controller GC.
5. Controller pod stays running for the next Deploy.

## kubeops-monitoring notes

- Controller runs in **`kubeops-addon-controller`**. Workloads live in
  **`kubeops-monitoring`** (CR `spec.namespace`). Uninstall GCs owned
  Deployments/Services/PVCs/ConfigMaps; it does **not** delete the Namespace.
- Stack: Prometheus (**30446**), Grafana (**30445**), Alertmanager (**30447**),
  kube-state-metrics, node-exporter. Prefers StorageClass `kubeops-nfs-storage`;
  otherwise emptyDir with an ephemeral warning in status.
- Console embeds Grafana at `/kubeops/mon-ui/` (sidebar **Monitoring**).
- Phase 2 (out of v1): Loki, Tempo, Thanos, remote_write.

## kubeops-virtualization notes

- Controller runs in **`kubeops-addon-controller`** (with NFS). Workloads
  (KubeVirt + CDI + kubevirt-manager UI) live in **`kubeops-virtualization`**
  (upstream manifests default to `kubevirt`/`cdi` and are relocated at apply time).
  Uninstall must not delete that workload namespace (would be a no-op for the
  controller home, but keeps re-Deploy simple).
- Vendored release YAMLs live under `operator/internal/virtualization/manifests/`
  (KubeVirt `v1.10.0`, CDI `v1.66.1`, kubevirt-manager UI `v1.5.4`).
- First Deploy pulls many images; status stays `Progressing` until KubeVirt + CDI
  are `Deployed` and the UI Deployment is ready.
- UI URL (NodePort **30444**) is written to `status.uiURL` / Operators URL column.
- Set `spec.useEmulation: true` (Addons checkbox) on labs without `/dev/kvm`.
