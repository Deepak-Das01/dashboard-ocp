package virtualization

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
	operatorReportName = "kubeops-virtualization"
	operatorVersion    = "1.3.0"
	finalizerName      = "dashboard.ocp/virtualization"
)

// VirtualizationReconciler installs KubeVirt + CDI from vendored release manifests.
type VirtualizationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *VirtualizationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dashboardv1alpha1.Virtualization{}).
		Complete(r)
}

func (r *VirtualizationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("virtualization", req.Name)
	cr := &dashboardv1alpha1.Virtualization{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	kvVer, cdiVer := resolveVersions(cr.Spec.KubeVirtVersion, cr.Spec.CDIVersion)

	if !cr.DeletionTimestamp.IsZero() {
		done, err := r.cleanup(ctx, cr, kvVer, cdiVer)
		if err != nil {
			_ = r.patchStatus(ctx, cr, dashboardv1alpha1.VirtualizationPhaseProgressing,
				fmt.Sprintf("Uninstalling: %v", err), "", "", kvVer, cdiVer, "")
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}
		if !done {
			_ = r.patchStatus(ctx, cr, dashboardv1alpha1.VirtualizationPhaseProgressing,
				"Uninstalling KubeVirt/CDI/UI — waiting for teardown", "", "", kvVer, cdiVer, "")
			_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentVirtualization, operatorVersion,
				dashboardv1alpha1.OperatorPhaseProgressing, "", "Uninstalling virtualization stack…")
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		controllerutil.RemoveFinalizer(cr, finalizerName)
		if err := r.Update(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentVirtualization, operatorVersion,
			dashboardv1alpha1.OperatorPhaseUnavailable, "",
			"Unavailable — virtualization not installed; deploy from Addons")
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(cr, finalizerName) {
		controllerutil.AddFinalizer(cr, finalizerName)
		if err := r.Update(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Move legacy kubevirt/cdi installs into kubeops-virtualization before proceeding.
	if waiting, err := r.migrateLegacyNamespaces(ctx, kvVer, cdiVer); err != nil {
		return r.fail(ctx, cr, kvVer, cdiVer, err)
	} else if waiting {
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.VirtualizationPhaseProgressing,
			"Migrating KubeVirt/CDI into namespace kubeops-virtualization…", "", "", kvVer, cdiVer, "")
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentVirtualization, operatorVersion,
			dashboardv1alpha1.OperatorPhaseProgressing, "", "Migrating stack into kubeops-virtualization…")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Fast path: if both CRs are already Deployed, ensure UI + report Ready.
	kvPhase, kvErr := getPhase(ctx, r.Client, kubeVirtGVK, installNamespace, "kubevirt")
	cdiPhase, cdiErr := getPhase(ctx, r.Client, cdiGVK, "", "cdi")
	if kvErr == nil && cdiErr == nil &&
		strings.EqualFold(kvPhase, "Deployed") && strings.EqualFold(cdiPhase, "Deployed") {
		if err := ensureKubeVirtCR(ctx, r.Client, kvVer, cr.Spec.UseEmulation); err != nil {
			logger.Info("ensure kubevirt CR (non-fatal)", "err", err)
		}
		if err := applyUIManifest(ctx, r.Client); err != nil {
			return r.fail(ctx, cr, kvVer, cdiVer, fmt.Errorf("kubevirt-manager UI: %w", err))
		}
		url := uiURL(ctx, r.Client)
		if !uiReady(ctx, r.Client) {
			msg := "KubeVirt+CDI Ready — waiting for kubevirt-manager UI…"
			_ = r.patchStatus(ctx, cr, dashboardv1alpha1.VirtualizationPhaseProgressing, msg, kvPhase, cdiPhase, kvVer, cdiVer, url)
			_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentVirtualization, operatorVersion,
				dashboardv1alpha1.OperatorPhaseProgressing, url, msg)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		okMsg := fmt.Sprintf("Ready — KubeVirt %s + CDI %s + UI", kvVer, cdiVer)
		if cr.Spec.UseEmulation {
			okMsg += " (emulation enabled)"
		}
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.VirtualizationPhaseReady, okMsg, kvPhase, cdiPhase, kvVer, cdiVer, url)
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentVirtualization, operatorVersion,
			dashboardv1alpha1.OperatorPhaseReady, url, okMsg)
		return ctrl.Result{RequeueAfter: 2 * time.Minute}, nil
	}

	needKVOperator := !crdExists(ctx, r.Client, "kubevirts.kubevirt.io") ||
		!deployExists(ctx, r.Client, installNamespace, "virt-operator")
	if needKVOperator {
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.VirtualizationPhaseProgressing,
			"Installing KubeVirt operator into kubeops-virtualization…", "", "", kvVer, cdiVer, "")
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentVirtualization, operatorVersion,
			dashboardv1alpha1.OperatorPhaseProgressing, "", "Installing KubeVirt operator…")
		if err := applyReleaseManifest(ctx, r.Client, "kubevirt-operator", kvVer); err != nil {
			return r.fail(ctx, cr, kvVer, cdiVer, fmt.Errorf("kubevirt-operator: %w", err))
		}
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	if err := ensureKubeVirtCR(ctx, r.Client, kvVer, cr.Spec.UseEmulation); err != nil {
		if isMissingAPI(err) {
			_ = r.patchStatus(ctx, cr, dashboardv1alpha1.VirtualizationPhaseProgressing,
				"Waiting for KubeVirt CRD to register…", "", "", kvVer, cdiVer, "")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		return r.fail(ctx, cr, kvVer, cdiVer, fmt.Errorf("kubevirt CR: %w", err))
	}

	needCDIOperator := !crdExists(ctx, r.Client, "cdis.cdi.kubevirt.io") ||
		!deployExists(ctx, r.Client, installNamespace, "cdi-operator")
	if needCDIOperator {
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.VirtualizationPhaseProgressing,
			"Installing CDI operator into kubeops-virtualization…", kvPhase, "", kvVer, cdiVer, "")
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentVirtualization, operatorVersion,
			dashboardv1alpha1.OperatorPhaseProgressing, "", "Installing CDI operator…")
		if err := applyReleaseManifest(ctx, r.Client, "cdi-operator", cdiVer); err != nil {
			return r.fail(ctx, cr, kvVer, cdiVer, fmt.Errorf("cdi-operator: %w", err))
		}
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	if err := ensureCDICR(ctx, r.Client, cdiVer); err != nil {
		if isMissingAPI(err) {
			_ = r.patchStatus(ctx, cr, dashboardv1alpha1.VirtualizationPhaseProgressing,
				"Waiting for CDI CRD to register…", kvPhase, "", kvVer, cdiVer, "")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		return r.fail(ctx, cr, kvVer, cdiVer, fmt.Errorf("cdi CR: %w", err))
	}

	// Refresh phases after ensure.
	kvPhase, kvErr = getPhase(ctx, r.Client, kubeVirtGVK, installNamespace, "kubevirt")
	cdiPhase, cdiErr = getPhase(ctx, r.Client, cdiGVK, "", "cdi")
	if kvErr != nil {
		kvPhase = "Unknown"
	}
	if cdiErr != nil {
		cdiPhase = "Unknown"
	}

	kvReady := strings.EqualFold(kvPhase, "Deployed")
	cdiReady := strings.EqualFold(cdiPhase, "Deployed")
	if !kvReady || !cdiReady {
		progress := fmt.Sprintf("Waiting for stack — KubeVirt=%s CDI=%s (images may still be pulling)", kvPhase, cdiPhase)
		logger.Info(progress)
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.VirtualizationPhaseProgressing, progress, kvPhase, cdiPhase, kvVer, cdiVer, "")
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentVirtualization, operatorVersion,
			dashboardv1alpha1.OperatorPhaseProgressing, "", progress)
		return ctrl.Result{RequeueAfter: 20 * time.Second}, nil
	}

	// Stack ready — install kubevirt-manager UI into kubeops-virtualization.
	if err := applyUIManifest(ctx, r.Client); err != nil {
		return r.fail(ctx, cr, kvVer, cdiVer, fmt.Errorf("kubevirt-manager UI: %w", err))
	}
	url := uiURL(ctx, r.Client)
	if !uiReady(ctx, r.Client) {
		msg := "KubeVirt+CDI Ready — waiting for kubevirt-manager UI…"
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.VirtualizationPhaseProgressing, msg, kvPhase, cdiPhase, kvVer, cdiVer, url)
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentVirtualization, operatorVersion,
			dashboardv1alpha1.OperatorPhaseProgressing, url, msg)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	okMsg := fmt.Sprintf("Ready — KubeVirt %s + CDI %s + UI", kvVer, cdiVer)
	if cr.Spec.UseEmulation {
		okMsg += " (emulation enabled)"
	}
	_ = r.patchStatus(ctx, cr, dashboardv1alpha1.VirtualizationPhaseReady, okMsg, kvPhase, cdiPhase, kvVer, cdiVer, url)
	_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentVirtualization, operatorVersion,
		dashboardv1alpha1.OperatorPhaseReady, url, okMsg)
	return ctrl.Result{RequeueAfter: 2 * time.Minute}, nil
}

func (r *VirtualizationReconciler) fail(ctx context.Context, cr *dashboardv1alpha1.Virtualization, kvVer, cdiVer string, err error) (ctrl.Result, error) {
	msg := err.Error()
	_ = r.patchStatus(ctx, cr, dashboardv1alpha1.VirtualizationPhaseDegraded, msg, "", "", kvVer, cdiVer, "")
	_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentVirtualization, operatorVersion,
		dashboardv1alpha1.OperatorPhaseDegraded, "", msg)
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *VirtualizationReconciler) patchStatus(ctx context.Context, cr *dashboardv1alpha1.Virtualization, phase dashboardv1alpha1.VirtualizationPhase, msg, kvPhase, cdiPhase, kvVer, cdiVer, url string) error {
	fresh := &dashboardv1alpha1.Virtualization{}
	if err := r.Get(ctx, client.ObjectKey{Name: cr.Name}, fresh); err != nil {
		return err
	}
	fresh.Status.Phase = phase
	fresh.Status.Message = msg
	fresh.Status.KubeVirtPhase = kvPhase
	fresh.Status.CDIPhase = cdiPhase
	fresh.Status.KubeVirtVersion = kvVer
	fresh.Status.CDIVersion = cdiVer
	fresh.Status.UIURL = url
	fresh.Status.ObservedGeneration = fresh.Generation
	if err := r.Status().Update(ctx, fresh); err != nil {
		if apierrors.IsConflict(err) {
			return nil
		}
		return err
	}
	return nil
}

// migrateLegacyNamespaces tears down installs left in kubevirt/cdi namespaces.
// Returns waiting=true while teardown is still in progress.
func (r *VirtualizationReconciler) migrateLegacyNamespaces(ctx context.Context, kvVer, cdiVer string) (bool, error) {
	legacyKV := false
	if _, err := getPhase(ctx, r.Client, kubeVirtGVK, legacyKubeVirtNS, "kubevirt"); err == nil {
		legacyKV = true
		_ = deleteCR(ctx, r.Client, kubeVirtGVK, legacyKubeVirtNS, "kubevirt")
	} else if err != nil && !isMissingAPI(err) {
		return false, err
	}
	if deployExists(ctx, r.Client, legacyKubeVirtNS, "virt-operator") {
		legacyKV = true
		// Only delete operator once the CR is gone (avoid fighting teardown).
		if _, err := getPhase(ctx, r.Client, kubeVirtGVK, legacyKubeVirtNS, "kubevirt"); isMissingAPI(err) {
			_ = deleteReleaseManifestFrom(ctx, r.Client, "kubevirt-operator", kvVer, "")
		}
	}

	legacyCDI := false
	if deployExists(ctx, r.Client, legacyCDINS, "cdi-operator") {
		legacyCDI = true
		_ = deleteCR(ctx, r.Client, cdiGVK, "", "cdi")
		if _, err := getPhase(ctx, r.Client, cdiGVK, "", "cdi"); isMissingAPI(err) {
			_ = deleteReleaseManifestFrom(ctx, r.Client, "cdi-operator", cdiVer, "")
		} else if err == nil {
			// CR still present — wait for operator to finish uninstall.
		} else if !isMissingAPI(err) {
			return false, err
		}
	}

	if legacyKV || legacyCDI {
		return true, nil
	}
	return false, nil
}

// cleanup deletes CRs then operator manifests. Returns done=true when teardown finished.
func (r *VirtualizationReconciler) cleanup(ctx context.Context, cr *dashboardv1alpha1.Virtualization, kvVer, cdiVer string) (bool, error) {
	_ = deleteCR(ctx, r.Client, kubeVirtGVK, installNamespace, "kubevirt")
	_ = deleteCR(ctx, r.Client, kubeVirtGVK, legacyKubeVirtNS, "kubevirt")
	_ = deleteCR(ctx, r.Client, cdiGVK, "", "cdi")

	if crdExists(ctx, r.Client, "kubevirts.kubevirt.io") {
		if _, err := getPhase(ctx, r.Client, kubeVirtGVK, installNamespace, "kubevirt"); err == nil {
			return false, nil
		} else if err != nil && !isMissingAPI(err) {
			return false, err
		}
		if _, err := getPhase(ctx, r.Client, kubeVirtGVK, legacyKubeVirtNS, "kubevirt"); err == nil {
			return false, nil
		} else if err != nil && !isMissingAPI(err) {
			return false, err
		}
	}
	if crdExists(ctx, r.Client, "cdis.cdi.kubevirt.io") {
		if _, err := getPhase(ctx, r.Client, cdiGVK, "", "cdi"); err == nil {
			return false, nil
		} else if err != nil && !isMissingAPI(err) {
			return false, err
		}
	}

	_ = deleteUIManifest(ctx, r.Client)

	if err := deleteReleaseManifest(ctx, r.Client, "cdi-operator", cdiVer); err != nil {
		return false, err
	}
	_ = deleteReleaseManifestFrom(ctx, r.Client, "cdi-operator", cdiVer, "")
	if err := deleteReleaseManifest(ctx, r.Client, "kubevirt-operator", kvVer); err != nil {
		return false, err
	}
	_ = deleteReleaseManifestFrom(ctx, r.Client, "kubevirt-operator", kvVer, "")
	return true, nil
}
