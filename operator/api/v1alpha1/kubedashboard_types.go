package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AuthMode for the console operator only.
//   - secret: kubeadmin password Secret + NGINX basic auth
//   - token:  classic Dashboard SA token login (no gate)
//   - oidc:   Dex via oauth2-proxy (requires ClusterOIDC)
type AuthMode string

const (
	AuthModeSecret AuthMode = "secret"
	AuthModeToken  AuthMode = "token"
	AuthModeOIDC   AuthMode = "oidc"
)

// KubeDashboardSpec — console operator (dashboard + metrics).
type KubeDashboardSpec struct {
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// +optional
	Version string `json:"version,omitempty"`
	// +optional
	Service ServiceSpec `json:"service,omitempty"`
	// +optional
	MetricsServer MetricsServerSpec `json:"metricsServer,omitempty"`
	// +optional
	Images ImagesSpec `json:"images,omitempty"`
	// +optional
	Auth AuthSpec `json:"auth,omitempty"`
}

type ServiceSpec struct {
	// +optional
	HTTPSNodePort int32 `json:"httpsNodePort,omitempty"`
	// +optional
	HTTPNodePort int32 `json:"httpNodePort,omitempty"`
}

type MetricsServerSpec struct {
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

type ImagesSpec struct {
	// +optional
	Proxy string `json:"proxy,omitempty"`
	// +optional
	Dashboard string `json:"dashboard,omitempty"`
	// +optional
	MetricsScraper string `json:"metricsScraper,omitempty"`
}

// AuthSpec — console secret/token/oidc auth (Dex IdP comes from ClusterOIDC).
type AuthSpec struct {
	// Mode is "secret", "token", or "oidc".
	// +optional
	Mode AuthMode `json:"mode,omitempty"`

	// Secret auth bootstrap user (default kubeadmin, 16-char password).
	// +optional
	BootstrapAdmin BootstrapAdminSpec `json:"bootstrapAdmin,omitempty"`
}

type BootstrapAdminSpec struct {
	// +optional
	Username string `json:"username,omitempty"`
	// +optional
	PasswordLength int `json:"passwordLength,omitempty"`
}

// KubeDashboardStatus defines the observed state.
type KubeDashboardStatus struct {
	// +optional
	Phase string `json:"phase,omitempty"`
	// +optional
	URL string `json:"url,omitempty"`
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +optional
	Auth AuthStatus `json:"auth,omitempty"`
}

type AuthStatus struct {
	Mode               AuthMode `json:"mode,omitempty"`
	Ready              bool     `json:"ready,omitempty"`
	BootstrapSecretRef string   `json:"bootstrapSecretRef,omitempty"`
	Message            string   `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=kd
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Auth",type=string,JSONPath=`.status.auth.mode`
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.status.url`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// KubeDashboard is managed by the console operator.
type KubeDashboard struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KubeDashboardSpec   `json:"spec,omitempty"`
	Status KubeDashboardStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KubeDashboardList contains a list of KubeDashboard.
type KubeDashboardList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubeDashboard `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KubeDashboard{}, &KubeDashboardList{})
}
