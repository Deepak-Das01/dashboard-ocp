package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MonitoringPhase is shown on the Addons form and kubectl get.
type MonitoringPhase string

const (
	MonitoringPhaseProgressing MonitoringPhase = "Progressing"
	MonitoringPhaseReady       MonitoringPhase = "Ready"
	MonitoringPhaseDegraded    MonitoringPhase = "Degraded"
)

// MonitoringSpec is filled by the Addons form.
type MonitoringSpec struct {
	// Namespace for the monitoring stack. Defaults to kubeops-monitoring.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// StorageClassName for Prometheus/Grafana PVCs. Defaults to kubeops-nfs-storage.
	// Empty string forces ephemeral emptyDir storage.
	// +optional
	StorageClassName string `json:"storageClassName,omitempty"`

	// Retention for Prometheus TSDB (e.g. 15d). Defaults to 15d.
	// +optional
	Retention string `json:"retention,omitempty"`

	// GrafanaAdminUser defaults to admin.
	// +optional
	GrafanaAdminUser string `json:"grafanaAdminUser,omitempty"`

	// GrafanaAdminPassword; when empty a Secret is generated/reused.
	// +optional
	GrafanaAdminPassword string `json:"grafanaAdminPassword,omitempty"`

	// EnableAlertmanager defaults to true.
	// +optional
	EnableAlertmanager *bool `json:"enableAlertmanager,omitempty"`
}

// MonitoringStatus is written by kubeops-monitoring-controller.
type MonitoringStatus struct {
	// +optional
	Phase MonitoringPhase `json:"phase,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// +optional
	GrafanaURL string `json:"grafanaURL,omitempty"`
	// +optional
	PrometheusURL string `json:"prometheusURL,omitempty"`
	// +optional
	AlertmanagerURL string `json:"alertmanagerURL,omitempty"`
	// +optional
	StorageMode string `json:"storageMode,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=mon;monitoring
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Grafana",type=string,JSONPath=`.status.grafanaURL`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Monitoring installs Prometheus + Grafana + Alertmanager + exporters.
type Monitoring struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MonitoringSpec   `json:"spec,omitempty"`
	Status MonitoringStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MonitoringList contains a list of Monitoring.
type MonitoringList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Monitoring `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Monitoring{}, &MonitoringList{})
}
