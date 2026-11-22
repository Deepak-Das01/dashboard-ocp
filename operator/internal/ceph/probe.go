package ceph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DiskInfo is one candidate device on a node.
type DiskInfo struct {
	Name       string `json:"name"`
	Size       string `json:"size,omitempty"`
	Type       string `json:"type,omitempty"`
	Model      string `json:"model,omitempty"`
	Serial     string `json:"serial,omitempty"`
	Rotational bool   `json:"rotational,omitempty"`
}

// NodeDisks groups candidate disks by node.
type NodeDisks struct {
	Node  string     `json:"node"`
	Disks []DiskInfo `json:"disks"`
	Error string     `json:"error,omitempty"`
}

type probeResponse struct {
	Nodes   []NodeDisks `json:"nodes"`
	Message string      `json:"message,omitempty"`
}

// DiskProbeServer serves GET /disks for the Addons form.
type DiskProbeServer struct {
	Client    client.Client
	Clientset kubernetes.Interface
}

// RegisterDiskProbeHandlers exposes GET /disks?nodes=a,b for the Addons form.
func RegisterDiskProbeHandlers(mux *http.ServeMux, c client.Client, cfg *rest.Config) {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		mux.HandleFunc("/disks", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusInternalServerError, probeResponse{Message: "clientset: " + err.Error()})
		})
		return
	}
	s := &DiskProbeServer{Client: c, Clientset: cs}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/disks", s.handleDisks)
	mux.HandleFunc("/session", s.handleSession)
}

func (s *DiskProbeServer) handleDisks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	nodesParam := strings.TrimSpace(r.URL.Query().Get("nodes"))
	var nodeNames []string
	if nodesParam != "" {
		for _, n := range strings.Split(nodesParam, ",") {
			n = strings.TrimSpace(n)
			if n != "" {
				nodeNames = append(nodeNames, n)
			}
		}
	} else {
		var list corev1.NodeList
		if err := s.Client.List(ctx, &list); err != nil {
			writeJSON(w, http.StatusInternalServerError, probeResponse{Message: err.Error()})
			return
		}
		for i := range list.Items {
			nodeNames = append(nodeNames, list.Items[i].Name)
		}
	}

	ns := strings.TrimSpace(r.URL.Query().Get("namespace"))
	if ns == "" {
		ns = defaultNamespace
	}
	_ = ensureWorkloadNamespace(ctx, s.Client, ns)

	out := probeResponse{Nodes: []NodeDisks{}}
	for _, name := range nodeNames {
		nd, err := s.probeNodeDisks(ctx, ns, name)
		if err != nil {
			out.Nodes = append(out.Nodes, NodeDisks{Node: name, Disks: []DiskInfo{}, Error: err.Error()})
			continue
		}
		out.Nodes = append(out.Nodes, nd)
	}
	writeJSON(w, http.StatusOK, out)
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *DiskProbeServer) probeNodeDisks(ctx context.Context, ns, nodeName string) (NodeDisks, error) {
	podName := "kubeops-ceph-disk-probe-" + sanitizeName(nodeName)
	_ = deleteProbePod(ctx, s.Client, ns, podName)

	hostPathDir := corev1.HostPathDirectory
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: ns,
			Labels: map[string]string{
				managedLabel: managedValue,
				"app":        "kubeops-ceph-disk-probe",
			},
		},
		Spec: corev1.PodSpec{
			NodeName:      nodeName,
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:            "probe",
				Image:           "busybox:1.36",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command: []string{
					"/bin/sh", "-c",
					// Use the node's lsblk via host root mount (no heavy Ceph image pull).
					`chroot /host lsblk -J -o NAME,SIZE,TYPE,MOUNTPOINT,ROTA,MODEL,SERIAL,PKNAME 2>/dev/null || chroot /host lsblk -J`,
				},
				SecurityContext: &corev1.SecurityContext{
					Privileged: boolPtr(true),
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "host", MountPath: "/host", ReadOnly: true},
					{Name: "dev", MountPath: "/dev", ReadOnly: true},
				},
			}},
			Volumes: []corev1.Volume{
				{
					Name: "host",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{Path: "/", Type: &hostPathDir},
					},
				},
				{
					Name: "dev",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{Path: "/dev"},
					},
				},
			},
			Tolerations: []corev1.Toleration{{
				Operator: corev1.TolerationOpExists,
			}},
		},
	}
	if err := s.Client.Create(ctx, pod); err != nil {
		return NodeDisks{}, err
	}
	defer func() { _ = deleteProbePod(context.Background(), s.Client, ns, podName) }()

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		cur := &corev1.Pod{}
		if err := s.Client.Get(ctx, types.NamespacedName{Namespace: ns, Name: podName}, cur); err != nil {
			return NodeDisks{}, err
		}
		switch cur.Status.Phase {
		case corev1.PodSucceeded, corev1.PodFailed:
			logs, err := s.readPodLogs(ctx, ns, podName)
			if err != nil {
				return NodeDisks{Node: nodeName, Disks: []DiskInfo{}, Error: "probe logs: " + err.Error()}, nil
			}
			disks := filterCandidateDisks(logs)
			if disks == nil {
				disks = []DiskInfo{}
			}
			return NodeDisks{Node: nodeName, Disks: disks}, nil
		}
		select {
		case <-ctx.Done():
			return NodeDisks{}, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return NodeDisks{}, fmt.Errorf("probe timed out on node %s", nodeName)
}

func (s *DiskProbeServer) readPodLogs(ctx context.Context, ns, name string) (string, error) {
	req := s.Clientset.CoreV1().Pods(ns).GetLogs(name, &corev1.PodLogOptions{Container: "probe"})
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer stream.Close()
	b, err := io.ReadAll(stream)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func deleteProbePod(ctx context.Context, c client.Client, ns, name string) error {
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
	err := c.Delete(ctx, p)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func boolPtr(b bool) *bool { return &b }

func sanitizeName(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := b.String()
	if len(out) > 40 {
		out = out[:40]
	}
	return strings.Trim(out, "-")
}

type lsblkJSON struct {
	BlockDevices []lsblkDev `json:"blockdevices"`
}

type lsblkDev struct {
	Name       string      `json:"name"`
	Size       string      `json:"size"`
	Type       string      `json:"type"`
	Mountpoint *string     `json:"mountpoint"`
	Rota       interface{} `json:"rota"`
	Model      *string     `json:"model"`
	Serial     *string     `json:"serial"`
	PkName     *string     `json:"pkname"`
	Children   []lsblkDev  `json:"children"`
}

// FilterCandidateDevices exports filtering for reuse/tests.
func FilterCandidateDevices(lsblkOutput string) []DiskInfo {
	return filterCandidateDisks(lsblkOutput)
}

func filterCandidateDisks(raw string) []DiskInfo {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	idx := strings.Index(raw, "{")
	if idx >= 0 {
		raw = raw[idx:]
	}
	var parsed lsblkJSON
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}
	var out []DiskInfo
	var walk func(devs []lsblkDev)
	walk = func(devs []lsblkDev) {
		for _, d := range devs {
			if strings.EqualFold(d.Type, "disk") && isCandidateDisk(d) {
				rot := false
				switch v := d.Rota.(type) {
				case bool:
					rot = v
				case float64:
					rot = v != 0
				case string:
					rot = v == "1" || strings.EqualFold(v, "true")
				}
				model, serial := "", ""
				if d.Model != nil {
					model = strings.TrimSpace(*d.Model)
				}
				if d.Serial != nil {
					serial = strings.TrimSpace(*d.Serial)
				}
				out = append(out, DiskInfo{
					Name:       d.Name,
					Size:       d.Size,
					Type:       d.Type,
					Model:      model,
					Serial:     serial,
					Rotational: rot,
				})
			}
			if len(d.Children) > 0 {
				walk(d.Children)
			}
		}
	}
	walk(parsed.BlockDevices)
	return out
}

func isCandidateDisk(d lsblkDev) bool {
	name := strings.TrimSpace(d.Name)
	if name == "" {
		return false
	}
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "loop") || strings.HasPrefix(lower, "sr") || strings.HasPrefix(lower, "fd") {
		return false
	}
	if d.Mountpoint != nil && strings.TrimSpace(*d.Mountpoint) != "" {
		mp := *d.Mountpoint
		if mp == "/" || strings.HasPrefix(mp, "/boot") || strings.HasPrefix(mp, "/var/lib/kubelet") {
			return false
		}
	}
	for _, ch := range d.Children {
		if ch.Mountpoint != nil {
			mp := *ch.Mountpoint
			if mp == "/" || strings.HasPrefix(mp, "/boot") {
				return false
			}
		}
		for _, gch := range ch.Children {
			if gch.Mountpoint != nil {
				mp := *gch.Mountpoint
				if mp == "/" || strings.HasPrefix(mp, "/boot") {
					return false
				}
			}
		}
	}
	return true
}
