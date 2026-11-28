package images

import "testing"

func TestQualify(t *testing.T) {
	cases := map[string]string{
		"nginx:1.27-alpine":                     "docker.io/library/nginx:1.27-alpine",
		"kubeops-console:1.3.0":                 "localhost/kubeops-console:1.3.0",
		"kubernetesui/dashboard:v2.7.0":         "docker.io/kubernetesui/dashboard:v2.7.0",
		"docker.io/library/nginx:1.27-alpine":   "docker.io/library/nginx:1.27-alpine",
		"localhost/kubeops-nfs:1.3.0":           "localhost/kubeops-nfs:1.3.0",
		"quay.io/oauth2-proxy/oauth2-proxy:v7":  "quay.io/oauth2-proxy/oauth2-proxy:v7",
		"ghcr.io/dexidp/dex:v2.41.1":            "ghcr.io/dexidp/dex:v2.41.1",
		"registry.k8s.io/metrics-server/x:v1":   "registry.k8s.io/metrics-server/x:v1",
	}
	for in, want := range cases {
		if got := Qualify(in); got != want {
			t.Errorf("Qualify(%q)=%q want %q", in, got, want)
		}
	}
}
