package ceph

import (
	"context"
	"fmt"
	"strings"

	dashboardv1alpha1 "github.com/Deepak-Das01/dashboard-ocp/operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	cephClusterGVK = schema.GroupVersionKind{Group: "ceph.rook.io", Version: "v1", Kind: "CephCluster"}
	blockPoolGVK   = schema.GroupVersionKind{Group: "ceph.rook.io", Version: "v1", Kind: "CephBlockPool"}
	filesystemGVK  = schema.GroupVersionKind{Group: "ceph.rook.io", Version: "v1", Kind: "CephFilesystem"}
)

type clusterOpts struct {
	Namespace                   string
	CephImage                   string
	DataDirHostPath             string
	MonCount                    int
	MonAllowMultiple            bool
	MgrCount                    int
	MgrAllowMultiple            bool
	LabelKey                    string
	LabelValue                  string
	Nodes                       []dashboardv1alpha1.RookCephNode
	EnableDashboard             bool
	EnableToolbox               bool
	SetBlockDefaultStorageClass bool
	DashboardNodePort           int32
	PoolSize                    int
	FailureDomain               string
	TaintStorageNodes           bool
	// wipeGeneration tags the device-wipe Job so each uninstall runs a fresh wipe
	// even if a prior Succeeded wipe Job is still present (TTL not expired).
	wipeGeneration string
}

func deviceCount(nodes []dashboardv1alpha1.RookCephNode) int {
	n := 0
	for _, node := range nodes {
		for _, d := range node.Devices {
			d = strings.TrimPrefix(strings.TrimSpace(d), "/dev/")
			if d != "" && d != "REPLACE_ME" {
				n++
			}
		}
	}
	return n
}

// failureDomainFor picks crush leaf type. Single-node multi-disk labs must use
// "osd" so size>1 replicas can place across devices on the same host.
func failureDomainFor(nodes []dashboardv1alpha1.RookCephNode, poolSize int) string {
	if poolSize <= 1 {
		return "osd"
	}
	if len(nodes) >= poolSize {
		return "host"
	}
	return "osd"
}

func defaultsFromCR(cr *dashboardv1alpha1.RookCeph) clusterOpts {
	ns := strings.TrimSpace(cr.Spec.Namespace)
	if ns == "" {
		ns = defaultNamespace
	}
	img := strings.TrimSpace(cr.Spec.CephImage)
	if img == "" {
		img = defaultCephImage
	}
	hostPath := strings.TrimSpace(cr.Spec.DataDirHostPath)
	if hostPath == "" {
		hostPath = defaultHostPath
	}
	monCount := cr.Spec.Mon.Count
	if monCount <= 0 {
		monCount = 1
	}
	mgrCount := cr.Spec.Mgr.Count
	if mgrCount <= 0 {
		mgrCount = 1
	}
	lk := strings.TrimSpace(cr.Spec.NodeLabel.Key)
	lv := strings.TrimSpace(cr.Spec.NodeLabel.Value)
	if lk == "" {
		lk = "ceph"
	}
	if lv == "" {
		lv = "storage"
	}
	enableDash := true
	if cr.Spec.EnableDashboard != nil {
		enableDash = *cr.Spec.EnableDashboard
	}
	enableToolbox := true
	if cr.Spec.EnableToolbox != nil {
		enableToolbox = *cr.Spec.EnableToolbox
	}
	port := cr.Spec.DashboardNodePort
	if port <= 0 {
		port = defaultDashPort
	}
	poolSize := cr.Spec.PoolSize
	if poolSize <= 0 {
		devices := deviceCount(cr.Spec.Nodes)
		switch {
		case len(cr.Spec.Nodes) >= 3 || devices >= 3:
			poolSize = 3
		case devices >= 2:
			poolSize = 2
		default:
			poolSize = 1
		}
	}
	return clusterOpts{
		Namespace:                   ns,
		CephImage:                   img,
		DataDirHostPath:             hostPath,
		MonCount:                    monCount,
		MonAllowMultiple:            cr.Spec.Mon.AllowMultiplePerNode || monCount == 1,
		MgrCount:                    mgrCount,
		MgrAllowMultiple:            cr.Spec.Mgr.AllowMultiplePerNode || mgrCount == 1,
		LabelKey:                    lk,
		LabelValue:                  lv,
		Nodes:                       cr.Spec.Nodes,
		EnableDashboard:             enableDash,
		EnableToolbox:               enableToolbox,
		SetBlockDefaultStorageClass: cr.Spec.SetBlockDefaultStorageClass,
		DashboardNodePort:           port,
		PoolSize:                    poolSize,
		FailureDomain:               failureDomainFor(cr.Spec.Nodes, poolSize),
		TaintStorageNodes:           cr.Spec.TaintStorageNodes,
	}
}

func labelNodes(ctx context.Context, c client.Client, opt clusterOpts) error {
	for _, n := range opt.Nodes {
		name := strings.TrimSpace(n.Name)
		if name == "" || name == "REPLACE_ME" {
			continue
		}
		node := &corev1.Node{}
		if err := c.Get(ctx, types.NamespacedName{Name: name}, node); err != nil {
			return fmt.Errorf("node %s: %w", name, err)
		}
		if node.Labels != nil && node.Labels[opt.LabelKey] == opt.LabelValue {
			continue
		}
		base := node.DeepCopy()
		if node.Labels == nil {
			node.Labels = map[string]string{}
		}
		node.Labels[opt.LabelKey] = opt.LabelValue
		if err := c.Patch(ctx, node, client.MergeFrom(base)); err != nil {
			return fmt.Errorf("label node %s: %w", name, err)
		}
	}
	return nil
}

func unlabelNodes(ctx context.Context, c client.Client, opt clusterOpts) error {
	for _, n := range opt.Nodes {
		name := strings.TrimSpace(n.Name)
		if name == "" || name == "REPLACE_ME" {
			continue
		}
		node := &corev1.Node{}
		if err := c.Get(ctx, types.NamespacedName{Name: name}, node); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if node.Labels == nil || node.Labels[opt.LabelKey] != opt.LabelValue {
			continue
		}
		base := node.DeepCopy()
		delete(node.Labels, opt.LabelKey)
		if err := c.Patch(ctx, node, client.MergeFrom(base)); err != nil {
			return err
		}
	}
	return nil
}

func storageTaint(opt clusterOpts) corev1.Taint {
	return corev1.Taint{
		Key:    opt.LabelKey,
		Value:  opt.LabelValue,
		Effect: corev1.TaintEffectNoSchedule,
	}
}

func storageToleration(opt clusterOpts) map[string]interface{} {
	return map[string]interface{}{
		"key":      opt.LabelKey,
		"operator": "Equal",
		"value":    opt.LabelValue,
		"effect":   "NoSchedule",
	}
}

func hasStorageTaint(node *corev1.Node, want corev1.Taint) bool {
	for _, t := range node.Spec.Taints {
		if t.Key == want.Key && t.Value == want.Value && t.Effect == want.Effect {
			return true
		}
	}
	return false
}

// taintNodes applies nodeLabel as NoSchedule on CR-selected nodes (dedicated storage).
func taintNodes(ctx context.Context, c client.Client, opt clusterOpts) error {
	if !opt.TaintStorageNodes {
		return nil
	}
	want := storageTaint(opt)
	for _, n := range opt.Nodes {
		name := strings.TrimSpace(n.Name)
		if name == "" || name == "REPLACE_ME" {
			continue
		}
		node := &corev1.Node{}
		if err := c.Get(ctx, types.NamespacedName{Name: name}, node); err != nil {
			return fmt.Errorf("node %s: %w", name, err)
		}
		if hasStorageTaint(node, want) {
			continue
		}
		base := node.DeepCopy()
		node.Spec.Taints = append(node.Spec.Taints, want)
		if err := c.Patch(ctx, node, client.MergeFrom(base)); err != nil {
			return fmt.Errorf("taint node %s: %w", name, err)
		}
	}
	return nil
}

// untaintNodes removes the storage taint we manage (same key/value/effect as nodeLabel).
func untaintNodes(ctx context.Context, c client.Client, opt clusterOpts) error {
	want := storageTaint(opt)
	for _, n := range opt.Nodes {
		name := strings.TrimSpace(n.Name)
		if name == "" || name == "REPLACE_ME" {
			continue
		}
		node := &corev1.Node{}
		if err := c.Get(ctx, types.NamespacedName{Name: name}, node); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if !hasStorageTaint(node, want) {
			continue
		}
		base := node.DeepCopy()
		filtered := node.Spec.Taints[:0]
		for _, t := range node.Spec.Taints {
			if t.Key == want.Key && t.Value == want.Value && t.Effect == want.Effect {
				continue
			}
			filtered = append(filtered, t)
		}
		node.Spec.Taints = filtered
		if err := c.Patch(ctx, node, client.MergeFrom(base)); err != nil {
			return err
		}
	}
	return nil
}

// ensureStorageTaintTolerations makes rook-ceph-operator + CSI schedule on tainted storage nodes.
func ensureStorageTaintTolerations(ctx context.Context, c client.Client, opt clusterOpts) error {
	if !opt.TaintStorageNodes {
		return nil
	}
	tolYAML := fmt.Sprintf("- key: %s\n  operator: Equal\n  value: %s\n  effect: NoSchedule\n",
		opt.LabelKey, opt.LabelValue)

	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: opt.Namespace, Name: "rook-ceph-operator-config"}, cm); err == nil {
		base := cm.DeepCopy()
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		cm.Data["CSI_PLUGIN_TOLERATIONS"] = tolYAML
		cm.Data["CSI_PROVISIONER_TOLERATIONS"] = tolYAML
		if err := c.Patch(ctx, cm, client.MergeFrom(base)); err != nil {
			return fmt.Errorf("CSI tolerations: %w", err)
		}
	}

	dep := &appsv1.Deployment{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: opt.Namespace, Name: "rook-ceph-operator"}, dep); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if deploymentHasToleration(&dep.Spec.Template.Spec, opt) {
		return nil
	}
	base := dep.DeepCopy()
	dep.Spec.Template.Spec.Tolerations = append(dep.Spec.Template.Spec.Tolerations, corev1.Toleration{
		Key:      opt.LabelKey,
		Operator: corev1.TolerationOpEqual,
		Value:    opt.LabelValue,
		Effect:   corev1.TaintEffectNoSchedule,
	})
	return c.Patch(ctx, dep, client.MergeFrom(base))
}

func deploymentHasToleration(spec *corev1.PodSpec, opt clusterOpts) bool {
	for _, t := range spec.Tolerations {
		if t.Key == opt.LabelKey && t.Value == opt.LabelValue && t.Effect == corev1.TaintEffectNoSchedule {
			return true
		}
	}
	return false
}

func ensureToolboxToleration(ctx context.Context, c client.Client, opt clusterOpts) error {
	if !opt.TaintStorageNodes {
		return nil
	}
	dep := &appsv1.Deployment{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: opt.Namespace, Name: "rook-ceph-tools"}, dep); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if deploymentHasToleration(&dep.Spec.Template.Spec, opt) {
		return nil
	}
	base := dep.DeepCopy()
	dep.Spec.Template.Spec.Tolerations = append(dep.Spec.Template.Spec.Tolerations, corev1.Toleration{
		Key:      opt.LabelKey,
		Operator: corev1.TolerationOpEqual,
		Value:    opt.LabelValue,
		Effect:   corev1.TaintEffectNoSchedule,
	})
	return c.Patch(ctx, dep, client.MergeFrom(base))
}

func placementAll(opt clusterOpts) map[string]interface{} {
	all := map[string]interface{}{
		"nodeAffinity": map[string]interface{}{
			"requiredDuringSchedulingIgnoredDuringExecution": map[string]interface{}{
				"nodeSelectorTerms": []interface{}{
					map[string]interface{}{
						"matchExpressions": []interface{}{
							map[string]interface{}{
								"key":      opt.LabelKey,
								"operator": "In",
								"values":   []interface{}{opt.LabelValue},
							},
						},
					},
				},
			},
		},
	}
	if opt.TaintStorageNodes {
		all["tolerations"] = []interface{}{storageToleration(opt)}
	}
	return all
}

func ensureCephCluster(ctx context.Context, c client.Client, opt clusterOpts) error {
	nodes := []interface{}{}
	for _, n := range opt.Nodes {
		devs := []interface{}{}
		for _, d := range n.Devices {
			d = strings.TrimPrefix(strings.TrimSpace(d), "/dev/")
			if d == "" || d == "REPLACE_ME" {
				continue
			}
			devs = append(devs, map[string]interface{}{"name": d})
		}
		if len(devs) == 0 {
			continue
		}
		nodes = append(nodes, map[string]interface{}{
			"name":    n.Name,
			"devices": devs,
		})
	}
	if len(nodes) == 0 {
		return fmt.Errorf("no valid nodes/devices (replace REPLACE_ME placeholders)")
	}

	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "ceph.rook.io/v1",
		"kind":       "CephCluster",
		"metadata": map[string]interface{}{
			"name":      cephClusterName,
			"namespace": opt.Namespace,
			"labels": map[string]interface{}{
				managedLabel: managedValue,
			},
		},
		"spec": map[string]interface{}{
			"cephVersion": map[string]interface{}{
				"image":            opt.CephImage,
				"allowUnsupported": false,
			},
			"dataDirHostPath":     opt.DataDirHostPath,
			"skipUpgradeChecks":   false,
			"continueUpgradeAfterChecksEvenIfNotHealthy": false,
			"mon": map[string]interface{}{
				"count":                int64(opt.MonCount),
				"allowMultiplePerNode": opt.MonAllowMultiple,
			},
			"mgr": map[string]interface{}{
				"count":                int64(opt.MgrCount),
				"allowMultiplePerNode": opt.MgrAllowMultiple,
			},
			"dashboard": map[string]interface{}{
				"enabled":   opt.EnableDashboard,
				"ssl":       false,
				"urlPrefix": "/kubeops/ceph-ui",
			},
			"network": map[string]interface{}{
				"connections": map[string]interface{}{
					"encryption":  map[string]interface{}{"enabled": false},
					"compression": map[string]interface{}{"enabled": false},
				},
			},
			"crashCollector": map[string]interface{}{"disable": false},
			"storage": map[string]interface{}{
				"useAllNodes":   false,
				"useAllDevices": false,
				"nodes":         nodes,
			},
			"placement": map[string]interface{}{
				"all": placementAll(opt),
			},
			"healthCheck": map[string]interface{}{
				"daemonHealth": map[string]interface{}{
					"mon": map[string]interface{}{"disabled": false, "interval": "45s"},
					"osd": map[string]interface{}{"disabled": false, "interval": "60s"},
					"status": map[string]interface{}{"disabled": false, "interval": "60s"},
				},
			},
			// Lab defaults: lower mon disk warn; default pool size/min_size.
			// Do NOT set osd_crush_chooseleaf_type here — Rook fails reconcile
			// (not runtime-settable). OSD-level crush is applied by health-fix Job.
			"cephConfig": map[string]interface{}{
				"global": map[string]interface{}{
					"mon_data_avail_warn":       "10",
					"osd_pool_default_size":     fmt.Sprintf("%d", opt.PoolSize),
					"osd_pool_default_min_size": fmt.Sprintf("%d", minSize(opt.PoolSize)),
				},
			},
		},
	}}
	obj.SetGroupVersionKind(cephClusterGVK)
	return createOrUpdateUnstructured(ctx, c, obj)
}

func minSize(size int) int {
	if size <= 1 {
		return 1
	}
	if size == 2 {
		return 1
	}
	return size - 1
}

func ensureBlockPoolAndSC(ctx context.Context, c client.Client, opt clusterOpts) error {
	pool := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "ceph.rook.io/v1",
		"kind":       "CephBlockPool",
		"metadata": map[string]interface{}{
			"name":      blockPoolName,
			"namespace": opt.Namespace,
			"labels":    map[string]interface{}{managedLabel: managedValue},
		},
		"spec": map[string]interface{}{
			"failureDomain": opt.FailureDomain,
			"replicated": map[string]interface{}{
				"size":                   int64(opt.PoolSize),
				"requireSafeReplicaSize": opt.PoolSize >= 3,
			},
		},
	}}
	pool.SetGroupVersionKind(blockPoolGVK)
	if err := createOrUpdateUnstructured(ctx, c, pool); err != nil {
		return err
	}

	allowExpansion := true
	reclaim := corev1.PersistentVolumeReclaimDelete
	binding := storagev1.VolumeBindingImmediate
	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:   blockSCName,
			Labels: map[string]string{managedLabel: managedValue},
		},
		Provisioner: opt.Namespace + ".rbd.csi.ceph.com",
		Parameters: map[string]string{
			"clusterID":     opt.Namespace,
			"pool":          blockPoolName,
			"imageFormat":   "2",
			"imageFeatures": "layering",
			"csi.storage.k8s.io/provisioner-secret-name":            "rook-csi-rbd-provisioner",
			"csi.storage.k8s.io/provisioner-secret-namespace":       opt.Namespace,
			"csi.storage.k8s.io/controller-expand-secret-name":      "rook-csi-rbd-provisioner",
			"csi.storage.k8s.io/controller-expand-secret-namespace": opt.Namespace,
			"csi.storage.k8s.io/node-stage-secret-name":             "rook-csi-rbd-node",
			"csi.storage.k8s.io/node-stage-secret-namespace":        opt.Namespace,
			"csi.storage.k8s.io/fstype":                             "ext4",
		},
		ReclaimPolicy:        &reclaim,
		AllowVolumeExpansion: &allowExpansion,
		VolumeBindingMode:    &binding,
	}
	if opt.SetBlockDefaultStorageClass {
		if sc.Annotations == nil {
			sc.Annotations = map[string]string{}
		}
		sc.Annotations["storageclass.kubernetes.io/is-default-class"] = "true"
	}
	return createOrUpdateStorageClass(ctx, c, sc)
}

func ensureFilesystemAndSC(ctx context.Context, c client.Client, opt clusterOpts) error {
	fs := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "ceph.rook.io/v1",
		"kind":       "CephFilesystem",
		"metadata": map[string]interface{}{
			"name":      fsName,
			"namespace": opt.Namespace,
			"labels":    map[string]interface{}{managedLabel: managedValue},
		},
		"spec": map[string]interface{}{
			"metadataPool": map[string]interface{}{
				"failureDomain": opt.FailureDomain,
				"replicated": map[string]interface{}{
					"size":                   int64(opt.PoolSize),
					"requireSafeReplicaSize": opt.PoolSize >= 3,
				},
			},
			"dataPools": []interface{}{
				map[string]interface{}{
					"name":          "data0",
					"failureDomain": opt.FailureDomain,
					"replicated": map[string]interface{}{
						"size":                   int64(opt.PoolSize),
						"requireSafeReplicaSize": opt.PoolSize >= 3,
					},
				},
			},
			"preserveFilesystemOnDelete": false,
			"metadataServer": map[string]interface{}{
				"activeCount":       int64(1),
				"activeStandby":     true,
				"priorityClassName": "system-cluster-critical",
			},
		},
	}}
	fs.SetGroupVersionKind(filesystemGVK)
	if err := createOrUpdateUnstructured(ctx, c, fs); err != nil {
		return err
	}

	allowExpansion := true
	reclaim := corev1.PersistentVolumeReclaimDelete
	binding := storagev1.VolumeBindingImmediate
	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:   fsSCName,
			Labels: map[string]string{managedLabel: managedValue},
		},
		Provisioner: opt.Namespace + ".cephfs.csi.ceph.com",
		Parameters: map[string]string{
			"clusterID": opt.Namespace,
			"fsName":    fsName,
			"pool":      fsName + "-data0",
			"csi.storage.k8s.io/provisioner-secret-name":            "rook-csi-cephfs-provisioner",
			"csi.storage.k8s.io/provisioner-secret-namespace":       opt.Namespace,
			"csi.storage.k8s.io/controller-expand-secret-name":      "rook-csi-cephfs-provisioner",
			"csi.storage.k8s.io/controller-expand-secret-namespace": opt.Namespace,
			"csi.storage.k8s.io/node-stage-secret-name":             "rook-csi-cephfs-node",
			"csi.storage.k8s.io/node-stage-secret-namespace":        opt.Namespace,
		},
		ReclaimPolicy:        &reclaim,
		AllowVolumeExpansion: &allowExpansion,
		VolumeBindingMode:    &binding,
	}
	return createOrUpdateStorageClass(ctx, c, sc)
}

func ensureDashboardNodePort(ctx context.Context, c client.Client, opt clusterOpts) error {
	if !opt.EnableDashboard {
		return nil
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rook-ceph-mgr-dashboard-nodeport",
			Namespace: opt.Namespace,
			Labels: map[string]string{
				managedLabel:             managedValue,
				"app":                    "rook-ceph-mgr",
				"rook_cluster":           opt.Namespace,
				"ceph_dashboard_service": "true",
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
			Ports: []corev1.ServicePort{{
				Name:     "http-dashboard",
				Port:     7000,
				Protocol: corev1.ProtocolTCP,
				NodePort: opt.DashboardNodePort,
			}},
			Selector: map[string]string{
				"app":          "rook-ceph-mgr",
				"rook_cluster": opt.Namespace,
				"mgr_role":     "active",
			},
		},
	}
	existing := &corev1.Service{}
	err := c.Get(ctx, client.ObjectKey{Namespace: opt.Namespace, Name: svc.Name}, existing)
	if apierrors.IsNotFound(err) {
		return c.Create(ctx, svc)
	}
	if err != nil {
		return err
	}
	existing.Spec.Type = svc.Spec.Type
	existing.Spec.Ports = svc.Spec.Ports
	existing.Spec.Selector = svc.Spec.Selector
	return c.Update(ctx, existing)
}

func createOrUpdateUnstructured(ctx context.Context, c client.Client, obj *unstructured.Unstructured) error {
	stampManaged(obj)
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())
	key := client.ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetName()}
	err := c.Get(ctx, key, existing)
	if apierrors.IsNotFound(err) {
		return c.Create(ctx, obj)
	}
	if err != nil {
		return err
	}
	// Patch spec + labels only — a full Update can wipe Rook status.
	base := existing.DeepCopy()
	if spec, found, _ := unstructured.NestedMap(obj.Object, "spec"); found {
		_ = unstructured.SetNestedMap(existing.Object, spec, "spec")
	}
	labels := existing.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	for k, v := range obj.GetLabels() {
		labels[k] = v
	}
	existing.SetLabels(labels)
	return c.Patch(ctx, existing, client.MergeFrom(base))
}

func createOrUpdateStorageClass(ctx context.Context, c client.Client, sc *storagev1.StorageClass) error {
	existing := &storagev1.StorageClass{}
	err := c.Get(ctx, client.ObjectKey{Name: sc.Name}, existing)
	if apierrors.IsNotFound(err) {
		return c.Create(ctx, sc)
	}
	if err != nil {
		return err
	}
	sc.ObjectMeta.ResourceVersion = existing.ResourceVersion
	// StorageClass immutable fields: recreate if provisioner changed.
	if existing.Provisioner != sc.Provisioner {
		if err := c.Delete(ctx, existing); err != nil {
			return err
		}
		sc.ResourceVersion = ""
		return c.Create(ctx, sc)
	}
	existing.Parameters = sc.Parameters
	existing.ReclaimPolicy = sc.ReclaimPolicy
	existing.AllowVolumeExpansion = sc.AllowVolumeExpansion
	existing.VolumeBindingMode = sc.VolumeBindingMode
	existing.Labels = sc.Labels
	if sc.Annotations != nil {
		if existing.Annotations == nil {
			existing.Annotations = map[string]string{}
		}
		for k, v := range sc.Annotations {
			existing.Annotations[k] = v
		}
	}
	return c.Update(ctx, existing)
}

func firstNodeIP(ctx context.Context, c client.Client) string {
	var nodes corev1.NodeList
	if err := c.List(ctx, &nodes); err != nil || len(nodes.Items) == 0 {
		return ""
	}
	for _, a := range nodes.Items[0].Status.Addresses {
		if a.Type == corev1.NodeInternalIP && a.Address != "" {
			return a.Address
		}
	}
	return ""
}

func dashboardURL(ctx context.Context, c client.Client, port int32) string {
	ip := firstNodeIP(ctx, c)
	if ip == "" {
		ip = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d/", ip, port)
}

func operatorDeployReady(ctx context.Context, c client.Client, ns string) bool {
	var list unstructured.UnstructuredList
	list.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "DeploymentList"})
	// rook-ceph-operator Deployment
	dep := &unstructured.Unstructured{}
	dep.SetAPIVersion("apps/v1")
	dep.SetKind("Deployment")
	if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: "rook-ceph-operator"}, dep); err != nil {
		return false
	}
	ready, _, _ := unstructured.NestedInt64(dep.Object, "status", "readyReplicas")
	return ready > 0
}
