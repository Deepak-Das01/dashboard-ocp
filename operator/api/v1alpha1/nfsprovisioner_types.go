package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NFSProvisionerPhase is shown on the Addons form and kubectl get.
type NFSProvisionerPhase string

const (
	NFSProvisionerPhaseProgressing NFSProvisionerPhase = "Progressing"
	NFSProvisionerPhaseReady       NFSProvisionerPhase = "Ready"
	NFSProvisionerPhaseDegraded    NFSProvisionerPhase = "Degraded"
)

// NFSProvisionerSpec is filled by the Addons form (user-entered values).
type NFSProvisionerSpec struct {
	// NFS server IP or hostname (required).
	Server string `json:"server"`

	// Exported NFS path on the server (required).
	Path string `json:"path"`

	// Namespace for the provisioner workload. Defaults to kubeops-nfs.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// StorageClass name to create (required).
	StorageClassName string `json:"storageClassName"`

	// When true, annotate the StorageClass as the cluster default.
	// +optional
	DefaultClass bool `json:"defaultClass,omitempty"`
}

// NFSProvisionerStatus is written by kubeops-nfs-controller.
type NFSProvisionerStatus struct {
	// +optional
	Phase NFSProvisionerPhase `json:"phase,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// +optional
	StorageClass string `json:"storageClass,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=nfsprov
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Server",type=string,JSONPath=`.spec.server`
// +kubebuilder:printcolumn:name="StorageClass",type=string,JSONPath=`.status.storageClass`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// NFSProvisioner installs nfs-subdir-external-provisioner from user form values.
type NFSProvisioner struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NFSProvisionerSpec   `json:"spec,omitempty"`
	Status NFSProvisionerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NFSProvisionerList contains a list of NFSProvisioner.
type NFSProvisionerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NFSProvisioner `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NFSProvisioner{}, &NFSProvisionerList{})
}
