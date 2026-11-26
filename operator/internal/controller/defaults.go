package controller

import (
	dashboardv1alpha1 "github.com/Deepak-Das01/kubeops-platform/operator/api/v1alpha1"
)

const (
	defaultNamespace      = "kubeops-console"
	defaultVersion        = "1.3.0"
	defaultHTTPSNodePort  = int32(30443)
	defaultHTTPNodePort   = int32(30080)
	defaultProxyImage     = "nginx:1.27-alpine"
	defaultDashboardImage = "kubernetesui/dashboard:v2.7.0"
	defaultScraperImage   = "kubernetesui/metrics-scraper:v1.0.8"
	defaultBootstrapUser  = "kubeadmin"
	defaultPasswordLength = 16
	tlsSecretName         = "kube-dashboard-tls"
	bootstrapSecretName   = "kubeadmin-bootstrap"
	operatorReportName    = "kubeops-console"
)

func applyDefaults(kd *dashboardv1alpha1.KubeDashboard) {
	if kd.Spec.Namespace == "" {
		kd.Spec.Namespace = defaultNamespace
	}
	if kd.Spec.Version == "" {
		kd.Spec.Version = defaultVersion
	}
	if kd.Spec.Service.HTTPSNodePort == 0 {
		kd.Spec.Service.HTTPSNodePort = defaultHTTPSNodePort
	}
	if kd.Spec.Service.HTTPNodePort == 0 {
		kd.Spec.Service.HTTPNodePort = defaultHTTPNodePort
	}
	if kd.Spec.Images.Proxy == "" {
		kd.Spec.Images.Proxy = defaultProxyImage
	}
	if kd.Spec.Images.Dashboard == "" {
		kd.Spec.Images.Dashboard = defaultDashboardImage
	}
	if kd.Spec.Images.MetricsScraper == "" {
		kd.Spec.Images.MetricsScraper = defaultScraperImage
	}
	if kd.Spec.MetricsServer.Enabled == nil {
		t := true
		kd.Spec.MetricsServer.Enabled = &t
	}
	if kd.Spec.Auth.Mode == "" {
		// Token mode opens the page in all browsers; secret/basic-auth is opt-in.
		kd.Spec.Auth.Mode = dashboardv1alpha1.AuthModeToken
	}
	if kd.Spec.Auth.BootstrapAdmin.Username == "" {
		kd.Spec.Auth.BootstrapAdmin.Username = defaultBootstrapUser
	}
	if kd.Spec.Auth.BootstrapAdmin.PasswordLength == 0 {
		kd.Spec.Auth.BootstrapAdmin.PasswordLength = defaultPasswordLength
	}
}
