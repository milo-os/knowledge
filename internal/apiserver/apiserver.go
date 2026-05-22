// Package apiserver wires the knowledge aggregated API server: scheme,
// codecs, options, and group installation.
package apiserver

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"

	storagepkg "go.miloapis.com/knowledge/internal/apiserver/storage"
	v1alpha1 "go.miloapis.com/knowledge/pkg/apis/knowledge/v1alpha1"
)

var (
	// Scheme is the runtime scheme registered for the knowledge API group.
	Scheme = runtime.NewScheme()
	// Codecs is the codec factory for the registered scheme.
	Codecs = serializer.NewCodecFactory(Scheme)
)

func init() {
	utilruntime.Must(v1alpha1.AddToScheme(Scheme))
	metav1.AddToGroupVersion(Scheme, schema.GroupVersion{Version: "v1"})
	Scheme.AddUnversionedTypes(schema.GroupVersion{Version: "v1"},
		&metav1.Status{},
		&metav1.APIVersions{},
		&metav1.APIGroupList{},
		&metav1.APIGroup{},
		&metav1.APIResourceList{},
	)
}

// Config bundles everything the API server needs to build APIGroupInfo and
// install resources.
type Config struct {
	GenericConfig *genericapiserver.RecommendedConfig
	StorageProv   *storagepkg.StorageProvider
}

// Server wraps a generic API server with knowledge-specific group info.
type Server struct {
	GenericAPIServer *genericapiserver.GenericAPIServer
}

// CompletedConfig is the completed, ready-to-construct server config.
type CompletedConfig struct {
	GenericConfig genericapiserver.CompletedConfig
	StorageProv   *storagepkg.StorageProvider
}

// Complete returns a completed config with required defaults filled in.
func (c *Config) Complete() CompletedConfig {
	return CompletedConfig{
		GenericConfig: c.GenericConfig.Complete(),
		StorageProv:   c.StorageProv,
	}
}

// New constructs the API server with the knowledge group installed.
func (c CompletedConfig) New() (*Server, error) {
	genericServer, err := c.GenericConfig.New("knowledge-apiserver", genericapiserver.NewEmptyDelegate())
	if err != nil {
		return nil, fmt.Errorf("create generic server: %w", err)
	}

	apiGroupInfo := genericapiserver.NewDefaultAPIGroupInfo(
		v1alpha1.GroupVersion.Group,
		Scheme,
		metav1.ParameterCodec,
		Codecs,
	)

	storageMap, err := c.StorageProv.NewRESTStorage()
	if err != nil {
		return nil, fmt.Errorf("build REST storage: %w", err)
	}
	apiGroupInfo.VersionedResourcesStorageMap = map[string]map[string]rest.Storage{
		v1alpha1.GroupVersion.Version: storageMap,
	}

	if err := genericServer.InstallAPIGroup(&apiGroupInfo); err != nil {
		return nil, fmt.Errorf("install knowledge API group: %w", err)
	}

	return &Server{GenericAPIServer: genericServer}, nil
}

// Run starts the API server and blocks until stopCh is closed.
func (s *Server) Run(stopCh <-chan struct{}) error {
	return s.GenericAPIServer.PrepareRun().Run(stopCh)
}
