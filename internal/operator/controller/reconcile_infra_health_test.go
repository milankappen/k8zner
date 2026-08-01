package controller

import (
	"context"
	"fmt"
	"net"
	"testing"

	hcloudgo "github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	k8znerv1alpha1 "github.com/milankappen/k8zner/api/v1alpha1"
)

// newInfraHealthTestScheme builds a fresh Scheme for a subtest. Each
// t.Parallel() subtest needs its own instance: controller-runtime's fake
// client lazily mutates the Scheme's internal type map on first Get() for
// a given GVK, so subtests sharing one Scheme race on that mutation.
func newInfraHealthTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, k8znerv1alpha1.AddToScheme(scheme))
	return scheme
}

func TestReconcileInfraHealth(t *testing.T) {
	t.Parallel()

	t.Run("all infra healthy", func(t *testing.T) {
		t.Parallel()

		scheme := newInfraHealthTestScheme(t)
		mockHCloud := &MockHCloudClient{
			GetNetworkFunc: func(_ context.Context, _ string) (*hcloudgo.Network, error) {
				return &hcloudgo.Network{ID: 1}, nil
			},
			GetFirewallFunc: func(_ context.Context, _ string) (*hcloudgo.Firewall, error) {
				return &hcloudgo.Firewall{ID: 2}, nil
			},
			GetLoadBalancerFunc: func(_ context.Context, _ string) (*hcloudgo.LoadBalancer, error) {
				return &hcloudgo.LoadBalancer{
					ID: 3,
					PublicNet: hcloudgo.LoadBalancerPublicNet{
						Enabled: true,
						IPv4:    hcloudgo.LoadBalancerPublicNetIPv4{IP: net.ParseIP("1.2.3.4")},
					},
				}, nil
			},
		}

		k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		recorder := record.NewFakeRecorder(10)
		r := NewClusterReconciler(k8sClient, scheme, recorder, WithHCloudClient(mockHCloud))

		cluster := &k8znerv1alpha1.K8znerCluster{
			ObjectMeta: k8znerv1alpha1.K8znerCluster{}.ObjectMeta,
		}
		cluster.Name = "test-cluster"

		r.reconcileInfraHealth(context.Background(), cluster)

		assert.True(t, cluster.Status.Infrastructure.NetworkReady)
		assert.True(t, cluster.Status.Infrastructure.FirewallReady)
		assert.True(t, cluster.Status.Infrastructure.LoadBalancerReady)
		assert.Equal(t, "1.2.3.4", cluster.Status.Infrastructure.LoadBalancerIP)
	})

	t.Run("network not found", func(t *testing.T) {
		t.Parallel()

		scheme := newInfraHealthTestScheme(t)
		mockHCloud := &MockHCloudClient{
			GetNetworkFunc: func(_ context.Context, _ string) (*hcloudgo.Network, error) {
				return nil, nil
			},
			GetFirewallFunc: func(_ context.Context, _ string) (*hcloudgo.Firewall, error) {
				return &hcloudgo.Firewall{ID: 2}, nil
			},
			GetLoadBalancerFunc: func(_ context.Context, _ string) (*hcloudgo.LoadBalancer, error) {
				return &hcloudgo.LoadBalancer{ID: 3}, nil
			},
		}

		k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		recorder := record.NewFakeRecorder(10)
		r := NewClusterReconciler(k8sClient, scheme, recorder, WithHCloudClient(mockHCloud))

		cluster := &k8znerv1alpha1.K8znerCluster{}
		cluster.Name = "test-cluster"

		r.reconcileInfraHealth(context.Background(), cluster)

		assert.False(t, cluster.Status.Infrastructure.NetworkReady)
		assert.True(t, cluster.Status.Infrastructure.FirewallReady)
		assert.True(t, cluster.Status.Infrastructure.LoadBalancerReady)
	})

	t.Run("API error marks unhealthy", func(t *testing.T) {
		t.Parallel()

		scheme := newInfraHealthTestScheme(t)
		mockHCloud := &MockHCloudClient{
			GetNetworkFunc: func(_ context.Context, _ string) (*hcloudgo.Network, error) {
				return nil, fmt.Errorf("API error")
			},
			GetFirewallFunc: func(_ context.Context, _ string) (*hcloudgo.Firewall, error) {
				return nil, fmt.Errorf("API error")
			},
			GetLoadBalancerFunc: func(_ context.Context, _ string) (*hcloudgo.LoadBalancer, error) {
				return nil, fmt.Errorf("API error")
			},
		}

		k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		recorder := record.NewFakeRecorder(10)
		r := NewClusterReconciler(k8sClient, scheme, recorder, WithHCloudClient(mockHCloud))

		cluster := &k8znerv1alpha1.K8znerCluster{}
		cluster.Name = "test-cluster"

		r.reconcileInfraHealth(context.Background(), cluster)

		assert.False(t, cluster.Status.Infrastructure.NetworkReady)
		assert.False(t, cluster.Status.Infrastructure.FirewallReady)
		assert.False(t, cluster.Status.Infrastructure.LoadBalancerReady)
	})
}
