package nfs

import (
	"context"
	"fmt"
	"strings"
	"time"

	dashboardv1alpha1 "github.com/Deepak-Das01/kubeops-platform/operator/api/v1alpha1"
	"github.com/Deepak-Das01/kubeops-platform/operator/internal/placement"
	opstatus "github.com/Deepak-Das01/kubeops-platform/operator/internal/status"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	operatorReportName = "kubeops-nfs"
	operatorVersion    = "1.3.0"
	finalizerName      = "dashboard.ocp/nfs-provisioner"
	provisionerName    = "cluster.local/nfs-subdir-external-provisioner"
	provisionerImage   = "registry.k8s.io/sig-storage/nfs-subdir-external-provisioner:v4.0.2"
	defaultNamespace   = "kubeops-nfs"
	managedLabel       = "dashboard.ocp/addon"
	managedValue       = "kubeops-nfs"
)

// NFSProvisionerReconciler installs nfs-subdir-external-provisioner from the CR.
type NFSProvisionerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *NFSProvisionerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Watch only the CR — owning Deployment caused reconcile storms / update conflicts.
	return ctrl.NewControllerManagedBy(mgr).
		For(&dashboardv1alpha1.NFSProvisioner{}).
		Complete(r)
}

func (r *NFSProvisionerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("nfsprovisioner", req.Name)
	cr := &dashboardv1alpha1.NFSProvisioner{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !cr.DeletionTimestamp.IsZero() {
		if err := r.cleanup(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}
		controllerutil.RemoveFinalizer(cr, finalizerName)
		if err := r.Update(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentNFS, operatorVersion,
			dashboardv1alpha1.OperatorPhaseUnavailable, "",
			"Unavailable — NFS provisioner not installed; deploy from Addons with your NFS server details")
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(cr, finalizerName) {
		controllerutil.AddFinalizer(cr, finalizerName)
		if err := r.Update(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	ns := strings.TrimSpace(cr.Spec.Namespace)
	if ns == "" {
		ns = defaultNamespace
	}
	server := strings.TrimSpace(cr.Spec.Server)
	path := strings.TrimSpace(cr.Spec.Path)
	scName := strings.TrimSpace(cr.Spec.StorageClassName)
	release := cr.Name
	if release == "" {
		release = "nfs-provisioner"
	}

	if server == "" || path == "" || scName == "" {
		msg := "server, path, and storageClassName are required"
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.NFSProvisionerPhaseDegraded, msg, ns, scName)
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentNFS, operatorVersion,
			dashboardv1alpha1.OperatorPhaseDegraded, "", msg)
		return ctrl.Result{}, nil
	}

	if err := r.ensureNamespace(ctx, ns); err != nil {
		return r.fail(ctx, cr, ns, scName, err)
	}
	if err := r.ensureRBAC(ctx, ns, release); err != nil {
		return r.fail(ctx, cr, ns, scName, err)
	}
	if err := r.ensureDeployment(ctx, ns, release, server, path); err != nil {
		return r.fail(ctx, cr, ns, scName, err)
	}
	if err := r.ensureStorageClass(ctx, scName, cr.Spec.DefaultClass); err != nil {
		return r.fail(ctx, cr, ns, scName, err)
	}

	ready, msg := r.deploymentReady(ctx, ns, release)
	if !ready {
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.NFSProvisionerPhaseProgressing, msg, ns, scName)
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentNFS, operatorVersion,
			dashboardv1alpha1.OperatorPhaseProgressing, "", msg)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	okMsg := fmt.Sprintf("Ready — StorageClass %s (NFS %s:%s)", scName, server, path)
	// Avoid thrashing status on every Deployment watch event once healthy.
	if cr.Status.Phase != dashboardv1alpha1.NFSProvisionerPhaseReady ||
		cr.Status.Message != okMsg ||
		cr.Status.ObservedGeneration != cr.Generation {
		_ = r.patchStatus(ctx, cr, dashboardv1alpha1.NFSProvisionerPhaseReady, okMsg, ns, scName)
		_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentNFS, operatorVersion,
			dashboardv1alpha1.OperatorPhaseReady, "", okMsg)
		logger.Info("nfs provisioner ready", "namespace", ns, "storageClass", scName)
	}
	return ctrl.Result{}, nil
}

func (r *NFSProvisionerReconciler) fail(ctx context.Context, cr *dashboardv1alpha1.NFSProvisioner, ns, sc string, err error) (ctrl.Result, error) {
	msg := err.Error()
	_ = r.patchStatus(ctx, cr, dashboardv1alpha1.NFSProvisionerPhaseDegraded, msg, ns, sc)
	_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentNFS, operatorVersion,
		dashboardv1alpha1.OperatorPhaseDegraded, "", msg)
	// Requeue slowly — returning the error caused tight reconcile loops.
	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

func (r *NFSProvisionerReconciler) patchStatus(ctx context.Context, cr *dashboardv1alpha1.NFSProvisioner, phase dashboardv1alpha1.NFSProvisionerPhase, msg, ns, sc string) error {
	fresh := &dashboardv1alpha1.NFSProvisioner{}
	if err := r.Get(ctx, types.NamespacedName{Name: cr.Name}, fresh); err != nil {
		return err
	}
	fresh.Status.Phase = phase
	fresh.Status.Message = msg
	fresh.Status.Namespace = ns
	fresh.Status.StorageClass = sc
	fresh.Status.ObservedGeneration = fresh.Generation
	if err := r.Status().Update(ctx, fresh); err != nil && !apierrors.IsConflict(err) {
		return err
	}
	return nil
}

func (r *NFSProvisionerReconciler) ensureNamespace(ctx context.Context, ns string) error {
	obj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		if obj.Labels == nil {
			obj.Labels = map[string]string{}
		}
		obj.Labels[managedLabel] = managedValue
		obj.Labels["app.kubernetes.io/part-of"] = "kube-dashboard"
		return nil
	})
	return err
}

func labelsFor(release string) map[string]string {
	return map[string]string{
		managedLabel:                managedValue,
		"app":                       release,
		"app.kubernetes.io/name":    release,
		"app.kubernetes.io/part-of": "kube-dashboard",
		"app.kubernetes.io/component": "nfs-provisioner",
	}
}

func (r *NFSProvisionerReconciler) ensureRBAC(ctx context.Context, ns, release string) error {
	labels := labelsFor(release)
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, sa, func() error {
		sa.Labels = labels
		return nil
	}); err != nil {
		return fmt.Errorf("serviceaccount: %w", err)
	}

	cr := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: release}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, cr, func() error {
		cr.Labels = labels
		cr.Rules = []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"nodes"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{""}, Resources: []string{"persistentvolumes"}, Verbs: []string{"get", "list", "watch", "create", "delete"}},
			{APIGroups: []string{""}, Resources: []string{"persistentvolumeclaims"}, Verbs: []string{"get", "list", "watch", "update"}},
			{APIGroups: []string{"storage.k8s.io"}, Resources: []string{"storageclasses"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"create", "update", "patch"}},
		}
		return nil
	}); err != nil {
		return fmt.Errorf("clusterrole: %w", err)
	}

	crb := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: release}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, crb, func() error {
		crb.Labels = labels
		crb.RoleRef = rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: release}
		crb.Subjects = []rbacv1.Subject{{Kind: "ServiceAccount", Name: release, Namespace: ns}}
		return nil
	}); err != nil {
		return fmt.Errorf("clusterrolebinding: %w", err)
	}

	role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "leader-locking-" + release, Namespace: ns}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, role, func() error {
		role.Labels = labels
		role.Rules = []rbacv1.PolicyRule{
			{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch"}},
			{APIGroups: []string{""}, Resources: []string{"endpoints"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch"}},
		}
		return nil
	}); err != nil {
		return fmt.Errorf("role: %w", err)
	}

	rb := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "leader-locking-" + release, Namespace: ns}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, rb, func() error {
		rb.Labels = labels
		rb.RoleRef = rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: "leader-locking-" + release}
		rb.Subjects = []rbacv1.Subject{{Kind: "ServiceAccount", Name: release, Namespace: ns}}
		return nil
	}); err != nil {
		return fmt.Errorf("rolebinding: %w", err)
	}
	return nil
}

func (r *NFSProvisionerReconciler) ensureDeployment(ctx context.Context, ns, release, server, path string) error {
	labels := labelsFor(release)
	replicas := int32(1)
	desiredPodSpec := func(d *appsv1.Deployment) {
		d.Labels = labels
		d.Spec.Replicas = &replicas
		if d.Spec.Selector == nil {
			d.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": release}}
		}
		d.Spec.Template.ObjectMeta.Labels = labels
		d.Spec.Template.Spec.ServiceAccountName = release
		d.Spec.Template.Spec.Containers = []corev1.Container{{
			Name:            "nfs-subdir-external-provisioner",
			Image:           provisionerImage,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Env: []corev1.EnvVar{
				{Name: "PROVISIONER_NAME", Value: provisionerName},
				{Name: "NFS_SERVER", Value: server},
				{Name: "NFS_PATH", Value: path},
			},
			VolumeMounts: []corev1.VolumeMount{{Name: "nfs-client-root", MountPath: "/persistentvolumes"}},
		}}
		d.Spec.Template.Spec.Volumes = []corev1.Volume{{
			Name: "nfs-client-root",
			VolumeSource: corev1.VolumeSource{
				NFS: &corev1.NFSVolumeSource{Server: server, Path: path},
			},
		}}
		placement.EnsureControlPlaneTolerations(&d.Spec.Template.Spec)
	}

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		deploy := &appsv1.Deployment{}
		err := r.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, deploy)
		if apierrors.IsNotFound(err) {
			deploy = &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns}}
			desiredPodSpec(deploy)
			if err := r.Create(ctx, deploy); err != nil {
				if apierrors.IsAlreadyExists(err) || apierrors.IsConflict(err) {
					lastErr = err
					continue
				}
				return fmt.Errorf("deployment: %w", err)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("deployment: %w", err)
		}

		// Skip update when already matching desired NFS target.
		curServer, curPath := "", ""
		for _, c := range deploy.Spec.Template.Spec.Containers {
			for _, e := range c.Env {
				if e.Name == "NFS_SERVER" {
					curServer = e.Value
				}
				if e.Name == "NFS_PATH" {
					curPath = e.Value
				}
			}
		}
		if curServer == server && curPath == path && deploy.Spec.Replicas != nil && *deploy.Spec.Replicas == replicas {
			return nil
		}

		desiredPodSpec(deploy)
		if err := r.Update(ctx, deploy); err != nil {
			if apierrors.IsConflict(err) {
				lastErr = err
				continue
			}
			return fmt.Errorf("deployment: %w", err)
		}
		return nil
	}
	if lastErr != nil {
		return fmt.Errorf("deployment: %w", lastErr)
	}
	return fmt.Errorf("deployment: retries exhausted")
}

func (r *NFSProvisionerReconciler) ensureStorageClass(ctx context.Context, name string, defaultClass bool) error {
	reclaim := corev1.PersistentVolumeReclaimDelete
	binding := storagev1.VolumeBindingImmediate
	existing := &storagev1.StorageClass{}
	err := r.Get(ctx, types.NamespacedName{Name: name}, existing)
	if apierrors.IsNotFound(err) {
		sc := &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: map[string]string{managedLabel: managedValue},
			},
			Provisioner:          provisionerName,
			ReclaimPolicy:        &reclaim,
			VolumeBindingMode:    &binding,
			AllowVolumeExpansion: boolPtr(true),
			Parameters:           map[string]string{"archiveOnDelete": "false"},
		}
		if defaultClass {
			sc.Annotations = map[string]string{"storageclass.kubernetes.io/is-default-class": "true"}
		}
		if err := r.Create(ctx, sc); err != nil {
			return fmt.Errorf("storageclass: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("storageclass: %w", err)
	}
	// StorageClass provisioner/reclaim are immutable — only refresh labels/default annotation.
	wantedDefault := defaultClass
	hasDefault := existing.Annotations != nil && existing.Annotations["storageclass.kubernetes.io/is-default-class"] == "true"
	need := false
	if existing.Labels == nil || existing.Labels[managedLabel] != managedValue {
		need = true
	}
	if wantedDefault != hasDefault {
		need = true
	}
	if !need {
		return nil
	}
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	existing.Labels[managedLabel] = managedValue
	if existing.Annotations == nil {
		existing.Annotations = map[string]string{}
	}
	if wantedDefault {
		existing.Annotations["storageclass.kubernetes.io/is-default-class"] = "true"
	} else {
		delete(existing.Annotations, "storageclass.kubernetes.io/is-default-class")
	}
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("storageclass: %w", err)
	}
	return nil
}

func boolPtr(v bool) *bool { return &v }

func (r *NFSProvisionerReconciler) deploymentReady(ctx context.Context, ns, name string) (bool, string) {
	d := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, d); err != nil {
		return false, "Waiting for deployment…"
	}
	if d.Status.ReadyReplicas >= 1 && d.Status.AvailableReplicas >= 1 {
		return true, "Deployment ready"
	}
	return false, fmt.Sprintf("Waiting for provisioner pod (ready %d/%d)", d.Status.ReadyReplicas, ptrInt32(d.Spec.Replicas))
}

func ptrInt32(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}

func (r *NFSProvisionerReconciler) cleanup(ctx context.Context, cr *dashboardv1alpha1.NFSProvisioner) error {
	ns := strings.TrimSpace(cr.Spec.Namespace)
	if ns == "" {
		ns = defaultNamespace
	}
	release := cr.Name
	if release == "" {
		release = "nfs-provisioner"
	}
	scName := strings.TrimSpace(cr.Spec.StorageClassName)

	_ = r.Delete(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns}})
	_ = r.Delete(ctx, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "leader-locking-" + release, Namespace: ns}})
	_ = r.Delete(ctx, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "leader-locking-" + release, Namespace: ns}})
	_ = r.Delete(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns}})
	_ = r.Delete(ctx, &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: release}})
	_ = r.Delete(ctx, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: release}})
	if scName != "" {
		_ = r.Delete(ctx, &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: scName}})
	}
	// Do not delete the workload namespace — user may reuse it; controller lives in kubeops-addon-controller.
	return nil
}
