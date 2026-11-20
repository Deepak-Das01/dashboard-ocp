package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterOIDCSpec configures cluster-wide OIDC (second operator).
type ClusterOIDCSpec struct {
	// Namespace for Dex / related resources. Defaults to kubeops-authentication.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Provider is "dex" (embedded) or "external".
	// +optional
	Provider string `json:"provider,omitempty"`

	// IssuerURL advertised to clients and kube-apiserver.
	// +optional
	IssuerURL string `json:"issuerURL,omitempty"`

	// ClientID for kubectl / console OIDC clients.
	// +optional
	ClientID string `json:"clientID,omitempty"`

	// DexImage overrides the embedded Dex image.
	// +optional
	DexImage string `json:"dexImage,omitempty"`

	BootstrapAdmin BootstrapAdminSpec `json:"bootstrapAdmin,omitempty"`

	// ConfigureAPIServer requests apiserver OIDC flag wiring when supported.
	// +optional
	ConfigureAPIServer bool `json:"configureAPIServer,omitempty"`
}

// ClusterOIDCStatus is the observed state of cluster OIDC.
type ClusterOIDCStatus struct {
	Phase string `json:"phase,omitempty"`

	IssuerURL string `json:"issuerURL,omitempty"`

	BootstrapSecretRef string `json:"bootstrapSecretRef,omitempty"`

	// APIServerOIDC: not-configured | required | configured | requested-manual
	APIServerOIDC string `json:"apiServerOIDC,omitempty"`

	Message string `json:"message,omitempty"`

	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=coidc
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Issuer",type=string,JSONPath=`.status.issuerURL`
// +kubebuilder:printcolumn:name="APIServer",type=string,JSONPath=`.status.apiServerOIDC`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ClusterOIDC is the Schema for cluster OIDC integration (operator 2).
type ClusterOIDC struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterOIDCSpec   `json:"spec,omitempty"`
	Status ClusterOIDCStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterOIDCList contains a list of ClusterOIDC.
type ClusterOIDCList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterOIDC `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterOIDC{}, &ClusterOIDCList{})
}
