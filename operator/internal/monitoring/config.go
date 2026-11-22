package monitoring

const prometheusConfig = `
global:
  scrape_interval: 30s
  evaluation_interval: 30s
alerting:
  alertmanagers:
    - static_configs:
        - targets: ["alertmanager:9093"]
rule_files:
  - /etc/prometheus/rules/*.yml
scrape_configs:
  - job_name: prometheus
    static_configs:
      - targets: ["localhost:9090"]
  - job_name: kube-state-metrics
    static_configs:
      - targets: ["kube-state-metrics:8080"]
  - job_name: node-exporter
    kubernetes_sd_configs:
      - role: endpoints
        namespaces:
          names: ["__INSTALL_NS__"]
    relabel_configs:
      - source_labels: [__meta_kubernetes_service_name]
        action: keep
        regex: node-exporter
      - source_labels: [__meta_kubernetes_endpoint_port_name]
        action: keep
        regex: metrics
  - job_name: kubernetes-nodes-cadvisor
    scheme: https
    tls_config:
      insecure_skip_verify: true
    bearer_token_file: /var/run/secrets/kubernetes.io/serviceaccount/token
    kubernetes_sd_configs:
      - role: node
    relabel_configs:
      - action: labelmap
        regex: __meta_kubernetes_node_label_(.+)
      - target_label: __address__
        replacement: kubernetes.default.svc:443
      - source_labels: [__meta_kubernetes_node_name]
        regex: (.+)
        target_label: __metrics_path__
        replacement: /api/v1/nodes/${1}/proxy/metrics/cadvisor
  - job_name: kubernetes-nodes-kubelet
    scheme: https
    tls_config:
      insecure_skip_verify: true
    bearer_token_file: /var/run/secrets/kubernetes.io/serviceaccount/token
    kubernetes_sd_configs:
      - role: node
    relabel_configs:
      - action: labelmap
        regex: __meta_kubernetes_node_label_(.+)
      - target_label: __address__
        replacement: kubernetes.default.svc:443
      - source_labels: [__meta_kubernetes_node_name]
        regex: (.+)
        target_label: __metrics_path__
        replacement: /api/v1/nodes/${1}/proxy/metrics
  - job_name: coredns
    kubernetes_sd_configs:
      - role: endpoints
        namespaces:
          names: ["kube-system"]
    relabel_configs:
      - source_labels: [__meta_kubernetes_service_name]
        action: keep
        regex: kube-dns
      - source_labels: [__meta_kubernetes_endpoint_port_name]
        action: keep
        regex: metrics
  - job_name: kubernetes-apiserver
    scheme: https
    tls_config:
      ca_file: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
      insecure_skip_verify: true
    bearer_token_file: /var/run/secrets/kubernetes.io/serviceaccount/token
    kubernetes_sd_configs:
      - role: endpoints
    relabel_configs:
      - source_labels: [__meta_kubernetes_namespace, __meta_kubernetes_service_name, __meta_kubernetes_endpoint_port_name]
        action: keep
        regex: default;kubernetes;https
`

const prometheusRules = `
groups:
  - name: kubeops.rules
    rules:
      - alert: TargetDown
        expr: up == 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Target {{ $labels.job }} down"
          description: "{{ $labels.instance }} of job {{ $labels.job }} has been down for more than 5 minutes."
      - alert: KubePodCrashLooping
        expr: increase(kube_pod_container_status_restarts_total[15m]) > 3
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Pod {{ $labels.namespace }}/{{ $labels.pod }} crash looping"
      - alert: NodeNotReady
        expr: kube_node_status_condition{condition="Ready",status="true"} == 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Node {{ $labels.node }} not Ready"
`

const alertmanagerConfig = `
global:
  resolve_timeout: 5m
route:
  receiver: default
  group_by: ["alertname", "job"]
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 12h
receivers:
  - name: default
`

const grafanaDatasources = `
apiVersion: 1
datasources:
  - name: Prometheus
    type: prometheus
    uid: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    editable: false
`

const grafanaDashboardProvider = `
apiVersion: 1
providers:
  - name: kubeops
    orgId: 1
    folder: KubeOps
    type: file
    disableDeletion: false
    editable: true
    options:
      path: /var/lib/grafana/dashboards/kubeops
`

// Default home dashboard JSON is embedded from
// manifests/dashboards/kubernetes-enterprise-observability.json
// (see dashboards_embed.go).