package storage

import (
	"database/sql"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/registry/generic"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/registry/rest"

	"go.miloapis.com/knowledge/internal/apiserver/graphquery"
	"go.miloapis.com/knowledge/pkg/apis/knowledge/v1alpha1"
)

// StorageProvider wires the four knowledge resource types into a versioned
// resources storage map ready to be installed into an aggregated API server.
type StorageProvider struct {
	// Scheme is the runtime scheme used for object typing.
	Scheme *runtime.Scheme
	// RESTOptionsGetter produces storage backends for the CRUD-style types.
	// In production this is the Postgres-backed RESTOptionsGetter; it
	// implements generic.RESTOptionsGetter so we can swap backends.
	RESTOptionsGetter generic.RESTOptionsGetter
	// DB is the raw *sql.DB used by the GraphQuery REST handler to run BFS.
	DB *sql.DB
}

// GroupName returns the API group served by this provider.
func (p *StorageProvider) GroupName() string { return v1alpha1.GroupVersion.Group }

// NewRESTStorage returns a map of versioned resource storages keyed by
// resource name suitable for installation via
// genericapiserver.GenericAPIServer.InstallAPIGroup.
func (p *StorageProvider) NewRESTStorage() (map[string]rest.Storage, error) {
	storageMap := map[string]rest.Storage{}

	// RelationshipType — cluster-scoped CRUD.
	rtStore, err := p.newCRUDStore(
		schema.GroupResource{Group: v1alpha1.GroupVersion.Group, Resource: "relationshiptypes"},
		"relationshiptype",
		func() runtime.Object { return &v1alpha1.RelationshipType{} },
		func() runtime.Object { return &v1alpha1.RelationshipTypeList{} },
		false,
	)
	if err != nil {
		return nil, fmt.Errorf("RelationshipType storage: %w", err)
	}
	storageMap["relationshiptypes"] = rtStore

	// RelationshipPolicy — namespaced CRUD.
	rpStore, err := p.newCRUDStore(
		schema.GroupResource{Group: v1alpha1.GroupVersion.Group, Resource: "relationshippolicies"},
		"relationshippolicy",
		func() runtime.Object { return &v1alpha1.RelationshipPolicy{} },
		func() runtime.Object { return &v1alpha1.RelationshipPolicyList{} },
		true,
	)
	if err != nil {
		return nil, fmt.Errorf("RelationshipPolicy storage: %w", err)
	}
	storageMap["relationshippolicies"] = rpStore
	storageMap["relationshippolicies/status"] = &statusREST{store: rpStore}

	// ResourceRelationship — namespaced CRUD backed by Postgres.
	rrStore, err := p.newCRUDStore(
		schema.GroupResource{Group: v1alpha1.GroupVersion.Group, Resource: "resourcerelationships"},
		"resourcerelationship",
		func() runtime.Object { return &v1alpha1.ResourceRelationship{} },
		func() runtime.Object { return &v1alpha1.ResourceRelationshipList{} },
		true,
	)
	if err != nil {
		return nil, fmt.Errorf("ResourceRelationship storage: %w", err)
	}
	// Override strategies to stamp filter labels on every write.
	rrStrat := newResourceRelationshipStrategy(p.Scheme)
	rrStore.CreateStrategy = rrStrat
	rrStore.UpdateStrategy = rrStrat
	rrStore.DeleteStrategy = rrStrat
	storageMap["resourcerelationships"] = rrStore
	storageMap["resourcerelationships/status"] = &statusREST{store: rrStore}

	// GraphQuery — create-only, custom REST handler, no storage backend.
	storageMap["graphqueries"] = graphquery.NewREST(p.DB)

	return storageMap, nil
}

// newCRUDStore constructs a generic registry-backed Store for one of the
// standard CRUD resource types.
func (p *StorageProvider) newCRUDStore(
	gr schema.GroupResource,
	singularName string,
	newFunc, newListFunc func() runtime.Object,
	namespaceScoped bool,
) (*genericregistry.Store, error) {
	strategy := newStrategy(p.Scheme, namespaceScoped)

	store := &genericregistry.Store{
		NewFunc:                   newFunc,
		NewListFunc:               newListFunc,
		PredicateFunc:             matchPredicate,
		DefaultQualifiedResource:  gr,
		SingularQualifiedResource: schema.GroupResource{Group: gr.Group, Resource: singularName},

		CreateStrategy: strategy,
		UpdateStrategy: strategy,
		DeleteStrategy: strategy,

		TableConvertor: rest.NewDefaultTableConvertor(gr),
	}
	options := &generic.StoreOptions{
		RESTOptions: p.RESTOptionsGetter,
		AttrFunc:    getAttrs,
	}
	if err := store.CompleteWithOptions(options); err != nil {
		return nil, err
	}
	return store, nil
}
