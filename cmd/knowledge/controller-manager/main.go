package main

import (
	"flag"
	"fmt"
	"os"

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

func main() {
	var (
		metricsAddr          string
		probeAddr            string
		enableLeaderElection bool
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "Address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "Address the health probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for the controller manager.")
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
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
		fail("unable to create manager", err)
	}

	dynClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		fail("unable to create dynamic client", err)
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		fail("unable to create kubernetes client", err)
	}
	cachedDisc := memory.NewMemCacheClient(discovery.NewDiscoveryClient(kubeClient.Discovery().RESTClient()))
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(cachedDisc)

	celEng, err := celengine.NewEngine()
	if err != nil {
		fail("unable to create CEL engine", err)
	}

	if err := (&policyctrl.Reconciler{
		Client:        mgr.GetClient(),
		DynamicClient: dynClient,
		RESTMapper:    mapper,
		CEL:           celEng,
	}).SetupWithManager(mgr); err != nil {
		fail("unable to set up policy controller", err)
	}

	if err := (&relationshipctrl.Reconciler{
		Client:        mgr.GetClient(),
		DynamicClient: dynClient,
		RESTMapper:    mapper,
	}).SetupWithManager(mgr); err != nil {
		fail("unable to set up relationship controller", err)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		fail("unable to register healthz", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		fail("unable to register readyz", err)
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		fail("manager exited with error", err)
	}
}

func leaderElectionNamespace() string {
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		return ns
	}
	return "knowledge-system"
}

func fail(msg string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", msg, err)
	os.Exit(1)
}
