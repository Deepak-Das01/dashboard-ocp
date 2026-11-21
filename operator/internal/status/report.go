package status

import (
	"context"
	"fmt"

	dashboardv1alpha1 "github.com/Deepak-Das01/dashboard-ocp/operator/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Report upserts a cluster-scoped Operator row for `kubectl get operators`.
func Report(ctx context.Context, c client.Client, name string, component dashboardv1alpha1.OperatorComponent, version string, phase dashboardv1alpha1.OperatorPhase, url, message string) error {
	op := &dashboardv1alpha1.Operator{ObjectMeta: metav1.ObjectMeta{Name: name}}
	_, err := controllerutil.CreateOrUpdate(ctx, c, op, func() error {
		op.Spec.Component = component
		op.Spec.Version = version
		return nil
	})
	if err != nil {
		return fmt.Errorf("upsert operator %s: %w", name, err)
	}

	// Re-get for status resourceVersion
	fresh := &dashboardv1alpha1.Operator{}
	if err := c.Get(ctx, client.ObjectKey{Name: name}, fresh); err != nil {
		return err
	}
	fresh.Status.Phase = phase
	fresh.Status.URL = url
	fresh.Status.Message = message
	fresh.Status.ObservedGeneration = fresh.Generation
	condStatus := metav1.ConditionTrue
	reason := string(phase)
	if phase == dashboardv1alpha1.OperatorPhaseDegraded ||
		phase == dashboardv1alpha1.OperatorPhaseUnavailable ||
		phase == dashboardv1alpha1.OperatorPhaseProgressing {
		condStatus = metav1.ConditionFalse
	}
	setCond(&fresh.Status.Conditions, "Ready", condStatus, reason, message)
	if err := c.Status().Update(ctx, fresh); err != nil {
		if apierrors.IsConflict(err) {
			return nil
		}
		return fmt.Errorf("status operator %s: %w", name, err)
	}
	return nil
}

func setCond(conds *[]metav1.Condition, ctype string, status metav1.ConditionStatus, reason, msg string) {
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
