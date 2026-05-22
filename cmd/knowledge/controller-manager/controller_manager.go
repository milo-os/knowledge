// Package controllermanager provides the cobra subcommand for the knowledge
// controller manager.
package controllermanager

import (
	"flag"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	celengine "go.miloapis.com/knowledge/internal/cel"
	policyctrl "go.miloapis.com/knowledge/internal/controllers/policy"
	relationshipctrl "go.miloapis.com/knowledge/internal/controllers/relationship"
	knowledgev1alpha1 "go.miloapis.com/knowledge/pkg/apis/knowledge/v1alpha1"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

// NewCommand returns the cobra command for the knowledge controller-manager subcommand.
func NewCommand() *cobra.Command {
	var (
		metricsAddr          string
		probeAddr            string
		enableLeaderElection bool
	)
	opts := zap.Options{Development: true}

	cmd := &cobra.Command{
		Use:   "controller-manager",
		Short: "Controller manager for the Milo OS knowledge graph service",
		RunE: func(_ *cobra.Command, _ []string) error {
			return run(metricsAddr, probeAddr, enableLeaderElection, &opts)
		},
	}

	cmd.Flags().StringVar(&metricsAddr, "metrics-bind-address", ":8080", "Address the metrics endpoint binds to.")
	cmd.Flags().StringVar(&probeAddr, "health-probe-bind-address", ":8081", "Address the health probe endpoint binds to.")
	cmd.Flags().BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for the controller manager.")
	// Bind zap flags via the stdlib flag package, then add them to cobra's pflag set.
	goFlagSet := flag.NewFlagSet("controller-manager", flag.ContinueOnError)
	opts.BindFlags(goFlagSet)
	cmd.Flags().AddGoFlagSet(goFlagSet)

	return cmd
}

func run(metricsAddr, probeAddr string, enableLeaderElection bool, opts *zap.Options) error {
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(opts)))
	klog.SetLogger(ctrl.Log)

	cfg := ctrl.GetConfigOrDie()

	s := clientgoscheme.Scheme
	utilruntime.Must(knowledgev1alpha1.AddToScheme(s))

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                  s,
		Metrics:                 metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress:  probeAddr,
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "knowledge-controller-manager",
		LeaderElectionNamespace: leaderElectionNamespace(),
	})
	if err != nil {
		return err
	}

	dynClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return err
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}
	cachedDisc := memory.NewMemCacheClient(discovery.NewDiscoveryClient(kubeClient.Discovery().RESTClient()))
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(cachedDisc)

	celEng, err := celengine.NewEngine()
	if err != nil {
		return err
	}

	if err := (&policyctrl.Reconciler{
		Client:        mgr.GetClient(),
		DynamicClient: dynClient,
		RESTMapper:    mapper,
		CEL:           celEng,
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	if err := (&relationshipctrl.Reconciler{
		Client:        mgr.GetClient(),
		DynamicClient: dynClient,
		RESTMapper:    mapper,
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return err
	}

	return mgr.Start(ctrl.SetupSignalHandler())
}

func leaderElectionNamespace() string {
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		return ns
	}
	return "knowledge-system"
}
