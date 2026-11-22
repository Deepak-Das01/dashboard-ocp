package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RookCephPhase is shown on the Addons form and kubectl get.
type RookCephPhase string

const (
	RookCephPhaseProgressing RookCephPhase = "Progressing"
	RookCephPhaseReady       RookCephPhase = "Ready"
	RookCephPhaseDegraded    RookCephPhase = "Degraded"
	RookCephPhaseFailed      RookCephPhase = "Failed"
)

// RookCephNodeLabel selects placement nodes for mons/OSDs.
type RookCephNodeLabel struct {
	// +optional
	Key string `json:"key,omitempty"`
	// +optional
	Value string `json:"value,omitempty"`
}

// RookCephMonSpec configures Ceph monitors.
type RookCephMonSpec struct {
	// +optional
	// +kubebuilder:default=1
	Count int `json:"count,omitempty"`
	// +optional
	AllowMultiplePerNode bool `json:"allowMultiplePerNode,omitempty"`
}

// RookCephMgrSpec configures Ceph managers.
type RookCephMgrSpec struct {
	// +optional
	// +kubebuilder:default=1
	Count int `json:"count,omitempty"`
	// +optional
	AllowMultiplePerNode bool `json:"allowMultiplePerNode,omitempty"`
}

// RookCephNode lists devices to use on a Kubernetes node.
type RookCephNode struct {
	// Kubernetes node name.
	Name string `json:"name"`
	// Device names (e.g. sdb, nvme0n1) without /dev/ prefix.
	// +optional
	Devices []string `json:"devices,omitempty"`
}

// RookCephSpec is filled by the Addons form.
type RookCephSpec struct {
	// Namespace for Rook operator + CephCluster. Defaults to kubeops-ceph-cluster.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// CephImage defaults to quay.io/ceph/ceph:v19.2.3.
	// +optional
	CephImage string `json:"cephImage,omitempty"`

	// DataDirHostPath defaults to /var/lib/rook.
	// +optional
	DataDirHostPath string `json:"dataDirHostPath,omitempty"`

	// +optional
	Mon RookCephMonSpec `json:"mon,omitempty"`

	// +optional
	Mgr RookCephMgrSpec `json:"mgr,omitempty"`

	// NodeLabel applied to selected nodes for placement. Defaults to ceph=storage.
	// +optional
	NodeLabel RookCephNodeLabel `json:"nodeLabel,omitempty"`

	// Nodes and devices for OSDs (required for Deploy).
	// +optional
	Nodes []RookCephNode `json:"nodes,omitempty"`

	// EnableDashboard defaults to true.
	// +optional
	EnableDashboard *bool `json:"enableDashboard,omitempty"`

	// EnableToolbox defaults to true.
	// +optional
	EnableToolbox *bool `json:"enableToolbox,omitempty"`

	// SetBlockDefaultStorageClass marks rook-ceph-block as the default StorageClass.
	// +optional
	SetBlockDefaultStorageClass bool `json:"setBlockDefaultStorageClass,omitempty"`

	// DashboardNodePort defaults to 30448.
	// +optional
	DashboardNodePort int32 `json:"dashboardNodePort,omitempty"`

	// PoolSize for block/filesystem pools. 0 = auto (3 when ≥3 nodes/devices, else 2/1).
	// Single-node multi-disk uses failureDomain=osd so replicas place across devices.
	// +optional
	PoolSize int `json:"poolSize,omitempty"`
}

// RookCephStatus is written by kubeops-ceph-controller.
type RookCephStatus struct {
	// +optional
	Phase RookCephPhase `json:"phase,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// +optional
	CephHealth string `json:"cephHealth,omitempty"`
	// +optional
	DashboardURL string `json:"dashboardURL,omitempty"`
	// +optional
	StorageClasses []string `json:"storageClasses,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=ceph;rookceph
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Health",type=string,JSONPath=`.status.cephHealth`
// +kubebuilder:printcolumn:name="Dashboard",type=string,JSONPath=`.status.dashboardURL`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// RookCeph installs Rook + CephCluster with RBD and CephFS StorageClasses.
type RookCeph struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RookCephSpec   `json:"spec,omitempty"`
	Status RookCephStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RookCephList contains a list of RookCeph.
type RookCephList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RookCeph `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RookCeph{}, &RookCephList{})
}
