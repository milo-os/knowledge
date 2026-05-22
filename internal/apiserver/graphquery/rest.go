// Package graphquery implements the create-only REST handler for GraphQuery.
//
// Follows the SubjectAccessReview pattern: clients POST a spec; the handler
// executes a BFS traversal synchronously and returns the populated object
// in the HTTP response. The object is never persisted.
package graphquery

import (
	"context"
	"database/sql"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/klog/v2"

	"go.miloapis.com/knowledge/internal/bfs"
	v1alpha1 "go.miloapis.com/knowledge/pkg/apis/knowledge/v1alpha1"
)

// REST is the create-only REST handler for GraphQuery.
type REST struct {
	db *sql.DB
}

// Compile-time interface assertions.
var _ rest.Creater = &REST{} //nolint:misspell
var _ rest.Scoper = &REST{}
var _ rest.Storage = &REST{}
var _ rest.SingularNameProvider = &REST{}

// NewREST returns a REST handler that runs BFS traversals against db.
func NewREST(db *sql.DB) *REST {
	return &REST{db: db}
}

// New returns a new empty GraphQuery object used by the API machinery to
// decode incoming request bodies.
func (r *REST) New() runtime.Object { return &v1alpha1.GraphQuery{} }

// NewList is required for resource registration but GraphQuery is not listable.
func (r *REST) NewList() runtime.Object { return &v1alpha1.GraphQueryList{} }

// Destroy releases any resources held by this REST handler.
func (r *REST) Destroy() {}

// NamespaceScoped reports that GraphQuery is a namespaced resource.
func (r *REST) NamespaceScoped() bool { return true }

// GetSingularName returns the singular resource name used by kubectl.
func (r *REST) GetSingularName() string { return "graphquery" }

// Kind returns the resource kind for this handler.
func (r *REST) Kind() string { return "GraphQuery" }

// Create executes a synchronous BFS traversal and returns the populated object.
// The object is never persisted.
func (r *REST) Create(
	ctx context.Context,
	obj runtime.Object,
	createValidation rest.ValidateObjectFunc,
	_ *metav1.CreateOptions,
) (runtime.Object, error) {
	logger := klog.FromContext(ctx)
	query, ok := obj.(*v1alpha1.GraphQuery)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("not a GraphQuery: %T", obj))
	}

	if createValidation != nil {
		// Pass a deep copy: createValidation may mutate the object (e.g. admission
		// defaulting), but GraphQuery is never persisted — we want the original
		// spec preserved for BFS execution.
		if err := createValidation(ctx, query.DeepCopyObject()); err != nil {
			return nil, err
		}
	}

	if err := validateRoot(query.Spec.Root); err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}

	if query.Spec.Traverse.Direction == "" {
		query.Spec.Traverse.Direction = "Outbound"
	}

	logger.V(4).Info("Executing GraphQuery",
		"rootKind", query.Spec.Root.Kind,
		"rootName", query.Spec.Root.Name,
		"maxDepth", query.Spec.Traverse.MaxDepth,
		"maxNodes", query.Spec.Traverse.MaxNodes,
		"direction", query.Spec.Traverse.Direction,
	)

	nodes, edges, truncated, err := bfs.Traverse(
		ctx,
		r.db,
		query.Spec.Root,
		query.Spec.Traverse.MaxDepth,
		query.Spec.Traverse.MaxNodes,
		query.Spec.Traverse.RelationshipTypes,
		query.Spec.Traverse.Direction,
	)
	if err != nil {
		logger.Error(err, "BFS traversal failed")
		return nil, apierrors.NewInternalError(fmt.Errorf("graph traversal failed: %w", err))
	}

	query.Status.Nodes = make([]v1alpha1.GraphNode, 0, len(nodes))
	for _, n := range nodes {
		query.Status.Nodes = append(query.Status.Nodes, v1alpha1.GraphNode{
			ID:       n.ID,
			Endpoint: n.Endpoint,
			Depth:    n.Depth,
		})
	}
	query.Status.Edges = make([]v1alpha1.GraphEdge, 0, len(edges))
	for _, e := range edges {
		query.Status.Edges = append(query.Status.Edges, v1alpha1.GraphEdge{
			ID:               e.ID,
			RelationshipType: e.RelationshipType,
			SubjectNodeID:    e.SubjectNodeID,
			ObjectNodeID:     e.ObjectNodeID,
		})
	}
	query.Status.Truncated = truncated

	return query, nil
}

func validateRoot(r v1alpha1.ResourceEndpoint) error {
	if r.Kind == "" {
		return fmt.Errorf("spec.root.kind is required")
	}
	if r.Name == "" {
		return fmt.Errorf("spec.root.name is required")
	}
	if r.ControlPlaneContextRef.Kind == "" {
		return fmt.Errorf("spec.root.controlPlaneContextRef.kind is required")
	}
	if r.ControlPlaneContextRef.Name == "" {
		return fmt.Errorf("spec.root.controlPlaneContextRef.name is required")
	}
	return nil
}
