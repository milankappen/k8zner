package addons

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

// mockK8sClient is a local mock for testing
type mockK8sClient struct {
	mock.Mock
}

func (m *mockK8sClient) ApplyManifests(ctx context.Context, manifests []byte, fieldManager string) error {
	args := m.Called(ctx, manifests, fieldManager)
	return args.Error(0)
}

func (m *mockK8sClient) CreateSecret(ctx context.Context, secret *corev1.Secret) error {
	args := m.Called(ctx, secret)
	return args.Error(0)
}

func (m *mockK8sClient) DeleteSecret(ctx context.Context, namespace, name string) error {
	args := m.Called(ctx, namespace, name)
	return args.Error(0)
}

func (m *mockK8sClient) RefreshDiscovery(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *mockK8sClient) HasCRD(ctx context.Context, crdName string) (bool, error) {
	args := m.Called(ctx, crdName)
	return args.Bool(0), args.Error(1)
}

func (m *mockK8sClient) HasReadyEndpoints(ctx context.Context, namespace, serviceName string) (bool, error) {
	args := m.Called(ctx, namespace, serviceName)
	return args.Bool(0), args.Error(1)
}

func (m *mockK8sClient) GetWorkerExternalIPs(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	return args.Get(0).([]string), args.Error(1)
}

func (m *mockK8sClient) HasIngressClass(ctx context.Context, name string) (bool, error) {
	args := m.Called(ctx, name)
	return args.Bool(0), args.Error(1)
}

func TestWithBaselinePodSecurity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		labels map[string]string
		want   map[string]string
	}{
		{
			name:   "nil map gets baseline labels",
			labels: nil,
			want: map[string]string{
				"pod-security.kubernetes.io/enforce": "baseline",
				"pod-security.kubernetes.io/audit":   "baseline",
				"pod-security.kubernetes.io/warn":    "baseline",
			},
		},
		{
			name:   "existing labels preserved",
			labels: map[string]string{"name": "argocd"},
			want: map[string]string{
				"name":                               "argocd",
				"pod-security.kubernetes.io/enforce": "baseline",
				"pod-security.kubernetes.io/audit":   "baseline",
				"pod-security.kubernetes.io/warn":    "baseline",
			},
		},
		{
			name:   "caller-provided keys are not overwritten",
			labels: map[string]string{"pod-security.kubernetes.io/enforce": "privileged"},
			want: map[string]string{
				"pod-security.kubernetes.io/enforce": "privileged",
				"pod-security.kubernetes.io/audit":   "baseline",
				"pod-security.kubernetes.io/warn":    "baseline",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := withBaselinePodSecurity(tt.labels)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWithBaselinePodSecurity_DoesNotMutateInput(t *testing.T) {
	t.Parallel()
	input := map[string]string{"name": "monitoring"}

	_ = withBaselinePodSecurity(input)

	assert.Equal(t, map[string]string{"name": "monitoring"}, input)
}

func TestApplyManifests(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		addonName   string
		manifests   []byte
		mockErr     error
		expectErr   bool
		errContains string
	}{
		{
			name:      "success",
			addonName: "test-addon",
			manifests: []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test"),
			mockErr:   nil,
			expectErr: false,
		},
		{
			name:        "apply error",
			addonName:   "test-addon",
			manifests:   []byte("apiVersion: v1\nkind: ConfigMap"),
			mockErr:     errors.New("apply failed"),
			expectErr:   true,
			errContains: "failed to apply manifests for addon test-addon",
		},
		{
			name:      "empty manifest",
			addonName: "test-addon",
			manifests: []byte{},
			mockErr:   nil,
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := new(mockK8sClient)
			client.On("ApplyManifests", mock.Anything, tt.manifests, tt.addonName).Return(tt.mockErr)

			err := applyManifests(context.Background(), client, tt.addonName, tt.manifests)

			if tt.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestApplyFromURL_Success(t *testing.T) {
	t.Parallel()
	manifestContent := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
  namespace: default
data:
  key: value`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(manifestContent))
	}))
	defer server.Close()

	client := new(mockK8sClient)
	client.On("ApplyManifests", mock.Anything, []byte(manifestContent), "test-addon").Return(nil)

	err := applyFromURL(context.Background(), client, "test-addon", server.URL)
	require.NoError(t, err)
	client.AssertExpectations(t)
}

func TestApplyFromURL_HTTPError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := new(mockK8sClient)

	err := applyFromURL(context.Background(), client, "test-addon", server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404")
}

func TestApplyFromURL_ServerError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := new(mockK8sClient)

	err := applyFromURL(context.Background(), client, "test-addon", server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
}

func TestApplyFromURL_InvalidURL(t *testing.T) {
	t.Parallel()
	client := new(mockK8sClient)

	err := applyFromURL(context.Background(), client, "test-addon", "http://[::1]:namedport")
	require.Error(t, err)
	// Could fail on request creation or download
	assert.True(t,
		strings.Contains(err.Error(), "failed to download manifest") ||
			strings.Contains(err.Error(), "failed to create request"),
		"Expected error about download or request creation, got: %s", err.Error())
}

func TestApplyFromURL_ApplyError(t *testing.T) {
	t.Parallel()
	manifestContent := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(manifestContent))
	}))
	defer server.Close()

	client := new(mockK8sClient)
	client.On("ApplyManifests", mock.Anything, []byte(manifestContent), "test-addon").Return(errors.New("apply failed"))

	err := applyFromURL(context.Background(), client, "test-addon", server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to apply manifests for addon test-addon")
}

func TestFetchManifestURL_Success(t *testing.T) {
	t.Parallel()
	expectedContent := "apiVersion: v1\nkind: ConfigMap"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(expectedContent))
	}))
	defer server.Close()

	result, err := fetchManifestURL(context.Background(), server.URL)
	require.NoError(t, err)
	assert.Equal(t, expectedContent, string(result))
}

func TestFetchManifestURL_Non200Status(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	_, err := fetchManifestURL(context.Background(), server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 403")
}

func TestFetchManifestURL_InvalidURL(t *testing.T) {
	t.Parallel()
	// Use a URL with a null byte which makes NewRequestWithContext fail
	_, err := fetchManifestURL(context.Background(), "http://example.com/\x00invalid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create request")
}

func TestApplyFromURL_ContextCanceled(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow response that will be canceled
		select {
		case <-r.Context().Done():
			return
		}
	}))
	defer server.Close()

	client := new(mockK8sClient)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := applyFromURL(ctx, client, "test-addon", server.URL)
	require.Error(t, err)
}

func TestWithHostAccessPodSecurity(t *testing.T) {
	t.Parallel()

	labels := withHostAccessPodSecurity(map[string]string{"name": "monitoring"})

	assert.Equal(t, "privileged", labels["pod-security.kubernetes.io/enforce"])
	assert.Equal(t, "baseline", labels["pod-security.kubernetes.io/audit"])
	assert.Equal(t, "baseline", labels["pod-security.kubernetes.io/warn"])
	assert.Equal(t, "monitoring", labels["name"])
}
