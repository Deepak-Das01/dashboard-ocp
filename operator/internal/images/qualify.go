// Package images normalizes container image references for CRI-O on FCOS
// (unqualified short names fail without a TTY / short-name prompt).
package images

import "strings"

// Qualify expands known short names to fully-qualified refs that CRI-O can resolve
// without interactive short-name selection.
func Qualify(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ref
	}
	// Already registry-qualified (docker.io/…, quay.io/…, localhost/…, ghcr.io/…).
	if strings.Contains(ref, "/") {
		first := ref
		if i := strings.IndexByte(ref, '/'); i >= 0 {
			first = ref[:i]
		}
		if strings.Contains(first, ".") || first == "localhost" || first == "docker.io" {
			return ref
		}
		// org/name:tag → docker.io/org/name:tag
		return "docker.io/" + ref
	}
	// bare name:tag (e.g. nginx:1.27-alpine, kubeops-console:1.3.0)
	name := ref
	if i := strings.LastIndexByte(ref, ':'); i > 0 {
		name = ref[:i]
	}
	switch {
	case strings.HasPrefix(name, "kubeops-"):
		return "localhost/" + ref
	default:
		return "docker.io/library/" + ref
	}
}
