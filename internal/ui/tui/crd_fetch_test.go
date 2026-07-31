package tui

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	k8znerv1alpha1 "github.com/milankappen/k8zner/api/v1alpha1"
)

func newTestCluster(namespace, name string, phase k8znerv1alpha1.ClusterPhase, provPhase k8znerv1alpha1.ProvisioningPhase) *k8znerv1alpha1.K8znerCluster {
	return &k8znerv1alpha1.K8znerCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Status: k8znerv1alpha1.K8znerClusterStatus{
			Phase:             phase,
			ProvisioningPhase: provPhase,
		},
	}
}

func TestFetchCRDStatus_UsesConfiguredClusterName(t *testing.T) {
	t.Parallel()

	// Two clusters in the same namespace: the lookup must pick the configured
	// one, not assume a single fixed name.
	k8sClient := fake.NewClientBuilder().
		WithScheme(k8znerv1alpha1.Scheme).
		WithObjects(
			newTestCluster(Namespace, "cluster-a", k8znerv1alpha1.ClusterPhaseProvisioning, k8znerv1alpha1.PhaseCNI),
			newTestCluster(Namespace, "cluster-b", k8znerv1alpha1.ClusterPhaseRunning, k8znerv1alpha1.PhaseComplete),
		).
		Build()

	msg, done := fetchCRDStatus(context.Background(), k8sClient, "cluster-b")
	assert.True(t, done)
	assert.Equal(t, k8znerv1alpha1.ClusterPhaseRunning, msg.ClusterPhase)
	assert.Equal(t, k8znerv1alpha1.PhaseComplete, msg.ProvPhase)

	msg, done = fetchCRDStatus(context.Background(), k8sClient, "cluster-a")
	assert.False(t, done)
	assert.Equal(t, k8znerv1alpha1.ClusterPhaseProvisioning, msg.ClusterPhase)
	assert.Equal(t, k8znerv1alpha1.PhaseCNI, msg.ProvPhase)
}

func TestFetchCRDStatus_NameNotFound(t *testing.T) {
	t.Parallel()

	k8sClient := fake.NewClientBuilder().
		WithScheme(k8znerv1alpha1.Scheme).
		WithObjects(newTestCluster(Namespace, "cluster-a", k8znerv1alpha1.ClusterPhaseRunning, k8znerv1alpha1.PhaseComplete)).
		Build()

	msg, done := fetchCRDStatus(context.Background(), k8sClient, "no-such-cluster")
	assert.False(t, done)
	assert.Empty(t, msg.ClusterPhase)
}

func TestFetchCRDStatus_LooksUpInK8znerSystemNamespace(t *testing.T) {
	t.Parallel()

	// A cluster with the right name in the wrong namespace must not be found.
	k8sClient := fake.NewClientBuilder().
		WithScheme(k8znerv1alpha1.Scheme).
		WithObjects(newTestCluster("default", "my-cluster", k8znerv1alpha1.ClusterPhaseRunning, k8znerv1alpha1.PhaseComplete)).
		Build()

	msg, done := fetchCRDStatus(context.Background(), k8sClient, "my-cluster")
	assert.False(t, done)
	assert.Empty(t, msg.ClusterPhase)
}

func TestFetchDoctorStatus_UsesConfiguredClusterName(t *testing.T) {
	t.Parallel()

	k8sClient := fake.NewClientBuilder().
		WithScheme(k8znerv1alpha1.Scheme).
		WithObjects(
			newTestCluster(Namespace, "cluster-a", k8znerv1alpha1.ClusterPhaseProvisioning, k8znerv1alpha1.PhaseCNI),
			newTestCluster(Namespace, "cluster-b", k8znerv1alpha1.ClusterPhaseRunning, k8znerv1alpha1.PhaseComplete),
		).
		Build()

	msg, done := fetchDoctorStatus(context.Background(), k8sClient, "cluster-b")
	require.False(t, msg.NotFound)
	assert.True(t, done)
	assert.Equal(t, k8znerv1alpha1.ClusterPhaseRunning, msg.ClusterPhase)
}

func TestFetchDoctorStatus_NameNotFound(t *testing.T) {
	t.Parallel()

	k8sClient := fake.NewClientBuilder().
		WithScheme(k8znerv1alpha1.Scheme).
		WithObjects(newTestCluster(Namespace, "cluster-a", k8znerv1alpha1.ClusterPhaseRunning, k8znerv1alpha1.PhaseComplete)).
		Build()

	msg, done := fetchDoctorStatus(context.Background(), k8sClient, "no-such-cluster")
	assert.True(t, msg.NotFound)
	assert.False(t, done)
}

func TestFetchDoctorStatus_FetchError(t *testing.T) {
	t.Parallel()

	// A client whose scheme does not know K8znerCluster fails with a
	// non-NotFound error, which must surface as FetchErr.
	k8sClient := fake.NewClientBuilder().
		WithScheme(runtime.NewScheme()).
		Build()

	msg, done := fetchDoctorStatus(context.Background(), k8sClient, "cluster-a")
	assert.False(t, done)
	assert.False(t, msg.NotFound)
	assert.NotEmpty(t, msg.FetchErr)
}
