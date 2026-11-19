# Changelog

## v1.1 — 1.1.0

### Changed
- HTTPS-only glass UI (HTTP NodePort redirects to HTTPS `:30443`)
- Header **kubernetes** wordmark forced to white
- Header toolbar Material icons forced to white/default
- Docs updated for HTTPS-first access

## v1 — 1.0.0

### Added
- Glass UI nginx reverse-proxy for official Kubernetes Dashboard v2.7.0
- macOS-style translucent `glass-theme.css`
- Kustomize package under `deploy/`
- `dashboard-admin` ServiceAccount + ClusterRoleBinding for token login
- Install / uninstall / TLS / get-token scripts
- Product README with multi-cluster deployment steps

### Ports
- HTTP NodePort `30080` (redirect → HTTPS)
- HTTPS NodePort `30443`
