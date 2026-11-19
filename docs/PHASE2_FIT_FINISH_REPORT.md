# Phase 2 — Fit & Finish Report (v1.2.1)

## 1. Original visual defects identified

| ID | Defect | Severity |
|----|--------|----------|
| D1 | Namespace selector nested backgrounds/borders | High |
| D2 | Search field nested surfaces / misaligned icon text | High |
| D3 | Clipped sidebar selected item / thick active indicator | High |
| D4 | Fragmented table row borders | Critical |
| D5 | Misaligned / uneven table columns & wrapping | Critical |
| D6 | Heavy / broken label chips | High |
| D7 | Settings floating labels overlapping / nested outlines | High |
| D8 | Excessively flat/opaque surfaces vs intended glass | Medium |
| D9 | Header icon/wordmark contrast issues | Medium |
| D10 | Competing nested card borders | Medium |

## 2. Root causes

| ID | Root cause |
|----|------------|
| D1/D2 | Theme painted `background`+`border` on `.kd-namespace-select`, `-input`, `-input-container`, and `.mat-form-field-flex` simultaneously |
| D3 | Nav items used full decorative borders + margins colliding with drawer scrollbar/overflow |
| D4 | `border-bottom` applied to every `.mat-cell` / `.mat-header-cell` instead of the flex row |
| D5 | Mixed cell paddings + `overflow-wrap`/`word-break` inherited poorly for long names/images |
| D6 | Chip styles allowed full-width / aggressive wrapping without nowrap+ellipsis |
| D7 | Form theme styled both Material outline layers and flex fill, fighting floating labels |
| D8 | Prior tokens used near-opaque table/card fills and weak ambient gradients |

## 3. Corrective implementation

- Consolidated design tokens (navy/teal restrained glass).
- Namespace/search: transparent nested wrappers; **one** visible surface on container.
- Tables: preserve Material flex rows; **one** separator on `.mat-row` / `.mat-header-row`; shared cell padding; scoped horizontal scroll via `display:block` + `min-width:max-content` on rows.
- Sidebar: quieter selection, **2px** inset indicator, no item border box growth.
- Forms: single flex surface; hide outline/underline artifacts.
- Chips: compact nowrap ellipsis chips with flex gap wrapping between chips.
- Glass restored on header/sidebar/cards; tables stay denser/more opaque for readability.

## 4. Selectors / components touched

Header/toolbar, namespace select, search, actionbar, sidenav/nav items, mat-card, mat-table/header/row/cell, mat-chip, mat-form-field*, mat-slide-toggle, mat-slider, mat-dialog/menu/tooltip, paginator, login card.

## 5. Files changed

- `deploy/glass/glass-theme.css` (rewrite)
- `VERSION`, `RELEASE`, `CHANGELOG.md`
- Manifest labels (`1.2.1`)
- `docs/PHASE2_FIT_FINISH_REPORT.md` (this file)

## 6. Design tokens

See `:root` in `glass-theme.css` (`--app-background`, `--surface-*`, `--border-*`, `--text-*`, `--accent-*`, `--status-*`, layout heights/widths, radii, spacing, motion).

## 7. Screenshots

Browser visual capture over self-signed HTTPS was **blocked** in the automation browser (`chrome-error://chromewebdata`) even after `Security.setIgnoreCertificateErrors`. Manual verification required on:

`https://192.168.222.141:30443/` (accept cert once)

## 8. Browser / viewport validation

| Check | Result |
|-------|--------|
| Theme ConfigMap deployed | Pass |
| HTTPS 200 + CSS injection | Pass |
| HTTP→HTTPS 301 | Pass (prior phase) |
| Automated multi-viewport screenshots | **Blocked** (cert/browser) |
| Zoom 100/125/150 | Not automated |

## 9. Functional regression checks

| Check | Result |
|-------|--------|
| Proxy/nginx still serves Dashboard | Pass (HTTP 200) |
| No API/RBAC/manifest logic changes | Pass (CSS/labels/docs only) |
| Login/token flows | Not re-exercised in automation this pass |

## 10. Remaining limitations

| Item | Status | Notes |
|------|--------|-------|
| Pie→donut charts | Blocked | Requires Angular/chart source |
| Perfect per-resource column width policy | Partial | Upstream uses `col-min-*`; avoided brittle nth-child |
| Automated pixel screenshots | Blocked | Self-signed cert in Cursor browser |
| Further reducing `!important` | Partial | Still required vs compiled Material CSS |

## 11. Rollback

```bash
cd /root/dashboard-ocp
git checkout v1.2.0 -- deploy/glass/glass-theme.css
kubectl apply -k deploy/
kubectl -n kubernetes-dashboard rollout restart deploy/dashboard-glass
```

## Issue classification

| ID | Result |
|----|--------|
| D1 Nested namespace surfaces | Fixed (CSS) |
| D2 Nested search surfaces | Fixed (CSS) |
| D3 Clipped nav / thick indicator | Fixed (CSS) |
| D4 Fragmented table borders | Fixed (CSS) |
| D5 Table wrap/alignment | Partially Fixed (improved defaults; resource-specific widths limited by injection architecture) |
| D6 Label chips | Fixed (CSS) |
| D7 Settings floating labels | Partially Fixed (outline suppressed + flex ownership; needs visual confirm after login) |
| D8 Flat/opaque glass | Fixed (CSS tokens) |
| D9 Header contrast | Fixed (CSS) |
| D10 Nested card borders | Partially Fixed |
