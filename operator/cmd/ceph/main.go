package main

import (
	"flag"
	"net/http"
	"os"

	dashboardv1alpha1 "github.com/Deepak-Das01/dashboard-ocp/operator/api/v1alpha1"
	cephctrl "github.com/Deepak-Das01/dashboard-ocp/operator/internal/ceph"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(dashboardv1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr, probeAddr, diskProbeAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":18086", "metrics")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":18087", "health")
	flag.StringVar(&diskProbeAddr, "disk-probe-bind-address", ":18088", "disk probe HTTP API")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "leader election")
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	cfg := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "kubeops-ceph-controller.dashboard.ocp",
	})
	if err != nil {
		ctrl.Log.Error(err, "manager")
		os.Exit(1)
	}
	reconciler := &cephctrl.RookCephReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		ctrl.Log.Error(err, "controller")
		os.Exit(1)
	}
	if err := mgr.Add(&cephctrl.IdleStatusReporter{Client: mgr.GetClient()}); err != nil {
		ctrl.Log.Error(err, "idle status reporter")
		os.Exit(1)
	}
	_ = mgr.AddHealthzCheck("healthz", healthz.Ping)
	_ = mgr.AddReadyzCheck("readyz", healthz.Ping)

	// Disk probe HTTP API for the Addons form (Service :18088).
	go func() {
		mux := http.NewServeMux()
		cephctrl.RegisterDiskProbeHandlers(mux, mgr.GetClient(), cfg)
		ctrl.Log.Info("starting disk probe API", "addr", diskProbeAddr)
		if err := http.ListenAndServe(diskProbeAddr, mux); err != nil {
			ctrl.Log.Error(err, "disk probe server")
		}
	}()

	ctrl.Log.Info("starting kubeops-ceph-controller")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		ctrl.Log.Error(err, "exit")
		os.Exit(1)
	}
}
