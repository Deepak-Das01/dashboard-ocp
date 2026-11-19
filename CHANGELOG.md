# Changelog

## v1.2.1 — 1.2.1

### Fixed (Phase 2 fit & finish)
- Nested namespace/search control surfaces (single border/background owner)
- Fragmented table row borders (row-level separators; cell borders removed)
- Clipped sidebar selection (padding model + 2px inset indicator)
- Aggressive text wrapping on chips/cells
- Floating-label / nested outline conflicts on Material form fields
- Restored restrained glass translucency on header/sidebar/cards

## v1.2 — 1.2.0

### Changed
- Enterprise Glass UI refinement (design tokens, restrained glass, denser tables)
- Header/sidebar/search/chip/dialog/settings polish
- Removed external Google Fonts CDN (system font stack for air-gapped use)
- Stronger opaque table surfaces for readability

### Notes
- Workload pie→donut conversion not applied (chart internals not themeable without Angular source)

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
