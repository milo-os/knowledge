// Command knowledge-apiserver runs the aggregated API server for the
// knowledge graph service.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/endpoints/openapi"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	"k8s.io/component-base/cli"
	"k8s.io/component-base/cli/globalflag"
	utilversion "k8s.io/component-base/version"
	openapicommon "k8s.io/kube-openapi/pkg/common"
	"k8s.io/klog/v2"

	knowledgeapiserver "go.miloapis.com/knowledge/internal/apiserver"
	storagepkg "go.miloapis.com/knowledge/internal/apiserver/storage"
	postgresstorage "go.miloapis.com/knowledge/internal/apiserver/storage/postgres"
	v1alpha1 "go.miloapis.com/knowledge/pkg/apis/knowledge/v1alpha1"
)

// options bundles command-line flags for the knowledge-apiserver.
type options struct {
	RecommendedOptions *genericoptions.RecommendedOptions
	PostgresDSN        string
}

func newOptions() *options {
	o := &options{
		RecommendedOptions: genericoptions.NewRecommendedOptions(
			"/registry/knowledge.miloapis.com",
			knowledgeapiserver.Codecs.LegacyCodec(v1alpha1.GroupVersion),
		),
	}
	// We don't use admission webhooks for now.
	o.RecommendedOptions.Admission = nil
	// Storage is backed by Postgres, not etcd; drop the etcd options so the
	// --etcd-servers flag isn't required by Validate().
	o.RecommendedOptions.Etcd = nil
	return o
}

func (o *options) addFlags(fs *flag.FlagSet) {
	fs.StringVar(&o.PostgresDSN, "postgres-dsn", "",
		"Postgres DSN for ResourceRelationship and graph traversal storage (required).")
}

func (o *options) validate() error {
	var errs []error
	if o.PostgresDSN == "" {
		errs = append(errs, fmt.Errorf("--postgres-dsn is required"))
	}
	if o.RecommendedOptions != nil {
		errs = append(errs, o.RecommendedOptions.Validate()...)
	}
	return utilerrors.NewAggregate(errs)
}

func (o *options) config() (*knowledgeapiserver.Config, error) {
	// Default secure serving cert dir if not set.
	if err := o.RecommendedOptions.SecureServing.MaybeDefaultWithSelfSignedCerts(
		"knowledge-apiserver.knowledge-system.svc",
		[]string{
			"knowledge-apiserver.knowledge-system.svc",
			"knowledge-apiserver.knowledge-system.svc.cluster.local",
			"localhost",
		},
		[]net.IP{net.ParseIP("127.0.0.1")},
	); err != nil {
		return nil, fmt.Errorf("default secure serving: %w", err)
	}

	serverConfig := genericapiserver.NewRecommendedConfig(knowledgeapiserver.Codecs)
	if err := o.RecommendedOptions.ApplyTo(serverConfig); err != nil {
		return nil, fmt.Errorf("apply recommended options: %w", err)
	}

	pgGetter, err := postgresstorage.NewRESTOptionsGetter(o.PostgresDSN)
	if err != nil {
		return nil, fmt.Errorf("build postgres rest options getter: %w", err)
	}
	pgGetter.SetCodec(knowledgeapiserver.Codecs.LegacyCodec(v1alpha1.GroupVersion))

	// Override the etcd-derived RESTOptionsGetter with our Postgres one so
	// Config.Complete() doesn't dereference a nil pointer.
	serverConfig.Config.RESTOptionsGetter = pgGetter
	// EffectiveVersion is normally set by GenericServerRunOptions.ApplyTo which
	// isn't wired in RecommendedOptions for aggregated servers; set it explicitly.
	if serverConfig.Config.EffectiveVersion == nil {
		serverConfig.Config.EffectiveVersion = utilversion.DefaultBuildEffectiveVersion()
	}

	// OpenAPIV3Config is required by InstallAPIGroup for SSA type conversion.
	// We don't have generated OpenAPI definitions yet, so ignore our API prefix
	// to produce an empty spec; SSA falls back to unstructured mode.
	// OpenAPIConfig (v2) is intentionally left nil to avoid the /openapi/v2
	// handler that would fatal on missing built-in type definitions.
	nopDefs := func(_ openapicommon.ReferenceCallback) map[string]openapicommon.OpenAPIDefinition { return nil }
	defNamer := openapi.NewDefinitionNamer(knowledgeapiserver.Scheme)
	serverConfig.Config.OpenAPIV3Config = genericapiserver.DefaultOpenAPIV3Config(nopDefs, defNamer)
	serverConfig.Config.OpenAPIV3Config.Info.Title = "knowledge-apiserver"
	serverConfig.Config.OpenAPIV3Config.Info.Version = "v1alpha1"
	serverConfig.Config.OpenAPIV3Config.IgnorePrefixes = []string{"/apis/knowledge.miloapis.com"}

	provider := &storagepkg.StorageProvider{
		Scheme:            knowledgeapiserver.Scheme,
		RESTOptionsGetter: pgGetter,
		DB:                pgGetter.DB(),
	}

	return &knowledgeapiserver.Config{
		GenericConfig: serverConfig,
		StorageProv:   provider,
	}, nil
}

func (o *options) run(stopCh <-chan struct{}) error {
	cfg, err := o.config()
	if err != nil {
		return err
	}
	server, err := cfg.Complete().New()
	if err != nil {
		return err
	}
	return server.Run(stopCh)
}

func newCommand() *cobra.Command {
	o := newOptions()
	cmd := &cobra.Command{
		Use:   "knowledge-apiserver",
		Short: "Aggregated API server for the Milo OS knowledge graph service",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := o.validate(); err != nil {
				return err
			}
			return o.run(genericapiserver.SetupSignalHandler())
		},
	}
	fs := cmd.Flags()
	gofs := flag.NewFlagSet("knowledge-apiserver", flag.ExitOnError)
	klog.InitFlags(gofs)
	globalflag.AddGlobalFlags(fs, cmd.Name())
	o.RecommendedOptions.AddFlags(fs)
	o.addFlags(gofs)
	fs.AddGoFlagSet(gofs)
	return cmd
}

func main() {
	utilruntime.Must(v1alpha1.AddToScheme(knowledgeapiserver.Scheme))
	_ = schema.GroupVersion{} // keep import
	code := cli.Run(newCommand())
	os.Exit(code)
}
