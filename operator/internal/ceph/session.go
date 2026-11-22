package ceph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	dashboardPasswordSecret = "rook-ceph-dashboard-password"
	dashboardUser           = "admin"
)

type cephAuthResponse struct {
	Token             string                 `json:"token"`
	Username          string                 `json:"username"`
	Permissions       map[string]interface{} `json:"permissions"`
	SSO               bool                   `json:"sso"`
	PwdUpdateRequired bool                   `json:"pwdUpdateRequired"`
	PwdExpirationDate interface{}            `json:"pwdExpirationDate"`
}

// handleSession logs into the Rook mgr dashboard as admin and returns a JWT
// session for the console embed (same pattern as Grafana anonymous/proxy auth:
// the console is already OIDC-gated, so the Ceph login form is skipped).
func (s *DiskProbeServer) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	ns := strings.TrimSpace(r.URL.Query().Get("namespace"))
	if ns == "" {
		ns = defaultNamespace
	}

	pw, err := s.dashboardPassword(ctx, ns)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"message": "dashboard password secret unavailable: " + err.Error(),
		})
		return
	}

	auth, err := s.loginDashboard(ctx, ns, dashboardUser, pw)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"message": "dashboard login failed: " + err.Error(),
		})
		return
	}

	// HttpOnly cookie so /kubeops/ceph-ui/api/* requests authenticate.
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    auth.Token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		// Secure left false so lab NodePort HTTPS via termination still works;
		// console is already TLS at the edge.
		MaxAge: 8 * 60 * 60,
	})

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"username":          auth.Username,
		"permissions":       auth.Permissions,
		"sso":               auth.SSO,
		"pwdUpdateRequired": auth.PwdUpdateRequired,
		"pwdExpirationDate": auth.PwdExpirationDate,
		// Token also returned so the embed can seed Angular localStorage state.
		"token": auth.Token,
	})
}

func (s *DiskProbeServer) dashboardPassword(ctx context.Context, ns string) (string, error) {
	sec := &corev1.Secret{}
	if err := s.Client.Get(ctx, types.NamespacedName{Namespace: ns, Name: dashboardPasswordSecret}, sec); err != nil {
		return "", err
	}
	raw, ok := sec.Data["password"]
	if !ok || len(raw) == 0 {
		return "", fmt.Errorf("secret %s missing password key", dashboardPasswordSecret)
	}
	return string(raw), nil
}

func (s *DiskProbeServer) loginDashboard(ctx context.Context, ns, user, password string) (*cephAuthResponse, error) {
	url := fmt.Sprintf("http://rook-ceph-mgr-dashboard.%s.svc.cluster.local:7000/kubeops/ceph-ui/api/auth", ns)
	body, _ := json.Marshal(map[string]string{
		"username": user,
		"password": password,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.ceph.api.v1.0+json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out cephAuthResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.Token) == "" {
		return nil, fmt.Errorf("empty token in auth response")
	}
	if out.Username == "" {
		out.Username = user
	}
	return &out, nil
}
