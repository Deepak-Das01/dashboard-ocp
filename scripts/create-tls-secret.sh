#!/usr/bin/env bash
# Generate a self-signed TLS secret for the Glass UI (HTTPS NodePort).
set -euo pipefail

NS="${NS:-kubernetes-dashboard}"
SECRET_NAME="${SECRET_NAME:-dashboard-glass-tls}"
DAYS="${DAYS:-825}"
CN="${CN:-dashboard-glass}"

# Optional: pass extra SANs as env EXTRA_SAN="IP:10.0.0.1,DNS:dashboard.example.com"
EXTRA_SAN="${EXTRA_SAN:-}"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

SAN="DNS:localhost,IP:127.0.0.1,DNS:${CN}"
if [[ -n "$EXTRA_SAN" ]]; then
  SAN="${SAN},${EXTRA_SAN}"
fi

openssl req -x509 -nodes -days "$DAYS" -newkey rsa:2048 \
  -keyout "$TMP/tls.key" \
  -out "$TMP/tls.crt" \
  -subj "/CN=${CN}/O=kubernetes-dashboard-glass" \
  -addext "subjectAltName=${SAN}" >/dev/null 2>&1

kubectl -n "$NS" create secret tls "$SECRET_NAME" \
  --cert="$TMP/tls.crt" \
  --key="$TMP/tls.key" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "Secret ${NS}/${SECRET_NAME} applied (SAN: ${SAN})"
