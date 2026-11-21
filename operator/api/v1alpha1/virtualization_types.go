package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VirtualizationPhase is shown on the Addons form and kubectl get.
type VirtualizationPhase string

const (
	VirtualizationPhaseProgressing VirtualizationPhase = "Progressing"
	VirtualizationPhaseReady       VirtualizationPhase = "Ready"
	VirtualizationPhaseDegraded    VirtualizationPhase = "Degraded"
)

// VirtualizationSpec is filled by the Addons form.
type VirtualizationSpec struct {
	// KubeVirt release tag (e.g. v1.10.0). Defaults to the vendored pin.
	// +optional
	KubeVirtVersion string `json:"kubevirtVersion,omitempty"`

	// CDI release tag (e.g. v1.66.1). Defaults to the vendored pin.
	// +optional
	CDIVersion string `json:"cdiVersion,omitempty"`

	// When true, enable KubeVirt developerConfiguration.useEmulation (labs without /dev/kvm).
	// +optional
	UseEmulation bool `json:"useEmulation,omitempty"`
}

// VirtualizationStatus is written by kubeops-virtualization-controller.
type VirtualizationStatus struct {
	// +optional
	Phase VirtualizationPhase `json:"phase,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
	// +optional
	KubeVirtPhase string `json:"kubevirtPhase,omitempty"`
	// +optional
	CDIPhase string `json:"cdiPhase,omitempty"`
	// +optional
	KubeVirtVersion string `json:"kubevirtVersion,omitempty"`
	// +optional
	CDIVersion string `json:"cdiVersion,omitempty"`
	// UIURL is the kubevirt-manager NodePort URL when the UI is ready.
	// +optional
	UIURL string `json:"uiURL,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=virt;virtualization
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="KubeVirt",type=string,JSONPath=`.status.kubevirtPhase`
// +kubebuilder:printcolumn:name="CDI",type=string,JSONPath=`.status.cdiPhase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Virtualization installs KubeVirt + CDI from pinned release manifests.
type Virtualization struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VirtualizationSpec   `json:"spec,omitempty"`
	Status VirtualizationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VirtualizationList contains a list of Virtualization.
type VirtualizationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Virtualization `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Virtualization{}, &VirtualizationList{})
}
