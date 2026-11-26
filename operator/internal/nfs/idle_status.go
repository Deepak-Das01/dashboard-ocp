package nfs

import (
	"context"
	"time"

	dashboardv1alpha1 "github.com/Deepak-Das01/kubeops-platform/operator/api/v1alpha1"
	opstatus "github.com/Deepak-Das01/kubeops-platform/operator/internal/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// IdleStatusReporter keeps kubectl get operators populated when no NFSProvisioner exists.
type IdleStatusReporter struct {
	Client client.Client
}

var _ manager.Runnable = &IdleStatusReporter{}
var _ manager.LeaderElectionRunnable = &IdleStatusReporter{}

func (r *IdleStatusReporter) NeedLeaderElection() bool { return false }

func (r *IdleStatusReporter) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("nfs-idle-status")
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	report := func() {
		list := &dashboardv1alpha1.NFSProvisionerList{}
		if err := r.Client.List(ctx, list); err != nil {
			logger.Error(err, "list NFSProvisioner")
			return
		}
		if len(list.Items) > 0 {
			return
		}
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentNFS, operatorVersion,
			dashboardv1alpha1.OperatorPhaseUnavailable, "",
			"Unavailable — NFS provisioner not installed; deploy from Addons with your NFS server details")
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
