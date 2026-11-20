#!/usr/bin/env bash
# Phase C: wire Dex (public) + apiserver OIDC + oauth2-proxy in front of kube-dashboard.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NS_DASH="${NS_DASH:-kubeops-console}"
NS_OIDC="${NS_OIDC:-kubeops-authentication}"
NODE_IP="$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')"
# K8s 1.34+ requires HTTPS OIDC issuer
ISSUER="https://${NODE_IP}:32056/dex"
REDIRECT="https://${NODE_IP}:30443/oauth2/callback"
CLIENT_ID="kube-dashboard"
DEX_NODEPORT=32056

echo "==> Node ${NODE_IP}"
echo "    issuer ${ISSUER}"
echo "    redirect ${REDIRECT}"

# --- secrets / password ---
if ! kubectl -n "$NS_OIDC" get secret kubeadmin-oidc-bootstrap >/dev/null 2>&1; then
  echo "Missing kubeadmin-oidc-bootstrap — apply ClusterOIDC first" >&2
  exit 1
fi
PASS="$(kubectl -n "$NS_OIDC" get secret kubeadmin-oidc-bootstrap -o jsonpath='{.data.password}' | base64 -d)"
HASH="$(kubectl -n "$NS_OIDC" get secret kubeadmin-oidc-bootstrap -o jsonpath='{.data.passwordHash}' | base64 -d)"
USER="$(kubectl -n "$NS_OIDC" get secret kubeadmin-oidc-bootstrap -o jsonpath='{.data.username}' | base64 -d)"

CLIENT_SECRET="$(kubectl -n "$NS_OIDC" get secret oauth2-proxy-oidc -o jsonpath='{.data.client-secret}' 2>/dev/null | base64 -d || true)"
COOKIE_SECRET="$(kubectl -n "$NS_OIDC" get secret oauth2-proxy-oidc -o jsonpath='{.data.cookie-secret}' 2>/dev/null | base64 -d || true)"
if [[ -z "$CLIENT_SECRET" ]]; then
  CLIENT_SECRET="$(openssl rand -hex 16)"
fi
if [[ -z "$COOKIE_SECRET" ]]; then
  COOKIE_SECRET="$(openssl rand -base64 32 | tr -d '\n' | head -c 32)"
fi

kubectl -n "$NS_OIDC" create secret generic oauth2-proxy-oidc \
  --from-literal=client-id="$CLIENT_ID" \
  --from-literal=client-secret="$CLIENT_SECRET" \
  --from-literal=cookie-secret="$COOKIE_SECRET" \
  --dry-run=client -o yaml | kubectl apply -f -

# Also copy into dashboard ns for the sidecar
kubectl -n "$NS_DASH" create secret generic oauth2-proxy-oidc \
  --from-literal=client-id="$CLIENT_ID" \
  --from-literal=client-secret="$CLIENT_SECRET" \
  --from-literal=cookie-secret="$COOKIE_SECRET" \
  --from-literal=issuer-url="$ISSUER" \
  --from-literal=redirect-url="$REDIRECT" \
  --dry-run=client -o yaml | kubectl apply -f -

# --- Dex config (HTTPS; skipApproval) ---
cat > /tmp/dex-config.yaml <<EOF
issuer: ${ISSUER}
storage:
  type: memory
web:
  https: 0.0.0.0:5556
  tlsCert: /etc/dex/tls/tls.crt
  tlsKey: /etc/dex/tls/tls.key
oauth2:
  skipApprovalScreen: true
staticClients:
  - id: ${CLIENT_ID}
    secret: ${CLIENT_SECRET}
    name: kube-dashboard
    redirectURIs:
      - ${REDIRECT}
enablePasswordDB: true
staticPasswords:
  - email: "${USER}"
    username: "${USER}"
    userID: "kubeadmin"
    hash: "${HASH}"
EOF

kubectl -n "$NS_OIDC" create configmap dex --from-file=config.yaml=/tmp/dex-config.yaml -o yaml --dry-run=client | kubectl apply -f -

# Dex Service NodePort (HTTPS)
kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: dex
  namespace: ${NS_OIDC}
  labels:
    app: dex
spec:
  type: NodePort
  selector:
    app: dex
  ports:
    - name: https
      port: 5556
      targetPort: 5556
      nodePort: ${DEX_NODEPORT}
EOF

kubectl -n "$NS_OIDC" rollout restart deploy/dex
kubectl -n "$NS_OIDC" rollout status deploy/dex --timeout=120s

# --- RBAC for OIDC user (email claim) ---
kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: oidc-kubeadmin
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
  - kind: User
    name: ${USER}
    apiGroup: rbac.authorization.k8s.io
EOF

# --- Apiserver OIDC ---
OIDC_ISSUER_URL="$ISSUER" OIDC_CLIENT_ID="$CLIENT_ID" "$ROOT/scripts/configure-apiserver-oidc.sh"

# --- Long-lived dashboard-admin token (injected after oauth2 gate) ---
# KD's browser client does not reliably consume oauth2 ID tokens; gate with
# oauth2-proxy then inject the cluster-admin SA bearer into Dashboard API calls.
SA_TOKEN="$(kubectl -n "$NS_DASH" create token dashboard-admin --duration=8760h 2>/dev/null || true)"
if [[ -z "$SA_TOKEN" ]]; then
  SA_TOKEN="$(kubectl -n "$NS_DASH" get secret kube-dashboard-sa-token -o jsonpath='{.data.token}' 2>/dev/null | base64 -d || true)"
fi
if [[ -z "$SA_TOKEN" ]]; then
  echo "Unable to mint dashboard-admin token" >&2
  exit 1
fi
kubectl -n "$NS_DASH" create secret generic kube-dashboard-sa-token \
  --from-literal=token="$SA_TOKEN" \
  --dry-run=client -o yaml | kubectl apply -f -

# --- nginx: auth_request gate + SA token inject (NodePort-safe redirects) ---
cat > /tmp/nginx-oidc.conf <<NGINX
worker_processes auto;
error_log /var/log/nginx/error.log warn;
pid /tmp/nginx.pid;

events { worker_connections 1024; }

http {
  include       /etc/nginx/mime.types;
  default_type  application/octet-stream;
  sendfile on;
  keepalive_timeout 65;
  client_max_body_size 32m;
  proxy_buffer_size 128k;
  proxy_buffers 4 256k;
  proxy_busy_buffers_size 256k;
  sub_filter_once off;
  sub_filter_types text/html;
  absolute_redirect off;
  port_in_redirect off;

  upstream oauth2 { server 127.0.0.1:4180; }
  upstream kd { server 127.0.0.1:9090; }

  server {
    listen 8080;
    server_name _;
    return 301 https://\$host:30443\$request_uri;
  }

  server {
    listen 8443 ssl;
    server_name _;
    ssl_certificate     /etc/nginx/certs/tls.crt;
    ssl_certificate_key /etc/nginx/certs/tls.key;
    ssl_protocols TLSv1.2 TLSv1.3;

    location /oauth2/ {
      proxy_pass http://oauth2;
      proxy_set_header Host \$host;
      proxy_set_header X-Real-IP \$remote_addr;
      proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
      proxy_set_header X-Forwarded-Proto https;
      proxy_set_header X-Forwarded-Host \$host:30443;
      proxy_set_header X-Forwarded-Port 30443;
    }

    location = /oauth2/auth {
      proxy_pass http://oauth2;
      proxy_set_header Host \$host;
      proxy_set_header X-Real-IP \$remote_addr;
      proxy_set_header X-Forwarded-Uri \$request_uri;
      proxy_set_header X-Forwarded-Proto https;
      proxy_set_header X-Forwarded-Host \$host:30443;
      proxy_set_header X-Forwarded-Port 30443;
      proxy_set_header Content-Length "";
      proxy_pass_request_body off;
    }

    location @login {
      return 302 /oauth2/start?rd=https://\$host:30443\$request_uri;
    }

    location = /theme.css {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      alias /usr/share/nginx/html/theme.css;
      default_type text/css;
      add_header Cache-Control "no-cache";
    }

    location = /nav-accordion.js {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      alias /usr/share/nginx/html/nav-accordion.js;
      default_type application/javascript;
      add_header Cache-Control "no-cache";
    }

    location / {
      auth_request /oauth2/auth;
      error_page 401 = @login;

      proxy_pass http://kd;
      proxy_http_version 1.1;
      proxy_set_header Host \$host;
      proxy_set_header X-Real-IP \$remote_addr;
      proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
      proxy_set_header X-Forwarded-Proto https;
      proxy_set_header Accept-Encoding "";
      proxy_set_header Authorization "Bearer ${SA_TOKEN}";

      sub_filter '</head>' '<link rel="stylesheet" href="/theme.css?v=kube-dashboard-oidc"><style>html{color-scheme:dark}</style></head>';
      sub_filter '<body>' '<body class="kd-theme">';
      sub_filter '</body>' '<script src="/nav-accordion.js?v=kube-dashboard-oidc" defer></script></body>';
    }
  }
}
NGINX

kubectl -n "$NS_DASH" create configmap kube-dashboard-nginx --from-file=nginx.conf=/tmp/nginx-oidc.conf -o yaml --dry-run=client | kubectl apply -f -

# Patch deployment: oauth2-proxy as auth gate (static 202), whitelist NodePort
kubectl -n "$NS_DASH" get deploy kube-dashboard -o json > /tmp/kd-deploy.json
python3 <<PY
import json
p="/tmp/kd-deploy.json"
d=json.load(open(p))
pods=d["spec"]["template"]["spec"]
containers=pods["containers"]
containers[:]=[c for c in containers if c["name"]!="oauth2-proxy"]
vols=pods.setdefault("volumes", [])
vols[:]=[v for v in vols if v.get("name")!="oauth2-proxy-oidc"]
vols.append({
  "name": "oauth2-proxy-oidc",
  "secret": {"secretName": "oauth2-proxy-oidc"},
})
oauth2={
  "name": "oauth2-proxy",
  "image": "quay.io/oauth2-proxy/oauth2-proxy:v7.7.1",
  "imagePullPolicy": "IfNotPresent",
  "args": [
    "--http-address=0.0.0.0:4180",
    "--metrics-address=0.0.0.0:44180",
    "--provider=oidc",
    "--upstream=static://202",
  ],
  "env": [
    {"name": "OAUTH2_PROXY_CLIENT_ID", "valueFrom": {"secretKeyRef": {"name": "oauth2-proxy-oidc", "key": "client-id"}}},
    {"name": "OAUTH2_PROXY_CLIENT_SECRET", "valueFrom": {"secretKeyRef": {"name": "oauth2-proxy-oidc", "key": "client-secret"}}},
    {"name": "OAUTH2_PROXY_COOKIE_SECRET", "valueFrom": {"secretKeyRef": {"name": "oauth2-proxy-oidc", "key": "cookie-secret"}}},
    {"name": "OAUTH2_PROXY_OIDC_ISSUER_URL", "valueFrom": {"secretKeyRef": {"name": "oauth2-proxy-oidc", "key": "issuer-url"}}},
    {"name": "OAUTH2_PROXY_REDIRECT_URL", "valueFrom": {"secretKeyRef": {"name": "oauth2-proxy-oidc", "key": "redirect-url"}}},
    {"name": "OAUTH2_PROXY_EMAIL_DOMAINS", "value": "*"},
    {"name": "OAUTH2_PROXY_COOKIE_SECURE", "value": "true"},
    {"name": "OAUTH2_PROXY_COOKIE_SAMESITE", "value": "lax"},
    {"name": "OAUTH2_PROXY_COOKIE_DOMAINS", "value": "${NODE_IP}"},
    {"name": "OAUTH2_PROXY_WHITELIST_DOMAINS", "value": "${NODE_IP}:30443,${NODE_IP}"},
    {"name": "OAUTH2_PROXY_PROMPT", "value": "login"},
    {"name": "OAUTH2_PROXY_PASS_AUTHORIZATION_HEADER", "value": "false"},
    {"name": "OAUTH2_PROXY_PASS_ACCESS_TOKEN", "value": "false"},
    {"name": "OAUTH2_PROXY_SET_AUTHORIZATION_HEADER", "value": "false"},
    {"name": "OAUTH2_PROXY_OIDC_GROUPS_CLAIM", "value": "groups"},
    {"name": "OAUTH2_PROXY_SCOPE", "value": "openid email profile groups"},
    {"name": "OAUTH2_PROXY_SKIP_PROVIDER_BUTTON", "value": "true"},
    {"name": "OAUTH2_PROXY_REVERSE_PROXY", "value": "true"},
    {"name": "OAUTH2_PROXY_SSL_INSECURE_SKIP_VERIFY", "value": "true"},
    {"name": "OAUTH2_PROXY_INSECURE_OIDC_ALLOW_UNVERIFIED_EMAIL", "value": "true"},
    {"name": "OAUTH2_PROXY_PROVIDER_DISPLAY_NAME", "value": "Dex"},
  ],
  "ports": [{"name": "oauth2", "containerPort": 4180}],
  "resources": {"requests": {"cpu": "20m", "memory": "32Mi"}, "limits": {"cpu": "200m", "memory": "128Mi"}},
}
containers.append(oauth2)
assert any(c["name"]=="proxy" for c in containers)
assert any(c["name"]=="dashboard" for c in containers)
json.dump(d, open(p,"w"))
print("patched deployment json")
PY
kubectl apply -f /tmp/kd-deploy.json
kubectl -n "$NS_DASH" rollout status deploy/kube-dashboard --timeout=180s

# Update ClusterOIDC status fields via annotate/patch message
kubectl patch clusteroidc cluster --type=merge -p "{\"status\":{\"apiServerOIDC\":\"configured\",\"issuerURL\":\"${ISSUER}\",\"message\":\"Phase C: Dex+oauth2-proxy+apiserver OIDC wired\",\"phase\":\"Ready\"}}" --subresource=status 2>/dev/null || true

echo
echo "Phase C installed."
echo "  Console: https://${NODE_IP}:30443/"
echo "  Dex:     ${ISSUER}"
echo "  Login:   username ${USER}"
echo "  Password: (from secret ${NS_OIDC}/kubeadmin-oidc-bootstrap)"
echo "    kubectl -n ${NS_OIDC} get secret kubeadmin-oidc-bootstrap -o jsonpath='{.data.password}' | base64 -d; echo"
echo
echo "Browser: open console → Dex login as ${USER}."
echo "Accept self-signed TLS on BOTH :30443 and :32056 (private window if stuck)."
