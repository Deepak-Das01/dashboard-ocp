package controller

import (
	"context"
	"fmt"
	"time"

	dashboardv1alpha1 "github.com/Deepak-Das01/dashboard-ocp/operator/api/v1alpha1"
	opstatus "github.com/Deepak-Das01/dashboard-ocp/operator/internal/status"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// KubeDashboardReconciler — console operator (dashboard + metrics + secret/token auth).
type KubeDashboardReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *KubeDashboardReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	kd := &dashboardv1alpha1.KubeDashboard{}
	if err := r.Get(ctx, req.NamespacedName, kd); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	applyDefaults(kd)
	kd.Status.Phase = "Reconciling"
	kd.Status.ObservedGeneration = kd.Generation

	if err := r.ensureNamespace(ctx, kd.Spec.Namespace); err != nil {
		logger.Error(err, "namespace ensure failed")
		kd.Status.Phase = "Error"
		_ = r.patchStatus(ctx, kd, err.Error())
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentConsole, kd.Spec.Version,
			dashboardv1alpha1.OperatorPhaseDegraded, "", err.Error())
		return ctrl.Result{RequeueAfter: 15 * time.Second}, err
	}

	htpasswd, err := r.reconcileAuth(ctx, kd)
	if err != nil {
		logger.Error(err, "auth reconcile failed")
		kd.Status.Phase = "Error"
		_ = r.patchStatus(ctx, kd, err.Error())
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentConsole, kd.Spec.Version,
			dashboardv1alpha1.OperatorPhaseDegraded, "", err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	if err := r.reconcileConsole(ctx, kd, htpasswd); err != nil {
		logger.Error(err, "console reconcile failed")
		kd.Status.Phase = "Error"
		_ = r.patchStatus(ctx, kd, err.Error())
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentConsole, kd.Spec.Version,
			dashboardv1alpha1.OperatorPhaseDegraded, "", err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	kd.Status.Phase = "Ready"
	kd.Status.URL = fmt.Sprintf("https://<node-ip>:%d/", kd.Spec.Service.HTTPSNodePort)
	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes); err == nil && len(nodes.Items) > 0 {
		for _, a := range nodes.Items[0].Status.Addresses {
			if a.Type == corev1.NodeInternalIP && a.Address != "" {
				kd.Status.URL = fmt.Sprintf("https://%s:%d/", a.Address, kd.Spec.Service.HTTPSNodePort)
				break
			}
		}
	}
	setCondition(&kd.Status.Conditions, "Ready", metav1.ConditionTrue, "Reconciled", "Console resources applied")
	if err := r.patchStatus(ctx, kd, ""); err != nil {
		return ctrl.Result{}, err
	}

	msg := kd.Status.Auth.Message
	if msg == "" {
		msg = "Console Ready"
	}
	_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentConsole, kd.Spec.Version,
		dashboardv1alpha1.OperatorPhaseReady, kd.Status.URL, msg)

	logger.Info("reconciled KubeDashboard", "namespace", kd.Spec.Namespace, "auth", kd.Spec.Auth.Mode)
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *KubeDashboardReconciler) patchStatus(ctx context.Context, kd *dashboardv1alpha1.KubeDashboard, errMsg string) error {
	if errMsg != "" {
		setCondition(&kd.Status.Conditions, "Ready", metav1.ConditionFalse, "Error", errMsg)
	}
	return r.Status().Update(ctx, kd)
}

func setCondition(conds *[]metav1.Condition, ctype string, status metav1.ConditionStatus, reason, msg string) {
	now := metav1.Now()
	for i := range *conds {
		if (*conds)[i].Type == ctype {
			(*conds)[i].Status = status
			(*conds)[i].Reason = reason
			(*conds)[i].Message = msg
			(*conds)[i].LastTransitionTime = now
			return
		}
	}
	*conds = append(*conds, metav1.Condition{
		Type:               ctype,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: now,
	})
}

func (r *KubeDashboardReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dashboardv1alpha1.KubeDashboard{}).
		Complete(r)
}
