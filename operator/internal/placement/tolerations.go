// Package placement holds shared pod scheduling helpers for kubeops workloads.
package placement

import (
	corev1 "k8s.io/api/core/v1"
)

// ControlPlaneTolerations lets platform pods schedule on control-plane nodes
// (CP-only / SNO labs and tainted masters).
func ControlPlaneTolerations() []corev1.Toleration {
	return []corev1.Toleration{
		{
			Key:      "node-role.kubernetes.io/control-plane",
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		},
		{
			Key:      "node-role.kubernetes.io/master",
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		},
	}
}

// EnsureControlPlaneTolerations merges ControlPlaneTolerations into spec without duplicates.
func EnsureControlPlaneTolerations(spec *corev1.PodSpec) {
	if spec == nil {
		return
	}
	for _, want := range ControlPlaneTolerations() {
		if hasToleration(spec.Tolerations, want) {
			continue
		}
		spec.Tolerations = append(spec.Tolerations, want)
	}
}

func hasToleration(existing []corev1.Toleration, want corev1.Toleration) bool {
	for _, t := range existing {
		if t.Key == want.Key && t.Operator == want.Operator && t.Effect == want.Effect {
			return true
		}
	}
	return false
}
