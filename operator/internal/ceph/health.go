package ceph

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const healthFixJobName = "kubeops-ceph-health-fix"

// clusterFormed is true once Rook reports a usable cluster (OK or WARN).
// HEALTH_ERR keeps us waiting.
func clusterFormed(ctx context.Context, c client.Client, ns string) (formed bool, health, msg string, err error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(cephClusterGVK)
	if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: cephClusterName}, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return false, "", "Waiting for CephCluster…", nil
		}
		return false, "", "", err
	}
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	health, _, _ = unstructured.NestedString(obj.Object, "status", "ceph", "health")
	state, _, _ := unstructured.NestedString(obj.Object, "status", "state")
	if phase == "" {
		phase = state
	}
	if strings.EqualFold(health, "HEALTH_ERR") {
		return false, health, "Ceph HEALTH_ERR — check OSDs/mons", nil
	}
	if strings.EqualFold(phase, "Ready") ||
		strings.EqualFold(health, "HEALTH_OK") ||
		strings.EqualFold(health, "HEALTH_WARN") {
		return true, health, "CephCluster formed", nil
	}
	if phase == "" {
		phase = "Progressing"
	}
	return false, health, fmt.Sprintf("Waiting for CephCluster (phase=%s health=%s)", phase, health), nil
}

func cephHealthOK(ctx context.Context, c client.Client, ns string) (ok bool, health string, err error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(cephClusterGVK)
	if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: cephClusterName}, obj); err != nil {
		return false, "", err
	}
	health, _, _ = unstructured.NestedString(obj.Object, "status", "ceph", "health")
	if !strings.EqualFold(health, "HEALTH_OK") {
		return false, health, nil
	}
	// Rook can briefly report HEALTH_OK before any OSDs exist — require real capacity.
	osdCount := int64(0)
	if n, found, _ := unstructured.NestedInt64(obj.Object, "status", "ceph", "capacity", "bytesTotal"); found && n > 0 {
		osdCount = 1
	}
	// Prefer explicit OSD counts when Rook publishes them.
	if storage, found, _ := unstructured.NestedMap(obj.Object, "status", "storage"); found {
		if osd, ok := storage["osd"].(map[string]interface{}); ok {
			if s, ok := osd["storeType"].(map[string]interface{}); ok && len(s) > 0 {
				osdCount = int64(len(s))
			}
		}
	}
	var podList corev1.PodList
	if err := c.List(ctx, &podList, client.InNamespace(ns), client.MatchingLabels{"app": "rook-ceph-osd"}); err == nil {
		readyOSDs := 0
		for i := range podList.Items {
			p := &podList.Items[i]
			if p.Status.Phase != corev1.PodRunning {
				continue
			}
			for _, cs := range p.Status.ContainerStatuses {
				if cs.Ready {
					readyOSDs++
					break
				}
			}
		}
		if readyOSDs == 0 {
			return false, health + " (waiting for OSDs)", nil
		}
		osdCount = int64(readyOSDs)
	}
	if osdCount <= 0 {
		return false, health + " (no OSDs yet)", nil
	}
	return true, health, nil
}

// ensureHealthFixJob runs a one-shot Job that aligns crush rules / pool size /
// mon warn so single-node multi-disk labs reach HEALTH_OK without manual ceph CLI.
func ensureHealthFixJob(ctx context.Context, c client.Client, opt clusterOpts) (done bool, err error) {
	var job batchv1.Job
	err = c.Get(ctx, client.ObjectKey{Namespace: opt.Namespace, Name: healthFixJobName}, &job)
	if err == nil {
		if job.Status.Succeeded > 0 {
			if ok, _, _ := cephHealthOK(ctx, c, opt.Namespace); ok {
				return true, nil
			}
			// Status lag or regression — drop job so the next reconcile recreates it.
			prop := metav1.DeletePropagationBackground
			_ = c.Delete(ctx, &job, &client.DeleteOptions{PropagationPolicy: &prop})
			return false, nil
		}
		if job.Status.Failed > 0 && isJobComplete(&job) {
			prop := metav1.DeletePropagationBackground
			_ = c.Delete(ctx, &job, &client.DeleteOptions{PropagationPolicy: &prop})
			return false, nil
		}
		return false, nil
	}
	if !apierrors.IsNotFound(err) {
		return false, err
	}

	script := healthFixScript(opt)
	job = batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      healthFixJobName,
			Namespace: opt.Namespace,
			Labels: map[string]string{
				managedLabel:             managedValue,
				"app":                    healthFixJobName,
				"dashboard.ocp/healthfix": "true",
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To(int32(5)),
			TTLSecondsAfterFinished: ptr.To(int32(600)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": healthFixJobName},
				},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyOnFailure,
					ServiceAccountName: "rook-ceph-default",
					DNSPolicy:          corev1.DNSClusterFirstWithHostNet,
					Containers: []corev1.Container{{
						Name:            "health-fix",
						Image:           opt.CephImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"/bin/bash", "-c", script},
						Env: []corev1.EnvVar{{
							Name: "ROOK_CEPH_USERNAME",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon"},
									Key:                  "ceph-username",
								},
							},
						}},
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot: ptr.To(true),
							RunAsUser:    ptr.To(int64(2016)),
							RunAsGroup:   ptr.To(int64(2016)),
							Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "ceph-config", MountPath: "/etc/ceph"},
							{Name: "mon-endpoint-volume", MountPath: "/etc/rook"},
							{Name: "ceph-admin-secret", MountPath: "/var/lib/rook-ceph-mon", ReadOnly: true},
						},
					}},
					Volumes: []corev1.Volume{
						{Name: "ceph-admin-secret", VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "rook-ceph-mon",
								Items:      []corev1.KeyToPath{{Key: "ceph-secret", Path: "secret.keyring"}},
							},
						}},
						{Name: "mon-endpoint-volume", VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon-endpoints"},
								Items:                []corev1.KeyToPath{{Key: "data", Path: "mon-endpoints"}},
							},
						}},
						{Name: "ceph-config", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					},
				},
			},
		},
	}
	if err := c.Create(ctx, &job); err != nil {
		return false, err
	}
	return false, nil
}

func healthFixScript(opt clusterOpts) string {
	size := opt.PoolSize
	if size <= 0 {
		size = 1
	}
	min := minSize(size)
	fd := opt.FailureDomain
	if fd == "" {
		fd = "osd"
	}
	// Keep script deterministic and free of Go fmt verbs colliding with bash.
	var b strings.Builder
	b.WriteString("set -euo pipefail\n")
	b.WriteString("CEPH_CONFIG=/etc/ceph/ceph.conf\n")
	b.WriteString("MON_CONFIG=/etc/rook/mon-endpoints\n")
	b.WriteString("KEYRING_FILE=/etc/ceph/keyring\n")
	b.WriteString("POOL_SIZE=" + strconv.Itoa(size) + "\n")
	b.WriteString("MIN_SIZE=" + strconv.Itoa(min) + "\n")
	b.WriteString("FAIL_DOMAIN=" + fd + "\n")
	b.WriteString(`
ceph_secret=$(cat /var/lib/rook-ceph-mon/secret.keyring)
cat > "${KEYRING_FILE}" <<EOF
[${ROOK_CEPH_USERNAME}]
key = ${ceph_secret}
EOF
mon_endpoints=$(sed 's/[a-z0-9_-]\+=//g' "${MON_CONFIG}")
cat > "${CEPH_CONFIG}" <<EOF
[global]
mon_host = ${mon_endpoints}
[client.admin]
keyring = ${KEYRING_FILE}
EOF

echo "waiting for ceph quorum…"
for i in $(seq 1 90); do
  if ceph status >/dev/null 2>&1; then
    break
  fi
  sleep 5
done
ceph status >/dev/null

ceph config set mon mon_data_avail_warn 10 || true

if [ "${FAIL_DOMAIN}" = "osd" ]; then
  ceph osd crush rule create-replicated replicated_osd default osd || true
  RULE=replicated_osd
else
  RULE=replicated_rule
fi

for p in $(ceph osd pool ls); do
  echo "align pool ${p} size=${POOL_SIZE} min_size=${MIN_SIZE} rule=${RULE}"
  ceph osd pool set "${p}" size "${POOL_SIZE}" || true
  ceph osd pool set "${p}" min_size "${MIN_SIZE}" || true
  ceph osd pool set "${p}" crush_rule "${RULE}" || true
done

echo "waiting for HEALTH_OK…"
for i in $(seq 1 120); do
  h=$(ceph health 2>/dev/null | head -1 || true)
  echo "health=${h}"
  if [ "${h}" = "HEALTH_OK" ]; then
    exit 0
  fi
  sleep 5
done
ceph health detail || true
exit 1
`)
	return b.String()
}
