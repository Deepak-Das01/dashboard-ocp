package ceph

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:embed manifests/*.yaml manifests/*.txt manifests/csi-rbd/*.yaml manifests/csi-cephfs/*.yaml
var manifestFS embed.FS

const (
	upstreamNamespace = "rook-ceph"
	managedLabel      = "dashboard.ocp/addon"
	managedValue      = "kubeops-ceph"
	defaultCephImage  = "quay.io/ceph/ceph:v19.2.3"
	defaultHostPath   = "/var/lib/rook"
	defaultDashPort   = int32(30448)
	blockPoolName     = "replicapool"
	blockSCName       = "rook-ceph-block"
	fsName            = "ceph-filesystem"
	fsSCName          = "rook-cephfs"
	cephClusterName   = "rook-ceph"
	wipeAnnotation    = "kubeops.io/wipe-disks"
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
		// Rewrite nested string values that hardcode rook-ceph as the cluster ns
		// (clusterID, secret namespaces in StorageClasses, etc.).
		rewriteNestedStrings(obj.Object, from, toNS)

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
	}
}

func rewriteNestedStrings(v interface{}, from, to string) {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, val := range t {
			if s, ok := val.(string); ok {
				if s == from {
					t[k] = to
				} else if strings.Contains(s, from) {
					// Avoid rewriting image refs / unrelated strings that merely contain the token.
					if s == from ||
						strings.HasPrefix(s, from+"/") ||
						strings.HasSuffix(s, "/"+from) ||
						strings.Contains(s, "namespace: "+from) {
						t[k] = strings.ReplaceAll(s, from, to)
					}
				}
			} else {
				rewriteNestedStrings(val, from, to)
			}
		}
	case []interface{}:
		for i := range t {
			if s, ok := t[i].(string); ok && s == from {
				t[i] = to
			} else {
				rewriteNestedStrings(t[i], from, to)
			}
		}
	}
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
		if obj.GetKind() == "Namespace" {
			merged := map[string]string{}
			for k, v := range existing.GetLabels() {
				merged[k] = v
			}
			for k, v := range obj.GetLabels() {
				merged[k] = v
			}
			obj.SetLabels(merged)
		}
		obj.SetResourceVersion(existing.GetResourceVersion())
		if err := c.Update(ctx, obj); err != nil {
			if err := c.Patch(ctx, obj, client.MergeFrom(existing)); err != nil {
				return fmt.Errorf("update %s/%s %s: %w", obj.GetKind(), obj.GetNamespace(), obj.GetName(), err)
			}
		}
	}
	return nil
}

func applyEmbeddedManifest(ctx context.Context, c client.Client, path, ns string) error {
	data, err := manifestFS.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	objs, err := decodeObjects(data)
	if err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	relocateObjects(objs, ns)
	return applyObjects(ctx, c, objs)
}

func ensureWorkloadNamespace(ctx context.Context, c client.Client, ns string) error {
	obj := &corev1.Namespace{}
	err := c.Get(ctx, client.ObjectKey{Name: ns}, obj)
	if apierrors.IsNotFound(err) {
		obj = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: ns,
				Labels: map[string]string{
					"app.kubernetes.io/name":              ns,
					"app.kubernetes.io/part-of":           "kube-dashboard",
					"app.kubernetes.io/component":         "ceph",
					managedLabel:                          managedValue,
					"pod-security.kubernetes.io/enforce":  "privileged",
					"pod-security.kubernetes.io/audit":    "privileged",
					"pod-security.kubernetes.io/warn":     "privileged",
				},
			},
		}
		return c.Create(ctx, obj)
	}
	return err
}

func applyRookOperator(ctx context.Context, c client.Client, ns string) error {
	if err := ensureWorkloadNamespace(ctx, c, ns); err != nil {
		return err
	}
	// CRDs are cluster-scoped — apply without namespace rewrite of Namespace kind only.
	if err := applyEmbeddedManifest(ctx, c, "manifests/crds.yaml", ns); err != nil {
		return fmt.Errorf("rook crds: %w", err)
	}
	if err := applyEmbeddedManifest(ctx, c, "manifests/common.yaml", ns); err != nil {
		return fmt.Errorf("rook common: %w", err)
	}
	if err := applyEmbeddedManifest(ctx, c, "manifests/operator.yaml", ns); err != nil {
		return fmt.Errorf("rook operator: %w", err)
	}
	return nil
}

func applyToolbox(ctx context.Context, c client.Client, ns, cephImage string) error {
	data, err := manifestFS.ReadFile("manifests/toolbox.yaml")
	if err != nil {
		return err
	}
	if cephImage != "" {
		// Upstream uses quay.io/ceph/ceph:v19 — pin to CR image.
		data = bytes.ReplaceAll(data, []byte("image: quay.io/ceph/ceph:v19\n"), []byte("image: "+cephImage+"\n"))
		data = bytes.ReplaceAll(data, []byte("image: quay.io/ceph/ceph:v19 "), []byte("image: "+cephImage+" "))
	}
	objs, err := decodeObjects(data)
	if err != nil {
		return err
	}
	relocateObjects(objs, ns)
	return applyObjects(ctx, c, objs)
}

func deleteUnstructured(ctx context.Context, c client.Client, gvk, ns, name string) error {
	parts := strings.SplitN(gvk, "|", 3)
	if len(parts) != 3 {
		return fmt.Errorf("bad gvk %q", gvk)
	}
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(parts[0])
	obj.SetKind(parts[1])
	key := client.ObjectKey{Namespace: ns, Name: name}
	if parts[2] == "cluster" {
		key.Namespace = ""
	}
	if err := c.Get(ctx, key, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return c.Delete(ctx, obj)
}
