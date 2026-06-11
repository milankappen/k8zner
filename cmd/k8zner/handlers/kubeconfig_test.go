package handlers

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// newFakeClientsetWithToken returns a fake clientset whose TokenRequest
// subresource answers with a fixed token.
func newFakeClientsetWithToken(token string) *fake.Clientset {
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("create", "serviceaccounts", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
		if action.GetSubresource() != "token" {
			return false, nil, nil
		}
		return true, &authenticationv1.TokenRequest{
			Status: authenticationv1.TokenRequestStatus{
				Token:               token,
				ExpirationTimestamp: metav1.Time{Time: time.Now().Add(time.Hour)},
			},
		}, nil
	})
	return cs
}

func adminKubeconfigFixture() *clientcmdapi.Config {
	return &clientcmdapi.Config{
		CurrentContext: "admin@my-cluster",
		Clusters: map[string]*clientcmdapi.Cluster{
			"my-cluster": {
				Server:                   "https://203.0.113.10:6443",
				CertificateAuthorityData: []byte("fake-ca-data"),
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"admin@my-cluster": {Cluster: "my-cluster", AuthInfo: "admin"},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"admin": {ClientCertificateData: []byte("cert"), ClientKeyData: []byte("key")},
		},
	}
}

func TestGenerateReadOnlyKubeconfig(t *testing.T) {
	t.Parallel()

	t.Run("creates viewer service account, binding, and kubeconfig", func(t *testing.T) {
		t.Parallel()
		cs := newFakeClientsetWithToken("sa-token-123")

		out, err := generateReadOnlyKubeconfig(context.Background(), cs, adminKubeconfigFixture(), "my-cluster", time.Hour)
		require.NoError(t, err)

		sa, err := cs.CoreV1().ServiceAccounts(viewerNamespace).Get(context.Background(), viewerName, metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, viewerName, sa.Name)

		crb, err := cs.RbacV1().ClusterRoleBindings().Get(context.Background(), viewerName, metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, "view", crb.RoleRef.Name)
		require.Len(t, crb.Subjects, 1)
		assert.Equal(t, viewerNamespace, crb.Subjects[0].Namespace)

		rendered := string(out)
		assert.Contains(t, rendered, "sa-token-123")
		assert.Contains(t, rendered, "https://203.0.113.10:6443")
		assert.Contains(t, rendered, "my-cluster-readonly")
	})

	t.Run("idempotent when SA and binding already exist", func(t *testing.T) {
		t.Parallel()
		cs := newFakeClientsetWithToken("tok")

		_, err := generateReadOnlyKubeconfig(context.Background(), cs, adminKubeconfigFixture(), "my-cluster", time.Hour)
		require.NoError(t, err)
		_, err = generateReadOnlyKubeconfig(context.Background(), cs, adminKubeconfigFixture(), "my-cluster", time.Hour)
		require.NoError(t, err)
	})

	t.Run("fails when admin kubeconfig has no current cluster", func(t *testing.T) {
		t.Parallel()
		cs := newFakeClientsetWithToken("tok")
		broken := adminKubeconfigFixture()
		broken.CurrentContext = "missing"

		_, err := generateReadOnlyKubeconfig(context.Background(), cs, broken, "my-cluster", time.Hour)
		require.Error(t, err)
	})
}

func TestWriteReadOnlyKubeconfig_FilePermissions(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not meaningful on windows")
	}

	cs := newFakeClientsetWithToken("tok")
	out, err := generateReadOnlyKubeconfig(context.Background(), cs, adminKubeconfigFixture(), "my-cluster", time.Hour)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "kubeconfig-readonly")
	require.NoError(t, writeFile(path, out, 0600))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(data), "tok"))
}
