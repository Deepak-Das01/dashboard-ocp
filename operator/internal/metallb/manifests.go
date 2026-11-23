package metallb

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io"
	"reflect"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:embed manifests/*.yaml manifests/*.txt
var manifestFS embed.FS

const (
	upstreamNamespace = "metallb-system"
	managedLabel      = "dashboard.ocp/addon"
	managedValue      = "kubeops-metallb"
	nativeManifest    = "manifests/metallb-native.yaml"
	defaultPoolName   = "first-pool"
	defaultL2Name     = "default"
)

func decodeObjects(data []byte) ([]*unstructured.Unstructured, error) {
	dec := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var out []*unstructured.Unstructured
	for {
		var raw map[string]interface{}
		if err := dec.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if len(raw) == 0 {
			continue
		}
		obj := &unstructured.Unstructured{Object: raw}
		if obj.GetKind() == "" || obj.GetAPIVersion() == "" {
			continue
		}
		out = append(out, obj)
	}
	return out, nil
}

func stampManaged(obj *unstructured.Unstructured) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[managedLabel] = managedValue
	obj.SetLabels(labels)
}

func relocateObjects(objs []*unstructured.Unstructured, toNS string) {
	if toNS == "" || toNS == upstreamNamespace {
		return
	}
	from := upstreamNamespace
	for _, obj := range objs {
		if obj.GetKind() == "Namespace" && obj.GetName() == from {
			obj.SetName(toNS)
		}
		if obj.GetNamespace() == from {
			obj.SetNamespace(toNS)
		}
		subjects, found, _ := unstructured.NestedSlice(obj.Object, "subjects")
		if found {
			changed := false
			for i, s := range subjects {
				m, ok := s.(map[string]interface{})
				if !ok {
					continue
				}
				if ns, _ := m["namespace"].(string); ns == from {
					m["namespace"] = toNS
					subjects[i] = m
					changed = true
				}
			}
			if changed {
				_ = unstructured.SetNestedSlice(obj.Object, subjects, "subjects")
			}
		}
		if obj.GetKind() == "ClusterRole" || obj.GetKind() == "ClusterRoleBinding" {
			if strings.HasPrefix(obj.GetName(), from+":") {
				obj.SetName(toNS + ":" + strings.TrimPrefix(obj.GetName(), from+":"))
			}
		}
		if obj.GetKind() == "ClusterRoleBinding" {
			if roleRef, found, _ := unstructured.NestedString(obj.Object, "roleRef", "name"); found && strings.HasPrefix(roleRef, from+":") {
				_ = unstructured.SetNestedField(obj.Object, toNS+":"+strings.TrimPrefix(roleRef, from+":"), "roleRef", "name")
			}
		}
		// Validating/Mutating webhook clientConfig.service.namespace
		if obj.GetKind() == "ValidatingWebhookConfiguration" || obj.GetKind() == "MutatingWebhookConfiguration" {
			webhooks, found, _ := unstructured.NestedSlice(obj.Object, "webhooks")
			if !found {
				continue
			}
			changed := false
			for i, w := range webhooks {
				wm, ok := w.(map[string]interface{})
				if !ok {
					continue
				}
				svc, ok, _ := unstructured.NestedMap(wm, "clientConfig", "service")
				if !ok {
					continue
				}
				if ns, _ := svc["namespace"].(string); ns == from {
					svc["namespace"] = toNS
					_ = unstructured.SetNestedMap(wm, svc, "clientConfig", "service")
					webhooks[i] = wm
					changed = true
				}
			}
			if changed {
				_ = unstructured.SetNestedSlice(obj.Object, webhooks, "webhooks")
			}
		}
	}
}

// createOnlyKind: never Update after first Create — mutating these fights MetalLB
// (webhook CA/certs) or thrashs the apiserver (CRDs).
func createOnlyKind(kind string) bool {
	switch kind {
	case "Secret",
		"ValidatingWebhookConfiguration",
		"MutatingWebhookConfiguration",
		"CustomResourceDefinition":
		return true
	default:
		return false
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func specEqual(desired, existing *unstructured.Unstructured) bool {
	ds, _, _ := unstructured.NestedFieldNoCopy(desired.Object, "spec")
	es, _, _ := unstructured.NestedFieldNoCopy(existing.Object, "spec")
	if ds == nil && es == nil {
		return true
	}
	return reflect.DeepEqual(ds, es)
}

func managedLabelOK(existing *unstructured.Unstructured) bool {
	labels := existing.GetLabels()
	return labels != nil && labels[managedLabel] == managedValue
}

func applyObjects(ctx context.Context, c client.Client, objs []*unstructured.Unstructured) error {
	for _, obj := range objs {
		stampManaged(obj)
		obj.SetManagedFields(nil)
		obj.SetResourceVersion("")
		obj.SetUID("")
		obj.SetCreationTimestamp(metav1.Time{})
		obj.SetGeneration(0)

		existing := &unstructured.Unstructured{}
		existing.SetGroupVersionKind(obj.GroupVersionKind())
		key := client.ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetName()}
		err := c.Get(ctx, key, existing)
		if apierrors.IsNotFound(err) {
			if err := c.Create(ctx, obj); err != nil {
				return fmt.Errorf("create %s/%s %s: %w", obj.GetKind(), obj.GetNamespace(), obj.GetName(), err)
			}
			continue
		}
		if err != nil {
			return err
		}
		if createOnlyKind(obj.GetKind()) {
			continue
		}
		if obj.GetKind() == "Namespace" {
			if managedLabelOK(existing) {
				continue
			}
			merged := map[string]string{}
			for k, v := range existing.GetLabels() {
				merged[k] = v
			}
			for k, v := range obj.GetLabels() {
				merged[k] = v
			}
			existing.SetLabels(merged)
			if err := c.Update(ctx, existing); err != nil {
				return fmt.Errorf("update Namespace %s: %w", existing.GetName(), err)
			}
			continue
		}
		// No-op when spec already matches and our managed label is present.
		if managedLabelOK(existing) && specEqual(obj, existing) {
			continue
		}
		obj.SetResourceVersion(existing.GetResourceVersion())
		// Preserve resource-versioned identity; avoid wiping status via full replace when possible.
		if err := c.Patch(ctx, obj, client.MergeFrom(existing)); err != nil {
			obj.SetResourceVersion(existing.GetResourceVersion())
			if err2 := c.Update(ctx, obj); err2 != nil {
				return fmt.Errorf("update %s/%s %s: patch=%v update=%w", obj.GetKind(), obj.GetNamespace(), obj.GetName(), err, err2)
			}
		}
	}
	return nil
}

func applyMetalLBNative(ctx context.Context, c client.Client, ns string) error {
	data, err := manifestFS.ReadFile(nativeManifest)
	if err != nil {
		return fmt.Errorf("read %s: %w", nativeManifest, err)
	}
	objs, err := decodeObjects(data)
	if err != nil {
		return fmt.Errorf("decode %s: %w", nativeManifest, err)
	}
	relocateObjects(objs, ns)
	return applyObjects(ctx, c, objs)
}

func ensureIPAddressPool(ctx context.Context, c client.Client, ns, name string, addresses []string) error {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "metallb.io", Version: "v1beta1", Kind: "IPAddressPool",
	})
	err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, existing)
	if apierrors.IsNotFound(err) {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(existing.GroupVersionKind())
		obj.SetNamespace(ns)
		obj.SetName(name)
		stampManaged(obj)
		_ = unstructured.SetNestedStringSlice(obj.Object, addresses, "spec", "addresses")
		return c.Create(ctx, obj)
	}
	if err != nil {
		return err
	}
	cur, _, _ := unstructured.NestedStringSlice(existing.Object, "spec", "addresses")
	if managedLabelOK(existing) && stringSlicesEqual(cur, addresses) {
		return nil
	}
	stampManaged(existing)
	_ = unstructured.SetNestedStringSlice(existing.Object, addresses, "spec", "addresses")
	return c.Update(ctx, existing)
}

func ensureL2Advertisement(ctx context.Context, c client.Client, ns, name, poolName string) error {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "metallb.io", Version: "v1beta1", Kind: "L2Advertisement",
	})
	err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, existing)
	if apierrors.IsNotFound(err) {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(existing.GroupVersionKind())
		obj.SetNamespace(ns)
		obj.SetName(name)
		stampManaged(obj)
		_ = unstructured.SetNestedStringSlice(obj.Object, []string{poolName}, "spec", "ipAddressPools")
		return c.Create(ctx, obj)
	}
	if err != nil {
		return err
	}
	cur, _, _ := unstructured.NestedStringSlice(existing.Object, "spec", "ipAddressPools")
	if managedLabelOK(existing) && stringSlicesEqual(cur, []string{poolName}) {
		return nil
	}
	stampManaged(existing)
	_ = unstructured.SetNestedStringSlice(existing.Object, []string{poolName}, "spec", "ipAddressPools")
	return c.Update(ctx, existing)
}

func deleteConfigCRs(ctx context.Context, c client.Client, ns, poolName, l2Name string) {
	pool := &unstructured.Unstructured{}
	pool.SetGroupVersionKind(schema.GroupVersionKind{Group: "metallb.io", Version: "v1beta1", Kind: "IPAddressPool"})
	pool.SetNamespace(ns)
	pool.SetName(poolName)
	_ = c.Delete(ctx, pool)

	l2 := &unstructured.Unstructured{}
	l2.SetGroupVersionKind(schema.GroupVersionKind{Group: "metallb.io", Version: "v1beta1", Kind: "L2Advertisement"})
	l2.SetNamespace(ns)
	l2.SetName(l2Name)
	_ = c.Delete(ctx, l2)
}

func deleteManagedWorkloads(ctx context.Context, c client.Client, ns string) {
	// Delete stamped namespaced workloads (keep Namespace + CRDs for fast re-Deploy).
	kinds := []schema.GroupVersionKind{
		{Group: "apps", Version: "v1", Kind: "Deployment"},
		{Group: "apps", Version: "v1", Kind: "DaemonSet"},
		{Group: "", Version: "v1", Kind: "Service"},
		{Group: "", Version: "v1", Kind: "ConfigMap"},
		{Group: "", Version: "v1", Kind: "Secret"},
		{Group: "", Version: "v1", Kind: "ServiceAccount"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"},
	}
	sel := client.MatchingLabels{managedLabel: managedValue}
	for _, gvk := range kinds {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind + "List"})
		if err := c.List(ctx, list, client.InNamespace(ns), sel); err != nil {
			continue
		}
		for i := range list.Items {
			_ = c.Delete(ctx, &list.Items[i])
		}
	}

	clusterKinds := []schema.GroupVersionKind{
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"},
		{Group: "admissionregistration.k8s.io", Version: "v1", Kind: "ValidatingWebhookConfiguration"},
	}
	for _, gvk := range clusterKinds {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind + "List"})
		if err := c.List(ctx, list, sel); err != nil {
			continue
		}
		for i := range list.Items {
			_ = c.Delete(ctx, &list.Items[i])
		}
	}
}
