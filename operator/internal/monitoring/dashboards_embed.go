package monitoring

import _ "embed"

//go:embed manifests/dashboards/kubernetes-enterprise-observability.json
var grafanaEnterpriseDashboard string
