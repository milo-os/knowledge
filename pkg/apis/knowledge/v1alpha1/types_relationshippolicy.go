package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SubjectSelector defines which resources the policy applies to.
type SubjectSelector struct {
	// APIGroup is the API group of the subject resources to watch.
	APIGroup string `json:"apiGroup"`

	// Kind is the kind of the subject resources to watch.
	Kind string `json:"kind"`

	// Namespaces is the list of namespaces to watch. If empty, all namespaces are watched.
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`
}

// DiscoverySpec defines how relationships are discovered from subject resources.
// Exactly ONE field must be specified.
type DiscoverySpec struct {
	// Expression is a CEL expression that receives `subject` (the full Kubernetes object as a map)
	// and returns a list of maps with fields: apiGroup, kind, name, namespace,
	// controlPlaneContextKind, controlPlaneContextName.
	Expression string `json:"expression"`
}

// RelationshipPolicySpec defines the desired state of RelationshipPolicy
type RelationshipPolicySpec struct {
	// RelationshipType references the RelationshipType that discovered edges will be of.
	// +kubebuilder:validation:Required
	RelationshipType corev1.LocalObjectReference `json:"relationshipType"`

	// Subject defines which resources this policy watches and evaluates.
	// +kubebuilder:validation:Required
	Subject SubjectSelector `json:"subject"`

	// Discovery defines how relationships are discovered from subject resources.
	// +kubebuilder:validation:Required
	Discovery DiscoverySpec `json:"discovery"`

	// ControlPlaneContextRef identifies the control plane context where discovered
	// ResourceRelationship objects will be created.
	// +kubebuilder:validation:Required
	ControlPlaneContextRef ControlPlaneContextRef `json:"controlPlaneContextRef"`
}

// RelationshipPolicyStatus defines the observed state of RelationshipPolicy
type RelationshipPolicyStatus struct {
	// ObservedGeneration is the most recent generation observed for this RelationshipPolicy by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// DiscoveredEdgesCount is the total number of ResourceRelationship objects currently managed by this policy.
	// +optional
	DiscoveredEdgesCount int64 `json:"discoveredEdgesCount,omitempty"`

	// Conditions represents the observations of a relationship policy's current state.
	// Known condition types are: "Ready"
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories=knowledge
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="RelationshipType",type=string,JSONPath=".spec.relationshipType.name"
// +kubebuilder:printcolumn:name="Subject Kind",type=string,JSONPath=".spec.subject.kind"
// +kubebuilder:printcolumn:name="Discovered Edges",type=integer,JSONPath=".status.discoveredEdgesCount"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// RelationshipPolicy is the Schema for the relationshippolicies API
type RelationshipPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RelationshipPolicySpec   `json:"spec,omitempty"`
	Status RelationshipPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RelationshipPolicyList contains a list of RelationshipPolicy
type RelationshipPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RelationshipPolicy `json:"items"`
}
