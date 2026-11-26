package ceph

import (
	"context"
	"fmt"
	"strings"
	"time"

	dashboardv1alpha1 "github.com/Deepak-Das01/kubeops-platform/operator/api/v1alpha1"
	opstatus "github.com/Deepak-Das01/kubeops-platform/operator/internal/status"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	operatorReportName = "kubeops-ceph"
	operatorVersion    = "1.3.0"
	finalizerName      = "dashboard.ocp/ceph"
	defaultNamespace   = "kubeops-ceph-cluster"
)

// RookCephReconciler installs Rook + CephCluster + RBD/CephFS StorageClasses.
type RookCephReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *RookCephReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dashboardv1alpha1.RookCeph{}).
		Complete(r)
}

func (r *RookCephReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("rookceph", req.Name)
	cr := &dashboardv1alpha1.RookCeph{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	opt := defaultsFromCR(cr)

	if !cr.DeletionTimestamp.IsZero() {
		done, err := r.cleanup(ctx, cr, opt)
		if err != nil {
			msg := fmt.Sprintf("Uninstalling: %v", err)
			_ = r.patchStatus(ctx, cr, dashboardv1alpha1.RookCephPhaseProgressing, msg, opt.Namespace, "", nil, "")
			_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentCeph, operatorVersion,
				dashboardv1alpha1.OperatorPhaseProgressing, "", msg)
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}
		if !done {
			msg := "Uninstalling Ceph — waiting for wipe Jobs…"
			_ = r.patchStatus(ctx, cr, dashboardv1alpha1.RookCephPhaseProgressing, msg, opt.Namespace, "", nil, "")
			_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentCeph, operatorVersion,
				dashboardv1alpha1.OperatorPhaseProgressing, "", msg)
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}
		controllerutil.RemoveFinalizer(cr, finalizerName)
		if err := r.Update(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentCeph, operatorVersion,
			dashboardv1alpha1.OperatorPhaseUnavailable, "",
			"Unavailable — Ceph not installed; deploy from Addons")
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(cr, finalizerName) {
		controllerutil.AddFinalizer(cr, finalizerName)
		if err := r.Update(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if err := validateNodes(opt); err != nil {
		msg := err.Error()
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.RookCephPhaseFailed, msg, opt.Namespace, "", nil, "")
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentCeph, operatorVersion,
			dashboardv1alpha1.OperatorPhaseDegraded, "", msg)
		return ctrl.Result{}, nil
	}

	if cr.Status.Phase == "" {
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.RookCephPhaseProgressing,
			"Installing Rook operator…", opt.Namespace, "", nil, "")
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentCeph, operatorVersion,
			dashboardv1alpha1.OperatorPhaseProgressing, "", "Installing Rook operator…")
	}

	if err := applyRookOperator(ctx, r.Client, opt.Namespace); err != nil {
		return r.fail(ctx, cr, opt, fmt.Errorf("rook operator: %w", err))
	}
	// Tolerations before taints so rook-ceph-operator/CSI survive dedicated-node taints.
	if err := ensureStorageTaintTolerations(ctx, r.Client, opt); err != nil {
		return r.fail(ctx, cr, opt, fmt.Errorf("storage taint tolerations: %w", err))
	}
	if !operatorDeployReady(ctx, r.Client, opt.Namespace) {
		msg := "Waiting for rook-ceph-operator Deployment…"
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.RookCephPhaseProgressing, msg, opt.Namespace, "", nil, "")
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentCeph, operatorVersion,
			dashboardv1alpha1.OperatorPhaseProgressing, "", msg)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if err := labelNodes(ctx, r.Client, opt); err != nil {
		return r.fail(ctx, cr, opt, err)
	}
	if err := taintNodes(ctx, r.Client, opt); err != nil {
		return r.fail(ctx, cr, opt, fmt.Errorf("taint storage nodes: %w", err))
	}

	if err := ensureCephCluster(ctx, r.Client, opt); err != nil {
		if isMissingAPI(err) {
			msg := "Waiting for CephCluster CRD…"
			_ = r.patchStatus(ctx, cr, dashboardv1alpha1.RookCephPhaseProgressing, msg, opt.Namespace, "", nil, "")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		return r.fail(ctx, cr, opt, fmt.Errorf("CephCluster: %w", err))
	}

	formed, health, waitMsg, err := clusterFormed(ctx, r.Client, opt.Namespace)
	if err != nil {
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}
	if !formed {
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.RookCephPhaseProgressing, waitMsg, opt.Namespace, health, nil, "")
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentCeph, operatorVersion,
			dashboardv1alpha1.OperatorPhaseProgressing, "", waitMsg)
		return ctrl.Result{RequeueAfter: 20 * time.Second}, nil
	}

	if opt.EnableToolbox {
		if err := applyToolbox(ctx, r.Client, opt.Namespace, opt.CephImage); err != nil {
			logger.Info("toolbox (non-fatal)", "err", err)
		}
		if err := ensureToolboxToleration(ctx, r.Client, opt); err != nil {
			logger.Info("toolbox toleration (non-fatal)", "err", err)
		}
	}

	if err := ensureBlockPoolAndSC(ctx, r.Client, opt); err != nil {
		if isMissingAPI(err) {
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		return r.fail(ctx, cr, opt, fmt.Errorf("block pool/SC: %w", err))
	}
	if err := ensureFilesystemAndSC(ctx, r.Client, opt); err != nil {
		if isMissingAPI(err) {
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		return r.fail(ctx, cr, opt, fmt.Errorf("filesystem/SC: %w", err))
	}
	if err := ensureDashboardNodePort(ctx, r.Client, opt); err != nil {
		return r.fail(ctx, cr, opt, fmt.Errorf("dashboard NodePort: %w", err))
	}

	// Align crush/.mgr/mon warn so we reach HEALTH_OK without manual ceph CLI.
	ok, health, err := cephHealthOK(ctx, r.Client, opt.Namespace)
	if err != nil {
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}
	if !ok {
		fixDone, fixErr := ensureHealthFixJob(ctx, r.Client, opt)
		if fixErr != nil {
			logger.Info("health-fix job", "err", fixErr)
		}
		msg := fmt.Sprintf("Waiting for Ceph HEALTH_OK (current %s)", health)
		if !fixDone {
			msg = fmt.Sprintf("Applying pool/crush health fix (current %s)…", health)
		}
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.RookCephPhaseProgressing, msg, opt.Namespace, health, nil, "")
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentCeph, operatorVersion,
			dashboardv1alpha1.OperatorPhaseProgressing, "", msg)
		return ctrl.Result{RequeueAfter: 20 * time.Second}, nil
	}

	// Sweep leftover Job pods from prior installs / non-cascading Job deletes.
	_ = deleteJob(ctx, r.Client, opt.Namespace, healthFixJobName)
	_ = deleteFinishedCleanupJobs(ctx, r.Client, opt.Namespace)
	_ = gcOrphanJobPods(ctx, r.Client, opt.Namespace)

	scs := []string{blockSCName, fsSCName}
	url := ""
	if opt.EnableDashboard {
		url = dashboardURL(ctx, r.Client, opt.DashboardNodePort)
	}
	msg := fmt.Sprintf("Ready — Ceph %s + %s (RWO) + %s (RWX)", health, blockSCName, fsSCName)
	_ = r.patchStatus(ctx, cr, dashboardv1alpha1.RookCephPhaseReady, msg, opt.Namespace, health, scs, url)
	_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentCeph, operatorVersion,
		dashboardv1alpha1.OperatorPhaseReady, url, msg)
	return ctrl.Result{RequeueAfter: 2 * time.Minute}, nil
}

func validateNodes(opt clusterOpts) error {
	if len(opt.Nodes) == 0 {
		return fmt.Errorf("spec.nodes is required — select at least one node and disk")
	}
	ok := false
	for _, n := range opt.Nodes {
		if strings.TrimSpace(n.Name) == "" || n.Name == "REPLACE_ME" {
			continue
		}
		for _, d := range n.Devices {
			d = strings.TrimSpace(d)
			if d != "" && d != "REPLACE_ME" {
				ok = true
			}
		}
	}
	if !ok {
		return fmt.Errorf("spec.nodes must list real node names and devices (REPLACE_ME is not valid)")
	}
	return nil
}

func (r *RookCephReconciler) fail(ctx context.Context, cr *dashboardv1alpha1.RookCeph, opt clusterOpts, err error) (ctrl.Result, error) {
	msg := fmt.Sprintf("Install error: %v", err)
	_ = r.patchStatus(ctx, cr, dashboardv1alpha1.RookCephPhaseDegraded, msg, opt.Namespace, "", nil, "")
	_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentCeph, operatorVersion,
		dashboardv1alpha1.OperatorPhaseDegraded, "", msg)
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func isMissingAPI(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return apierrors.IsNotFound(err) ||
		strings.Contains(s, "no matches for kind") ||
		strings.Contains(s, "failed to get API group resources") ||
		strings.Contains(s, "could not find the requested resource")
}

func (r *RookCephReconciler) patchStatus(ctx context.Context, cr *dashboardv1alpha1.RookCeph, phase dashboardv1alpha1.RookCephPhase, msg, ns, health string, scs []string, dashURL string) error {
	latest := &dashboardv1alpha1.RookCeph{}
	if err := r.Get(ctx, client.ObjectKey{Name: cr.Name}, latest); err != nil {
		return err
	}
	sameSC := len(latest.Status.StorageClasses) == len(scs)
	if sameSC {
		for i := range scs {
			if latest.Status.StorageClasses[i] != scs[i] {
				sameSC = false
				break
			}
		}
	}
	if latest.Status.Phase == phase &&
		latest.Status.Message == msg &&
		latest.Status.Namespace == ns &&
		latest.Status.CephHealth == health &&
		latest.Status.DashboardURL == dashURL &&
		sameSC &&
		latest.Status.ObservedGeneration == latest.Generation {
		return nil
	}
	latest.Status.Phase = phase
	latest.Status.Message = msg
	latest.Status.Namespace = ns
	latest.Status.CephHealth = health
	latest.Status.DashboardURL = dashURL
	latest.Status.StorageClasses = scs
	latest.Status.ObservedGeneration = latest.Generation
	return r.Status().Update(ctx, latest)
}
