package virtualization

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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

//go:embed manifests/*.yaml manifests/versions.txt
var manifestFS embed.FS

const (
	defaultKubeVirtVersion = "v1.10.0"
	defaultCDIVersion      = "v1.66.1"
	defaultUIManifest      = "kubevirt-manager-ui-v1.5.4.yaml"
	defaultUINodePort      = int32(30444)
	// installNamespace hosts controller, kubevirt-manager UI, KubeVirt, and CDI.
	// Upstream manifests default to "kubevirt" / "cdi"; we relocate at apply time.
	installNamespace = "kubeops-virtualization"
	legacyKubeVirtNS = "kubevirt"
	legacyCDINS      = "cdi"
	managedLabel     = "dashboard.ocp/addon"
	managedValue     = "kubeops-virtualization"
)

func resolveVersions(kv, cdi string) (string, string) {
	if strings.TrimSpace(kv) == "" {
		kv = defaultKubeVirtVersion
	}
	if strings.TrimSpace(cdi) == "" {
		cdi = defaultCDIVersion
	}
	return kv, cdi
}

func manifestName(kind, version string) string {
	return fmt.Sprintf("%s-%s.yaml", kind, version)
}

func readManifest(kind, version string) ([]byte, error) {
	name := "manifests/" + manifestName(kind, version)
	data, err := manifestFS.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("vendored manifest %s missing (only pinned versions are supported): %w", name, err)
	}
	return data, nil
}

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
		// Preserve install-namespace labels from both KubeVirt and CDI manifests.
		if obj.GetKind() == "Namespace" && obj.GetName() == installNamespace {
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
			// CRDs / large objects may conflict — retry via patch merge of spec/data
			if err := c.Patch(ctx, obj, client.MergeFrom(existing)); err != nil {
				return fmt.Errorf("update %s/%s %s: %w", obj.GetKind(), obj.GetNamespace(), obj.GetName(), err)
			}
		}
	}
	return nil
}

func deleteObjects(ctx context.Context, c client.Client, objs []*unstructured.Unstructured) error {
	// Reverse order helps dependents go first.
	for i := len(objs) - 1; i >= 0; i-- {
		obj := objs[i]
		// Never delete the workload install namespace on uninstall. Upstream
		// kubevirt/cdi/UI manifests include a Namespace we relocate here; removing
		// it is unnecessary (controller lives in kubeops-addon-controller) and
		// makes the next Deploy recreate more work.
		if obj.GetKind() == "Namespace" && obj.GetName() == installNamespace {
			continue
		}
		existing := &unstructured.Unstructured{}
		existing.SetGroupVersionKind(obj.GroupVersionKind())
		key := client.ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetName()}
		if err := c.Get(ctx, key, existing); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if err := c.Delete(ctx, existing); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete %s/%s %s: %w", obj.GetKind(), obj.GetNamespace(), obj.GetName(), err)
		}
	}
	return nil
}

// relocateObjects rewrites upstream install namespace (kubevirt/cdi) → installNamespace.
func relocateObjects(objs []*unstructured.Unstructured, from string) {
	if from == "" || from == installNamespace {
		return
	}
	for _, obj := range objs {
		if obj.GetKind() == "Namespace" && obj.GetName() == from {
			obj.SetName(installNamespace)
		}
		if obj.GetNamespace() == from {
			obj.SetNamespace(installNamespace)
		}
		subjects, found, _ := unstructured.NestedSlice(obj.Object, "subjects")
		if !found {
			continue
		}
		changed := false
		for i, s := range subjects {
			m, ok := s.(map[string]interface{})
			if !ok {
				continue
			}
			if ns, _ := m["namespace"].(string); ns == from {
				m["namespace"] = installNamespace
				subjects[i] = m
				changed = true
			}
		}
		if changed {
			_ = unstructured.SetNestedSlice(obj.Object, subjects, "subjects")
		}
	}
}

func upstreamNSForKind(kind string) string {
	switch {
	case strings.HasPrefix(kind, "kubevirt"):
		return legacyKubeVirtNS
	case strings.HasPrefix(kind, "cdi"):
		return legacyCDINS
	default:
		return ""
	}
}

func ensureInstallNamespace(ctx context.Context, c client.Client) error {
	ns := &corev1.Namespace{}
	err := c.Get(ctx, client.ObjectKey{Name: installNamespace}, ns)
	if apierrors.IsNotFound(err) {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: installNamespace,
				Labels: map[string]string{
					"app.kubernetes.io/name":                     installNamespace,
					"kubevirt.io":                                "",
					"cdi.kubevirt.io":                            "",
					"pod-security.kubernetes.io/enforce":         "privileged",
					"pod-security.kubernetes.io/audit":           "privileged",
					"pod-security.kubernetes.io/warn":            "privileged",
					managedLabel:                                 managedValue,
				},
			},
		}
		return c.Create(ctx, ns)
	}
	if err != nil {
		return err
	}
	labels := ns.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	need := map[string]string{
		"kubevirt.io":                        "",
		"cdi.kubevirt.io":                    "",
		"pod-security.kubernetes.io/enforce": "privileged",
		"pod-security.kubernetes.io/audit":   "privileged",
		"pod-security.kubernetes.io/warn":    "privileged",
		managedLabel:                         managedValue,
	}
	changed := false
	for k, v := range need {
		if labels[k] != v {
			labels[k] = v
			changed = true
		}
	}
	if !changed {
		return nil
	}
	ns.Labels = labels
	return c.Update(ctx, ns)
}

func applyReleaseManifest(ctx context.Context, c client.Client, kind, version string) error {
	if err := ensureInstallNamespace(ctx, c); err != nil {
		return err
	}
	data, err := readManifest(kind, version)
	if err != nil {
		return err
	}
	objs, err := decodeObjects(data)
	if err != nil {
		return fmt.Errorf("decode %s: %w", kind, err)
	}
	relocateObjects(objs, upstreamNSForKind(kind))
	return applyObjects(ctx, c, objs)
}

func applyUIManifest(ctx context.Context, c client.Client) error {
	data, err := manifestFS.ReadFile("manifests/" + defaultUIManifest)
	if err != nil {
		return fmt.Errorf("vendored UI manifest missing: %w", err)
	}
	objs, err := decodeObjects(data)
	if err != nil {
		return fmt.Errorf("decode UI: %w", err)
	}
	return applyObjects(ctx, c, objs)
}

func deleteUIManifest(ctx context.Context, c client.Client) error {
	data, err := manifestFS.ReadFile("manifests/" + defaultUIManifest)
	if err != nil {
		return nil
	}
	objs, err := decodeObjects(data)
	if err != nil {
		return err
	}
	return deleteObjects(ctx, c, objs)
}

func uiReady(ctx context.Context, c client.Client) bool {
	dep := &unstructured.Unstructured{}
	dep.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"})
	if err := c.Get(ctx, client.ObjectKey{Namespace: installNamespace, Name: "kubevirt-manager"}, dep); err != nil {
		return false
	}
	ready, _, _ := unstructured.NestedInt64(dep.Object, "status", "readyReplicas")
	return ready >= 1
}

func deployExists(ctx context.Context, c client.Client, ns, name string) bool {
	dep := &unstructured.Unstructured{}
	dep.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"})
	err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, dep)
	return err == nil
}

func nodeInternalIP(ctx context.Context, c client.Client) string {
	list := &corev1.NodeList{}
	if err := c.List(ctx, list); err != nil || len(list.Items) == 0 {
		return ""
	}
	for _, a := range list.Items[0].Status.Addresses {
		if a.Type == corev1.NodeInternalIP && a.Address != "" {
			return a.Address
		}
	}
	return ""
}

func uiURL(ctx context.Context, c client.Client) string {
	ip := nodeInternalIP(ctx, c)
	if ip == "" {
		return fmt.Sprintf("http://<node-ip>:%d/", defaultUINodePort)
	}
	return fmt.Sprintf("http://%s:%d/", ip, defaultUINodePort)
}

func deleteReleaseManifest(ctx context.Context, c client.Client, kind, version string) error {
	return deleteReleaseManifestFrom(ctx, c, kind, version, upstreamNSForKind(kind))
}

// deleteReleaseManifestFrom deletes operator objects, optionally relocating from→installNamespace first.
// Pass relocateFrom="" to delete at the namespaces written in the vendored YAML (legacy teardown).
func deleteReleaseManifestFrom(ctx context.Context, c client.Client, kind, version, relocateFrom string) error {
	data, err := readManifest(kind, version)
	if err != nil {
		return err
	}
	objs, err := decodeObjects(data)
	if err != nil {
		return err
	}
	if relocateFrom != "" {
		relocateObjects(objs, relocateFrom)
	}
	return deleteObjects(ctx, c, objs)
}

func crdExists(ctx context.Context, c client.Client, name string) bool {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1",
		Kind:    "CustomResourceDefinition",
	})
	err := c.Get(ctx, client.ObjectKey{Name: name}, obj)
	return err == nil
}

func ensureKubeVirtCR(ctx context.Context, c client.Client, version string, useEmulation bool) error {
	if err := ensureInstallNamespace(ctx, c); err != nil {
		return err
	}
	data, err := readManifest("kubevirt-cr", version)
	if err != nil {
		return err
	}
	objs, err := decodeObjects(data)
	if err != nil || len(objs) == 0 {
		return fmt.Errorf("kubevirt-cr decode: %w", err)
	}
	relocateObjects(objs, legacyKubeVirtNS)
	obj := objs[0]
	obj.SetNamespace(installNamespace)
	if useEmulation {
		if err := unstructured.SetNestedField(obj.Object, true,
			"spec", "configuration", "developerConfiguration", "useEmulation"); err != nil {
			return err
		}
	}
	stampManaged(obj)
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())
	key := client.ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetName()}
	err = c.Get(ctx, key, existing)
	if apierrors.IsNotFound(err) {
		return c.Create(ctx, obj)
	}
	if err != nil {
		return err
	}
	_, err = controllerutil.CreateOrUpdate(ctx, c, existing, func() error {
		spec, _, _ := unstructured.NestedMap(obj.Object, "spec")
		if spec != nil {
			_ = unstructured.SetNestedMap(existing.Object, spec, "spec")
		}
		stampManaged(existing)
		return nil
	})
	return err
}

func ensureCDICR(ctx context.Context, c client.Client, version string) error {
	data, err := readManifest("cdi-cr", version)
	if err != nil {
		return err
	}
	objs, err := decodeObjects(data)
	if err != nil || len(objs) == 0 {
		return fmt.Errorf("cdi-cr decode: %w", err)
	}
	obj := objs[0]
	stampManaged(obj)
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())
	key := client.ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetName()}
	err = c.Get(ctx, key, existing)
	if apierrors.IsNotFound(err) {
		return c.Create(ctx, obj)
	}
	if err != nil {
		return err
	}
	_, err = controllerutil.CreateOrUpdate(ctx, c, existing, func() error {
		spec, _, _ := unstructured.NestedMap(obj.Object, "spec")
		if spec != nil {
			_ = unstructured.SetNestedMap(existing.Object, spec, "spec")
		}
		stampManaged(existing)
		return nil
	})
	return err
}

func isMissingAPI(err error) bool {
	if err == nil {
		return false
	}
	if apierrors.IsNotFound(err) {
		return true
	}
	// After a clean uninstall the CRD is gone; discovery returns "no matches for kind".
	msg := err.Error()
	return strings.Contains(msg, "no matches for kind") ||
		strings.Contains(msg, "could not find the requested resource")
}

func getPhase(ctx context.Context, c client.Client, gvk schema.GroupVersionKind, ns, name string) (string, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, obj); err != nil {
		return "", err
	}
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	return phase, nil
}

func deleteCR(ctx context.Context, c client.Client, gvk schema.GroupVersionKind, ns, name string) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	key := client.ObjectKey{Namespace: ns, Name: name}
	if err := c.Get(ctx, key, obj); err != nil {
		if isMissingAPI(err) {
			return nil
		}
		return err
	}
	return c.Delete(ctx, obj)
}

var (
	kubeVirtGVK = schema.GroupVersionKind{Group: "kubevirt.io", Version: "v1", Kind: "KubeVirt"}
	cdiGVK      = schema.GroupVersionKind{Group: "cdi.kubevirt.io", Version: "v1beta1", Kind: "CDI"}
)
