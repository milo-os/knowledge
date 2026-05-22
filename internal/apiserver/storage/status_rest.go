package storage

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/registry/rest"
)

// getterUpdater is the minimal interface needed by statusREST.
type getterUpdater interface {
	rest.Getter
	rest.Updater
}

// statusREST implements the /status subresource endpoint. It delegates Get and
// Update to the same underlying store so the full object (including spec and
// status) is persisted atomically — there is no separate status column in our
// Postgres schema, so a simple whole-object update is correct.
type statusREST struct {
	store getterUpdater
}

var _ rest.Getter = (*statusREST)(nil)
var _ rest.Updater = (*statusREST)(nil)

func (r *statusREST) New() runtime.Object                    { return r.store.New() }
func (r *statusREST) Destroy()                               { /* owned by the main store */ }
func (r *statusREST) GetSingularName() string                { return "" }
func (r *statusREST) NamespaceScoped() bool                  { return true }
func (r *statusREST) GroupVersionKind(_ schema.GroupVersion) (schema.GroupVersionKind, bool) {
	return schema.GroupVersionKind{}, false
}

func (r *statusREST) Get(ctx context.Context, name string, opts *metav1.GetOptions) (runtime.Object, error) {
	return r.store.Get(ctx, name, opts)
}

func (r *statusREST) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo, createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc, forceAllowCreate bool, options *metav1.UpdateOptions) (runtime.Object, bool, error) {
	return r.store.Update(ctx, name, objInfo, createValidation, updateValidation, forceAllowCreate, options)
}
