package v1alpha1

// ControlPlaneContextRef identifies which control plane context owns a resource endpoint.
// Kind must be one of: Platform, Organization, Project, User
type ControlPlaneContextRef struct {
	// Kind is the type of control plane context. Must be one of: Platform, Organization, Project, User.
	// +kubebuilder:validation:Enum=Platform;Organization;Project;User
	Kind string `json:"kind"`

	// Name is the name of the control plane context instance.
	Name string `json:"name"`
}

// GroupVersionKind identifies a Kubernetes API group, version, and kind.
type GroupVersionKind struct {
	Group   string `json:"group"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

// ResourceEndpoint identifies a specific Kubernetes resource within a control plane context.
type ResourceEndpoint struct {
	// APIGroup is the API group of the resource.
	APIGroup string `json:"apiGroup"`

	// Kind is the kind of the resource.
	Kind string `json:"kind"`

	// Name is the name of the resource.
	Name string `json:"name"`

	// Namespace is the namespace of the resource. Optional for cluster-scoped resources.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// ControlPlaneContextRef identifies which control plane context this resource belongs to.
	ControlPlaneContextRef ControlPlaneContextRef `json:"controlPlaneContextRef"`
}
