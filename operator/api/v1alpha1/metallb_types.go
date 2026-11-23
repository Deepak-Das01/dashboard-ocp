package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MetalLBPhase is shown on the Addons form and kubectl get.
type MetalLBPhase string

const (
	MetalLBPhaseProgressing MetalLBPhase = "Progressing"
	MetalLBPhaseReady       MetalLBPhase = "Ready"
	MetalLBPhaseDegraded    MetalLBPhase = "Degraded"
)

// MetalLBSpec is filled by the Addons form (user-entered L2 pool values).
type MetalLBSpec struct {
	// Namespace for MetalLB workloads. Defaults to metallb-system.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// PoolName is the IPAddressPool metadata.name. Defaults to first-pool.
	// +optional
	PoolName string `json:"poolName,omitempty"`

	// Addresses is one or more CIDRs or ranges (e.g. 192.168.222.200-192.168.222.210).
	// +kubebuilder:validation:MinItems=1
	Addresses []string `json:"addresses"`

	// L2AdvertisementName is the L2Advertisement metadata.name. Defaults to default.
	// +optional
	L2AdvertisementName string `json:"l2AdvertisementName,omitempty"`
}

// MetalLBStatus is written by kubeops-metallb-controller.
type MetalLBStatus struct {
	// +optional
	Phase MetalLBPhase `json:"phase,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// +optional
	PoolName string `json:"poolName,omitempty"`
	// +optional
	Addresses []string `json:"addresses,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=mlb
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Pool",type=string,JSONPath=`.status.poolName`
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.status.namespace`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// MetalLB installs upstream MetalLB (L2) and an IPAddressPool from user form values.
type MetalLB struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MetalLBSpec   `json:"spec,omitempty"`
	Status MetalLBStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MetalLBList contains a list of MetalLB.
type MetalLBList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MetalLB `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MetalLB{}, &MetalLBList{})
}
