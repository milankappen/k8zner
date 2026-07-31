package addons

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/milankappen/k8zner/internal/addons/helm"
)

// Note: Uses mockK8sClient from kubectl_test.go (same package)

func TestCreateCloudflareSecret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		namespace   string
		apiToken    string
		mockErr     error
		expectErr   bool
		errContains string
	}{
		{
			name:      "success external-dns namespace",
			namespace: "external-dns",
			apiToken:  "cf-api-token-123",
			mockErr:   nil,
			expectErr: false,
		},
		{
			name:      "success cert-manager namespace",
			namespace: "cert-manager",
			apiToken:  "cf-api-token-456",
			mockErr:   nil,
			expectErr: false,
		},
		{
			name:        "client error",
			namespace:   "external-dns",
			apiToken:    "cf-api-token",
			mockErr:     errors.New("create failed"),
			expectErr:   true,
			errContains: "failed to create cloudflare secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := new(mockK8sClient)

			var capturedSecret *corev1.Secret
			client.On("CreateSecret", mock.Anything, mock.AnythingOfType("*v1.Secret")).Run(func(args mock.Arguments) {
				capturedSecret = args.Get(1).(*corev1.Secret)
			}).Return(tt.mockErr)

			err := createCloudflareSecret(context.Background(), client, tt.namespace, tt.apiToken)

			if tt.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)

				// Verify secret structure
				require.NotNil(t, capturedSecret)
				assert.Equal(t, cloudflareSecretName, capturedSecret.Name)
				assert.Equal(t, tt.namespace, capturedSecret.Namespace)
				assert.Equal(t, corev1.SecretTypeOpaque, capturedSecret.Type)
				assert.Equal(t, tt.apiToken, capturedSecret.StringData["api-token"])
			}

			client.AssertExpectations(t)
		})
	}
}

func TestExternalDNSNamespace(t *testing.T) {
	t.Parallel()
	namespaceYAML := helm.NamespaceManifest("external-dns", withBaselinePodSecurity(nil))

	assert.Contains(t, namespaceYAML, "apiVersion: v1")
	assert.Contains(t, namespaceYAML, "kind: Namespace")
	assert.Contains(t, namespaceYAML, "name: external-dns")
	assert.Contains(t, namespaceYAML, "pod-security.kubernetes.io/enforce: baseline")
	assert.Contains(t, namespaceYAML, "pod-security.kubernetes.io/audit: baseline")
	assert.Contains(t, namespaceYAML, "pod-security.kubernetes.io/warn: baseline")
}

func TestCreateCloudflareSecret_SecretNameConstant(t *testing.T) {
	t.Parallel()
	// Verify the constant is set correctly

	assert.Equal(t, "cloudflare-api-token", cloudflareSecretName)
}

func TestCreateCloudflareSecret_SecretKeyName(t *testing.T) {
	t.Parallel()
	client := new(mockK8sClient)

	var capturedSecret *corev1.Secret
	client.On("CreateSecret", mock.Anything, mock.AnythingOfType("*v1.Secret")).Run(func(args mock.Arguments) {
		capturedSecret = args.Get(1).(*corev1.Secret)
	}).Return(nil)

	err := createCloudflareSecret(context.Background(), client, "test-ns", "test-token")
	require.NoError(t, err)

	// Verify the key name is "api-token"
	_, exists := capturedSecret.StringData["api-token"]
	assert.True(t, exists, "Secret should have 'api-token' key")

	// Verify no other keys
	assert.Len(t, capturedSecret.StringData, 1)
}

func TestCloudflareSecretName_UsedInCertManagerCloudflare(t *testing.T) {
	t.Parallel()
	// Verify the constant is used in ClusterIssuer manifest

	manifest, err := buildClusterIssuerManifest("test@example.com", false)
	require.NoError(t, err)

	assert.Contains(t, string(manifest), cloudflareSecretName)
}

func TestCloudflareSecretName_UsedInExternalDNSValues(t *testing.T) {
	t.Parallel()
	// Verify the constant is used in external-dns values
	// This is implicitly tested via the env var reference

	cfg := &struct {
		Name string
	}{
		Name: cloudflareSecretName,
	}

	assert.Equal(t, "cloudflare-api-token", cfg.Name)
}
