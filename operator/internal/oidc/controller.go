package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	dashboardv1alpha1 "github.com/Deepak-Das01/dashboard-ocp/operator/api/v1alpha1"
	"github.com/Deepak-Das01/dashboard-ocp/operator/internal/assets"
	opstatus "github.com/Deepak-Das01/dashboard-ocp/operator/internal/status"
	"golang.org/x/crypto/bcrypt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	defaultNS             = "kubeops-authentication"
	defaultDexImage       = "ghcr.io/dexidp/dex:v2.41.1"
	defaultClientID       = "kube-dashboard"
	defaultUser           = "kubeadmin"
	defaultPasswordLength = 16
	defaultDexNodePort    = int32(32056)
	bootstrapSecretName   = "kubeadmin-oidc-bootstrap"
	oauth2SecretName      = "oauth2-proxy-oidc"
	operatorReportName    = "kubeops-authentication"
	operatorVersion       = "1.3.0"
)

// ClusterOIDCReconciler — Dex + client secrets + RBAC (Phase C). Apiserver patch via script/host.
type ClusterOIDCReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *ClusterOIDCReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	co := &dashboardv1alpha1.ClusterOIDC{}
	if err := r.Get(ctx, req.NamespacedName, co); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	applyDefaults(co)

	nodeIP := r.nodeIP(ctx)
	issuer := co.Spec.IssuerURL
	if issuer == "" && nodeIP != "" {
		issuer = fmt.Sprintf("http://%s:%d/dex", nodeIP, defaultDexNodePort)
	}
	if issuer == "" {
		issuer = fmt.Sprintf("http://dex.%s.svc:5556/dex", co.Spec.Namespace)
	}
	redirect := fmt.Sprintf("https://%s:30443/oauth2/callback", nodeIP)
	if nodeIP == "" {
		redirect = "https://127.0.0.1:30443/oauth2/callback"
	}

	if err := r.ensureNamespace(ctx, co.Spec.Namespace); err != nil {
		return r.fail(ctx, co, err)
	}
	_, hash, err := r.ensureBootstrap(ctx, co)
	if err != nil {
		return r.fail(ctx, co, err)
	}
	clientSecret, cookieSecret, err := r.ensureOAuthSecrets(ctx, co, issuer, redirect)
	if err != nil {
		return r.fail(ctx, co, err)
	}
	_ = cookieSecret

	cfg := renderDexConfig(issuer, co.Spec.ClientID, clientSecret, redirect, co.Spec.BootstrapAdmin.Username, hash)
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "dex", Namespace: co.Spec.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		cm.Data = map[string]string{"config.yaml": cfg}
		return nil
	}); err != nil {
		return r.fail(ctx, co, err)
	}

	themeHash, err := r.ensureDexTheme(ctx, co.Spec.Namespace)
	if err != nil {
		return r.fail(ctx, co, err)
	}
	if err := r.ensureDex(ctx, co, themeHash); err != nil {
		return r.fail(ctx, co, err)
	}
	if err := r.ensureOIDCRBAC(ctx, co.Spec.BootstrapAdmin.Username); err != nil {
		return r.fail(ctx, co, err)
	}

	// Mirror oauth2 secret into kubeops-console for oauth2-proxy sidecar
	if err := r.mirrorOAuthSecret(ctx, co.Spec.Namespace, "kubeops-console"); err != nil {
		logger.Info("mirror oauth2 secret (console ns may not exist yet)", "err", err.Error())
	}

	co.Status.Phase = "Ready"
	co.Status.IssuerURL = issuer
	co.Status.BootstrapSecretRef = co.Spec.Namespace + "/" + bootstrapSecretName
	co.Status.APIServerOIDC = "required"
	co.Status.Message = fmt.Sprintf("Dex Ready at %s — run scripts/configure-apiserver-oidc.sh and scripts/install-phase-c-oidc.sh (or enable console oauth2-proxy)", issuer)
	if co.Spec.ConfigureAPIServer {
		co.Status.APIServerOIDC = "script-required"
		co.Status.Message = "Dex Ready; apply OIDC_ISSUER_URL=" + issuer + " ./scripts/configure-apiserver-oidc.sh on control-plane, then install-phase-c-oidc.sh"
	}
	if err := r.Status().Update(ctx, co); err != nil {
		return ctrl.Result{}, err
	}
	_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentOIDC, operatorVersion,
		dashboardv1alpha1.OperatorPhaseReady, issuer, co.Status.Message)

	logger.Info("reconciled ClusterOIDC", "issuer", issuer)
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *ClusterOIDCReconciler) fail(ctx context.Context, co *dashboardv1alpha1.ClusterOIDC, err error) (ctrl.Result, error) {
	co.Status.Phase = "Error"
	co.Status.Message = err.Error()
	_ = r.Status().Update(ctx, co)
	_ = opstatus.Report(ctx, r.Client, operatorReportName, dashboardv1alpha1.OperatorComponentOIDC, operatorVersion,
		dashboardv1alpha1.OperatorPhaseDegraded, "", err.Error())
	return ctrl.Result{RequeueAfter: 30 * time.Second}, err
}

func applyDefaults(co *dashboardv1alpha1.ClusterOIDC) {
	if co.Spec.Namespace == "" {
		co.Spec.Namespace = defaultNS
	}
	if co.Spec.Provider == "" {
		co.Spec.Provider = "dex"
	}
	if co.Spec.ClientID == "" {
		co.Spec.ClientID = defaultClientID
	}
	if co.Spec.DexImage == "" {
		co.Spec.DexImage = defaultDexImage
	}
	if co.Spec.BootstrapAdmin.Username == "" {
		co.Spec.BootstrapAdmin.Username = defaultUser
	}
	if co.Spec.BootstrapAdmin.PasswordLength == 0 {
		co.Spec.BootstrapAdmin.PasswordLength = defaultPasswordLength
	}
}

func (r *ClusterOIDCReconciler) nodeIP(ctx context.Context) string {
	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes); err != nil || len(nodes.Items) == 0 {
		return ""
	}
	for _, a := range nodes.Items[0].Status.Addresses {
		if a.Type == corev1.NodeInternalIP {
			return a.Address
		}
	}
	return ""
}

func (r *ClusterOIDCReconciler) ensureNamespace(ctx context.Context, ns string) error {
	n := &corev1.Namespace{}
	err := r.Get(ctx, client.ObjectKey{Name: ns}, n)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
	}
	return err
}

func (r *ClusterOIDCReconciler) ensureBootstrap(ctx context.Context, co *dashboardv1alpha1.ClusterOIDC) (plain, hash string, err error) {
	ns := co.Spec.Namespace
	user := co.Spec.BootstrapAdmin.Username
	length := co.Spec.BootstrapAdmin.PasswordLength
	sec := &corev1.Secret{}
	err = r.Get(ctx, client.ObjectKey{Namespace: ns, Name: bootstrapSecretName}, sec)
	if err == nil {
		plain = string(sec.Data["password"])
		hash = string(sec.Data["passwordHash"])
		if plain != "" && hash != "" {
			return plain, hash, nil
		}
	} else if !apierrors.IsNotFound(err) {
		return "", "", err
	}
	notFound := apierrors.IsNotFound(err)
	plain, err = randomPassword(length)
	if err != nil {
		return "", "", err
	}
	hb, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", "", err
	}
	hash = string(hb)
	data := map[string]string{"username": user, "password": plain, "passwordHash": hash}
	if notFound {
		return plain, hash, r.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: bootstrapSecretName, Namespace: ns, Labels: map[string]string{"app.kubernetes.io/part-of": "kubeops-authentication"}},
			Type:       corev1.SecretTypeOpaque,
			StringData: data,
		})
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, sec, func() error {
		sec.StringData = data
		return nil
	})
	return plain, hash, err
}

func (r *ClusterOIDCReconciler) ensureOAuthSecrets(ctx context.Context, co *dashboardv1alpha1.ClusterOIDC, issuer, redirect string) (clientSecret, cookieSecret string, err error) {
	ns := co.Spec.Namespace
	sec := &corev1.Secret{}
	err = r.Get(ctx, client.ObjectKey{Namespace: ns, Name: oauth2SecretName}, sec)
	if err == nil {
		clientSecret = string(sec.Data["client-secret"])
		cookieSecret = string(sec.Data["cookie-secret"])
	} else if !apierrors.IsNotFound(err) {
		return "", "", err
	}
	if clientSecret == "" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		clientSecret = hex.EncodeToString(b)
	}
	if cookieSecret == "" {
		b := make([]byte, 24)
		_, _ = rand.Read(b)
		cookieSecret = hex.EncodeToString(b)[:32]
	}
	data := map[string]string{
		"client-id":     co.Spec.ClientID,
		"client-secret": clientSecret,
		"cookie-secret": cookieSecret,
		"issuer-url":    issuer,
		"redirect-url":  redirect,
	}
	out := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: oauth2SecretName, Namespace: ns}}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, out, func() error {
		out.Type = corev1.SecretTypeOpaque
		out.StringData = data
		return nil
	})
	return clientSecret, cookieSecret, err
}

func (r *ClusterOIDCReconciler) mirrorOAuthSecret(ctx context.Context, fromNS, toNS string) error {
	src := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: fromNS, Name: oauth2SecretName}, src); err != nil {
		return err
	}
	dst := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: oauth2SecretName, Namespace: toNS}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, dst, func() error {
		dst.Type = corev1.SecretTypeOpaque
		dst.Data = src.Data
		return nil
	})
	return err
}

func (r *ClusterOIDCReconciler) ensureOIDCRBAC(ctx context.Context, username string) error {
	crb := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "oidc-kubeadmin"}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, crb, func() error {
		crb.RoleRef = rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: "cluster-admin"}
		crb.Subjects = []rbacv1.Subject{{Kind: "User", Name: username, APIGroup: "rbac.authorization.k8s.io"}}
		return nil
	})
	return err
}

func (r *ClusterOIDCReconciler) ensureDexTheme(ctx context.Context, ns string) (string, error) {
	files := map[string]string{
		"styles.css":    "auth/dex/themes/kube-dashboard/styles.css",
		"header.html":   "auth/dex/templates/header.html",
		"password.html": "auth/dex/templates/password.html",
		"login.html":    "auth/dex/templates/login.html",
	}
	data := make(map[string]string, len(files))
	h := sha256.New()
	for key, path := range files {
		raw, err := assets.FS.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("embedded dex %s: %w", path, err)
		}
		data[key] = string(raw)
		_, _ = h.Write([]byte(key))
		_, _ = h.Write(raw)
	}
	hash := hex.EncodeToString(h.Sum(nil)[:8])
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "dex-theme", Namespace: ns}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		cm.Labels = map[string]string{"app": "dex", "app.kubernetes.io/component": "theme"}
		cm.Data = data
		return nil
	})
	return hash, err
}

func (r *ClusterOIDCReconciler) ensureDex(ctx context.Context, co *dashboardv1alpha1.ClusterOIDC, themeHash string) error {
	ns := co.Spec.Namespace
	labels := map[string]string{"app": "dex", "app.kubernetes.io/name": "dex"}
	deploy := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "dex", Namespace: ns}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
		replicas := int32(1)
		deploy.Labels = labels
		deploy.Spec.Replicas = &replicas
		deploy.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "dex"}}
		deploy.Spec.Template.ObjectMeta.Labels = labels
		if deploy.Spec.Template.ObjectMeta.Annotations == nil {
			deploy.Spec.Template.ObjectMeta.Annotations = map[string]string{}
		}
		deploy.Spec.Template.ObjectMeta.Annotations["kubeops.io/dex-theme-hash"] = themeHash
		// Seed theme logos + overlay glass CSS and custom login templates.
		deploy.Spec.Template.Spec.InitContainers = []corev1.Container{{
			Name:  "theme-seed",
			Image: co.Spec.DexImage,
			Command: []string{"/bin/sh", "-c", strings.Join([]string{
				"cp -r /srv/dex/web/themes/dark/. /theme/",
				"cp /theme-in/styles.css /theme/styles.css",
				"cp -r /srv/dex/web/templates/. /templates/",
				"cp /theme-in/header.html /templates/header.html",
				"cp /theme-in/password.html /templates/password.html",
				"cp /theme-in/login.html /templates/login.html",
			}, " && ")},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "theme", MountPath: "/theme"},
				{Name: "templates", MountPath: "/templates"},
				{Name: "theme-css", MountPath: "/theme-in", ReadOnly: true},
			},
		}}
		deploy.Spec.Template.Spec.Containers = []corev1.Container{{
			Name:  "dex",
			Image: co.Spec.DexImage,
			Args:  []string{"dex", "serve", "/etc/dex/config.yaml"},
			Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 5556}},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "config", MountPath: "/etc/dex", ReadOnly: true},
				{Name: "theme", MountPath: "/srv/dex/web/themes/kube-dashboard", ReadOnly: true},
				{Name: "templates", MountPath: "/srv/dex/web/templates", ReadOnly: true},
			},
		}}
		deploy.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name: "config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "dex"},
						Items:                []corev1.KeyToPath{{Key: "config.yaml", Path: "config.yaml"}},
					},
				},
			},
			{
				Name: "theme-css",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "dex-theme"},
					},
				},
			},
			{Name: "theme", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			{Name: "templates", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		}
		return nil
	})
	if err != nil {
		return err
	}
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "dex", Namespace: ns}}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Labels = labels
		svc.Spec.Type = corev1.ServiceTypeNodePort
		svc.Spec.Selector = map[string]string{"app": "dex"}
		svc.Spec.Ports = []corev1.ServicePort{{
			Name: "http", Port: 5556, TargetPort: intstr.FromInt(5556), NodePort: defaultDexNodePort,
		}}
		return nil
	})
	return err
}

func renderDexConfig(issuer, clientID, clientSecret, redirect, username, bcryptHash string) string {
	return fmt.Sprintf(`issuer: %s
storage:
  type: memory
web:
  http: 0.0.0.0:5556
frontend:
  dir: /srv/dex/web
  theme: kube-dashboard
  issuer: Sign in
oauth2:
  skipApprovalScreen: true
staticClients:
  - id: %s
    secret: %s
    name: kube-dashboard
    redirectURIs:
      - %s
enablePasswordDB: true
staticPasswords:
  - email: "%s"
    username: "%s"
    userID: "kubeadmin"
    hash: '%s'
`, issuer, clientID, clientSecret, redirect, username, username, bcryptHash)
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

func (r *ClusterOIDCReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dashboardv1alpha1.ClusterOIDC{}).
		Complete(r)
}
