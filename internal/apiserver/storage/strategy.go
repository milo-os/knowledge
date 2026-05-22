// Package storage wires the four knowledge resource types into the
// aggregated API server.
package storage

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/names"
	"k8s.io/apimachinery/pkg/util/validation/field"

	v1alpha1 "go.miloapis.com/knowledge/pkg/apis/knowledge/v1alpha1"
)

// genericStrategy is a minimal strategy that satisfies the create/update
// strategy interfaces required by genericregistry.Store for our CRD-style
// resources. Validation for these types is enforced via OpenAPI / CRD schemas.
type genericStrategy struct {
	runtime.ObjectTyper
	names.NameGenerator
	namespaceScoped bool
}

func newStrategy(typer runtime.ObjectTyper, namespaceScoped bool) genericStrategy {
	return genericStrategy{
		ObjectTyper:     typer,
		NameGenerator:   names.SimpleNameGenerator,
		namespaceScoped: namespaceScoped,
	}
}

func (s genericStrategy) NamespaceScoped() bool          { return s.namespaceScoped }
func (s genericStrategy) AllowCreateOnUpdate() bool      { return false }
func (s genericStrategy) AllowUnconditionalUpdate() bool { return false }
func (s genericStrategy) Canonicalize(_ runtime.Object)  {}

func (s genericStrategy) PrepareForCreate(_ context.Context, _ runtime.Object) {}
func (s genericStrategy) PrepareForUpdate(_ context.Context, _, _ runtime.Object) {}

func (s genericStrategy) Validate(_ context.Context, _ runtime.Object) field.ErrorList {
	return nil
}
func (s genericStrategy) ValidateUpdate(_ context.Context, _, _ runtime.Object) field.ErrorList {
	return nil
}
func (s genericStrategy) WarningsOnCreate(_ context.Context, _ runtime.Object) []string {
	return nil
}
func (s genericStrategy) WarningsOnUpdate(_ context.Context, _, _ runtime.Object) []string {
	return nil
}

// getAttrs extracts labels and fields used by the storage predicate matcher.
func getAttrs(obj runtime.Object) (labels.Set, fields.Set, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, nil, err
	}
	return labels.Set(accessor.GetLabels()), fields.Set{
		"metadata.name":      accessor.GetName(),
		"metadata.namespace": accessor.GetNamespace(),
	}, nil
}

// matchPredicate builds a selection predicate using the supplied selectors.
func matchPredicate(label labels.Selector, fieldSel fields.Selector) storage.SelectionPredicate {
	return storage.SelectionPredicate{
		Label:    label,
		Field:    fieldSel,
		GetAttrs: getAttrs,
	}
}

// ensure the imports used in interfaces are referenced.
var _ generic.RESTOptionsGetter = (generic.RESTOptionsGetter)(nil)

type resourceRelationshipStrategy struct {
	genericStrategy
}

func newResourceRelationshipStrategy(typer runtime.ObjectTyper) resourceRelationshipStrategy {
	return resourceRelationshipStrategy{genericStrategy: newStrategy(typer, true)}
}

func (s resourceRelationshipStrategy) PrepareForCreate(_ context.Context, obj runtime.Object) {
	stampFilterLabels(obj.(*v1alpha1.ResourceRelationship))
}

func (s resourceRelationshipStrategy) PrepareForUpdate(_ context.Context, obj, _ runtime.Object) {
	stampFilterLabels(obj.(*v1alpha1.ResourceRelationship))
}

// stampFilterLabels stamps three index labels onto a ResourceRelationship so
// the inventory UI can filter via standard k8s label selectors. Existing
// labels (e.g. policy-name, created-by-policy) are preserved.
// Note: rows written before this change will not carry these labels until
// next written (on re-create or policy reconcile). No migration is needed
// for new installs.
func stampFilterLabels(rr *v1alpha1.ResourceRelationship) {
	if rr.Labels == nil {
		rr.Labels = make(map[string]string)
	}
	rr.Labels["knowledge.miloapis.com/relationship-type"] = rr.Spec.RelationshipType.Name
	rr.Labels["knowledge.miloapis.com/subject-kind"] = rr.Spec.Subject.Kind
	rr.Labels["knowledge.miloapis.com/source-type"] = rr.Spec.Source.Type
}
