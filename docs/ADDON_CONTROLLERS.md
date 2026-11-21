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

## Registry (source of truth for install)

`operator/config/addons/controllers.list`

```text
# addon_id   go_cmd_pkg   image_name    deploy_name              component
nfs          nfs          kubeops-nfs   kubeops-nfs-controller   nfs
```

`install-operators.sh` loops this file to **build, pack, apply, set-image, and
wait** for every addon controller. NFS is not special-cased.

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
2. Addons UI → Deploy creates the addon CR (e.g. `NFSProvisioner`).
3. Controller creates workload objects (Deployment, StorageClass, …).
4. Uninstall deletes the CR → controller GC.
5. Controller pod stays running for the next Deploy.
