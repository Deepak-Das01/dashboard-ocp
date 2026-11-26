package monitoring

import (
	"context"
	"fmt"
	"strings"
	"time"

	dashboardv1alpha1 "github.com/Deepak-Das01/kubeops-platform/operator/api/v1alpha1"
	opstatus "github.com/Deepak-Das01/kubeops-platform/operator/internal/status"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	operatorReportName = "kubeops-monitoring"
	operatorVersion    = "1.3.0"
	finalizerName      = "dashboard.ocp/monitoring"
	defaultNamespace   = "kubeops-monitoring"
	defaultSC          = "kubeops-nfs-storage"
	defaultRetention   = "15d"
	managedLabel       = "dashboard.ocp/addon"
	managedValue       = "kubeops-monitoring"
)

// MonitoringReconciler installs the kubeops monitoring stack.
type MonitoringReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *MonitoringReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dashboardv1alpha1.Monitoring{}).
		Complete(r)
}

func (r *MonitoringReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("monitoring", req.Name)
	cr := &dashboardv1alpha1.Monitoring{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	ns := strings.TrimSpace(cr.Spec.Namespace)
	if ns == "" {
		ns = defaultNamespace
	}
	retention := strings.TrimSpace(cr.Spec.Retention)
	if retention == "" {
		retention = defaultRetention
	}
	user := strings.TrimSpace(cr.Spec.GrafanaAdminUser)
	if user == "" {
		user = "admin"
	}
	enableAM := true
	if cr.Spec.EnableAlertmanager != nil {
		enableAM = *cr.Spec.EnableAlertmanager
	}

	if !cr.DeletionTimestamp.IsZero() {
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.MonitoringPhaseProgressing,
			"Uninstalling monitoring stack…", ns, "", "", "", "")
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentMonitoring, operatorVersion,
			dashboardv1alpha1.OperatorPhaseProgressing, "", "Uninstalling monitoring stack…")
		if err := r.cleanupStack(ctx, ns); err != nil {
			logger.Error(err, "cleanup")
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		controllerutil.RemoveFinalizer(cr, finalizerName)
		if err := r.Update(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentMonitoring, operatorVersion,
			dashboardv1alpha1.OperatorPhaseUnavailable, "",
			"Unavailable — monitoring not installed; deploy from Addons")
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(cr, finalizerName) {
		controllerutil.AddFinalizer(cr, finalizerName)
		if err := r.Update(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	scName := strings.TrimSpace(cr.Spec.StorageClassName)
	if scName == "" {
		// Default to NFS SC when it exists; else ephemeral.
		sc := &storagev1.StorageClass{}
		if err := r.Get(ctx, types.NamespacedName{Name: defaultSC}, sc); err == nil {
			scName = defaultSC
		}
	} else if scName == "-" || strings.EqualFold(scName, "none") || strings.EqualFold(scName, "ephemeral") {
		scName = ""
	} else {
		sc := &storagev1.StorageClass{}
		if err := r.Get(ctx, types.NamespacedName{Name: scName}, sc); err != nil {
			msg := fmt.Sprintf("StorageClass %q not found — using ephemeral storage", scName)
			logger.Info(msg)
			scName = ""
			_ = r.patchStatus(ctx, cr, dashboardv1alpha1.MonitoringPhaseProgressing, msg, ns, "", "", "", "ephemeral")
		}
	}

	// Do not wipe Ready/wait status on every reconcile — that races with stackReady
	// and makes Operators/Addons flicker Progressing forever.
	if cr.Status.Phase == "" {
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.MonitoringPhaseProgressing,
			"Installing Prometheus + Grafana + exporters…", ns, "", "", "", "")
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentMonitoring, operatorVersion,
			dashboardv1alpha1.OperatorPhaseProgressing, "", "Installing monitoring stack…")
	}

	opt := stackOpts{
		Namespace:          ns,
		Retention:          retention,
		StorageClass:       scName,
		GrafanaUser:        user,
		GrafanaPassword:    strings.TrimSpace(cr.Spec.GrafanaAdminPassword),
		EnableAlertmanager: enableAM,
		Owner:              cr,
	}
	storageMode, err := r.ensureStack(ctx, opt)
	if err != nil {
		msg := fmt.Sprintf("Install error: %v", err)
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.MonitoringPhaseDegraded, msg, ns, "", "", "", storageMode)
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentMonitoring, operatorVersion,
			dashboardv1alpha1.OperatorPhaseDegraded, "", msg)
		return ctrl.Result{RequeueAfter: 20 * time.Second}, nil
	}

	// Stamp ownerRefs on key workloads (CreateOrUpdate paths that skipped scheme).
	_ = r.stampOwners(ctx, ns, cr)

	ready, waitMsg, err := r.stackReady(ctx, ns, enableAM)
	if err != nil {
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}
	gURL, pURL, aURL := serviceURLs(ctx, r.Client, enableAM)
	if !ready {
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.MonitoringPhaseProgressing, waitMsg, ns, gURL, pURL, aURL, storageMode)
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentMonitoring, operatorVersion,
			dashboardv1alpha1.OperatorPhaseProgressing, gURL, waitMsg)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	msg := "Ready — Prometheus + Grafana"
	if enableAM {
		msg += " + Alertmanager"
	}
	if storageMode == "ephemeral" {
		msg += " (ephemeral storage)"
	}
	_ = r.patchStatus(ctx, cr, dashboardv1alpha1.MonitoringPhaseReady, msg, ns, gURL, pURL, aURL, storageMode)
	_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentMonitoring, operatorVersion,
		dashboardv1alpha1.OperatorPhaseReady, gURL, msg)
	return ctrl.Result{RequeueAfter: 2 * time.Minute}, nil
}

func (r *MonitoringReconciler) stampOwners(ctx context.Context, ns string, cr *dashboardv1alpha1.Monitoring) error {
	names := []string{"prometheus", "grafana", "kube-state-metrics", "alertmanager"}
	for _, name := range names {
		d := &appsv1.Deployment{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, d); err != nil {
			continue
		}
		if metav1.GetControllerOf(d) != nil {
			continue
		}
		if err := controllerutil.SetControllerReference(cr, d, r.Scheme); err == nil {
			_ = r.Update(ctx, d)
		}
	}
	ds := &appsv1.DaemonSet{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: ns, Name: "node-exporter"}, ds); err == nil {
		if metav1.GetControllerOf(ds) == nil {
			if err := controllerutil.SetControllerReference(cr, ds, r.Scheme); err == nil {
				_ = r.Update(ctx, ds)
			}
		}
	}
	var svcs corev1.ServiceList
	if err := r.List(ctx, &svcs, client.InNamespace(ns), client.MatchingLabels{managedLabel: managedValue}); err == nil {
		for i := range svcs.Items {
			if metav1.GetControllerOf(&svcs.Items[i]) != nil {
				continue
			}
			if err := controllerutil.SetControllerReference(cr, &svcs.Items[i], r.Scheme); err == nil {
				_ = r.Update(ctx, &svcs.Items[i])
			}
		}
	}
	return nil
}

func serviceURLs(ctx context.Context, c client.Client, enableAM bool) (grafana, prometheus, alertmanager string) {
	nodeIP := firstNodeIP(ctx, c)
	if nodeIP == "" {
		nodeIP = "127.0.0.1"
	}
	grafana = fmt.Sprintf("http://%s:%d/kubeops/mon-ui/", nodeIP, grafanaNodePort)
	prometheus = fmt.Sprintf("http://%s:%d/", nodeIP, prometheusNodePort)
	if enableAM {
		alertmanager = fmt.Sprintf("http://%s:%d/", nodeIP, alertmanagerNodePort)
	}
	return
}

func firstNodeIP(ctx context.Context, c client.Client) string {
	var nodes corev1.NodeList
	if err := c.List(ctx, &nodes); err != nil || len(nodes.Items) == 0 {
		return ""
	}
	for _, a := range nodes.Items[0].Status.Addresses {
		if a.Type == corev1.NodeInternalIP && a.Address != "" {
			return a.Address
		}
	}
	return ""
}

func (r *MonitoringReconciler) patchStatus(ctx context.Context, cr *dashboardv1alpha1.Monitoring, phase dashboardv1alpha1.MonitoringPhase, msg, ns, gURL, pURL, aURL, storage string) error {
	latest := &dashboardv1alpha1.Monitoring{}
	if err := r.Get(ctx, client.ObjectKey{Name: cr.Name}, latest); err != nil {
		return err
	}
	if latest.Status.Phase == phase &&
		latest.Status.Message == msg &&
		latest.Status.Namespace == ns &&
		latest.Status.GrafanaURL == gURL &&
		latest.Status.PrometheusURL == pURL &&
		latest.Status.AlertmanagerURL == aURL &&
		latest.Status.StorageMode == storage &&
		latest.Status.ObservedGeneration == latest.Generation {
		return nil
	}
	latest.Status.Phase = phase
	latest.Status.Message = msg
	latest.Status.Namespace = ns
	latest.Status.GrafanaURL = gURL
	latest.Status.PrometheusURL = pURL
	latest.Status.AlertmanagerURL = aURL
	latest.Status.StorageMode = storage
	latest.Status.ObservedGeneration = latest.Generation
	return r.Status().Update(ctx, latest)
}
