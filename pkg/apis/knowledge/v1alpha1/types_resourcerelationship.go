package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RelationshipSource describes how a ResourceRelationship was created.
type RelationshipSource struct {
	// Type indicates how this relationship was created.
	// +kubebuilder:validation:Enum=Manual;Policy
	Type string `json:"type"`

	// PolicyRef references the RelationshipPolicy that created this relationship, if Type is Policy.
	// +optional
	PolicyRef *corev1.ObjectReference `json:"policyRef,omitempty"`
}

// ResourceRelationshipSpec defines the desired state of ResourceRelationship
type ResourceRelationshipSpec struct {
	// RelationshipType references the RelationshipType that categorizes this relationship.
	// +kubebuilder:validation:Required
	RelationshipType corev1.LocalObjectReference `json:"relationshipType"`

	// Subject is the subject endpoint of the relationship.
	// +kubebuilder:validation:Required
	Subject ResourceEndpoint `json:"subject"`

	// Object is the object endpoint of the relationship.
	// +kubebuilder:validation:Required
	Object ResourceEndpoint `json:"object"`

	// Source describes how this relationship was created.
	// +kubebuilder:validation:Required
	Source RelationshipSource `json:"source"`
}

// ResourceRelationshipStatus defines the observed state of ResourceRelationship
type ResourceRelationshipStatus struct {
	// ObservedGeneration is the most recent generation observed for this ResourceRelationship by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represents the observations of a resource relationship's current state.
	// Known condition types are: "Valid", "Reconciling"
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories=knowledge
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="RelationshipType",type=string,JSONPath=".spec.relationshipType.name"
// +kubebuilder:printcolumn:name="Subject",type=string,JSONPath=".spec.subject.name"
// +kubebuilder:printcolumn:name="Object",type=string,JSONPath=".spec.object.name"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ResourceRelationship is the Schema for the resourcerelationships API
type ResourceRelationship struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourceRelationshipSpec   `json:"spec,omitempty"`
	Status ResourceRelationshipStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ResourceRelationshipList contains a list of ResourceRelationship
type ResourceRelationshipList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourceRelationship `json:"items"`
}
