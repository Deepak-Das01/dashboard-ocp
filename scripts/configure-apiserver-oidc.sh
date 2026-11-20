#!/usr/bin/env bash
# Patch kubeadm kube-apiserver static pod with OIDC flags (lab / control-plane node).
set -euo pipefail

ISSUER="${OIDC_ISSUER_URL:-}"
CLIENT_ID="${OIDC_CLIENT_ID:-kube-dashboard}"
MANIFEST="${APISERVER_MANIFEST:-/etc/kubernetes/manifests/kube-apiserver.yaml}"

# Kubernetes 1.34+ requires HTTPS issuer URLs (http is rejected).
if [[ -z "$ISSUER" ]]; then
  NODE_IP="$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')"
  ISSUER="https://${NODE_IP}:32056/dex"
fi
OIDC_CA_FILE="${OIDC_CA_FILE:-/etc/kubernetes/pki/dex-ca.crt}"

if [[ ! -f "$MANIFEST" ]]; then
  echo "Missing $MANIFEST — run on a kubeadm control-plane node" >&2
  exit 1
fi

cp -a "$MANIFEST" "${MANIFEST}.bak.$(date +%s)"

python3 - "$MANIFEST" "$ISSUER" "$CLIENT_ID" <<'PY'
import sys
path, issuer, client_id = sys.argv[1], sys.argv[2], sys.argv[3]
text = open(path).read()
ca = __import__('os').environ.get('OIDC_CA_FILE', '/etc/kubernetes/pki/dex-ca.crt')
flags = [
    f"    - --oidc-issuer-url={issuer}\n",
    f"    - --oidc-client-id={client_id}\n",
    "    - --oidc-username-claim=email\n",
    "    - --oidc-groups-claim=groups\n",
    "    - --oidc-username-prefix=-\n",
    f"    - --oidc-ca-file={ca}\n",
]
# Remove existing oidc flags first
lines = [ln for ln in text.splitlines(True) if "--oidc-" not in ln]
# Insert after --authorization-mode line
out = []
inserted = False
for ln in lines:
    out.append(ln)
    if (not inserted) and "--authorization-mode=" in ln:
        out.extend(flags)
        inserted = True
if not inserted:
    # fallback: after kube-apiserver command name
    out = []
    for ln in lines:
        out.append(ln)
        if ln.strip() == "- kube-apiserver" and not inserted:
            out.extend(flags)
            inserted = True
open(path, "w").write("".join(out))
print(f"Patched {path}")
print(f"  issuer={issuer}")
print(f"  client-id={client_id}")
PY

echo "Waiting for kube-apiserver to restart..."
sleep 8
for i in $(seq 1 60); do
  if kubectl get --raw=/readyz >/dev/null 2>&1; then
    echo "kube-apiserver ready"
    grep -n 'oidc-' "$MANIFEST" || true
    exit 0
  fi
  sleep 2
done
echo "WARNING: apiserver not ready yet — check: crictl pods | grep apiserver" >&2
exit 1
