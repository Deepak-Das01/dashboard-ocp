// Package endpoint resolves public node / VIP addresses for console + OIDC URLs.
package endpoint

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NodeInternalIPs returns unique InternalIP addresses for all Ready nodes.
func NodeInternalIPs(ctx context.Context, c client.Client) []string {
	nodes := &corev1.NodeList{}
	if err := c.List(ctx, nodes); err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, n := range nodes.Items {
		ready := false
		for _, cond := range n.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}
		if !ready {
			continue
		}
		for _, a := range n.Status.Addresses {
			if a.Type != corev1.NodeInternalIP || a.Address == "" {
				continue
			}
			if _, ok := seen[a.Address]; ok {
				continue
			}
			seen[a.Address] = struct{}{}
			out = append(out, a.Address)
		}
	}
	return out
}

// ControlPlaneEndpointHost returns the host portion of kubeadm controlPlaneEndpoint
// (e.g. VIP 192.168.200.5 from "192.168.200.5:6443"), if present.
func ControlPlaneEndpointHost(ctx context.Context, c client.Client) string {
	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: "kube-system", Name: "kubeadm-config"}, cm); err != nil {
		return ""
	}
	raw := cm.Data["ClusterConfiguration"]
	if raw == "" {
		return ""
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "controlPlaneEndpoint:") {
			continue
		}
		ep := strings.TrimSpace(strings.TrimPrefix(line, "controlPlaneEndpoint:"))
		ep = strings.Trim(ep, `"'`)
		if i := strings.LastIndex(ep, ":"); i > 0 {
			// strip :port (IPv4 host:port)
			host := ep[:i]
			if strings.Count(host, ":") == 0 {
				return host
			}
		}
		return ep
	}
	return ""
}

// PreferredPublicIP picks VIP (controlPlaneEndpoint) when available, else first node IP.
func PreferredPublicIP(ctx context.Context, c client.Client) string {
	if vip := ControlPlaneEndpointHost(ctx, c); vip != "" {
		return vip
	}
	ips := NodeInternalIPs(ctx, c)
	if len(ips) == 0 {
		return ""
	}
	return ips[0]
}

// AllPublicIPs is VIP (if any) plus all Ready node InternalIPs (deduped, VIP first).
func AllPublicIPs(ctx context.Context, c client.Client) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(ip string) {
		if ip == "" {
			return
		}
		if _, ok := seen[ip]; ok {
			return
		}
		seen[ip] = struct{}{}
		out = append(out, ip)
	}
	add(ControlPlaneEndpointHost(ctx, c))
	for _, ip := range NodeInternalIPs(ctx, c) {
		add(ip)
	}
	return out
}
