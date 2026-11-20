package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OperatorPhase is shown by: kubectl get operators
type OperatorPhase string

const (
	OperatorPhaseProgressing OperatorPhase = "Progressing"
	OperatorPhaseReady       OperatorPhase = "Ready"
	OperatorPhaseDegraded    OperatorPhase = "Degraded"
)

// OperatorComponent is the operator role (console | oidc).
// Resource names/namespaces use the kubeops-* prefix
// (kubeops-console, kubeops-authentication).
type OperatorComponent string

const (
	OperatorComponentConsole OperatorComponent = "console"
	OperatorComponentOIDC    OperatorComponent = "oidc"
)

// OperatorSpec is mostly informational; controllers own status.
type OperatorSpec struct {
	// Component / role: "console" or "oidc".
	// +kubebuilder:validation:Enum=console;oidc
	Component OperatorComponent `json:"component"`

	// Version of the operator binary / packaging.
	// +optional
	Version string `json:"version,omitempty"`
}

// OperatorStatus is what `kubectl get operators` prints.
type OperatorStatus struct {
	// Phase is Ready, Degraded, or Progressing.
	// +optional
	Phase OperatorPhase `json:"phase,omitempty"`

	// Message is a short human-readable status.
	// +optional
	Message string `json:"message,omitempty"`

	// URL is an optional primary endpoint (console HTTPS, Dex issuer, …).
	// +optional
	URL string `json:"url,omitempty"`

	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=ops;operator
// +kubebuilder:printcolumn:name="Component",type=string,JSONPath=`.spec.component`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.status.url`
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=`.status.message`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Operator reports install health for each product operator (console / oidc).
// Use: kubectl get operators
type Operator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OperatorSpec   `json:"spec,omitempty"`
	Status OperatorStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OperatorList contains a list of Operator.
type OperatorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Operator `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Operator{}, &OperatorList{})
}
