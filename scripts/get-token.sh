#!/usr/bin/env bash
# Print a Bearer token for Dashboard login (dashboard-admin SA).
set -euo pipefail

NS="${NS:-kubernetes-dashboard}"
SA="${SA:-dashboard-admin}"

if kubectl -n "$NS" get sa "$SA" >/dev/null 2>&1; then
  if kubectl create token -h >/dev/null 2>&1; then
    kubectl -n "$NS" create token "$SA" --duration="${DURATION:-24h}"
  else
    SECRET="$(kubectl -n "$NS" get sa "$SA" -o jsonpath='{.secrets[0].name}' 2>/dev/null || true)"
    if [[ -z "$SECRET" ]]; then
      SECRET="${SA}-token"
    fi
    kubectl -n "$NS" get secret "$SECRET" -o jsonpath='{.data.token}' | base64 -d
    echo
  fi
else
  echo "ServiceAccount ${NS}/${SA} not found. Apply deploy/rbac/dashboard-admin.yaml first." >&2
  exit 1
fi
