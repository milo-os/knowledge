package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RelationshipTypeSpec defines the desired state of RelationshipType
type RelationshipTypeSpec struct {
	// DisplayName is a human-readable name for this relationship type.
	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// Description describes the semantics of this relationship type.
	// +optional
	Description string `json:"description,omitempty"`

	// SubjectGVK identifies the API group/version/kind of the subject resource.
	// +kubebuilder:validation:Required
	SubjectGVK GroupVersionKind `json:"subjectGVK"`

	// ObjectGVK identifies the API group/version/kind of the object resource.
	// +kubebuilder:validation:Required
	ObjectGVK GroupVersionKind `json:"objectGVK"`

	// Cardinality defines the cardinality of this relationship.
	// +kubebuilder:validation:Enum=OneToOne;OneToMany;ManyToMany
	// +kubebuilder:validation:Required
	Cardinality string `json:"cardinality"`

	// Transparent marks the subject resource kind as a bridge node. When true,
	// graph clients should collapse nodes of this subject GVK kind by stitching
	// their neighbours directly together rather than rendering them as full nodes.
	// +optional
	Transparent bool `json:"transparent,omitempty"`
}

// RelationshipTypeStatus defines the observed state of RelationshipType
type RelationshipTypeStatus struct {
	// ObservedGeneration is the most recent generation observed for this RelationshipType by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represents the observations of a relationship type's current state.
	// Known condition types are: "Ready"
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,categories=knowledge
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Cardinality",type=string,JSONPath=".spec.cardinality"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// RelationshipType is the Schema for the relationshiptypes API
type RelationshipType struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RelationshipTypeSpec   `json:"spec,omitempty"`
	Status RelationshipTypeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RelationshipTypeList contains a list of RelationshipType
type RelationshipTypeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RelationshipType `json:"items"`
}
