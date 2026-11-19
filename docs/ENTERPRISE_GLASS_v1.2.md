# Enterprise Glass UI v1.2 — Implementation notes

## Architecture
- Upstream Dashboard: `kubernetesui/dashboard:v2.7.0`
- Theme delivery: nginx reverse proxy injects `/glass-theme.css` from ConfigMap `dashboard-glass-css`
- No Angular source changes; CSS override layer only

## Design tokens
Centralized in `:root` inside `deploy/glass/glass-theme.css` (`--bg-*`, `--surface-*`, `--accent-*`, `--text-*`, `--status-*`, radii, spacing, transitions).

## Limitations
- Chart engine (canvas/SVG internals) cannot be converted to donuts via CSS alone without frontend source changes.
- Some Material internals still require targeted overrides (`!important`) due to view encapsulation and compiled stylesheets.

## Rollback
```bash
git checkout v1.1.0 -- deploy/glass/glass-theme.css
kubectl apply -k deploy/
kubectl -n kubernetes-dashboard rollout restart deploy/dashboard-glass
```
Or restore previous ConfigMap from git history / cluster backup.
