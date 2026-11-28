package ceph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
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

	nodesParam := strings.TrimSpace(r.URL.Query().Get("nodes"))
	var nodeNames []string
	notReady := map[string]string{}
	if nodesParam != "" {
		for _, n := range strings.Split(nodesParam, ",") {
			n = strings.TrimSpace(n)
			if n != "" {
				nodeNames = append(nodeNames, n)
			}
		}
	} else {
		var list corev1.NodeList
		if err := s.Client.List(r.Context(), &list); err != nil {
			writeJSON(w, http.StatusInternalServerError, probeResponse{Message: err.Error()})
			return
		}
		for i := range list.Items {
			n := &list.Items[i]
			nodeNames = append(nodeNames, n.Name)
			if !nodeIsReady(n) {
				notReady[n.Name] = "node NotReady — skipped disk probe"
			}
		}
	}
	if len(nodeNames) == 0 {
		writeJSON(w, http.StatusOK, probeResponse{Nodes: []NodeDisks{}, Message: "no nodes found"})
		return
	}

	// Parallel per-node probes (SNO = 1; large clusters probe many nodes at once).
	timeout := time.Duration(60+15*len(nodeNames)) * time.Second
	if timeout > 5*time.Minute {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	ns := strings.TrimSpace(r.URL.Query().Get("namespace"))
	if ns == "" {
		ns = defaultNamespace
	}
	_ = ensureWorkloadNamespace(ctx, s.Client, ns)

	results := make([]NodeDisks, len(nodeNames))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8) // cap concurrency for very large clusters
	for i, name := range nodeNames {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if msg, skip := notReady[name]; skip {
				results[i] = NodeDisks{Node: name, Disks: []DiskInfo{}, Error: msg}
				return
			}
			nd, err := s.probeNodeDisks(ctx, ns, name)
			if err != nil {
				results[i] = NodeDisks{Node: name, Disks: []DiskInfo{}, Error: err.Error()}
				return
			}
			results[i] = nd
		}(i, name)
	}
	wg.Wait()

	diskTotal := 0
	for _, nd := range results {
		diskTotal += len(nd.Disks)
	}
	msg := fmt.Sprintf("probed %d node(s), %d candidate disk(s) (excludes OS/boot, nbd, loop, zero-size)", len(results), diskTotal)
	writeJSON(w, http.StatusOK, probeResponse{Nodes: results, Message: msg})
}

func nodeIsReady(n *corev1.Node) bool {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
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
				Image:           "docker.io/library/busybox:1.36",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command: []string{
					"/bin/sh", "-c",
					// Use the node's lsblk via host root mount (no heavy Ceph image pull).
					// Do NOT bind-mount host /dev over container /dev — that breaks the
					// kubelet termination-log mount (read-only /dev) and the pod fails
					// before lsblk runs, so the form sees empty disks.
					`chroot /host lsblk -J -o NAME,SIZE,TYPE,MOUNTPOINT,ROTA,MODEL,SERIAL,PKNAME 2>/dev/null || chroot /host lsblk -J`,
				},
				SecurityContext: &corev1.SecurityContext{
					Privileged: boolPtr(true),
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "host", MountPath: "/host", ReadOnly: true},
				},
			}},
			Volumes: []corev1.Volume{
				{
					Name: "host",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{Path: "/", Type: &hostPathDir},
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
				msg := "probe logs: " + err.Error()
				if cur.Status.Phase == corev1.PodFailed {
					msg = probeFailReason(cur) + "; " + msg
				}
				return NodeDisks{Node: nodeName, Disks: []DiskInfo{}, Error: msg}, nil
			}
			disks := filterCandidateDisks(logs)
			if disks == nil {
				disks = []DiskInfo{}
			}
			if cur.Status.Phase == corev1.PodFailed && len(disks) == 0 {
				return NodeDisks{Node: nodeName, Disks: disks, Error: probeFailReason(cur)}, nil
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

func probeFailReason(p *corev1.Pod) string {
	for _, cs := range p.Status.ContainerStatuses {
		if cs.State.Terminated != nil && cs.State.Terminated.Message != "" {
			return cs.State.Terminated.Message
		}
		if cs.State.Waiting != nil && cs.State.Waiting.Message != "" {
			return cs.State.Waiting.Message
		}
	}
	if p.Status.Message != "" {
		return p.Status.Message
	}
	if p.Status.Reason != "" {
		return p.Status.Reason
	}
	return "probe pod failed"
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
	// Virtual / non-attachable devices. nbd* often appears as type=disk with 0B.
	for _, p := range []string{"loop", "nbd", "sr", "fd", "ram", "zram", "md", "dm-", "rbd"} {
		if strings.HasPrefix(lower, p) {
			return false
		}
	}
	// Skip empty / unbacked devices.
	size := strings.TrimSpace(strings.ToUpper(d.Size))
	if size == "" || size == "0" || size == "0B" {
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
