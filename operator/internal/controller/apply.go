package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *KubeDashboardReconciler) applyYAML(ctx context.Context, _ client.Object, raw []byte, targetNS string) error {
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(raw), 4096)
	for {
		var obj unstructured.Unstructured
		err := decoder.Decode(&obj)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("decode yaml: %w", err)
		}
		if len(obj.Object) == 0 {
			continue
		}
		// Console workloads live in kubeops-console (or kd.spec.namespace).
		// Skip embedded Namespace objects — ensureNamespace already owns that.
		if obj.GetKind() == "Namespace" {
			continue
		}
		if targetNS != "" {
			rewriteNamespace(&obj, targetNS)
		}
		if err := r.ensureUnstructured(ctx, &obj); err != nil {
			return err
		}
	}
	return nil
}

func rewriteNamespace(obj *unstructured.Unstructured, targetNS string) {
	if obj.GetNamespace() != "" {
		obj.SetNamespace(targetNS)
	}
	switch obj.GetKind() {
	case "ClusterRoleBinding", "RoleBinding":
		subjects, found, err := unstructured.NestedSlice(obj.Object, "subjects")
		if err != nil || !found {
			return
		}
		for i, s := range subjects {
			m, ok := s.(map[string]interface{})
			if !ok {
				continue
			}
			if _, hasNS := m["namespace"]; hasNS {
				m["namespace"] = targetNS
				subjects[i] = m
			}
		}
		_ = unstructured.SetNestedSlice(obj.Object, subjects, "subjects")
	}
}

func (r *KubeDashboardReconciler) ensureUnstructured(ctx context.Context, obj *unstructured.Unstructured) error {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())
	key := client.ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetName()}
	err := r.Get(ctx, key, existing)
	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, obj); err != nil {
			return fmt.Errorf("create %s/%s: %w", obj.GetKind(), obj.GetName(), err)
		}
		return nil
	}
	if err != nil {
		return err
	}

	kind := obj.GetKind()
	switch kind {
	case "Deployment", "Service", "ConfigMap", "Role", "RoleBinding", "ClusterRole", "ClusterRoleBinding", "APIService":
		obj.SetResourceVersion(existing.GetResourceVersion())
		obj.SetUID(existing.GetUID())
		if err := r.Update(ctx, obj); err != nil {
			return fmt.Errorf("update %s/%s: %w", kind, obj.GetName(), err)
		}
	default:
		// Secret / ServiceAccount — keep existing
	}
	return nil
}

func (r *KubeDashboardReconciler) ensureConfigMap(ctx context.Context, ns, name string, data map[string]string, labels map[string]string) error {
	cm := &corev1.ConfigMap{}
	cm.Name = name
	cm.Namespace = ns
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		cm.Labels = labels
		cm.Data = data
		return nil
	})
	if err != nil {
		return fmt.Errorf("configmap %s: %w", name, err)
	}
	return nil
}
