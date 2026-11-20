package oidc

import (
	"context"
	"time"

	dashboardv1alpha1 "github.com/Deepak-Das01/dashboard-ocp/operator/api/v1alpha1"
	opstatus "github.com/Deepak-Das01/dashboard-ocp/operator/internal/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// IdleStatusReporter keeps `kubectl get operators` populated for oidc when
// no ClusterOIDC CR exists yet (operator is installed but idle).
type IdleStatusReporter struct {
	Client client.Client
}

var _ manager.Runnable = &IdleStatusReporter{}
var _ manager.LeaderElectionRunnable = &IdleStatusReporter{}

func (r *IdleStatusReporter) NeedLeaderElection() bool { return false }

func (r *IdleStatusReporter) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("oidc-idle-status")
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	report := func() {
		list := &dashboardv1alpha1.ClusterOIDCList{}
		if err := r.Client.List(ctx, list); err != nil {
			logger.Error(err, "list ClusterOIDC")
			return
		}
		if len(list.Items) > 0 {
			// Active CRs — ClusterOIDCReconciler owns Operator status.
			return
		}
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentOIDC, operatorVersion,
			dashboardv1alpha1.OperatorPhaseReady, "",
			"Idle — apply ClusterOIDC to enable Dex")
	}

	// Wait briefly for cache, then report immediately.
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(2 * time.Second):
	}
	report()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			report()
		}
	}
}
