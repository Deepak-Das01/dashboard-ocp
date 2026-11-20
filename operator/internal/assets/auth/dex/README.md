# Embedded Dex (Phase B)

When `spec.auth.mode: oidc` and `spec.auth.oidc.provider: dex`, the operator
renders Dex manifests from templates in the controller (not static YAML here)
and stores the one-time `kubeadmin` password in a Secret.

Login UI uses the embedded glass theme at `themes/kube-dashboard/styles.css`
(console palette: ambient blue/teal gradients, frosted panels, Inter). The
auth operator mounts it as ConfigMap `dex-theme` and sets `frontend.theme:
kube-dashboard`.

API server OIDC flags are still required for `kubectl` — see docs/OPERATOR_AND_AUTH.md.
