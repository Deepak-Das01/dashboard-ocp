# Operators + Auth

## Naming

All product operators use the **`kubeops-`** prefix. Name = namespace. Role is `console` or `oidc`.

| Operator | Namespace | Role (`spec.component`) |
|----------|-----------|-------------------------|
| `kubeops-console` | `kubeops-console` | `console` (also hosts `kube-dashboard` + metrics-scraper pods) |
| `kubeops-authentication` | `kubeops-authentication` | `oidc` (operator + Dex in same ns) |

```bash
kubectl get operators
# NAME                     COMPONENT   PHASE   ...
# kubeops-console          console     Ready
# kubeops-authentication   oidc        Ready
```

## Single-script install (recommended)

```bash
./scripts/install-operators.sh
```

Default path: both kubeops operators + `ClusterOIDC` (Dex) + console `auth.mode: oidc`.

```bash
kubectl get operators
kubectl get kubedashboard,clusteroidc
kubectl -n kubeops-console get pods
kubectl -n kubeops-authentication get pods
```

### Browser login (OIDC / Dex)

```text
https://<node-ip>:30443/   → redirects to Dex
Username: kubeadmin
```

```bash
kubectl -n kubeops-authentication get secret kubeadmin-oidc-bootstrap \
  -o jsonpath='{.data.password}' | base64 -d; echo
```

### Flags

| Env | Default | Meaning |
|-----|---------|---------|
| `AUTH_MODE` | `oidc` | `oidc` \| `secret` \| `token` |
| `LOAD_IMAGES` | `true` | Download/import dashboard images |
| `BUILD_OPERATORS` | `true` | Build both kubeops binaries |
| `PACK_IMAGES` | `true` | Pack + `ctr import` |

```bash
./scripts/install-operators.sh
AUTH_MODE=secret ./scripts/install-operators.sh
LOAD_IMAGES=false ./scripts/install-operators.sh
```

### Optional: apiserver OIDC

```bash
./scripts/configure-apiserver-oidc.sh
# or:
./scripts/install-phase-c-oidc.sh
```

## Components

| Operator | CR | Role |
|----------|-----|------|
| `kubeops-console` | `KubeDashboard` | Dashboard + metrics + auth |
| `kubeops-authentication` | `ClusterOIDC` | Dex + oauth2 client secrets |
