package monitoring

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/Deepak-Das01/kubeops-platform/operator/internal/placement"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func withCPTolerations(d *appsv1.Deployment) *appsv1.Deployment {
	placement.EnsureControlPlaneTolerations(&d.Spec.Template.Spec)
	return d
}

const (
	imgPrometheus   = "quay.io/prometheus/prometheus:v2.54.1"
	imgAlertmanager = "quay.io/prometheus/alertmanager:v0.27.0"
	imgGrafana      = "docker.io/grafana/grafana:11.2.0"
	imgKSM          = "registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.13.0"
	imgNodeExporter = "quay.io/prometheus/node-exporter:v1.8.2"

	grafanaNodePort      int32 = 30445
	prometheusNodePort   int32 = 30446
	alertmanagerNodePort int32 = 30447

	promPVCSize = "10Gi"
	grafPVCSize = "5Gi"
)

type stackOpts struct {
	Namespace          string
	Retention          string
	StorageClass       string // empty => ephemeral
	GrafanaUser        string
	GrafanaPassword    string
	EnableAlertmanager bool
	Owner              metav1.Object
}

func labels(component string) map[string]string {
	return map[string]string{
		managedLabel:                     managedValue,
		"app.kubernetes.io/part-of":      "kube-dashboard",
		"app.kubernetes.io/component":    "monitoring",
		"app.kubernetes.io/name":         component,
		"app":                            component,
	}
}

func (r *MonitoringReconciler) ensureStack(ctx context.Context, opt stackOpts) (storageMode string, err error) {
	if err := r.ensureNamespace(ctx, opt.Namespace); err != nil {
		return "", err
	}
	if err := r.ensurePrometheusRBAC(ctx, opt); err != nil {
		return "", err
	}

	storageMode = "ephemeral"
	usePVC := opt.StorageClass != ""
	if usePVC {
		if err := r.ensurePVC(ctx, opt.Namespace, "prometheus-data", promPVCSize, opt.StorageClass, opt.Owner); err != nil {
			return "", err
		}
		if err := r.ensurePVC(ctx, opt.Namespace, "grafana-data", grafPVCSize, opt.StorageClass, opt.Owner); err != nil {
			return "", err
		}
		storageMode = "pvc:" + opt.StorageClass
	}

	promCfg := strings.ReplaceAll(prometheusConfig, "__INSTALL_NS__", opt.Namespace)
	promCfgHash := shortHash(promCfg + prometheusRules)
	dashHash := shortHash(grafanaEnterpriseDashboard + grafanaDatasources + grafanaDashboardProvider)
	if err := r.ensureConfigMap(ctx, opt.Namespace, "prometheus-config", map[string]string{
		"prometheus.yml": promCfg,
	}, opt.Owner); err != nil {
		return "", err
	}
	if err := r.ensureConfigMap(ctx, opt.Namespace, "prometheus-rules", map[string]string{
		"kubeops.yml": prometheusRules,
	}, opt.Owner); err != nil {
		return "", err
	}
	if err := r.ensureConfigMap(ctx, opt.Namespace, "alertmanager-config", map[string]string{
		"alertmanager.yml": alertmanagerConfig,
	}, opt.Owner); err != nil {
		return "", err
	}
	if err := r.ensureConfigMap(ctx, opt.Namespace, "grafana-datasources", map[string]string{
		"datasources.yaml": grafanaDatasources,
	}, opt.Owner); err != nil {
		return "", err
	}
	if err := r.ensureConfigMap(ctx, opt.Namespace, "grafana-dashboards-provider", map[string]string{
		"dashboards.yaml": grafanaDashboardProvider,
	}, opt.Owner); err != nil {
		return "", err
	}
	if err := r.ensureConfigMap(ctx, opt.Namespace, "grafana-dashboards", map[string]string{
		"kubernetes-enterprise-observability.json": grafanaEnterpriseDashboard,
	}, opt.Owner); err != nil {
		return "", err
	}

	pass := opt.GrafanaPassword
	if pass == "" {
		var genErr error
		pass, genErr = r.ensureGrafanaSecret(ctx, opt)
		if genErr != nil {
			return "", genErr
		}
	} else if err := r.writeGrafanaSecret(ctx, opt.Namespace, opt.GrafanaUser, pass, opt.Owner); err != nil {
		return "", err
	}

	if err := r.ensureDeployment(ctx, prometheusDeploy(opt, usePVC, promCfgHash), opt.Owner); err != nil {
		return "", err
	}
	if err := r.ensureService(ctx, nodePortService(opt.Namespace, "prometheus", 9090, "http", prometheusNodePort), opt.Owner); err != nil {
		return "", err
	}

	if opt.EnableAlertmanager {
		if err := r.ensureDeployment(ctx, alertmanagerDeploy(opt), opt.Owner); err != nil {
			return "", err
		}
		if err := r.ensureService(ctx, nodePortService(opt.Namespace, "alertmanager", 9093, "http", alertmanagerNodePort), opt.Owner); err != nil {
			return "", err
		}
	}

	if err := r.ensureDeployment(ctx, grafanaDeploy(opt, usePVC, dashHash), opt.Owner); err != nil {
		return "", err
	}
	if err := r.ensureService(ctx, nodePortService(opt.Namespace, "grafana", 3000, "http", grafanaNodePort), opt.Owner); err != nil {
		return "", err
	}

	if err := r.ensureDeployment(ctx, ksmDeploy(opt), opt.Owner); err != nil {
		return "", err
	}
	if err := r.ensureService(ctx, clusterIPService(opt.Namespace, "kube-state-metrics", 8080, "http"), opt.Owner); err != nil {
		return "", err
	}

	if err := r.ensureDaemonSet(ctx, nodeExporterDS(opt), opt.Owner); err != nil {
		return "", err
	}
	if err := r.ensureService(ctx, clusterIPService(opt.Namespace, "node-exporter", 9100, "metrics"), opt.Owner); err != nil {
		return "", err
	}

	return storageMode, nil
}

func (r *MonitoringReconciler) ensureNamespace(ctx context.Context, ns string) error {
	obj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		if obj.Labels == nil {
			obj.Labels = map[string]string{}
		}
		obj.Labels[managedLabel] = managedValue
		obj.Labels["app.kubernetes.io/part-of"] = "kube-dashboard"
		obj.Labels["app.kubernetes.io/component"] = "monitoring"
		return nil
	})
	return err
}

func (r *MonitoringReconciler) ensurePrometheusRBAC(ctx context.Context, opt stackOpts) error {
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "prometheus", Namespace: opt.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, sa, func() error {
		sa.Labels = labels("prometheus")
		return controllerutil.SetControllerReference(opt.Owner, sa, r.Scheme)
	}); err != nil {
		return err
	}
	cr := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "kubeops-monitoring-prometheus"}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, cr, func() error {
		cr.Labels = labels("prometheus")
		// Shared by Prometheus (node/proxy scrape) and kube-state-metrics (object collectors).
		cr.Rules = []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{
				"nodes", "nodes/metrics", "nodes/proxy", "pods", "services", "endpoints",
				"namespaces", "configmaps", "secrets", "serviceaccounts", "resourcequotas",
				"replicationcontrollers", "limitranges", "persistentvolumeclaims",
				"persistentvolumes", "events",
			}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"apps"}, Resources: []string{"statefulsets", "daemonsets", "deployments", "replicasets"}, Verbs: []string{"list", "watch"}},
			{APIGroups: []string{"batch"}, Resources: []string{"cronjobs", "jobs"}, Verbs: []string{"list", "watch"}},
			{APIGroups: []string{"autoscaling"}, Resources: []string{"horizontalpodautoscalers"}, Verbs: []string{"list", "watch"}},
			{APIGroups: []string{"policy"}, Resources: []string{"poddisruptionbudgets"}, Verbs: []string{"list", "watch"}},
			{APIGroups: []string{"storage.k8s.io"}, Resources: []string{"storageclasses", "volumeattachments"}, Verbs: []string{"list", "watch"}},
			{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"networkpolicies", "ingresses"}, Verbs: []string{"list", "watch"}},
			{APIGroups: []string{"discovery.k8s.io"}, Resources: []string{"endpointslices"}, Verbs: []string{"list", "watch"}},
			{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"list", "watch"}},
			{NonResourceURLs: []string{"/metrics", "/metrics/cadvisor"}, Verbs: []string{"get"}},
		}
		return nil
	}); err != nil {
		return err
	}
	crb := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "kubeops-monitoring-prometheus"}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, crb, func() error {
		crb.Labels = labels("prometheus")
		crb.RoleRef = rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: cr.Name}
		crb.Subjects = []rbacv1.Subject{{Kind: "ServiceAccount", Name: "prometheus", Namespace: opt.Namespace}}
		return nil
	})
	return err
}

func (r *MonitoringReconciler) ensureConfigMap(ctx context.Context, ns, name string, data map[string]string, owner metav1.Object) error {
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		cm.Labels = labels(name)
		cm.Data = data
		return controllerutil.SetControllerReference(owner, cm, r.Scheme)
	})
	return err
}

func (r *MonitoringReconciler) ensurePVC(ctx context.Context, ns, name, size, sc string, owner metav1.Object) error {
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, pvc, func() error {
		if pvc.Labels == nil {
			pvc.Labels = map[string]string{}
		}
		pvc.Labels[managedLabel] = managedValue
		if pvc.Spec.AccessModes == nil {
			pvc.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
		}
		if pvc.Spec.Resources.Requests == nil {
			pvc.Spec.Resources.Requests = corev1.ResourceList{}
		}
		pvc.Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse(size)
		if sc != "" {
			pvc.Spec.StorageClassName = &sc
		}
		// PVC immutability: only set ownerRef on create
		if pvc.UID == "" {
			return controllerutil.SetControllerReference(owner, pvc, r.Scheme)
		}
		return nil
	})
	return err
}

func (r *MonitoringReconciler) ensureGrafanaSecret(ctx context.Context, opt stackOpts) (string, error) {
	sec := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Namespace: opt.Namespace, Name: "grafana-admin"}, sec)
	if err == nil {
		if b, ok := sec.Data["admin-password"]; ok && len(b) > 0 {
			return string(b), nil
		}
	}
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	pass := base64.RawURLEncoding.EncodeToString(buf)
	if err := r.writeGrafanaSecret(ctx, opt.Namespace, opt.GrafanaUser, pass, opt.Owner); err != nil {
		return "", err
	}
	return pass, nil
}

func (r *MonitoringReconciler) writeGrafanaSecret(ctx context.Context, ns, user, pass string, owner metav1.Object) error {
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "grafana-admin", Namespace: ns}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, sec, func() error {
		sec.Labels = labels("grafana")
		sec.Type = corev1.SecretTypeOpaque
		if sec.StringData == nil {
			sec.StringData = map[string]string{}
		}
		sec.StringData["admin-user"] = user
		sec.StringData["admin-password"] = pass
		return controllerutil.SetControllerReference(owner, sec, r.Scheme)
	})
	return err
}

func (r *MonitoringReconciler) ensureDeployment(ctx context.Context, desired *appsv1.Deployment, owner metav1.Object) error {
	cur := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cur, func() error {
		if cur.Labels == nil {
			cur.Labels = map[string]string{}
		}
		for k, v := range desired.Labels {
			cur.Labels[k] = v
		}
		if cur.Spec.Selector == nil {
			cur.Spec.Selector = desired.Spec.Selector
		}
		cur.Spec.Replicas = desired.Spec.Replicas
		if desired.Spec.Strategy.Type != "" {
			cur.Spec.Strategy = desired.Spec.Strategy
		}
		// Replace pod template intentionally (image/args/volumes); avoid wiping
		// Deployment-level defaults (strategy, revisionHistoryLimit, …).
		cur.Spec.Template = desired.Spec.Template
		return controllerutil.SetControllerReference(owner, cur, r.Scheme)
	})
	return err
}

func (r *MonitoringReconciler) ensureDaemonSet(ctx context.Context, desired *appsv1.DaemonSet, owner metav1.Object) error {
	cur := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cur, func() error {
		if cur.Labels == nil {
			cur.Labels = map[string]string{}
		}
		for k, v := range desired.Labels {
			cur.Labels[k] = v
		}
		if cur.Spec.Selector == nil {
			cur.Spec.Selector = desired.Spec.Selector
		}
		cur.Spec.Template = desired.Spec.Template
		cur.Spec.UpdateStrategy = desired.Spec.UpdateStrategy
		return controllerutil.SetControllerReference(owner, cur, r.Scheme)
	})
	return err
}

func (r *MonitoringReconciler) ensureService(ctx context.Context, desired *corev1.Service, owner metav1.Object) error {
	cur := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cur, func() error {
		cur.Labels = desired.Labels
		cur.Spec.Selector = desired.Spec.Selector
		cur.Spec.Type = desired.Spec.Type
		cur.Spec.Ports = desired.Spec.Ports
		return controllerutil.SetControllerReference(owner, cur, r.Scheme)
	})
	return err
}

func int32ptr(v int32) *int32 { return &v }

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}

func prometheusDeploy(opt stackOpts, usePVC bool, cfgHash string) *appsv1.Deployment {
	volMounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/prometheus/prometheus.yml", SubPath: "prometheus.yml"},
		{Name: "rules", MountPath: "/etc/prometheus/rules"},
		{Name: "data", MountPath: "/prometheus"},
	}
	vols := []corev1.Volume{
		{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "prometheus-config"}}}},
		{Name: "rules", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "prometheus-rules"}}}},
	}
	if usePVC {
		vols = append(vols, corev1.Volume{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "prometheus-data"}}})
	} else {
		vols = append(vols, corev1.Volume{Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
	}
	return withCPTolerations(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "prometheus", Namespace: opt.Namespace, Labels: labels("prometheus")},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "prometheus"}},
			// PVC/NFS cannot be mounted by two Prometheus pods (TSDB lock).
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"app": "prometheus", managedLabel: managedValue},
					Annotations: map[string]string{"kubeops.io/config-hash": cfgHash},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "prometheus",
					SecurityContext:    &corev1.PodSecurityContext{FSGroup: int64ptr(65534)},
					Containers: []corev1.Container{{
						Name:  "prometheus",
						Image: imgPrometheus,
						Args: []string{
							"--config.file=/etc/prometheus/prometheus.yml",
							"--storage.tsdb.path=/prometheus",
							"--storage.tsdb.retention.time=" + opt.Retention,
							"--web.enable-lifecycle",
						},
						Ports:        []corev1.ContainerPort{{Name: "http", ContainerPort: 9090}},
						VolumeMounts: volMounts,
						ReadinessProbe: &corev1.Probe{
							ProbeHandler:        corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/-/ready", Port: intstr.FromString("http")}},
							InitialDelaySeconds: 10,
							PeriodSeconds:       10,
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("512Mi")},
							Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("2Gi")},
						},
					}},
					Volumes: vols,
				},
			},
		},
	})
}

func int64ptr(v int64) *int64 { return &v }

func alertmanagerDeploy(opt stackOpts) *appsv1.Deployment {
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "alertmanager", Namespace: opt.Namespace, Labels: labels("alertmanager")},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "alertmanager"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "alertmanager", managedLabel: managedValue}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "alertmanager",
						Image: imgAlertmanager,
						Args:  []string{"--config.file=/etc/alertmanager/alertmanager.yml", "--storage.path=/alertmanager"},
						Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 9093}},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "config", MountPath: "/etc/alertmanager/alertmanager.yml", SubPath: "alertmanager.yml"},
							{Name: "data", MountPath: "/alertmanager"},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("20m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
							Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m"), corev1.ResourceMemory: resource.MustParse("256Mi")},
						},
					}},
					Volumes: []corev1.Volume{
						{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "alertmanager-config"}}}},
						{Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					},
				},
			},
		},
	}
	return withCPTolerations(d)
}

func grafanaDeploy(opt stackOpts, usePVC bool, dashHash string) *appsv1.Deployment {
	dataVol := corev1.Volume{Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}
	if usePVC {
		dataVol = corev1.Volume{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "grafana-data"}}}
	}
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "grafana", Namespace: opt.Namespace, Labels: labels("grafana")},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "grafana"}},
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"app": "grafana", managedLabel: managedValue},
					Annotations: map[string]string{"kubeops.io/config-hash": dashHash},
				},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{FSGroup: int64ptr(472)},
					Containers: []corev1.Container{{
						Name:  "grafana",
						Image: imgGrafana,
						Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 3000}},
						Env: []corev1.EnvVar{
							{Name: "GF_SECURITY_ADMIN_USER", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "grafana-admin"}, Key: "admin-user"}}},
							{Name: "GF_SECURITY_ADMIN_PASSWORD", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "grafana-admin"}, Key: "admin-password"}}},
							// Path-only root URL so redirects work behind console proxy and NodePort.
							{Name: "GF_SERVER_ROOT_URL", Value: "/kubeops/mon-ui/"},
							{Name: "GF_SERVER_SERVE_FROM_SUB_PATH", Value: "true"},
							{Name: "GF_SECURITY_ALLOW_EMBEDDING", Value: "true"},
							{Name: "GF_USERS_ALLOW_SIGN_UP", Value: "false"},
							{Name: "GF_USERS_DEFAULT_THEME", Value: "dark"},
							{Name: "GF_DEFAULT_THEME", Value: "dark"},
							// Console is already OIDC-gated. Auth proxy + anonymous avoids Grafana login
							// and stops /api/user 401 "Unauthorized" toasts on every refresh.
							{Name: "GF_AUTH_ANONYMOUS_ENABLED", Value: "true"},
							{Name: "GF_AUTH_ANONYMOUS_ORG_ROLE", Value: "Admin"},
							{Name: "GF_AUTH_DISABLE_LOGIN_FORM", Value: "true"},
							{Name: "GF_AUTH_BASIC_ENABLED", Value: "false"},
							{Name: "GF_AUTH_PROXY_ENABLED", Value: "true"},
							{Name: "GF_AUTH_PROXY_HEADER_NAME", Value: "X-WEBAUTH-USER"},
							{Name: "GF_AUTH_PROXY_HEADER_PROPERTY", Value: "username"},
							{Name: "GF_AUTH_PROXY_AUTO_SIGN_UP", Value: "true"},
							{Name: "GF_USERS_AUTO_ASSIGN_ORG", Value: "true"},
							{Name: "GF_USERS_AUTO_ASSIGN_ORG_ROLE", Value: "Admin"},
							{Name: "GF_LIVE_ALLOWED_ORIGINS", Value: "*"},
							{Name: "GF_DASHBOARDS_DEFAULT_HOME_DASHBOARD_PATH", Value: "/var/lib/grafana/dashboards/kubeops/kubernetes-enterprise-observability.json"},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "data", MountPath: "/var/lib/grafana"},
							{Name: "datasources", MountPath: "/etc/grafana/provisioning/datasources"},
							{Name: "providers", MountPath: "/etc/grafana/provisioning/dashboards"},
							{Name: "dashboards", MountPath: "/var/lib/grafana/dashboards/kubeops"},
						},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler:        corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/api/health", Port: intstr.FromString("http")}},
							InitialDelaySeconds: 15,
							PeriodSeconds:       10,
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("128Mi")},
							Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("512Mi")},
						},
					}},
					Volumes: []corev1.Volume{
						dataVol,
						{Name: "datasources", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "grafana-datasources"}}}},
						{Name: "providers", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "grafana-dashboards-provider"}}}},
						{Name: "dashboards", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "grafana-dashboards"}}}},
					},
				},
			},
		},
	}
	return withCPTolerations(d)
}

func ksmDeploy(opt stackOpts) *appsv1.Deployment {
	return withCPTolerations(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "kube-state-metrics", Namespace: opt.Namespace, Labels: labels("kube-state-metrics")},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "kube-state-metrics"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "kube-state-metrics", managedLabel: managedValue}},
				Spec: corev1.PodSpec{
					ServiceAccountName: "prometheus",
					Containers: []corev1.Container{{
						Name:  "kube-state-metrics",
						Image: imgKSM,
						Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("20m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
							Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m"), corev1.ResourceMemory: resource.MustParse("256Mi")},
						},
					}},
				},
			},
		},
	})
}

func nodeExporterDS(opt stackOpts) *appsv1.DaemonSet {
	hostRoot := corev1.HostPathDirectory
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "node-exporter", Namespace: opt.Namespace, Labels: labels("node-exporter")},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "node-exporter"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "node-exporter", managedLabel: managedValue}},
				Spec: corev1.PodSpec{
					HostNetwork: true,
					HostPID:     true,
					Tolerations: []corev1.Toleration{{Operator: corev1.TolerationOpExists}},
					Containers: []corev1.Container{{
						Name:  "node-exporter",
						Image: imgNodeExporter,
						Args: []string{
							"--path.rootfs=/host/root",
							"--path.procfs=/host/proc",
							"--path.sysfs=/host/sys",
							"--web.listen-address=$(HOST_IP):9100",
						},
						Env: []corev1.EnvVar{{
							Name: "HOST_IP",
							ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"}},
						}},
						Ports: []corev1.ContainerPort{{Name: "metrics", ContainerPort: 9100, HostPort: 9100}},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "proc", MountPath: "/host/proc", ReadOnly: true},
							{Name: "sys", MountPath: "/host/sys", ReadOnly: true},
							{Name: "root", MountPath: "/host/root", ReadOnly: true, MountPropagation: mountPropagationHostToContainer()},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("20m"), corev1.ResourceMemory: resource.MustParse("32Mi")},
							Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m"), corev1.ResourceMemory: resource.MustParse("128Mi")},
						},
					}},
					Volumes: []corev1.Volume{
						{Name: "proc", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/proc", Type: &hostRoot}}},
						{Name: "sys", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/sys", Type: &hostRoot}}},
						{Name: "root", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/", Type: &hostRoot}}},
					},
				},
			},
		},
	}
}

func mountPropagationHostToContainer() *corev1.MountPropagationMode {
	m := corev1.MountPropagationHostToContainer
	return &m
}

func nodePortService(ns, name string, port int32, portName string, nodePort int32) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels(name)},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeNodePort,
			Selector: map[string]string{"app": name},
			Ports: []corev1.ServicePort{{
				Name:       portName,
				Port:       port,
				TargetPort: intstr.FromString(portName),
				NodePort:   nodePort,
			}},
		},
	}
}

func clusterIPService(ns, name string, port int32, portName string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels(name)},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: map[string]string{"app": name},
			Ports: []corev1.ServicePort{{
				Name:       portName,
				Port:       port,
				TargetPort: intstr.FromString(portName),
			}},
		},
	}
}

func (r *MonitoringReconciler) stackReady(ctx context.Context, ns string, wantAM bool) (bool, string, error) {
	needed := []string{"prometheus", "grafana", "kube-state-metrics"}
	if wantAM {
		needed = append(needed, "alertmanager")
	}
	for _, name := range needed {
		d := &appsv1.Deployment{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, d); err != nil {
			return false, fmt.Sprintf("waiting for deploy/%s", name), client.IgnoreNotFound(err)
		}
		if d.Status.ReadyReplicas < 1 {
			return false, fmt.Sprintf("waiting for deploy/%s ready", name), nil
		}
	}
	ds := &appsv1.DaemonSet{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: ns, Name: "node-exporter"}, ds); err != nil {
		return false, "waiting for daemonset/node-exporter", client.IgnoreNotFound(err)
	}
	if ds.Status.NumberReady < 1 {
		return false, "waiting for node-exporter ready", nil
	}
	return true, "", nil
}

func (r *MonitoringReconciler) cleanupStack(ctx context.Context, ns string) error {
	// Delete managed workloads; keep namespace (virt lesson).
	var deploys appsv1.DeploymentList
	if err := r.List(ctx, &deploys, client.InNamespace(ns), client.MatchingLabels{managedLabel: managedValue}); err == nil {
		for i := range deploys.Items {
			_ = r.Delete(ctx, &deploys.Items[i])
		}
	}
	var dss appsv1.DaemonSetList
	if err := r.List(ctx, &dss, client.InNamespace(ns), client.MatchingLabels{managedLabel: managedValue}); err == nil {
		for i := range dss.Items {
			_ = r.Delete(ctx, &dss.Items[i])
		}
	}
	var svcs corev1.ServiceList
	if err := r.List(ctx, &svcs, client.InNamespace(ns), client.MatchingLabels{managedLabel: managedValue}); err == nil {
		for i := range svcs.Items {
			_ = r.Delete(ctx, &svcs.Items[i])
		}
	}
	var cms corev1.ConfigMapList
	if err := r.List(ctx, &cms, client.InNamespace(ns), client.MatchingLabels{managedLabel: managedValue}); err == nil {
		for i := range cms.Items {
			_ = r.Delete(ctx, &cms.Items[i])
		}
	}
	var secs corev1.SecretList
	if err := r.List(ctx, &secs, client.InNamespace(ns), client.MatchingLabels{managedLabel: managedValue}); err == nil {
		for i := range secs.Items {
			_ = r.Delete(ctx, &secs.Items[i])
		}
	}
	var pvcs corev1.PersistentVolumeClaimList
	if err := r.List(ctx, &pvcs, client.InNamespace(ns), client.MatchingLabels{managedLabel: managedValue}); err == nil {
		for i := range pvcs.Items {
			_ = r.Delete(ctx, &pvcs.Items[i])
		}
	}
	_ = r.Delete(ctx, &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "kubeops-monitoring-prometheus"}})
	_ = r.Delete(ctx, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "kubeops-monitoring-prometheus"}})
	return nil
}
