# Kubernetes Dashboard Glass UI

**Product release: `v1.2` (version `1.2.0`)**

A translucent, macOS-inspired **glass theme** for the official [Kubernetes Dashboard](https://github.com/kubernetes/dashboard), delivered as a small nginx reverse-proxy that injects custom CSS—without rebuilding the upstream Dashboard image.

| Item | Value |
|------|--------|
| Release | **v1.2** |
| Version | **1.2.0** |
| Upstream Dashboard | `v2.7.0` (configurable) |
| Glass HTTPS NodePort | **`30443`** (use this) |
| Glass HTTP NodePort | `30080` (redirects to HTTPS only) |
| Namespace | `kubernetes-dashboard` |

Repository: [https://github.com/Deepak-Das01/dashboard-ocp](https://github.com/Deepak-Das01/dashboard-ocp)

---

## What this product is

1. Installs (or reuses) the **official Kubernetes Dashboard** recommended manifests.
2. Deploys **dashboard-glass** — nginx that:
   - Proxies to `kubernetes-dashboard` in-cluster (HTTPS).
   - Injects `glass-theme.css` into the Dashboard HTML.
3. Creates optional **dashboard-admin** ServiceAccount + token for login.
4. Creates a **self-signed TLS** secret for HTTPS access to the glass UI.

The original Dashboard Service (typically NodePort / ClusterIP on 443) remains available. The glass UI is an additional front door.

---

## Architecture

```
Browser
   │
   ├─ http://<node>:30080  ──301──► https://<node>:30443/
   └─ https://<node>:30443/ ──────────────────┐
                                              ▼
                                   dashboard-glass (nginx TLS)
                                    │  injects glass CSS
                                    ▼
                           kubernetes-dashboard:443
                                    │
                                    ▼
                              Kubernetes API
```

---

## Repository layout

```
dashboard-ocp/
├── README.md                 # Product documentation (this file)
├── VERSION                   # 1.0.0
├── RELEASE                   # v1
├── deploy/
│   ├── kustomization.yaml    # Glass UI + RBAC
│   ├── glass/
│   │   ├── glass-theme.css   # macOS glass theme
│   │   ├── nginx.conf        # reverse proxy + CSS injection
│   │   ├── deployment.yaml
│   │   └── service.yaml
│   └── rbac/
│       └── dashboard-admin.yaml
└── scripts/
    ├── install.sh            # Full install on any cluster
    ├── uninstall.sh
    ├── create-tls-secret.sh
    └── get-token.sh
```

---

## Prerequisites

On the machine where you run install:

- `kubectl` configured for the target cluster (`kubectl cluster-info` works)
- `openssl`
- Cluster can pull images:
  - `kubernetesui/dashboard:v2.7.0`
  - `kubernetesui/metrics-scraper:v1.0.8`
  - `nginx:1.27-alpine`
- Outbound HTTPS to GitHub (to fetch upstream Dashboard YAML), **or** provide a local copy via `DASHBOARD_URL`

Recommended cluster baseline (example):

- Kubernetes 1.24+ (tested with 1.34)
- CNI installed (e.g. Flannel / Calico)
- Control-plane schedulable **or** remove Dashboard tolerations as needed

---

## Quick start (any cluster)

```bash
git clone https://github.com/Deepak-Das01/dashboard-ocp.git
cd dashboard-ocp

chmod +x scripts/*.sh
./scripts/install.sh
```

When finished, open the **HTTPS** glass UI:

```text
https://<node-ip>:30443/
```

HTTP on `:30080` only redirects to HTTPS so Dashboard login does not show
“Insecure access detected.” Accept the self-signed certificate warning once.

### Login token

```bash
./scripts/get-token.sh
```

Paste the token into the Dashboard **Token** login form.

### Local access via port-forward

```bash
kubectl -n kubernetes-dashboard port-forward svc/dashboard-glass 8443:443
# open https://127.0.0.1:8443/  (accept self-signed cert once)
```

---

## Step-by-step deploy on another cluster

### 1. Point kubectl at the target cluster

```bash
export KUBECONFIG=/path/to/target.kubeconfig
kubectl get nodes
```

### 2. Clone this product repo

```bash
git clone https://github.com/Deepak-Das01/dashboard-ocp.git
cd dashboard-ocp
chmod +x scripts/*.sh
```

### 3. (Optional) Customize before install

| Variable | Default | Purpose |
|----------|---------|---------|
| `NS` | `kubernetes-dashboard` | Namespace |
| `DASHBOARD_VERSION` | `v2.7.0` | Upstream Dashboard tag |
| `DASHBOARD_URL` | GitHub recommended.yaml | Override manifest URL/path |
| `SKIP_UPSTREAM` | `false` | `true` if Dashboard already installed |
| `ENABLE_INSECURE_LOGIN` | `true` | Patch upstream with `--enable-insecure-login` |
| `EXTRA_SAN` | auto node InternalIP | Extra TLS SAN, e.g. `IP:10.0.0.5,DNS:dash.example.com` |

Examples:

```bash
# Dashboard already present
SKIP_UPSTREAM=true ./scripts/install.sh

# Custom TLS SANs for your VIP / DNS name
EXTRA_SAN="IP:192.168.1.50,DNS:k8s-dash.lab.local" ./scripts/install.sh

# Pin a different upstream Dashboard version
DASHBOARD_VERSION=v2.7.0 ./scripts/install.sh
```

### 4. Run install

```bash
./scripts/install.sh
```

This will:

1. `kubectl apply` the official recommended Dashboard YAML (unless skipped).
2. Wait for Dashboard + metrics-scraper rollouts.
3. Optionally enable insecure login for lab HTTP use.
4. Create `dashboard-glass-tls` Secret.
5. `kubectl apply -k deploy/` (glass ConfigMaps, Deployment, Service, admin RBAC).
6. Wait for `dashboard-glass` to become Ready.

### 5. Verify

```bash
kubectl -n kubernetes-dashboard get pods,svc -l app=dashboard-glass
kubectl -n kubernetes-dashboard get pods -l k8s-app=kubernetes-dashboard

curl -s -o /dev/null -w "%{http_code}\n" http://<node-ip>:30080/
curl -sk -o /dev/null -w "%{http_code}\n" https://<node-ip>:30443/
```

Expect HTTP `200` and HTML that references `/glass-theme.css`.

### 6. Sign in

```bash
./scripts/get-token.sh
```

Open the glass URL → **Token** → paste → **Sign in**.

---

## Manual install (without install.sh)

```bash
# 1) Upstream dashboard
kubectl apply -f https://raw.githubusercontent.com/kubernetes/dashboard/v2.7.0/aio/deploy/recommended.yaml

# 2) TLS for glass proxy
EXTRA_SAN="IP:<your-node-ip>" ./scripts/create-tls-secret.sh

# 3) Glass UI + admin SA
kubectl apply -k deploy/

# 4) Token
./scripts/get-token.sh
```

---

## Uninstall

```bash
# Remove only Glass UI + admin RBAC + TLS secret
./scripts/uninstall.sh

# Also remove upstream Kubernetes Dashboard
REMOVE_UPSTREAM=true ./scripts/uninstall.sh
```

---

## Configuration reference

### Services

| Service | Type | Ports |
|---------|------|-------|
| `kubernetes-dashboard` | usually ClusterIP or NodePort (upstream) | 443 → 8443 |
| `dashboard-glass` | NodePort | `80:30080`, `443:30443` |

Change NodePorts by editing `deploy/glass/service.yaml` before apply.

### Labels (v1)

Glass resources are labeled:

- `app.kubernetes.io/version: "1.1.0"`
- `dashboard.ocp/release: v1`
- `app.kubernetes.io/part-of: kubernetes-dashboard-glass`

### Theme customization

Edit `deploy/glass/glass-theme.css`, then:

```bash
kubectl apply -k deploy/
kubectl -n kubernetes-dashboard rollout restart deploy/dashboard-glass
```

### Nginx proxy

Edit `deploy/glass/nginx.conf` (CSS injection via `sub_filter`), then re-apply as above.

---

## Security notes

- `dashboard-admin` is bound to **cluster-admin**. Use only in labs, or replace with a least-privilege Role/ClusterRole.
- Self-signed TLS is for convenience; replace `dashboard-glass-tls` with a real certificate for production.
- `--enable-insecure-login` is intended for lab HTTP access. Prefer HTTPS or localhost port-forward in shared environments.
- Do not commit private keys; `scripts/create-tls-secret.sh` generates them on the target cluster.

---

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| Glass pod CrashLoop (nginx) | Ensure `dashboard-glass-tls` exists; run `./scripts/create-tls-secret.sh` |
| Blank / unthemed UI | Confirm HTML contains `glass-theme.css`; hard-refresh browser |
| “Insecure access detected” | Use `https://…:30443` or `kubectl port-forward` to `127.0.0.1` |
| Token login fails | Re-run `./scripts/get-token.sh`; confirm RBAC with `kubectl auth can-i --list --as=system:serviceaccount:kubernetes-dashboard:dashboard-admin` |
| Upstream images pull errors | Configure registry mirrors / imagePullSecrets |
| NodePort unreachable | Check cloud security groups / firewall for `30080` and `30443` |

Logs:

```bash
kubectl -n kubernetes-dashboard logs deploy/dashboard-glass
kubectl -n kubernetes-dashboard logs deploy/kubernetes-dashboard
```

---

## Versioning

| File | Meaning |
|------|---------|
| `RELEASE` | Product release label (`v1.1`) |
| `VERSION` | Semver of this Glass UI packaging (`1.1.0`) |
| Upstream | Kubernetes Dashboard tag via `DASHBOARD_VERSION` |

This repository versions the **Glass UI packaging and docs**, not a fork of the Dashboard application code.

---

## License

Upstream Kubernetes Dashboard is licensed by the Kubernetes Authors (Apache 2.0).  
Glass theme CSS, nginx packaging, scripts, and documentation in this repository are provided for use with that Dashboard.
