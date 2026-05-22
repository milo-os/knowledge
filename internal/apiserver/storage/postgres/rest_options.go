package postgres

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // Postgres driver (pgx)
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/storage"
	cacherstorage "k8s.io/apiserver/pkg/storage/cacher"
	"k8s.io/apiserver/pkg/storage/storagebackend"
	"k8s.io/apiserver/pkg/storage/storagebackend/factory"
	"k8s.io/client-go/tools/cache"

	v1alpha1 "go.miloapis.com/knowledge/pkg/apis/knowledge/v1alpha1"
)

// RESTOptionsGetter implements generic.RESTOptionsGetter for Postgres-backed storage.
type RESTOptionsGetter struct {
	db    *sql.DB
	dsn   string
	codec runtime.Codec
}

// NewRESTOptionsGetter creates a RESTOptionsGetter that produces Postgres-backed stores.
func NewRESTOptionsGetter(dsn string) (*RESTOptionsGetter, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres connection: %w", err)
	}
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(25)
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}
	if _, err := db.Exec(schemaDDL); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &RESTOptionsGetter{db: db, dsn: dsn}, nil
}

// GetRESTOptions returns the REST options for a given resource.
func (r *RESTOptionsGetter) GetRESTOptions(resource schema.GroupResource, example runtime.Object) (generic.RESTOptions, error) {
	ret := generic.RESTOptions{
		ResourcePrefix: resource.Group + "/" + resource.Resource,
		StorageConfig: &storagebackend.ConfigForResource{
			Config: storagebackend.Config{
				Type:  "postgres",
				Codec: r.codec,
			},
			GroupResource: resource,
		},
		Decorator: func(
			config *storagebackend.ConfigForResource,
			resourcePrefix string,
			keyFunc func(obj runtime.Object) (string, error),
			newFunc func() runtime.Object,
			newListFunc func() runtime.Object,
			getAttrsFunc storage.AttrFunc,
			trigger storage.IndexerFuncs,
			indexers *cache.Indexers,
		) (storage.Interface, factory.DestroyFunc, error) {
			// Use the BFS-optimised ResourceRelationship store only for that type;
			// all other types get the generic JSONB store.
			type stoppableStore interface {
				storage.Interface
				Stop()
			}
			var rawStore stoppableStore
			if _, ok := newFunc().(*v1alpha1.ResourceRelationship); ok {
				s := NewStore(r.db, r.codec, r.dsn)
				s.SetNewFunc(newFunc)
				rawStore = s
			} else {
				s := NewGenericStore(r.db, r.codec, r.dsn)
				s.SetNewFunc(newFunc)
				rawStore = s
			}

			cacherConfig := cacherstorage.Config{
				Storage:        rawStore,
				Versioner:      storage.APIObjectVersioner{},
				GroupResource:  config.GroupResource,
				ResourcePrefix: resourcePrefix,
				KeyFunc:        keyFunc,
				NewFunc:        newFunc,
				NewListFunc:    newListFunc,
				GetAttrsFunc:   getAttrsFunc,
				IndexerFuncs:   trigger,
				Indexers:       indexers,
				Codec:          r.codec,
			}
			cacher, err := cacherstorage.NewCacherFromConfig(cacherConfig)
			if err != nil {
				return nil, func() {}, fmt.Errorf("failed to create cacher for %s: %w", config.GroupResource, err)
			}
			var once sync.Once
			destroy := func() {
				once.Do(func() {
					cacher.Stop()
					rawStore.Stop()
				})
			}
			return cacher, destroy, nil
		},
	}
	return ret, nil
}

// SetCodec sets the codec used for encoding/decoding objects.
func (r *RESTOptionsGetter) SetCodec(codec runtime.Codec) {
	r.codec = codec
}

// DB exposes the underlying *sql.DB.
func (r *RESTOptionsGetter) DB() *sql.DB {
	return r.db
}

// DSN returns the DSN string.
func (r *RESTOptionsGetter) DSN() string {
	return r.dsn
}
