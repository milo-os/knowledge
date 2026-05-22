package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TraversalSpec defines how the graph traversal should be performed.
type TraversalSpec struct {
	// MaxDepth is the maximum number of hops to traverse.
	// +kubebuilder:default=5
	// +kubebuilder:validation:Maximum=20
	// +optional
	MaxDepth int `json:"maxDepth,omitempty"`

	// MaxNodes is the maximum number of nodes to return.
	// +kubebuilder:default=100
	// +kubebuilder:validation:Maximum=1000
	// +optional
	MaxNodes int `json:"maxNodes,omitempty"`

	// RelationshipTypes restricts traversal to only these relationship type names.
	// If empty, all relationship types are traversed.
	// +optional
	RelationshipTypes []string `json:"relationshipTypes,omitempty"`

	// Direction defines the direction of traversal relative to the root node.
	// +kubebuilder:validation:Enum=Outbound;Inbound;Both
	// +kubebuilder:default=Outbound
	// +optional
	Direction string `json:"direction,omitempty"`
}

// GraphNode represents a single node in the traversal result.
type GraphNode struct {
	// ID is the unique identifier of this node within the result set.
	ID string `json:"id"`

	// Endpoint identifies the Kubernetes resource this node represents.
	Endpoint ResourceEndpoint `json:"endpoint"`

	// Depth is the number of hops from the root node to this node.
	Depth int `json:"depth"`
}

// GraphEdge represents a relationship edge in the traversal result.
type GraphEdge struct {
	// ID is the unique identifier of this edge within the result set.
	ID string `json:"id"`

	// RelationshipType is the name of the RelationshipType for this edge.
	RelationshipType string `json:"relationshipType"`

	// SubjectNodeID is the ID of the subject node in this edge.
	SubjectNodeID string `json:"subjectNodeID"`

	// ObjectNodeID is the ID of the object node in this edge.
	ObjectNodeID string `json:"objectNodeID"`
}

// GraphQuerySpec defines the desired state of GraphQuery
type GraphQuerySpec struct {
	// Root is the starting resource endpoint for the traversal.
	// +kubebuilder:validation:Required
	Root ResourceEndpoint `json:"root"`

	// Traverse defines the traversal parameters.
	// +kubebuilder:validation:Required
	Traverse TraversalSpec `json:"traverse"`
}

// GraphQueryStatus defines the observed state of GraphQuery
type GraphQueryStatus struct {
	// Nodes is the list of nodes discovered during traversal.
	// +optional
	Nodes []GraphNode `json:"nodes,omitempty"`

	// Edges is the list of edges discovered during traversal.
	// +optional
	Edges []GraphEdge `json:"edges,omitempty"`

	// Truncated indicates whether the result was truncated due to MaxNodes or MaxDepth limits.
	// +optional
	Truncated bool `json:"truncated,omitempty"`
}

// +genclient:onlyVerbs=create
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories=knowledge

// GraphQuery is the Schema for the graphqueries API.
// It follows the SubjectAccessReview pattern: create-only, synchronous result in HTTP response.
type GraphQuery struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GraphQuerySpec   `json:"spec,omitempty"`
	Status GraphQueryStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GraphQueryList contains a list of GraphQuery
type GraphQueryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GraphQuery `json:"items"`
}
