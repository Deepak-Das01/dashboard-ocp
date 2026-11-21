package virtualization

import (
	"context"
	"time"

	dashboardv1alpha1 "github.com/Deepak-Das01/dashboard-ocp/operator/api/v1alpha1"
	opstatus "github.com/Deepak-Das01/dashboard-ocp/operator/internal/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// IdleStatusReporter keeps kubectl get operators populated when no Virtualization CR exists.
type IdleStatusReporter struct {
	Client client.Client
}

var _ manager.Runnable = &IdleStatusReporter{}
var _ manager.LeaderElectionRunnable = &IdleStatusReporter{}

func (r *IdleStatusReporter) NeedLeaderElection() bool { return false }

func (r *IdleStatusReporter) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("virtualization-idle-status")
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	report := func() {
		list := &dashboardv1alpha1.VirtualizationList{}
		if err := r.Client.List(ctx, list); err != nil {
			logger.Error(err, "list Virtualization")
			return
		}
		if len(list.Items) > 0 {
			return
		}
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentVirtualization, operatorVersion,
			dashboardv1alpha1.OperatorPhaseUnavailable, "",
			"Unavailable — virtualization not installed; deploy from Addons")
	}

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
