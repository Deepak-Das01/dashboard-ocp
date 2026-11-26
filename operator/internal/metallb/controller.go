package metallb

import (
	"context"
	"fmt"
	"strings"
	"time"

	dashboardv1alpha1 "github.com/Deepak-Das01/kubeops-platform/operator/api/v1alpha1"
	opstatus "github.com/Deepak-Das01/kubeops-platform/operator/internal/status"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	operatorReportName = "kubeops-metallb"
	operatorVersion    = "1.3.0"
	finalizerName      = "dashboard.ocp/metallb"
)

// MetalLBReconciler installs upstream MetalLB + L2 IP pool from the CR.
type MetalLBReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *MetalLBReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dashboardv1alpha1.MetalLB{}).
		Complete(r)
}

func (r *MetalLBReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("metallb", req.Name)
	cr := &dashboardv1alpha1.MetalLB{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !cr.DeletionTimestamp.IsZero() {
		if err := r.cleanup(ctx, cr); err != nil {
			_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentMetalLB, operatorVersion,
				dashboardv1alpha1.OperatorPhaseProgressing, "", "Uninstalling MetalLB…")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		controllerutil.RemoveFinalizer(cr, finalizerName)
		if err := r.Update(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentMetalLB, operatorVersion,
			dashboardv1alpha1.OperatorPhaseUnavailable, "",
			"Unavailable — MetalLB not installed; deploy from Addons with an L2 address pool")
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(cr, finalizerName) {
		controllerutil.AddFinalizer(cr, finalizerName)
		if err := r.Update(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	ns, poolName, l2Name, addresses := defaultsFromCR(cr)
	if len(addresses) == 0 {
		msg := "addresses is required (one or more CIDRs or ranges)"
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.MetalLBPhaseDegraded, msg, ns, poolName, addresses)
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentMetalLB, operatorVersion,
			dashboardv1alpha1.OperatorPhaseDegraded, "", msg)
		return ctrl.Result{}, nil
	}

	okMsg := fmt.Sprintf("Ready — L2 pool %s (%s)", poolName, strings.Join(addresses, ", "))
	alreadyReady := cr.Status.Phase == dashboardv1alpha1.MetalLBPhaseReady &&
		cr.Status.Message == okMsg &&
		cr.Status.ObservedGeneration == cr.Generation

	// Steady-state: do not re-apply the full native YAML (avoids Deploy churn / webhook fights).
	// Only keep pool + L2 in sync and re-heal if workloads dropped.
	if alreadyReady {
		ready, msg := r.workloadsReady(ctx, ns)
		if ready {
			if err := ensureIPAddressPool(ctx, r.Client, ns, poolName, addresses); err != nil {
				return r.fail(ctx, cr, ns, poolName, addresses, fmt.Errorf("IPAddressPool: %w", err))
			}
			if err := ensureL2Advertisement(ctx, r.Client, ns, l2Name, poolName); err != nil {
				return r.fail(ctx, cr, ns, poolName, addresses, fmt.Errorf("L2Advertisement: %w", err))
			}
			return ctrl.Result{}, nil
		}
		logger.Info("workloads not ready; re-applying MetalLB", "detail", msg)
	} else {
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.MetalLBPhaseProgressing, "Applying MetalLB native manifests…", ns, poolName, addresses)
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentMetalLB, operatorVersion,
			dashboardv1alpha1.OperatorPhaseProgressing, "", "Applying MetalLB native manifests…")
	}

	if err := applyMetalLBNative(ctx, r.Client, ns); err != nil {
		return r.fail(ctx, cr, ns, poolName, addresses, err)
	}

	ready, msg := r.workloadsReady(ctx, ns)
	if !ready {
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.MetalLBPhaseProgressing, msg, ns, poolName, addresses)
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentMetalLB, operatorVersion,
			dashboardv1alpha1.OperatorPhaseProgressing, "", msg)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	if err := ensureIPAddressPool(ctx, r.Client, ns, poolName, addresses); err != nil {
		return r.fail(ctx, cr, ns, poolName, addresses, fmt.Errorf("IPAddressPool: %w", err))
	}
	if err := ensureL2Advertisement(ctx, r.Client, ns, l2Name, poolName); err != nil {
		return r.fail(ctx, cr, ns, poolName, addresses, fmt.Errorf("L2Advertisement: %w", err))
	}

	if err := r.patchStatus(ctx, cr, dashboardv1alpha1.MetalLBPhaseReady, okMsg, ns, poolName, addresses); err != nil {
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
	_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentMetalLB, operatorVersion,
		dashboardv1alpha1.OperatorPhaseReady, "", okMsg)
	logger.Info("metallb ready", "namespace", ns, "pool", poolName)
	return ctrl.Result{}, nil
}

func defaultsFromCR(cr *dashboardv1alpha1.MetalLB) (ns, poolName, l2Name string, addresses []string) {
	ns = strings.TrimSpace(cr.Spec.Namespace)
	if ns == "" {
		ns = upstreamNamespace
	}
	poolName = strings.TrimSpace(cr.Spec.PoolName)
	if poolName == "" {
		poolName = defaultPoolName
	}
	l2Name = strings.TrimSpace(cr.Spec.L2AdvertisementName)
	if l2Name == "" {
		l2Name = defaultL2Name
	}
	for _, a := range cr.Spec.Addresses {
		a = strings.TrimSpace(a)
		if a != "" {
			addresses = append(addresses, a)
		}
	}
	return
}

func (r *MetalLBReconciler) fail(ctx context.Context, cr *dashboardv1alpha1.MetalLB, ns, pool string, addresses []string, err error) (ctrl.Result, error) {
	msg := err.Error()
	_ = r.patchStatus(ctx, cr, dashboardv1alpha1.MetalLBPhaseDegraded, msg, ns, pool, addresses)
	_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentMetalLB, operatorVersion,
		dashboardv1alpha1.OperatorPhaseDegraded, "", msg)
	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

func (r *MetalLBReconciler) patchStatus(ctx context.Context, cr *dashboardv1alpha1.MetalLB, phase dashboardv1alpha1.MetalLBPhase, msg, ns, pool string, addresses []string) error {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		fresh := &dashboardv1alpha1.MetalLB{}
		if err := r.Get(ctx, types.NamespacedName{Name: cr.Name}, fresh); err != nil {
			return err
		}
		if fresh.Status.Phase == phase &&
			fresh.Status.Message == msg &&
			fresh.Status.Namespace == ns &&
			fresh.Status.PoolName == pool &&
			stringSlicesEqual(fresh.Status.Addresses, addresses) &&
			fresh.Status.ObservedGeneration == fresh.Generation {
			return nil
		}
		fresh.Status.Phase = phase
		fresh.Status.Message = msg
		fresh.Status.Namespace = ns
		fresh.Status.PoolName = pool
		fresh.Status.Addresses = append([]string(nil), addresses...)
		fresh.Status.ObservedGeneration = fresh.Generation
		if err := r.Status().Update(ctx, fresh); err != nil {
			if apierrors.IsConflict(err) {
				lastErr = err
				time.Sleep(time.Duration(50*(attempt+1)) * time.Millisecond)
				continue
			}
			return err
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("status update retries exhausted")
}

func (r *MetalLBReconciler) workloadsReady(ctx context.Context, ns string) (bool, string) {
	d := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: "controller"}, d); err != nil {
		return false, "Waiting for MetalLB controller Deployment…"
	}
	if d.Status.ReadyReplicas < 1 || d.Status.AvailableReplicas < 1 {
		want := int32(1)
		if d.Spec.Replicas != nil {
			want = *d.Spec.Replicas
		}
		return false, fmt.Sprintf("Waiting for MetalLB controller (ready %d/%d)", d.Status.ReadyReplicas, want)
	}

	ds := &appsv1.DaemonSet{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: "speaker"}, ds); err != nil {
		return false, "Waiting for MetalLB speaker DaemonSet…"
	}
	if ds.Status.DesiredNumberScheduled < 1 {
		return false, "Waiting for MetalLB speaker schedule…"
	}
	if ds.Status.NumberReady < ds.Status.DesiredNumberScheduled {
		return false, fmt.Sprintf("Waiting for MetalLB speaker (ready %d/%d)", ds.Status.NumberReady, ds.Status.DesiredNumberScheduled)
	}
	return true, "MetalLB controller + speaker ready"
}

func (r *MetalLBReconciler) cleanup(ctx context.Context, cr *dashboardv1alpha1.MetalLB) error {
	ns, poolName, l2Name, _ := defaultsFromCR(cr)
	deleteConfigCRs(ctx, r.Client, ns, poolName, l2Name)
	deleteManagedWorkloads(ctx, r.Client, ns)

	// Block finalizer removal until controller Deploy + speaker DS are gone
	// so a fast re-Deploy does not race old pods.
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		d := &appsv1.Deployment{}
		derr := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: "controller"}, d)
		ds := &appsv1.DaemonSet{}
		dserr := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: "speaker"}, ds)
		goneDeploy := apierrors.IsNotFound(derr)
		goneDS := apierrors.IsNotFound(dserr)
		if goneDeploy && goneDS {
			return nil
		}
		// Re-issue deletes in case first pass raced create.
		deleteManagedWorkloads(ctx, r.Client, ns)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("timeout waiting for MetalLB workloads to terminate in %s", ns)
}
