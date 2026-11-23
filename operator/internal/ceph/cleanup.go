package ceph

import (
	"context"
	"fmt"
	"strings"
	"time"

	dashboardv1alpha1 "github.com/Deepak-Das01/dashboard-ocp/operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// cleanup tears down cluster workload (keeps Rook operator/CRDs for faster re-Deploy).
// Requires annotation kubeops.io/wipe-disks=confirmed so host path + disks are sanitized
// and a subsequent install can reuse the same devices without manual wipe.
func (r *RookCephReconciler) cleanup(ctx context.Context, cr *dashboardv1alpha1.RookCeph, opt clusterOpts) (done bool, err error) {
	logger := log.FromContext(ctx)

	wipeConfirmed := cr.Annotations != nil && cr.Annotations[wipeAnnotation] == "confirmed"
	if !wipeConfirmed {
		return false, fmt.Errorf("refusing uninstall without annotation %s=confirmed (Addons prompts for \"wipe disks\") — required so disks/host-path are sanitized for reuse", wipeAnnotation)
	}

	// Unique per uninstall so ensureDeviceWipeJob never skips on a stale Succeeded Job.
	if cr.UID != "" {
		opt.wipeGeneration = string(cr.UID)
	} else if cr.DeletionTimestamp != nil {
		opt.wipeGeneration = cr.DeletionTimestamp.UTC().Format(time.RFC3339Nano)
	} else {
		opt.wipeGeneration = fmt.Sprintf("%d", time.Now().UnixNano())
	}

	// Stop health-fix Job so it does not race teardown (Background cascades pods).
	_ = deleteJob(ctx, r.Client, opt.Namespace, healthFixJobName)
	_ = gcOrphanJobPods(ctx, r.Client, opt.Namespace)

	// Patch cleanupPolicy BEFORE deleting the CephCluster.
	if err := patchCleanupPolicy(ctx, r.Client, opt); err != nil && !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("cleanupPolicy: %w", err)
	}

	_ = deleteTyped(ctx, r.Client, &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: fsSCName}})
	_ = deleteTyped(ctx, r.Client, &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: blockSCName}})

	fs := &unstructured.Unstructured{}
	fs.SetGroupVersionKind(filesystemGVK)
	fs.SetNamespace(opt.Namespace)
	fs.SetName(fsName)
	_ = deleteObj(ctx, r.Client, fs)

	pool := &unstructured.Unstructured{}
	pool.SetGroupVersionKind(blockPoolGVK)
	pool.SetNamespace(opt.Namespace)
	pool.SetName(blockPoolName)
	_ = deleteObj(ctx, r.Client, pool)

	_ = deleteTyped(ctx, r.Client, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: opt.Namespace, Name: "rook-ceph-tools"},
	})
	_ = deleteTyped(ctx, r.Client, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: opt.Namespace, Name: "rook-ceph-mgr-dashboard-nodeport"},
	})

	if pending, err := resourcePending(ctx, r.Client, filesystemGVK, opt.Namespace, fsName); err != nil {
		return false, err
	} else if pending {
		// Unhealthy clusters (0 OSDs) never finish FS finalizers — strip them.
		if err := forceRemoveFinalizers(ctx, r.Client, filesystemGVK, opt.Namespace, fsName); err != nil {
			logger.Info("waiting for CephFilesystem deletion", "err", err)
			return false, nil
		}
		return false, nil
	}
	if pending, err := resourcePending(ctx, r.Client, blockPoolGVK, opt.Namespace, blockPoolName); err != nil {
		return false, err
	} else if pending {
		if err := forceRemoveFinalizers(ctx, r.Client, blockPoolGVK, opt.Namespace, blockPoolName); err != nil {
			logger.Info("waiting for CephBlockPool deletion", "err", err)
			return false, nil
		}
		return false, nil
	}

	cc := &unstructured.Unstructured{}
	cc.SetGroupVersionKind(cephClusterGVK)
	cc.SetNamespace(opt.Namespace)
	cc.SetName(cephClusterName)
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: opt.Namespace, Name: cephClusterName}, cc); err == nil {
		_ = patchCleanupPolicy(ctx, r.Client, opt)
		time.Sleep(1 * time.Second)
		if err := r.Client.Delete(ctx, cc); err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
		return false, nil // wait for CephCluster gone + cleanup Jobs
	} else if !apierrors.IsNotFound(err) {
		return false, err
	}

	still, err := cleanupJobsPending(ctx, r.Client, opt.Namespace)
	if err != nil {
		return false, err
	}
	if still {
		return false, nil
	}

	// Rook's host-cleanup Job only sanitizes devices it still associates with the
	// deleted cluster FSID. Leftover blueStore from a prior FSID blocks reinstall
	// ("belonging to a different ceph cluster"). Always wipe CR-listed devices.
	wipeDone, wipeErr := ensureDeviceWipeJob(ctx, r.Client, opt)
	if wipeErr != nil {
		return false, wipeErr
	}
	if !wipeDone {
		logger.Info("waiting for device wipe Job")
		return false, nil
	}

	// Rook cleanup Jobs have no TTL — delete finished ones so pods do not linger.
	_ = deleteFinishedCleanupJobs(ctx, r.Client, opt.Namespace)
	_ = gcOrphanJobPods(ctx, r.Client, opt.Namespace)

	_ = untaintNodes(ctx, r.Client, opt)
	_ = unlabelNodes(ctx, r.Client, opt)
	logger.Info("ceph cleanup complete — disks/host-path sanitized")
	return true, nil
}

func resourcePending(ctx context.Context, c client.Client, gvk schema.GroupVersionKind, ns, name string) (bool, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, obj)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func forceRemoveFinalizers(ctx context.Context, c client.Client, gvk schema.GroupVersionKind, ns, name string) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if len(obj.GetFinalizers()) == 0 {
		return nil
	}
	base := obj.DeepCopy()
	obj.SetFinalizers(nil)
	return c.Patch(ctx, obj, client.MergeFrom(base))
}

func patchCleanupPolicy(ctx context.Context, c client.Client, opt clusterOpts) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(cephClusterGVK)
	if err := c.Get(ctx, client.ObjectKey{Namespace: opt.Namespace, Name: cephClusterName}, obj); err != nil {
		return err
	}
	base := obj.DeepCopy()
	// Rook v1.16 rejects wipeDevicesFromOtherClusters; device wipe Job handles cross-FSID reuse.
	policy := map[string]interface{}{
		"confirmation": "yes-really-destroy-data",
		"sanitizeDisks": map[string]interface{}{
			"method":     "quick",
			"dataSource": "zero",
			"iteration":  int64(1),
		},
	}
	_ = unstructured.SetNestedMap(obj.Object, policy, "spec", "cleanupPolicy")
	return c.Patch(ctx, obj, client.MergeFrom(base))
}

func cleanupJobsPending(ctx context.Context, c client.Client, ns string) (bool, error) {
	var jobs batchv1.JobList
	if err := c.List(ctx, &jobs, client.InNamespace(ns)); err != nil {
		return false, err
	}
	pending := false
	for i := range jobs.Items {
		j := &jobs.Items[i]
		name := j.Name
		if !strings.Contains(name, "cleanup") && !strings.Contains(name, "sanitize") {
			continue
		}
		if j.Status.Succeeded > 0 {
			continue
		}
		if j.Status.Failed > 0 && isJobComplete(j) {
			continue
		}
		pending = true
	}
	return pending, nil
}

func isJobComplete(j *batchv1.Job) bool {
	for _, c := range j.Status.Conditions {
		if c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed {
			return true
		}
	}
	return false
}

func deleteObj(ctx context.Context, c client.Client, obj *unstructured.Unstructured) error {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())
	key := client.ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetName()}
	if err := c.Get(ctx, key, existing); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return c.Delete(ctx, existing)
}

func deleteTyped(ctx context.Context, c client.Client, obj client.Object) error {
	err := c.Delete(ctx, obj)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// deleteJob removes a Job and its Pods. Default Job deletion orphans child pods.
func deleteJob(ctx context.Context, c client.Client, ns, name string) error {
	prop := metav1.DeletePropagationBackground
	err := c.Delete(ctx, &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
	}, &client.DeleteOptions{PropagationPolicy: &prop})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// deleteFinishedCleanupJobs removes Rook host-cleanup Jobs (no TTL) after wipe.
func deleteFinishedCleanupJobs(ctx context.Context, c client.Client, ns string) error {
	var jobs batchv1.JobList
	if err := c.List(ctx, &jobs, client.InNamespace(ns)); err != nil {
		return err
	}
	prop := metav1.DeletePropagationBackground
	for i := range jobs.Items {
		j := &jobs.Items[i]
		name := j.Name
		if !strings.Contains(name, "cleanup") && !strings.Contains(name, "sanitize") {
			continue
		}
		if j.Status.Succeeded == 0 && !(j.Status.Failed > 0 && isJobComplete(j)) {
			continue
		}
		_ = c.Delete(ctx, j, &client.DeleteOptions{PropagationPolicy: &prop})
	}
	return nil
}

// gcOrphanJobPods deletes finished/orphan pods from health-fix, wipe, and cleanup Jobs.
// These linger when a Job was deleted without propagationPolicy=Background.
func gcOrphanJobPods(ctx context.Context, c client.Client, ns string) error {
	var pods corev1.PodList
	if err := c.List(ctx, &pods, client.InNamespace(ns)); err != nil {
		return err
	}
	for i := range pods.Items {
		p := &pods.Items[i]
		if !isManagedJobPod(p) {
			continue
		}
		orphan := len(p.OwnerReferences) == 0
		finished := p.Status.Phase == corev1.PodSucceeded ||
			p.Status.Phase == corev1.PodFailed ||
			p.Status.Phase == corev1.PodUnknown
		if !orphan && !finished {
			continue
		}
		// Keep pods still owned by an active Job.
		if !orphan {
			ownedByLiveJob := false
			for _, o := range p.OwnerReferences {
				if o.Kind == "Job" && o.Controller != nil && *o.Controller {
					ownedByLiveJob = true
					break
				}
			}
			if ownedByLiveJob && !finished {
				continue
			}
			// Finished + still owned: Job lacks TTL (Rook cleanup) — leave until Job GC.
			if ownedByLiveJob {
				continue
			}
		}
		_ = c.Delete(ctx, p)
	}
	return nil
}

func isManagedJobPod(p *corev1.Pod) bool {
	n := p.Name
	if strings.HasPrefix(n, healthFixJobName) ||
		strings.HasPrefix(n, deviceWipeJobName) ||
		strings.Contains(n, "cluster-cleanup-job") ||
		strings.Contains(n, "sanitize") {
		return true
	}
	if p.Labels != nil {
		app := p.Labels["app"]
		if app == healthFixJobName || app == deviceWipeJobName {
			return true
		}
	}
	return false
}

const deviceWipeJobName = "kubeops-ceph-device-wipe"
const deviceWipeFreshAnno = "kubeops.io/wipe-generation"

// ensureDeviceWipeJob destroys blueStore/LVM signatures on CR-listed disks and
// clears dataDirHostPath leftovers so a reinstall never inherits a prior FSID.
func ensureDeviceWipeJob(ctx context.Context, c client.Client, opt clusterOpts) (done bool, err error) {
	devs := []string{}
	for _, n := range opt.Nodes {
		for _, d := range n.Devices {
			d = strings.TrimPrefix(strings.TrimSpace(d), "/dev/")
			if d == "" || d == "REPLACE_ME" {
				continue
			}
			devs = append(devs, d)
		}
	}
	if len(devs) == 0 {
		return true, nil
	}

	// Always wipe once per uninstall. A Succeeded Job left from a prior uninstall
	// (TTL not expired) must not skip wiping the disks we just tore down.
	wipeGen := ""
	if opt.wipeGeneration != "" {
		wipeGen = opt.wipeGeneration
	}

	var job batchv1.Job
	err = c.Get(ctx, client.ObjectKey{Namespace: opt.Namespace, Name: deviceWipeJobName}, &job)
	if err == nil {
		sameGen := wipeGen != "" && job.Annotations != nil && job.Annotations[deviceWipeFreshAnno] == wipeGen
		if job.Status.Succeeded > 0 && sameGen {
			return true, nil
		}
		// Stale Succeeded Job or failed Job — delete and recreate for this uninstall.
		if job.Status.Succeeded > 0 || (job.Status.Failed > 0 && isJobComplete(&job)) || !sameGen {
			prop := metav1.DeletePropagationBackground
			_ = c.Delete(ctx, &job, &client.DeleteOptions{PropagationPolicy: &prop})
			if job.Status.Failed > 0 && isJobComplete(&job) && sameGen {
				return false, fmt.Errorf("device wipe Job failed")
			}
			return false, nil
		}
		return false, nil // still running
	}
	if !apierrors.IsNotFound(err) {
		return false, err
	}

	hostPath := strings.TrimSpace(opt.DataDirHostPath)
	if hostPath == "" {
		hostPath = defaultHostPath
	}
	ns := opt.Namespace

	var script strings.Builder
	script.WriteString("set -euo pipefail\n")
	script.WriteString("echo '=== host-path cleanup ==='\n")
	script.WriteString(fmt.Sprintf("rm -rf %q/%q || true\n", hostPath, ns))
	script.WriteString(fmt.Sprintf("rm -rf %q/mon-* || true\n", hostPath))
	script.WriteString(fmt.Sprintf("rm -rf %q/log || true\n", hostPath))
	for _, d := range devs {
		// Raw OSDs (ceph-volume raw prepare) are NOT fully cleared by lvm zap alone.
		// ceph-bluestore-tool zap-device is required; otherwise reinstall skips disks
		// as "belonging to a different ceph cluster".
		script.WriteString(fmt.Sprintf("echo '=== zapping /dev/%s ==='\n", d))
		script.WriteString(fmt.Sprintf("ceph-volume lvm zap --destroy /dev/%s || true\n", d))
		script.WriteString(fmt.Sprintf("ceph-bluestore-tool zap-device --dev /dev/%s --yes-i-really-really-mean-it || true\n", d))
		script.WriteString(fmt.Sprintf("dd if=/dev/zero of=/dev/%s bs=1M count=200 conv=fsync status=none || true\n", d))
		script.WriteString(fmt.Sprintf("sgdisk --zap-all /dev/%s || true\n", d))
		script.WriteString(fmt.Sprintf("wipefs -af /dev/%s || true\n", d))
	}
	script.WriteString("echo '=== verify raw list empty ==='\n")
	script.WriteString("out=$(ceph-volume raw list --format json 2>/dev/null || echo {})\n")
	script.WriteString("echo \"$out\"\n")
	script.WriteString("if [ \"$out\" != \"{}\" ] && [ -n \"$out\" ] && [ \"$out\" != \"null\" ]; then\n")
	script.WriteString("  echo 'ERROR: devices still have OSD signatures' >&2\n")
	script.WriteString("  exit 1\n")
	script.WriteString("fi\n")
	for _, d := range devs {
		// show-label must fail on a clean disk
		script.WriteString(fmt.Sprintf("if ceph-bluestore-tool show-label --dev /dev/%s >/tmp/blabel-%s 2>/dev/null; then\n", d, d))
		script.WriteString(fmt.Sprintf("  echo \"ERROR: /dev/%s still has blueStore label:\" >&2\n", d))
		script.WriteString(fmt.Sprintf("  cat /tmp/blabel-%s >&2 || true\n", d))
		script.WriteString("  exit 1\n")
		script.WriteString("fi\n")
	}
	script.WriteString("echo device wipe complete\n")

	nodeName := strings.TrimSpace(opt.Nodes[0].Name)
	img := opt.CephImage
	if img == "" {
		img = defaultCephImage
	}
	hostPathType := corev1.HostPathDirectoryOrCreate
	job = batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deviceWipeJobName,
			Namespace: opt.Namespace,
			Annotations: map[string]string{
				deviceWipeFreshAnno: wipeGen,
			},
			Labels: map[string]string{
				managedLabel: managedValue,
				"app":        deviceWipeJobName,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptrInt32(2),
			TTLSecondsAfterFinished: ptrInt32(300),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": deviceWipeJobName}},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					NodeName:      nodeName,
					HostNetwork:   true,
					Containers: []corev1.Container{{
						Name:            "wipe",
						Image:           img,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"/bin/bash", "-c", script.String()},
						SecurityContext: &corev1.SecurityContext{
							Privileged: ptrBool(true),
							RunAsUser:  ptrInt64(0),
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "dev", MountPath: "/dev"},
							{Name: "udev", MountPath: "/run/udev"},
							{Name: "rook", MountPath: hostPath},
						},
					}},
					Volumes: []corev1.Volume{
						{Name: "dev", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/dev"}}},
						{Name: "udev", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/run/udev"}}},
						{Name: "rook", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: hostPath, Type: &hostPathType}}},
					},
					Tolerations: []corev1.Toleration{{
						Operator: corev1.TolerationOpExists,
					}},
				},
			},
		},
	}
	if err := c.Create(ctx, &job); err != nil {
		return false, err
	}
	return false, nil
}

func ptrInt32(v int32) *int32 { return &v }
func ptrInt64(v int64) *int64 { return &v }
func ptrBool(v bool) *bool    { return &v }
