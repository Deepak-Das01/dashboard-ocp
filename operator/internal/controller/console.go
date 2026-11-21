package controller

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	dashboardv1alpha1 "github.com/Deepak-Das01/dashboard-ocp/operator/api/v1alpha1"
	"github.com/Deepak-Das01/dashboard-ocp/operator/internal/assets"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *KubeDashboardReconciler) reconcileConsole(ctx context.Context, kd *dashboardv1alpha1.KubeDashboard, htpasswdLine string) error {
	ns := kd.Spec.Namespace
	mode := kd.Spec.Auth.Mode
	basicAuth := mode == dashboardv1alpha1.AuthModeSecret && htpasswdLine != ""
	oidcAuth := mode == dashboardv1alpha1.AuthModeOIDC

	// Support RBAC + metrics-scraper (no separate dashboard Deployment)
	support, err := assets.FS.ReadFile("upstream/dashboard-support-v2.7.0.yaml")
	if err != nil {
		return err
	}
	if err := r.applyYAML(ctx, kd, support, ns); err != nil {
		return fmt.Errorf("support manifests: %w", err)
	}

	admin, err := assets.FS.ReadFile("rbac/dashboard-admin.yaml")
	if err != nil {
		return err
	}
	if err := r.applyYAML(ctx, kd, admin, ns); err != nil {
		return fmt.Errorf("dashboard-admin: %w", err)
	}

	// Token secret is created by control plane after the SA token Secret exists.
	bearer := ""
	if basicAuth || oidcAuth {
		bearer, err = r.dashboardAdminToken(ctx, ns)
		if err != nil {
			return fmt.Errorf("dashboard-admin token: %w", err)
		}
	}

	if kd.Spec.MetricsServer.Enabled != nil && *kd.Spec.MetricsServer.Enabled {
		ms, err := assets.FS.ReadFile("metrics-server/components.yaml")
		if err != nil {
			return err
		}
		if err := r.applyYAML(ctx, nil, ms, ""); err != nil {
			return fmt.Errorf("metrics-server: %w", err)
		}
	}

	var extraIPs []net.IP
	nodeIP := ""
	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes); err == nil && len(nodes.Items) > 0 {
		for _, a := range nodes.Items[0].Status.Addresses {
			if a.Type == corev1.NodeInternalIP {
				nodeIP = a.Address
				if ip := net.ParseIP(a.Address); ip != nil {
					extraIPs = append(extraIPs, ip)
				}
			}
		}
	}
	if err := r.ensureTLSSecret(ctx, ns, extraIPs); err != nil {
		return err
	}

	theme, err := assets.FS.ReadFile("kube-dashboard/theme.css")
	if err != nil {
		return err
	}
	nav, err := assets.FS.ReadFile("kube-dashboard/nav-accordion.js")
	if err != nil {
		return err
	}
	opsHTML, err := assets.FS.ReadFile("kube-dashboard/operators.html")
	if err != nil {
		return err
	}
	opsJS, err := assets.FS.ReadFile("kube-dashboard/operators.js")
	if err != nil {
		return err
	}
	addonsJS, err := assets.FS.ReadFile("kube-dashboard/addons.js")
	if err != nil {
		return err
	}
	virtJS, err := assets.FS.ReadFile("kube-dashboard/virtualization.js")
	if err != nil {
		return err
	}
	virtTheme, err := assets.FS.ReadFile("kube-dashboard/virt-theme.css")
	if err != nil {
		return err
	}

	labels := map[string]string{
		"app.kubernetes.io/name":    "kube-dashboard",
		"app.kubernetes.io/part-of": "kube-dashboard",
		"app.kubernetes.io/version": kd.Spec.Version,
		"dashboard.ocp/release":     "v" + strings.TrimPrefix(kd.Spec.Version, "v"),
	}

	if err := r.ensureConfigMap(ctx, ns, "kube-dashboard-ui", map[string]string{
		"theme.css":          string(theme),
		"virt-theme.css":     string(virtTheme),
		"nav-accordion.js":   string(nav),
		"operators.html":     string(opsHTML),
		"operators.js":       string(opsJS),
		"addons.js":          string(addonsJS),
		"virtualization.js":  string(virtJS),
	}, labels); err != nil {
		return err
	}

	nginxConf := renderNginxConf(basicAuth, bearer)
	if oidcAuth {
		nginxConf = renderNginxOIDC(bearer, nodeIP, kd.Spec.Service.HTTPSNodePort)
	}
	if err := r.ensureConfigMap(ctx, ns, "kube-dashboard-nginx", map[string]string{
		"nginx.conf": nginxConf,
	}, labels); err != nil {
		return err
	}

	if basicAuth {
		ht := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "kube-dashboard-htpasswd", Namespace: ns}}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, ht, func() error {
			ht.Type = corev1.SecretTypeOpaque
			ht.StringData = map[string]string{"htpasswd": htpasswdLine}
			ht.Labels = labels
			return nil
		}); err != nil {
			return fmt.Errorf("htpasswd secret: %w", err)
		}
	}

	if err := r.ensureConsoleDeployment(ctx, kd, labels, basicAuth, oidcAuth, nodeIP); err != nil {
		return err
	}
	return r.ensureConsoleService(ctx, kd, labels)
}

func (r *KubeDashboardReconciler) ensureConsoleDeployment(ctx context.Context, kd *dashboardv1alpha1.KubeDashboard, labels map[string]string, basicAuth, oidcAuth bool, nodeIP string) error {
	ns := kd.Spec.Namespace
	deploy := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "kube-dashboard", Namespace: ns}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
		replicas := int32(1)
		sel := map[string]string{"app": "kube-dashboard"}
		deploy.Labels = labels
		deploy.Spec.Replicas = &replicas
		deploy.Spec.Selector = &metav1.LabelSelector{MatchLabels: sel}
		deploy.Spec.Template.ObjectMeta.Labels = map[string]string{
			"app":                       "kube-dashboard",
			"app.kubernetes.io/name":    "kube-dashboard",
			"app.kubernetes.io/version": kd.Spec.Version,
		}
		deploy.Spec.Template.Spec.ServiceAccountName = "kubernetes-dashboard"
		proxyMounts := []corev1.VolumeMount{
			{Name: "nginx-conf", MountPath: "/etc/nginx/nginx.conf", SubPath: "nginx.conf"},
			{Name: "ui-assets", MountPath: "/usr/share/nginx/html/theme.css", SubPath: "theme.css"},
			{Name: "ui-assets", MountPath: "/usr/share/nginx/html/virt-theme.css", SubPath: "virt-theme.css"},
			{Name: "ui-assets", MountPath: "/usr/share/nginx/html/nav-accordion.js", SubPath: "nav-accordion.js"},
			{Name: "ui-assets", MountPath: "/usr/share/nginx/html/operators.html", SubPath: "operators.html"},
			{Name: "ui-assets", MountPath: "/usr/share/nginx/html/operators.js", SubPath: "operators.js"},
			{Name: "ui-assets", MountPath: "/usr/share/nginx/html/addons.js", SubPath: "addons.js"},
			{Name: "ui-assets", MountPath: "/usr/share/nginx/html/virtualization.js", SubPath: "virtualization.js"},
			{Name: "tls", MountPath: "/etc/nginx/certs", ReadOnly: true},
		}
		vols := []corev1.Volume{
			{Name: "nginx-conf", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "kube-dashboard-nginx"}}}},
			{Name: "ui-assets", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "kube-dashboard-ui"}}}},
			{Name: "tls", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: tlsSecretName}}},
			{Name: "tmp-volume", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		}
		if basicAuth {
			proxyMounts = append(proxyMounts, corev1.VolumeMount{Name: "htpasswd", MountPath: "/etc/nginx/htpasswd", SubPath: "htpasswd", ReadOnly: true})
			vols = append(vols, corev1.Volume{Name: "htpasswd", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "kube-dashboard-htpasswd"}}})
		}
		containers := []corev1.Container{
			{
				Name:            "proxy",
				Image:           kd.Spec.Images.Proxy,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Ports: []corev1.ContainerPort{
					{Name: "http", ContainerPort: 8080},
					{Name: "https", ContainerPort: 8443},
				},
				VolumeMounts:   proxyMounts,
				ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt(8443)}}, InitialDelaySeconds: 2, PeriodSeconds: 5},
				LivenessProbe:  &corev1.Probe{ProbeHandler: corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt(8443)}}, InitialDelaySeconds: 10, PeriodSeconds: 20},
			},
			{
				Name:            "dashboard",
				Image:           kd.Spec.Images.Dashboard,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Args: []string{
					"--namespace=" + ns,
					"--enable-insecure-login",
					"--insecure-bind-address=127.0.0.1",
					"--insecure-port=9090",
				},
				VolumeMounts: []corev1.VolumeMount{{Name: "tmp-volume", MountPath: "/tmp"}},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: boolPtr(false),
					ReadOnlyRootFilesystem:   boolPtr(true),
					RunAsUser:                int64Ptr(1001),
					RunAsGroup:               int64Ptr(2001),
				},
			},
		}
		if oidcAuth {
			if nodeIP == "" {
				nodeIP = "127.0.0.1"
			}
			whitelist := fmt.Sprintf("%s:30443,%s", nodeIP, nodeIP)
			containers = append(containers, corev1.Container{
				Name:            "oauth2-proxy",
				Image:           "quay.io/oauth2-proxy/oauth2-proxy:v7.7.1",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Args: []string{
					"--http-address=0.0.0.0:4180",
					"--metrics-address=0.0.0.0:44180",
					"--provider=oidc",
					"--upstream=static://202",
				},
				Env: []corev1.EnvVar{
					{Name: "OAUTH2_PROXY_CLIENT_ID", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "oauth2-proxy-oidc"}, Key: "client-id"}}},
					{Name: "OAUTH2_PROXY_CLIENT_SECRET", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "oauth2-proxy-oidc"}, Key: "client-secret"}}},
					{Name: "OAUTH2_PROXY_COOKIE_SECRET", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "oauth2-proxy-oidc"}, Key: "cookie-secret"}}},
					{Name: "OAUTH2_PROXY_OIDC_ISSUER_URL", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "oauth2-proxy-oidc"}, Key: "issuer-url"}}},
					{Name: "OAUTH2_PROXY_REDIRECT_URL", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "oauth2-proxy-oidc"}, Key: "redirect-url"}}},
					{Name: "OAUTH2_PROXY_EMAIL_DOMAINS", Value: "*"},
					{Name: "OAUTH2_PROXY_COOKIE_SECURE", Value: "true"},
					{Name: "OAUTH2_PROXY_COOKIE_SAMESITE", Value: "lax"},
					{Name: "OAUTH2_PROXY_COOKIE_DOMAINS", Value: nodeIP},
					{Name: "OAUTH2_PROXY_WHITELIST_DOMAINS", Value: whitelist},
					{Name: "OAUTH2_PROXY_PROMPT", Value: "login"},
					{Name: "OAUTH2_PROXY_PASS_AUTHORIZATION_HEADER", Value: "false"},
					{Name: "OAUTH2_PROXY_SET_AUTHORIZATION_HEADER", Value: "false"},
					{Name: "OAUTH2_PROXY_PASS_ACCESS_TOKEN", Value: "false"},
					{Name: "OAUTH2_PROXY_OIDC_GROUPS_CLAIM", Value: "groups"},
					{Name: "OAUTH2_PROXY_SCOPE", Value: "openid email profile groups"},
					{Name: "OAUTH2_PROXY_SKIP_PROVIDER_BUTTON", Value: "true"},
					{Name: "OAUTH2_PROXY_REVERSE_PROXY", Value: "true"},
					{Name: "OAUTH2_PROXY_SSL_INSECURE_SKIP_VERIFY", Value: "true"},
					{Name: "OAUTH2_PROXY_INSECURE_OIDC_ALLOW_UNVERIFIED_EMAIL", Value: "true"},
					{Name: "OAUTH2_PROXY_PROVIDER_DISPLAY_NAME", Value: "Dex"},
				},
				Ports: []corev1.ContainerPort{{Name: "oauth2", ContainerPort: 4180}},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resourceMust("20m"), corev1.ResourceMemory: resourceMust("32Mi")},
					Limits:   corev1.ResourceList{corev1.ResourceCPU: resourceMust("200m"), corev1.ResourceMemory: resourceMust("128Mi")},
				},
			})
		}
		deploy.Spec.Template.Spec.Containers = containers
		deploy.Spec.Template.Spec.Volumes = vols
		deploy.Spec.Template.Spec.NodeSelector = map[string]string{"kubernetes.io/os": "linux"}
		return nil
	})
	return err
}

func (r *KubeDashboardReconciler) ensureConsoleService(ctx context.Context, kd *dashboardv1alpha1.KubeDashboard, labels map[string]string) error {
	ns := kd.Spec.Namespace
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "kube-dashboard", Namespace: ns}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Labels = labels
		svc.Spec.Type = corev1.ServiceTypeNodePort
		svc.Spec.Selector = map[string]string{"app": "kube-dashboard"}
		svc.Spec.Ports = []corev1.ServicePort{
			{Name: "https", Port: 443, TargetPort: intstr.FromInt(8443), NodePort: kd.Spec.Service.HTTPSNodePort},
			{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), NodePort: kd.Spec.Service.HTTPNodePort},
		}
		return nil
	})
	return err
}

func boolPtr(v bool) *bool     { return &v }
func int64Ptr(v int64) *int64 { return &v }
func resourceMust(s string) resource.Quantity { return resource.MustParse(s) }

// ensureNamespace creates the target namespace if missing.
func (r *KubeDashboardReconciler) ensureNamespace(ctx context.Context, name string) error {
	ns := &corev1.Namespace{}
	err := r.Get(ctx, client.ObjectKey{Name: name}, ns)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	return r.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: name,
		Labels: map[string]string{
			"app.kubernetes.io/part-of": "kube-dashboard",
		},
	}})
}

// dashboardAdminToken returns a bearer token for SA dashboard-admin.
// Retries briefly while the control plane populates the SA token Secret.
func (r *KubeDashboardReconciler) dashboardAdminToken(ctx context.Context, ns string) (string, error) {
	var last error
	for i := 0; i < 20; i++ {
		sec := &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: ns, Name: "dashboard-admin-token"}, sec); err != nil {
			last = err
		} else if tok := string(sec.Data["token"]); tok != "" {
			return tok, nil
		} else {
			last = fmt.Errorf("secret dashboard-admin-token has empty token")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	if last == nil {
		last = fmt.Errorf("dashboard-admin-token not ready")
	}
	return "", last
}
