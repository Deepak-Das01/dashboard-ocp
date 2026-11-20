# Changelog

## main — metrics packaging (post v1.2.2)

### Added
- Vendored **metrics-server v0.7.2** (`deploy/metrics-server/components.yaml`, lab `--kubelet-insecure-tls`)
- Vendored Dashboard **v2.7.0** recommended manifest (`deploy/upstream/`)
- Offline image helpers: `download-images.sh`, `load-images.sh`, `export-images.sh`
- GitHub Release **`images-v1.2.2`** hosting amd64 image archives + checksums
- `install.sh` / `uninstall.sh` wired for metrics-server and local/Release images
- `docs/METRICS_AND_IMAGES.md`

### Notes
- Product cosmetic tag **`v1.2.2`** remains frozen; use **`main`** for metrics-inclusive installs

## v1.2.2 — 1.2.2

Stable cosmetic baseline for the Liquid Glass Kubernetes Dashboard packaging.

### Changed (Liquid Glass cumulative)
- Premium macOS-inspired Liquid Glass appearance with sapphire, charcoal and teal ambient gradients
- Refined translucent panels, reflective borders and elevated overlay materials
- Improved header, namespace selector and closed/open search surfaces
- Consistent sidebar selection without clipped active corners
- Polished resource tables and glass-aligned pagination footers
- Corrected notification panel stacking and compact empty-state frosting
- Improved form alignment, floating labels and file-selection control alignment
- Coordinated YAML editor, dropdown and button styling
- Emerald workload status colours retained
- Events table text containment via safe in-cell wrapping (no table layout changes)

### Notes
- CSS-only glass packaging over upstream Kubernetes Dashboard v2.7.0
- No functional, authentication or RBAC changes in this release

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
