package controller

import (
	"fmt"
	"strings"
)

func renderNginxConf(basicAuth bool, bearerToken string) string {
	authBlock := ""
	if basicAuth {
		authBlock = `
      auth_basic           "kube-dashboard";
      auth_basic_user_file /etc/nginx/htpasswd;
`
	}

	// Critical: browser Basic auth must NOT be forwarded to Dashboard.
	authzHeader := `      proxy_set_header Authorization "";`
	if bearerToken != "" {
		tok := strings.ReplaceAll(bearerToken, `"`, "")
		authzHeader = fmt.Sprintf(`      proxy_set_header Authorization "Bearer %s";`, tok)
	}

	return fmt.Sprintf(`worker_processes auto;
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

  map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
  }

  upstream kd {
    server 127.0.0.1:9090;
  }

  server {
    listen 8080;
    server_name _;
    return 301 https://$host:30443$request_uri;
  }

  server {
    listen 8443 ssl;
    server_name _;
    ssl_certificate     /etc/nginx/certs/tls.crt;
    ssl_certificate_key /etc/nginx/certs/tls.key;
    ssl_protocols TLSv1.2 TLSv1.3;
%s
    location = /theme.css {
      alias /usr/share/nginx/html/theme.css;
      default_type text/css;
      add_header Cache-Control "no-cache";
    }

    location = /virt-theme.css {
      alias /usr/share/nginx/html/virt-theme.css;
      default_type text/css;
      add_header Cache-Control "no-cache";
    }

    location = /ceph-theme.css {
      alias /usr/share/nginx/html/ceph-theme.css;
      default_type text/css;
      add_header Cache-Control "no-cache";
    }

    location = /nav-accordion.js {
      alias /usr/share/nginx/html/nav-accordion.js;
      default_type application/javascript;
      add_header Cache-Control "no-cache";
    }

    location = /operators.js {
      alias /usr/share/nginx/html/operators.js;
      default_type application/javascript;
      add_header Cache-Control "no-cache";
    }

    location = /addons.js {
      alias /usr/share/nginx/html/addons.js;
      default_type application/javascript;
      add_header Cache-Control "no-cache";
    }

    location = /virtualization.js {
      alias /usr/share/nginx/html/virtualization.js;
      default_type application/javascript;
      add_header Cache-Control "no-cache";
    }

    location = /monitoring.js {
      alias /usr/share/nginx/html/monitoring.js;
      default_type application/javascript;
      add_header Cache-Control "no-cache";
    }

    location = /ceph.js {
      alias /usr/share/nginx/html/ceph.js;
      default_type application/javascript;
      add_header Cache-Control "no-cache";
    }

    location = /mon-theme.css {
      alias /usr/share/nginx/html/mon-theme.css;
      default_type text/css;
      add_header Cache-Control "no-cache";
    }

    location = /operators.html {
      alias /usr/share/nginx/html/operators.html;
      default_type text/html;
      add_header Cache-Control "no-cache";
    }

    location = /operators {
      return 302 /operators.html;
    }

    location = /operators/ {
      return 302 /operators.html;
    }

    # Same-origin proxy for kubevirt-manager (embedded Virtualization tab).
    # Variable + resolver so nginx starts even when Virtualization is not installed.
    location /kubeops/virt-ui/ {
      resolver kube-dns.kube-system.svc.cluster.local valid=10s ipv6=off;
      set $virt_ui_upstream kubevirt-manager.kubeops-virtualization.svc.cluster.local:8080;
      rewrite ^/kubeops/virt-ui/(.*)$ /$1 break;
      proxy_pass http://$virt_ui_upstream;
      proxy_http_version 1.1;
      proxy_set_header Host $host;
      proxy_set_header X-Real-IP $remote_addr;
      proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
      proxy_set_header X-Forwarded-Proto https;
      proxy_set_header X-Forwarded-Prefix /kubeops/virt-ui;
      proxy_set_header Accept-Encoding "";
      proxy_set_header Upgrade $http_upgrade;
      proxy_set_header Connection $connection_upgrade;
      proxy_hide_header X-Frame-Options;
      proxy_hide_header Content-Security-Policy;
      # buffering required for sub_filter (base href rewrite)
      proxy_buffering on;
      proxy_read_timeout 3600s;
      sub_filter_types text/html;
      sub_filter_once off;
      sub_filter '<base href="/">' '<base href="/kubeops/virt-ui/">';
      sub_filter '<base href="/" />' '<base href="/kubeops/virt-ui/" />';
      # Brand logo is hardcoded href="/" and escapes the embed into the console.
      sub_filter 'class="brand-link" href="/"' 'class="brand-link" href="/kubeops/virt-ui/"';
      sub_filter 'href="/" class="brand-link"' 'href="/kubeops/virt-ui/" class="brand-link"';
      # Match kube-dashboard glass palette inside the embed.
      sub_filter '</head>' '<link rel="stylesheet" href="/virt-theme.css?v=addons-embed-30"><style>html{color-scheme:dark}</style></head>';
    }

    # Variable + resolver so nginx starts even when Monitoring is not installed.
    # Keep URI path: Grafana serve_from_sub_path expects /kubeops/mon-ui/.
    location = /kubeops/mon-ui/login {
      return 302 /kubeops/mon-ui/d/kubeops-enterprise/?orgId=1;
    }

    location /kubeops/mon-ui/ {
      resolver kube-dns.kube-system.svc.cluster.local valid=10s ipv6=off;
      set $mon_ui_upstream grafana.kubeops-monitoring.svc.cluster.local:3000;
      proxy_pass http://$mon_ui_upstream;
      proxy_http_version 1.1;
      proxy_set_header Host $host;
      proxy_set_header X-Real-IP $remote_addr;
      proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
      proxy_set_header X-Forwarded-Proto https;
      proxy_set_header X-Forwarded-Host $host;
      proxy_set_header X-WEBAUTH-USER kubeops;
      proxy_set_header Accept-Encoding "";
      proxy_set_header Upgrade $http_upgrade;
      proxy_set_header Connection $connection_upgrade;
      proxy_hide_header X-Frame-Options;
      proxy_hide_header Content-Security-Policy;
      proxy_redirect http://localhost:3000/ /;
      proxy_redirect http://$mon_ui_upstream/ /;
      proxy_buffering on;
      proxy_read_timeout 3600s;
      sub_filter_types text/html;
      sub_filter_once off;
      sub_filter '</head>' '<link rel="stylesheet" href="/mon-theme.css?v=addons-embed-35"><style>html{color-scheme:dark}</style></head>';
    }

    location /kubeops/api/operators {
      proxy_pass https://kubernetes.default.svc/apis/dashboard.ocp/v1alpha1/operators;
      proxy_http_version 1.1;
      proxy_ssl_server_name on;
      proxy_ssl_verify off;
      proxy_set_header Host kubernetes.default.svc;
      proxy_set_header Accept application/json;
%s
    }

    location /kubeops/api/nfsprovisioners {
      proxy_pass https://kubernetes.default.svc/apis/dashboard.ocp/v1alpha1/nfsprovisioners;
      proxy_http_version 1.1;
      proxy_ssl_server_name on;
      proxy_ssl_verify off;
      proxy_set_header Host kubernetes.default.svc;
      proxy_set_header Accept application/json;
      proxy_set_header Content-Type $http_content_type;
%s
    }

    location /kubeops/api/virtualizations {
      proxy_pass https://kubernetes.default.svc/apis/dashboard.ocp/v1alpha1/virtualizations;
      proxy_http_version 1.1;
      proxy_ssl_server_name on;
      proxy_ssl_verify off;
      proxy_set_header Host kubernetes.default.svc;
      proxy_set_header Accept application/json;
      proxy_set_header Content-Type $http_content_type;
%s
    }

    location /kubeops/api/monitorings {
      proxy_pass https://kubernetes.default.svc/apis/dashboard.ocp/v1alpha1/monitorings;
      proxy_http_version 1.1;
      proxy_ssl_server_name on;
      proxy_ssl_verify off;
      proxy_set_header Host kubernetes.default.svc;
      proxy_set_header Accept application/json;
      proxy_set_header Content-Type $http_content_type;
%s
    }

    location /kubeops/api/rookceps {
      proxy_pass https://kubernetes.default.svc/apis/dashboard.ocp/v1alpha1/rookceps;
      proxy_http_version 1.1;
      proxy_ssl_server_name on;
      proxy_ssl_verify off;
      proxy_set_header Host kubernetes.default.svc;
      proxy_set_header Accept application/json;
      proxy_set_header Content-Type $http_content_type;
%s
    }

    location /kubeops/api/ceph-disks {
      resolver kube-dns.kube-system.svc.cluster.local valid=10s ipv6=off;
      set $ceph_probe_upstream kubeops-ceph-controller.kubeops-addon-controller.svc.cluster.local:18088;
      proxy_pass http://$ceph_probe_upstream/disks$is_args$args;
      proxy_http_version 1.1;
      proxy_set_header Host $host;
      proxy_set_header Accept application/json;
      proxy_connect_timeout 5s;
      proxy_read_timeout 120s;
    }

    location /kubeops/api/ceph-session {
      resolver kube-dns.kube-system.svc.cluster.local valid=10s ipv6=off;
      set $ceph_probe_upstream kubeops-ceph-controller.kubeops-addon-controller.svc.cluster.local:18088;
      proxy_pass http://$ceph_probe_upstream/session$is_args$args;
      proxy_http_version 1.1;
      proxy_set_header Host $host;
      proxy_set_header Accept application/json;
      proxy_connect_timeout 5s;
      proxy_read_timeout 30s;
    }

    location /kubeops/ceph-ui/ {
      resolver kube-dns.kube-system.svc.cluster.local valid=10s ipv6=off;
      set $ceph_ui_upstream rook-ceph-mgr-dashboard.kubeops-ceph-cluster.svc.cluster.local:7000;
      # Keep /kubeops/ceph-ui prefix — CephCluster dashboard.urlPrefix matches.
      proxy_pass http://$ceph_ui_upstream;
      proxy_http_version 1.1;
      proxy_set_header Host $host;
      proxy_set_header X-Real-IP $remote_addr;
      proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
      proxy_set_header X-Forwarded-Proto https;
      proxy_set_header X-Forwarded-Prefix /kubeops/ceph-ui;
      proxy_set_header Accept-Encoding "";
      proxy_hide_header X-Frame-Options;
      proxy_hide_header Content-Security-Policy;
      proxy_buffering on;
      proxy_read_timeout 3600s;
      sub_filter_types text/html;
      sub_filter_once off;
      sub_filter '</head>' '<link rel="stylesheet" href="/ceph-theme.css?v=addons-embed-38"><script>(function(){function p(a){a=a||[];return{read:a.indexOf("read")>=0,create:a.indexOf("create")>=0,update:a.indexOf("update")>=0,delete:a.indexOf("delete")>=0}}function n(r){r=r||{};return{hosts:p(r.hosts),configOpt:p(r["config-opt"]),pool:p(r.pool),osd:p(r.osd),monitor:p(r.monitor),rbdImage:p(r["rbd-image"]),iscsi:p(r.iscsi),nvmeof:p(r["nvme-of"]),rbdMirroring:p(r["rbd-mirroring"]),rgw:p(r.rgw),cephfs:p(r.cephfs),manager:p(r.manager),log:p(r.log),user:p(r.user),grafana:p(r.grafana),prometheus:p(r.prometheus),nfs:p(r["nfs-ganesha"])}}try{var x=new XMLHttpRequest();x.open("GET","/kubeops/api/ceph-session?namespace=kubeops-ceph-cluster",false);x.withCredentials=true;x.setRequestHeader("Accept","application/json");x.send(null);if(x.status>=200&&x.status<300){var d=JSON.parse(x.responseText);if(d.username){localStorage.setItem("dashboard_username",d.username);localStorage.setItem("dashboard_permissions",JSON.stringify(n(d.permissions)));localStorage.setItem("sso",String(!!d.sso));localStorage.setItem("user_pwd_update_required",String(!!d.pwdUpdateRequired));if(d.pwdExpirationDate!=null)localStorage.setItem("user_pwd_expiration_date",String(d.pwdExpirationDate));else localStorage.removeItem("user_pwd_expiration_date")}}}catch(e){}var h=location.hash||"";if(!h||h==="#"||h==="#/"||h.indexOf("#/login")===0){location.replace(location.pathname+location.search+"#/dashboard")}})();</script><style>html{color-scheme:dark}</style></head>';
    }

    location / {
      proxy_pass http://kd;
      proxy_http_version 1.1;
      proxy_set_header Host $host;
      proxy_set_header X-Real-IP $remote_addr;
      proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
      proxy_set_header X-Forwarded-Proto https;
      proxy_set_header Accept-Encoding "";
%s

      sub_filter '</head>' '<link rel="stylesheet" href="/theme.css?v=addons-embed-30"><style>html{color-scheme:dark}</style></head>';
      sub_filter '<body>' '<body class="kd-theme">';
      sub_filter '</body>' '<script src="/nav-accordion.js?v=addons-embed-39" defer></script><script src="/operators.js?v=addons-embed-30" defer></script><script src="/addons.js?v=addons-embed-39" defer></script><script src="/virtualization.js?v=addons-embed-30" defer></script><script src="/monitoring.js?v=addons-embed-30" defer></script><script src="/ceph.js?v=addons-embed-39" defer></script></body>';
    }
  }
}
`, authBlock, authzHeader, authzHeader, authzHeader, authzHeader, authzHeader, authzHeader)
}

// renderNginxOIDC gates the console with oauth2-proxy auth_request, then injects
// a dashboard-admin SA bearer into upstream Dashboard API calls.
func renderNginxOIDC(bearerToken, nodeIP string, httpsNodePort int32) string {
	tok := strings.ReplaceAll(bearerToken, `"`, "")
	port := httpsNodePort
	if port == 0 {
		port = 30443
	}
	_ = nodeIP
	return fmt.Sprintf(`worker_processes auto;
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

  map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
  }

  upstream oauth2 { server 127.0.0.1:4180; }
  upstream kd { server 127.0.0.1:9090; }

  server {
    listen 8080;
    server_name _;
    return 301 https://$host:%d$request_uri;
  }

  server {
    listen 8443 ssl;
    server_name _;
    ssl_certificate     /etc/nginx/certs/tls.crt;
    ssl_certificate_key /etc/nginx/certs/tls.key;
    ssl_protocols TLSv1.2 TLSv1.3;

    location /oauth2/ {
      proxy_pass http://oauth2;
      proxy_set_header Host $host;
      proxy_set_header X-Real-IP $remote_addr;
      proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
      proxy_set_header X-Forwarded-Proto https;
      proxy_set_header X-Forwarded-Host $host:%d;
      proxy_set_header X-Forwarded-Port %d;
    }

    location = /oauth2/auth {
      proxy_pass http://oauth2;
      proxy_set_header Host $host;
      proxy_set_header X-Real-IP $remote_addr;
      proxy_set_header X-Forwarded-Uri $request_uri;
      proxy_set_header X-Forwarded-Proto https;
      proxy_set_header X-Forwarded-Host $host:%d;
      proxy_set_header X-Forwarded-Port %d;
      proxy_set_header Content-Length "";
      proxy_pass_request_body off;
    }

    location @login {
      return 302 /oauth2/start?rd=https://$host:%d$request_uri;
    }

    location = /theme.css {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      alias /usr/share/nginx/html/theme.css;
      default_type text/css;
      add_header Cache-Control "no-cache";
    }

    location = /virt-theme.css {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      alias /usr/share/nginx/html/virt-theme.css;
      default_type text/css;
      add_header Cache-Control "no-cache";
    }

    location = /mon-theme.css {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      alias /usr/share/nginx/html/mon-theme.css;
      default_type text/css;
      add_header Cache-Control "no-cache";
    }

    location = /ceph-theme.css {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      alias /usr/share/nginx/html/ceph-theme.css;
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

    location = /operators.js {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      alias /usr/share/nginx/html/operators.js;
      default_type application/javascript;
      add_header Cache-Control "no-cache";
    }

    location = /addons.js {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      alias /usr/share/nginx/html/addons.js;
      default_type application/javascript;
      add_header Cache-Control "no-cache";
    }

    location = /virtualization.js {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      alias /usr/share/nginx/html/virtualization.js;
      default_type application/javascript;
      add_header Cache-Control "no-cache";
    }

    location = /monitoring.js {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      alias /usr/share/nginx/html/monitoring.js;
      default_type application/javascript;
      add_header Cache-Control "no-cache";
    }

    location = /ceph.js {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      alias /usr/share/nginx/html/ceph.js;
      default_type application/javascript;
      add_header Cache-Control "no-cache";
    }

    location = /operators.html {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      alias /usr/share/nginx/html/operators.html;
      default_type text/html;
      add_header Cache-Control "no-cache";
    }

    location = /operators {
      return 302 /operators.html;
    }

    location = /operators/ {
      return 302 /operators.html;
    }

    # Variable + resolver so nginx starts even when Virtualization is not installed.
    location /kubeops/virt-ui/ {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      resolver kube-dns.kube-system.svc.cluster.local valid=10s ipv6=off;
      set $virt_ui_upstream kubevirt-manager.kubeops-virtualization.svc.cluster.local:8080;
      rewrite ^/kubeops/virt-ui/(.*)$ /$1 break;
      proxy_pass http://$virt_ui_upstream;
      proxy_http_version 1.1;
      proxy_set_header Host $host;
      proxy_set_header X-Real-IP $remote_addr;
      proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
      proxy_set_header X-Forwarded-Proto https;
      proxy_set_header X-Forwarded-Prefix /kubeops/virt-ui;
      proxy_set_header Accept-Encoding "";
      proxy_set_header Upgrade $http_upgrade;
      proxy_set_header Connection $connection_upgrade;
      proxy_hide_header X-Frame-Options;
      proxy_hide_header Content-Security-Policy;
      # buffering required for sub_filter (base href rewrite)
      proxy_buffering on;
      proxy_read_timeout 3600s;
      sub_filter_types text/html;
      sub_filter_once off;
      sub_filter '<base href="/">' '<base href="/kubeops/virt-ui/">';
      sub_filter '<base href="/" />' '<base href="/kubeops/virt-ui/" />';
      # Brand logo is hardcoded href="/" and escapes the embed into the console.
      sub_filter 'class="brand-link" href="/"' 'class="brand-link" href="/kubeops/virt-ui/"';
      sub_filter 'href="/" class="brand-link"' 'href="/kubeops/virt-ui/" class="brand-link"';
      # Match kube-dashboard glass palette inside the embed.
      sub_filter '</head>' '<link rel="stylesheet" href="/virt-theme.css?v=addons-embed-30"><style>html{color-scheme:dark}</style></head>';
    }

    location = /kubeops/mon-ui/login {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      return 302 /kubeops/mon-ui/d/kubeops-enterprise/?orgId=1;
    }

    location /kubeops/mon-ui/ {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      resolver kube-dns.kube-system.svc.cluster.local valid=10s ipv6=off;
      set $mon_ui_upstream grafana.kubeops-monitoring.svc.cluster.local:3000;
      proxy_pass http://$mon_ui_upstream;
      proxy_http_version 1.1;
      proxy_set_header Host $host;
      proxy_set_header X-Real-IP $remote_addr;
      proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
      proxy_set_header X-Forwarded-Proto https;
      proxy_set_header X-Forwarded-Host $host;
      proxy_set_header X-WEBAUTH-USER kubeops;
      proxy_set_header Accept-Encoding "";
      proxy_set_header Upgrade $http_upgrade;
      proxy_set_header Connection $connection_upgrade;
      proxy_hide_header X-Frame-Options;
      proxy_hide_header Content-Security-Policy;
      proxy_redirect http://localhost:3000/ /;
      proxy_redirect http://$mon_ui_upstream/ /;
      proxy_buffering on;
      proxy_read_timeout 3600s;
      sub_filter_types text/html;
      sub_filter_once off;
      sub_filter '</head>' '<link rel="stylesheet" href="/mon-theme.css?v=addons-embed-35"><style>html{color-scheme:dark}</style></head>';
    }

    location /kubeops/api/operators {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      proxy_pass https://kubernetes.default.svc/apis/dashboard.ocp/v1alpha1/operators;
      proxy_http_version 1.1;
      proxy_ssl_server_name on;
      proxy_ssl_verify off;
      proxy_set_header Host kubernetes.default.svc;
      proxy_set_header Accept application/json;
      proxy_set_header Authorization "Bearer %s";
    }

    location /kubeops/api/nfsprovisioners {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      proxy_pass https://kubernetes.default.svc/apis/dashboard.ocp/v1alpha1/nfsprovisioners;
      proxy_http_version 1.1;
      proxy_ssl_server_name on;
      proxy_ssl_verify off;
      proxy_set_header Host kubernetes.default.svc;
      proxy_set_header Accept application/json;
      proxy_set_header Content-Type $http_content_type;
      proxy_set_header Authorization "Bearer %s";
    }

    location /kubeops/api/virtualizations {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      proxy_pass https://kubernetes.default.svc/apis/dashboard.ocp/v1alpha1/virtualizations;
      proxy_http_version 1.1;
      proxy_ssl_server_name on;
      proxy_ssl_verify off;
      proxy_set_header Host kubernetes.default.svc;
      proxy_set_header Accept application/json;
      proxy_set_header Content-Type $http_content_type;
      proxy_set_header Authorization "Bearer %s";
    }

    location /kubeops/api/monitorings {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      proxy_pass https://kubernetes.default.svc/apis/dashboard.ocp/v1alpha1/monitorings;
      proxy_http_version 1.1;
      proxy_ssl_server_name on;
      proxy_ssl_verify off;
      proxy_set_header Host kubernetes.default.svc;
      proxy_set_header Accept application/json;
      proxy_set_header Content-Type $http_content_type;
      proxy_set_header Authorization "Bearer %s";
    }

    location /kubeops/api/rookceps {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      proxy_pass https://kubernetes.default.svc/apis/dashboard.ocp/v1alpha1/rookceps;
      proxy_http_version 1.1;
      proxy_ssl_server_name on;
      proxy_ssl_verify off;
      proxy_set_header Host kubernetes.default.svc;
      proxy_set_header Accept application/json;
      proxy_set_header Content-Type $http_content_type;
      proxy_set_header Authorization "Bearer %s";
    }

    location /kubeops/api/ceph-disks {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      resolver kube-dns.kube-system.svc.cluster.local valid=10s ipv6=off;
      set $ceph_probe_upstream kubeops-ceph-controller.kubeops-addon-controller.svc.cluster.local:18088;
      proxy_pass http://$ceph_probe_upstream/disks$is_args$args;
      proxy_http_version 1.1;
      proxy_set_header Host $host;
      proxy_set_header Accept application/json;
      proxy_connect_timeout 5s;
      proxy_read_timeout 120s;
    }

    location /kubeops/api/ceph-session {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      resolver kube-dns.kube-system.svc.cluster.local valid=10s ipv6=off;
      set $ceph_probe_upstream kubeops-ceph-controller.kubeops-addon-controller.svc.cluster.local:18088;
      proxy_pass http://$ceph_probe_upstream/session$is_args$args;
      proxy_http_version 1.1;
      proxy_set_header Host $host;
      proxy_set_header Accept application/json;
      proxy_connect_timeout 5s;
      proxy_read_timeout 30s;
    }

    location /kubeops/ceph-ui/ {
      auth_request /oauth2/auth;
      error_page 401 = @login;
      resolver kube-dns.kube-system.svc.cluster.local valid=10s ipv6=off;
      set $ceph_ui_upstream rook-ceph-mgr-dashboard.kubeops-ceph-cluster.svc.cluster.local:7000;
      proxy_pass http://$ceph_ui_upstream;
      proxy_http_version 1.1;
      proxy_set_header Host $host;
      proxy_set_header X-Real-IP $remote_addr;
      proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
      proxy_set_header X-Forwarded-Proto https;
      proxy_set_header X-Forwarded-Prefix /kubeops/ceph-ui;
      proxy_set_header Accept-Encoding "";
      proxy_hide_header X-Frame-Options;
      proxy_hide_header Content-Security-Policy;
      proxy_buffering on;
      proxy_read_timeout 3600s;
      sub_filter_types text/html;
      sub_filter_once off;
      sub_filter '</head>' '<link rel="stylesheet" href="/ceph-theme.css?v=addons-embed-38"><script>(function(){function p(a){a=a||[];return{read:a.indexOf("read")>=0,create:a.indexOf("create")>=0,update:a.indexOf("update")>=0,delete:a.indexOf("delete")>=0}}function n(r){r=r||{};return{hosts:p(r.hosts),configOpt:p(r["config-opt"]),pool:p(r.pool),osd:p(r.osd),monitor:p(r.monitor),rbdImage:p(r["rbd-image"]),iscsi:p(r.iscsi),nvmeof:p(r["nvme-of"]),rbdMirroring:p(r["rbd-mirroring"]),rgw:p(r.rgw),cephfs:p(r.cephfs),manager:p(r.manager),log:p(r.log),user:p(r.user),grafana:p(r.grafana),prometheus:p(r.prometheus),nfs:p(r["nfs-ganesha"])}}try{var x=new XMLHttpRequest();x.open("GET","/kubeops/api/ceph-session?namespace=kubeops-ceph-cluster",false);x.withCredentials=true;x.setRequestHeader("Accept","application/json");x.send(null);if(x.status>=200&&x.status<300){var d=JSON.parse(x.responseText);if(d.username){localStorage.setItem("dashboard_username",d.username);localStorage.setItem("dashboard_permissions",JSON.stringify(n(d.permissions)));localStorage.setItem("sso",String(!!d.sso));localStorage.setItem("user_pwd_update_required",String(!!d.pwdUpdateRequired));if(d.pwdExpirationDate!=null)localStorage.setItem("user_pwd_expiration_date",String(d.pwdExpirationDate));else localStorage.removeItem("user_pwd_expiration_date")}}}catch(e){}var h=location.hash||"";if(!h||h==="#"||h==="#/"||h.indexOf("#/login")===0){location.replace(location.pathname+location.search+"#/dashboard")}})();</script><style>html{color-scheme:dark}</style></head>';
    }

    location / {
      auth_request /oauth2/auth;
      error_page 401 = @login;

      proxy_pass http://kd;
      proxy_http_version 1.1;
      proxy_set_header Host $host;
      proxy_set_header X-Real-IP $remote_addr;
      proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
      proxy_set_header X-Forwarded-Proto https;
      proxy_set_header Accept-Encoding "";
      proxy_set_header Authorization "Bearer %s";

      sub_filter '</head>' '<link rel="stylesheet" href="/theme.css?v=addons-embed-30"><style>html{color-scheme:dark}</style></head>';
      sub_filter '<body>' '<body class="kd-theme">';
      sub_filter '</body>' '<script src="/nav-accordion.js?v=addons-embed-39" defer></script><script src="/operators.js?v=addons-embed-30" defer></script><script src="/addons.js?v=addons-embed-39" defer></script><script src="/virtualization.js?v=addons-embed-30" defer></script><script src="/monitoring.js?v=addons-embed-30" defer></script><script src="/ceph.js?v=addons-embed-39" defer></script></body>';
    }
  }
}
`, port, port, port, port, port, port, tok, tok, tok, tok, tok, tok)
}

