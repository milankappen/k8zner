package handlers

import (
	"context"
	"fmt"
	"log"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const (
	// viewerName names the read-only ServiceAccount and its ClusterRoleBinding.
	viewerName = "k8zner-viewer"
	// viewerNamespace hosts the viewer ServiceAccount.
	viewerNamespace = "kube-system"
)

// Kubeconfig handles the kubeconfig command: it generates a scoped kubeconfig
// for the cluster. Only read-only generation is supported; the credential is a
// ServiceAccount token bound to the built-in "view" ClusterRole, which grants
// no access to Secrets.
func Kubeconfig(ctx context.Context, adminKubeconfigPath, outputPath string, readOnly bool, ttl time.Duration) error {
	if !readOnly {
		return fmt.Errorf("only read-only kubeconfig generation is supported: pass --read-only")
	}

	adminCfg, err := clientcmd.LoadFromFile(adminKubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to load admin kubeconfig from %s: %w", adminKubeconfigPath, err)
	}

	restCfg, err := clientcmd.NewDefaultClientConfig(*adminCfg, nil).ClientConfig()
	if err != nil {
		return fmt.Errorf("failed to build client config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	clusterName := clusterNameFromKubeconfig(adminCfg)
	out, err := generateReadOnlyKubeconfig(ctx, clientset, adminCfg, clusterName, ttl)
	if err != nil {
		return err
	}

	if err := writeFile(outputPath, out, 0600); err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	log.Printf("Read-only kubeconfig written to %s (token valid for %s)", outputPath, ttl)
	return nil
}

// generateReadOnlyKubeconfig ensures the viewer ServiceAccount and its
// ClusterRoleBinding exist, requests a bound token, and renders a kubeconfig
// reusing the server endpoint and CA from the admin kubeconfig.
func generateReadOnlyKubeconfig(ctx context.Context, clientset kubernetes.Interface, adminCfg *clientcmdapi.Config, clusterName string, ttl time.Duration) ([]byte, error) {
	cluster, err := currentCluster(adminCfg)
	if err != nil {
		return nil, err
	}

	if err := ensureViewerServiceAccount(ctx, clientset); err != nil {
		return nil, err
	}
	if err := ensureViewerClusterRoleBinding(ctx, clientset); err != nil {
		return nil, err
	}

	expiration := int64(ttl.Seconds())
	tokenReq, err := clientset.CoreV1().ServiceAccounts(viewerNamespace).CreateToken(ctx, viewerName,
		&authenticationv1.TokenRequest{
			Spec: authenticationv1.TokenRequestSpec{
				ExpirationSeconds: &expiration,
			},
		}, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to request token for %s: %w", viewerName, err)
	}

	contextName := clusterName + "-readonly"
	out := clientcmdapi.NewConfig()
	out.Clusters[clusterName] = &clientcmdapi.Cluster{
		Server:                   cluster.Server,
		CertificateAuthorityData: cluster.CertificateAuthorityData,
	}
	out.AuthInfos[viewerName] = &clientcmdapi.AuthInfo{
		Token: tokenReq.Status.Token,
	}
	out.Contexts[contextName] = &clientcmdapi.Context{
		Cluster:  clusterName,
		AuthInfo: viewerName,
	}
	out.CurrentContext = contextName

	rendered, err := clientcmd.Write(*out)
	if err != nil {
		return nil, fmt.Errorf("failed to render kubeconfig: %w", err)
	}
	return rendered, nil
}

// currentCluster returns the cluster entry referenced by the current context.
func currentCluster(cfg *clientcmdapi.Config) (*clientcmdapi.Cluster, error) {
	kubeCtx, ok := cfg.Contexts[cfg.CurrentContext]
	if !ok {
		return nil, fmt.Errorf("admin kubeconfig has no context %q", cfg.CurrentContext)
	}
	cluster, ok := cfg.Clusters[kubeCtx.Cluster]
	if !ok {
		return nil, fmt.Errorf("admin kubeconfig has no cluster %q", kubeCtx.Cluster)
	}
	return cluster, nil
}

// clusterNameFromKubeconfig derives a display name from the current context.
func clusterNameFromKubeconfig(cfg *clientcmdapi.Config) string {
	if kubeCtx, ok := cfg.Contexts[cfg.CurrentContext]; ok && kubeCtx.Cluster != "" {
		return kubeCtx.Cluster
	}
	return "k8zner"
}

// ensureViewerServiceAccount creates the viewer SA if it does not exist.
func ensureViewerServiceAccount(ctx context.Context, clientset kubernetes.Interface) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      viewerName,
			Namespace: viewerNamespace,
			Labels:    map[string]string{"app.kubernetes.io/managed-by": "k8zner"},
		},
	}
	if _, err := clientset.CoreV1().ServiceAccounts(viewerNamespace).Create(ctx, sa, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create ServiceAccount %s: %w", viewerName, err)
	}
	return nil
}

// ensureViewerClusterRoleBinding binds the viewer SA to the built-in "view"
// ClusterRole, which deliberately excludes Secrets.
func ensureViewerClusterRoleBinding(ctx context.Context, clientset kubernetes.Interface) error {
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   viewerName,
			Labels: map[string]string{"app.kubernetes.io/managed-by": "k8zner"},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     "view",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      viewerName,
				Namespace: viewerNamespace,
			},
		},
	}
	if _, err := clientset.RbacV1().ClusterRoleBindings().Create(ctx, crb, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create ClusterRoleBinding %s: %w", viewerName, err)
	}
	return nil
}
