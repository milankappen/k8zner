package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	k8znerv1alpha1 "github.com/milankappen/k8zner/api/v1alpha1"
)

func TestReconcileConnectivityHealth(t *testing.T) {
	t.Parallel()

	scheme := setupTestScheme(t)

	t.Run("kube API always ready", func(t *testing.T) {
		t.Parallel()

		k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		recorder := record.NewFakeRecorder(10)
		r := NewClusterReconciler(k8sClient, scheme, recorder)

		cluster := &k8znerv1alpha1.K8znerCluster{}

		r.reconcileConnectivityHealth(context.Background(), cluster)

		assert.True(t, cluster.Status.Connectivity.KubeAPIReady)
		assert.NotNil(t, cluster.Status.Connectivity.LastCheck)
	})

	t.Run("no endpoints when no domain", func(t *testing.T) {
		t.Parallel()

		k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		recorder := record.NewFakeRecorder(10)
		r := NewClusterReconciler(k8sClient, scheme, recorder)

		cluster := &k8znerv1alpha1.K8znerCluster{
			Spec: k8znerv1alpha1.K8znerClusterSpec{
				Domain: "",
			},
		}

		r.reconcileConnectivityHealth(context.Background(), cluster)

		assert.Nil(t, cluster.Status.Connectivity.Endpoints)
	})

	t.Run("builds endpoints from spec", func(t *testing.T) {
		t.Parallel()

		k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		recorder := record.NewFakeRecorder(10)
		r := NewClusterReconciler(k8sClient, scheme, recorder)

		cluster := &k8znerv1alpha1.K8znerCluster{
			Spec: k8znerv1alpha1.K8znerClusterSpec{
				Domain: "example.com",
				Addons: &k8znerv1alpha1.AddonSpec{
					ArgoCD:           true,
					ArgoSubdomain:    "argo-test",
					Monitoring:       true,
					GrafanaSubdomain: "grafana-test",
				},
			},
		}

		r.reconcileConnectivityHealth(context.Background(), cluster)

		require.Len(t, cluster.Status.Connectivity.Endpoints, 2)
		assert.Equal(t, "argo-test.example.com", cluster.Status.Connectivity.Endpoints[0].Host)
		assert.Equal(t, "grafana-test.example.com", cluster.Status.Connectivity.Endpoints[1].Host)
		// DNS will fail for these fake hosts, which is expected
		assert.False(t, cluster.Status.Connectivity.Endpoints[0].DNSReady)
	})

	t.Run("uses default subdomains", func(t *testing.T) {
		t.Parallel()

		k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		recorder := record.NewFakeRecorder(10)
		r := NewClusterReconciler(k8sClient, scheme, recorder)

		cluster := &k8znerv1alpha1.K8znerCluster{
			Spec: k8znerv1alpha1.K8znerClusterSpec{
				Domain: "example.com",
				Addons: &k8znerv1alpha1.AddonSpec{
					ArgoCD:     true,
					Monitoring: true,
				},
			},
		}

		r.reconcileConnectivityHealth(context.Background(), cluster)

		require.Len(t, cluster.Status.Connectivity.Endpoints, 2)
		assert.Equal(t, "argo.example.com", cluster.Status.Connectivity.Endpoints[0].Host)
		assert.Equal(t, "grafana.example.com", cluster.Status.Connectivity.Endpoints[1].Host)
	})

	t.Run("metrics API false when no APIService", func(t *testing.T) {
		t.Parallel()

		k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		recorder := record.NewFakeRecorder(10)
		r := NewClusterReconciler(k8sClient, scheme, recorder)

		cluster := &k8znerv1alpha1.K8znerCluster{}

		r.reconcileConnectivityHealth(context.Background(), cluster)

		assert.False(t, cluster.Status.Connectivity.MetricsAPIReady)
	})
}
