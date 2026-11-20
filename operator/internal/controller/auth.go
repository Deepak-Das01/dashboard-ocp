package controller

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"

	dashboardv1alpha1 "github.com/Deepak-Das01/dashboard-ocp/operator/api/v1alpha1"
	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *KubeDashboardReconciler) reconcileAuth(ctx context.Context, kd *dashboardv1alpha1.KubeDashboard) (htpasswd string, err error) {
	ns := kd.Spec.Namespace
	mode := kd.Spec.Auth.Mode
	kd.Status.Auth.Mode = mode

	switch mode {
	case dashboardv1alpha1.AuthModeToken:
		kd.Status.Auth.Ready = true
		kd.Status.Auth.BootstrapSecretRef = ""
		kd.Status.Auth.Message = "Token auth: use SA dashboard-admin / scripts/get-token.sh"
		return "", nil

	case dashboardv1alpha1.AuthModeSecret:
		user := kd.Spec.Auth.BootstrapAdmin.Username
		length := kd.Spec.Auth.BootstrapAdmin.PasswordLength
		plain, hash, ht, err := r.ensureBootstrapSecret(ctx, ns, user, length)
		if err != nil {
			return "", err
		}
		_ = plain
		_ = hash
		kd.Status.Auth.Ready = true
		kd.Status.Auth.BootstrapSecretRef = ns + "/" + bootstrapSecretName
		kd.Status.Auth.Message = fmt.Sprintf("Secret auth: browser login as %q (password in Secret %s); Dashboard gets SA token automatically", user, bootstrapSecretName)
		return ht, nil

	case dashboardv1alpha1.AuthModeOIDC:
		// oauth2-proxy-oidc is mirrored by the OIDC operator from ClusterOIDC.
		sec := &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: ns, Name: "oauth2-proxy-oidc"}, sec); err != nil {
			kd.Status.Auth.Ready = false
			kd.Status.Auth.Message = "OIDC: waiting for Secret oauth2-proxy-oidc (apply ClusterOIDC)"
			return "", fmt.Errorf("oidc secret: %w", err)
		}
		kd.Status.Auth.Ready = true
		kd.Status.Auth.BootstrapSecretRef = "kubeops-authentication/kubeadmin-oidc-bootstrap"
		kd.Status.Auth.Message = "OIDC: browser redirects to Dex (kubeadmin); password in kubeops-authentication/kubeadmin-oidc-bootstrap"
		return "", nil

	default:
		return "", fmt.Errorf("unknown auth.mode %q (console supports secret|token|oidc)", mode)
	}
}

func (r *KubeDashboardReconciler) ensureBootstrapSecret(ctx context.Context, ns, user string, length int) (plain, bcryptHash, htpasswdLine string, err error) {
	if ns == "" {
		return "", "", "", fmt.Errorf("bootstrap secret namespace is empty")
	}
	sec := &corev1.Secret{}
	err = r.Get(ctx, client.ObjectKey{Namespace: ns, Name: bootstrapSecretName}, sec)
	if err == nil {
		plain = string(sec.Data["password"])
		bcryptHash = string(sec.Data["passwordHash"])
		htpasswdLine = string(sec.Data["htpasswd"])
		if plain != "" && htpasswdLine != "" {
			return plain, bcryptHash, htpasswdLine, nil
		}
	} else if !apierrors.IsNotFound(err) {
		return "", "", "", err
	}
	notFound := apierrors.IsNotFound(err)

	plain, err = randomPassword(length)
	if err != nil {
		return "", "", "", err
	}
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", "", "", err
	}
	bcryptHash = string(hashBytes)
	htpasswdLine, err = apr1Htpasswd(user, plain)
	if err != nil {
		return "", "", "", err
	}

	data := map[string]string{
		"username":     user,
		"password":     plain,
		"passwordHash": bcryptHash,
		"htpasswd":     htpasswdLine,
	}
	if notFound {
		out := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bootstrapSecretName,
				Namespace: ns,
				Labels: map[string]string{
					"app.kubernetes.io/name":    "kube-dashboard",
					"app.kubernetes.io/part-of": "kube-dashboard",
					"dashboard.ocp/auth":        "secret",
				},
			},
			Type:       corev1.SecretTypeOpaque,
			StringData: data,
		}
		return plain, bcryptHash, htpasswdLine, r.Create(ctx, out)
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, sec, func() error {
		sec.StringData = data
		return nil
	})
	return plain, bcryptHash, htpasswdLine, err
}

func randomPassword(n int) (string, error) {
	const alphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	var b strings.Builder
	max := big.NewInt(int64(len(alphabet)))
	for i := 0; i < n; i++ {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(alphabet[idx.Int64()])
	}
	return b.String(), nil
}
