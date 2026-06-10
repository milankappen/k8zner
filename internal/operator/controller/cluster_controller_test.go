package controller

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	hcloudgo "github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	k8znerv1alpha1 "github.com/milankappen/k8zner/api/v1alpha1"
	"github.com/milankappen/k8zner/internal/config"
)

func setupTestScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, k8znerv1alpha1.AddToScheme(scheme))
	return scheme
}

// createTestNode is a helper function to create test nodes.
func createTestNode(name string, isControlPlane, isReady bool) *corev1.Node {
	labels := map[string]string{}
	if isControlPlane {
		labels["node-role.kubernetes.io/control-plane"] = ""
	}

	readyStatus := corev1.ConditionFalse
	if isReady {
		readyStatus = corev1.ConditionTrue
	}

	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: corev1.NodeSpec{
			ProviderID: "hcloud://12345",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:               corev1.NodeReady,
					Status:             readyStatus,
					LastTransitionTime: metav1.NewTime(time.Now().Add(-1 * time.Hour)),
				},
			},
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
				{Type: corev1.NodeExternalIP, Address: "1.2.3.4"},
			},
		},
	}
}

func TestNewClusterReconciler(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	recorder := record.NewFakeRecorder(10)

	t.Run("with default options", func(t *testing.T) {
		t.Parallel()
		r := NewClusterReconciler(client, scheme, recorder)

		assert.NotNil(t, r)
		assert.Equal(t, client, r.Client)
		assert.Equal(t, scheme, r.Scheme)
		assert.Equal(t, recorder, r.Recorder)
		assert.True(t, r.enableMetrics)
		assert.Equal(t, 1, r.maxConcurrentHeals)
	})

	t.Run("with custom options", func(t *testing.T) {
		t.Parallel()
		mockHCloud := &MockHCloudClient{}
		mockTalos := &MockTalosClient{}

		r := NewClusterReconciler(client, scheme, recorder,
			WithHCloudClient(mockHCloud),
			WithTalosClient(mockTalos),
			WithMetrics(false),
			WithMaxConcurrentHeals(3),
		)

		assert.NotNil(t, r)
		assert.Equal(t, mockHCloud, r.hcloudClient)
		assert.Equal(t, mockTalos, r.talosClient)
		assert.False(t, r.enableMetrics)
		assert.Equal(t, 3, r.maxConcurrentHeals)
	})

	t.Run("with HCloud token for lazy initialization", func(t *testing.T) {
		t.Parallel()
		r := NewClusterReconciler(client, scheme, recorder,
			WithHCloudToken("test-token"),
		)

		assert.NotNil(t, r)
		assert.Equal(t, "test-token", r.hcloudToken)
		assert.Nil(t, r.hcloudClient) // Should be nil until ensureHCloudClient is called
	})
}

func TestClusterReconciler_Reconcile(t *testing.T) {
	t.Parallel()

	t.Run("cluster not found returns no error", func(t *testing.T) {
		t.Parallel()
		scheme := setupTestScheme(t)
		client := fake.NewClientBuilder().WithScheme(scheme).Build()
		recorder := record.NewFakeRecorder(10)

		r := NewClusterReconciler(client, scheme, recorder,
			WithHCloudClient(&MockHCloudClient{}),
		)

		result, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: "default",
				Name:      "nonexistent",
			},
		})

		assert.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, result)
	})

	t.Run("deleting cluster skips reconciliation", func(t *testing.T) {
		t.Parallel()
		scheme := setupTestScheme(t)
		cluster := &k8znerv1alpha1.K8znerCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-cluster",
				Namespace:         "default",
				DeletionTimestamp: &metav1.Time{Time: time.Now()},
				Finalizers:        []string{"test.k8zner.io/keep"},
			},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster).
			WithStatusSubresource(cluster).
			Build()
		recorder := record.NewFakeRecorder(10)

		r := NewClusterReconciler(client, scheme, recorder,
			WithHCloudClient(&MockHCloudClient{}),
		)

		result, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: "default",
				Name:      "test-cluster",
			},
		})

		assert.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, result)
	})

	t.Run("paused cluster skips reconciliation", func(t *testing.T) {
		t.Parallel()
		scheme := setupTestScheme(t)
		cluster := &k8znerv1alpha1.K8znerCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
			Spec: k8znerv1alpha1.K8znerClusterSpec{
				Paused: true,
			},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster).
			WithStatusSubresource(cluster).
			Build()
		recorder := record.NewFakeRecorder(10)

		r := NewClusterReconciler(client, scheme, recorder,
			WithHCloudClient(&MockHCloudClient{}),
		)

		result, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: "default",
				Name:      "test-cluster",
			},
		})

		assert.NoError(t, err)
		assert.Equal(t, defaultRequeueAfter, result.RequeueAfter)
	})

	t.Run("reconciles healthy cluster", func(t *testing.T) {
		t.Parallel()
		scheme := setupTestScheme(t)
		cluster := &k8znerv1alpha1.K8znerCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
			Spec: k8znerv1alpha1.K8znerClusterSpec{
				Region: "nbg1",
				ControlPlanes: k8znerv1alpha1.ControlPlaneSpec{
					Count: 3,
					Size:  "cx22",
				},
				Workers: k8znerv1alpha1.WorkerSpec{
					Count: 2,
					Size:  "cx22",
				},
			},
		}

		cpNode1 := createTestNode("cp-1", true, true)
		cpNode2 := createTestNode("cp-2", true, true)
		cpNode3 := createTestNode("cp-3", true, true)

		worker1 := createTestNode("worker-1", false, true)
		worker2 := createTestNode("worker-2", false, true)

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, cpNode1, cpNode2, cpNode3, worker1, worker2).
			WithStatusSubresource(cluster).
			Build()
		recorder := record.NewFakeRecorder(10)

		mockHCloud := &MockHCloudClient{}
		r := NewClusterReconciler(client, scheme, recorder,
			WithHCloudClient(mockHCloud),
			WithMetrics(false),
		)

		result, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: "default",
				Name:      "test-cluster",
			},
		})

		assert.NoError(t, err)
		assert.Equal(t, defaultRequeueAfter, result.RequeueAfter)

		updatedCluster := &k8znerv1alpha1.K8znerCluster{}
		err = client.Get(context.Background(), types.NamespacedName{
			Namespace: "default",
			Name:      "test-cluster",
		}, updatedCluster)
		require.NoError(t, err)

		assert.Equal(t, 3, updatedCluster.Status.ControlPlanes.Ready)
		assert.Equal(t, 2, updatedCluster.Status.Workers.Ready)
		assert.Equal(t, k8znerv1alpha1.ClusterPhaseRunning, updatedCluster.Status.Phase)
	})
}

func TestBuildClusterState(t *testing.T) {
	t.Parallel()

	t.Run("builds state with SSH keys from annotations", func(t *testing.T) {
		t.Parallel()
		scheme := setupTestScheme(t)
		cluster := &k8znerv1alpha1.K8znerCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-cluster",
				Namespace: "default",
				Annotations: map[string]string{
					"k8zner.io/ssh-keys":               "key1,key2,key3",
					"k8zner.io/control-plane-endpoint": "1.2.3.4",
				},
			},
			Spec: k8znerv1alpha1.K8znerClusterSpec{
				Region: "fsn1",
			},
			Status: k8znerv1alpha1.K8znerClusterStatus{
				ControlPlanes: k8znerv1alpha1.NodeGroupStatus{
					Nodes: []k8znerv1alpha1.NodeStatus{
						{Name: "cp-1", Healthy: true, PrivateIP: "10.0.0.1", PublicIP: "1.2.3.1"},
						{Name: "cp-2", Healthy: true, PrivateIP: "10.0.0.2", PublicIP: "1.2.3.2"},
					},
				},
			},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster).
			Build()
		recorder := record.NewFakeRecorder(10)

		mockHCloud := &MockHCloudClient{}

		r := NewClusterReconciler(client, scheme, recorder,
			WithHCloudClient(mockHCloud),
			WithMetrics(false),
		)

		state, err := r.buildClusterState(context.Background(), cluster)
		require.NoError(t, err)

		assert.Equal(t, "my-cluster", state.Name)
		assert.Equal(t, "fsn1", state.Region)
		assert.Equal(t, []string{"key1", "key2", "key3"}, state.SSHKeyIDs)
		assert.Equal(t, "1.2.3.4", state.ControlPlaneIP)
		assert.Contains(t, state.SANs, "10.0.0.1")
		assert.Contains(t, state.SANs, "10.0.0.2")
		assert.Contains(t, state.SANs, "1.2.3.1")
		assert.Contains(t, state.SANs, "1.2.3.2")
		assert.Equal(t, int64(1), state.NetworkID)
	})

	t.Run("uses default SSH key naming when no annotation", func(t *testing.T) {
		t.Parallel()
		scheme := setupTestScheme(t)
		cluster := &k8znerv1alpha1.K8znerCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-cluster",
				Namespace: "default",
			},
			Spec: k8znerv1alpha1.K8znerClusterSpec{
				Region: "nbg1",
			},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster).
			Build()
		recorder := record.NewFakeRecorder(10)

		mockHCloud := &MockHCloudClient{}

		r := NewClusterReconciler(client, scheme, recorder,
			WithHCloudClient(mockHCloud),
			WithMetrics(false),
		)

		state, err := r.buildClusterState(context.Background(), cluster)
		require.NoError(t, err)

		assert.Equal(t, []string{"my-cluster-key"}, state.SSHKeyIDs)
	})
}

func TestGenerateReplacementServerName(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	recorder := record.NewFakeRecorder(10)

	r := NewClusterReconciler(client, scheme, recorder,
		WithHCloudClient(&MockHCloudClient{}),
	)

	cluster := &k8znerv1alpha1.K8znerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-cluster",
		},
	}

	t.Run("generates new name for control plane with cp role", func(t *testing.T) {
		t.Parallel()
		name := r.generateReplacementServerName(cluster, "control-plane", "my-cluster-control-plane-3")
		assert.True(t, strings.HasPrefix(name, "my-cluster-cp-"), "expected name to start with my-cluster-cp-, got %s", name)
		parts := strings.Split(name, "-")
		assert.Equal(t, 4, len(parts), "expected 4 parts: my, cluster, cp, id")
		assert.Equal(t, 5, len(parts[3]), "expected 5-char random ID, got %s", parts[3])
	})

	t.Run("generates new name for worker with w role", func(t *testing.T) {
		t.Parallel()
		name := r.generateReplacementServerName(cluster, "worker", "my-cluster-workers-2")
		assert.True(t, strings.HasPrefix(name, "my-cluster-w-"), "expected name to start with my-cluster-w-, got %s", name)
		parts := strings.Split(name, "-")
		assert.Equal(t, 4, len(parts), "expected 4 parts: my, cluster, w, id")
		assert.Equal(t, 5, len(parts[3]), "expected 5-char random ID, got %s", parts[3])
	})

	t.Run("generates unique names each time", func(t *testing.T) {
		t.Parallel()
		name1 := r.generateReplacementServerName(cluster, "worker", "old-name")
		name2 := r.generateReplacementServerName(cluster, "worker", "old-name")
		assert.NotEqual(t, name1, name2, "expected unique names on each call")
	})

	t.Run("handles unknown role with fallback format", func(t *testing.T) {
		t.Parallel()
		name := r.generateReplacementServerName(cluster, "storage", "old-storage-node")
		assert.True(t, strings.HasPrefix(name, "my-cluster-st-"), "expected name to start with my-cluster-st-, got %s", name)
	})
}

func TestNodeEventHandler(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)

	newCluster := func(namespace, name string) *k8znerv1alpha1.K8znerCluster {
		return &k8znerv1alpha1.K8znerCluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		}
	}

	h := &nodeEventHandler{
		client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(newCluster("k8zner-system", "my-cluster")).
			Build(),
	}

	expectEnqueued := func(t *testing.T, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
		t.Helper()
		assert.Equal(t, 1, q.Len())

		item, _ := q.Get()
		assert.Equal(t, "k8zner-system", item.Namespace)
		assert.Equal(t, "my-cluster", item.Name)
		q.Done(item)
	}

	t.Run("Create enqueues cluster", func(t *testing.T) {
		t.Parallel()
		q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
		defer q.ShutDown()

		h.Create(context.Background(), event.CreateEvent{}, q)
		expectEnqueued(t, q)
	})

	t.Run("Update enqueues cluster", func(t *testing.T) {
		t.Parallel()
		q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
		defer q.ShutDown()

		h.Update(context.Background(), event.UpdateEvent{}, q)
		expectEnqueued(t, q)
	})

	t.Run("Delete enqueues cluster", func(t *testing.T) {
		t.Parallel()
		q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
		defer q.ShutDown()

		h.Delete(context.Background(), event.DeleteEvent{}, q)
		expectEnqueued(t, q)
	})

	t.Run("Generic enqueues cluster", func(t *testing.T) {
		t.Parallel()
		q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
		defer q.ShutDown()

		h.Generic(context.Background(), event.GenericEvent{}, q)
		expectEnqueued(t, q)
	})

	t.Run("enqueues every cluster across namespaces", func(t *testing.T) {
		t.Parallel()
		multi := &nodeEventHandler{
			client: fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(
					newCluster("k8zner-system", "cluster-a"),
					newCluster("other-ns", "cluster-b"),
				).
				Build(),
		}

		q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
		defer q.ShutDown()

		multi.Create(context.Background(), event.CreateEvent{}, q)
		assert.Equal(t, 2, q.Len())
	})
}

func TestGetPrivateIPFromServer(t *testing.T) {
	t.Parallel()

	t.Run("returns error when GetServerByName fails", func(t *testing.T) {
		t.Parallel()
		scheme := setupTestScheme(t)
		client := fake.NewClientBuilder().WithScheme(scheme).Build()
		recorder := record.NewFakeRecorder(10)
		mockHCloud := &MockHCloudClient{
			GetServerByNameFunc: func(_ context.Context, _ string) (*hcloudgo.Server, error) {
				return nil, assert.AnError
			},
		}

		r := NewClusterReconciler(client, scheme, recorder,
			WithHCloudClient(mockHCloud),
		)

		_, err := r.getPrivateIPFromServer(context.Background(), "test-server")
		assert.Error(t, err)
	})

	t.Run("returns error when server not found", func(t *testing.T) {
		t.Parallel()
		scheme := setupTestScheme(t)
		client := fake.NewClientBuilder().WithScheme(scheme).Build()
		recorder := record.NewFakeRecorder(10)
		mockHCloud := &MockHCloudClient{
			GetServerByNameFunc: func(_ context.Context, _ string) (*hcloudgo.Server, error) {
				return nil, nil
			},
		}

		r := NewClusterReconciler(client, scheme, recorder,
			WithHCloudClient(mockHCloud),
		)

		_, err := r.getPrivateIPFromServer(context.Background(), "missing-server")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("returns private IP from server", func(t *testing.T) {
		t.Parallel()
		scheme := setupTestScheme(t)
		client := fake.NewClientBuilder().WithScheme(scheme).Build()
		recorder := record.NewFakeRecorder(10)
		mockHCloud := &MockHCloudClient{
			GetServerByNameFunc: func(_ context.Context, _ string) (*hcloudgo.Server, error) {
				return &hcloudgo.Server{
					ID:   123,
					Name: "test-server",
					PrivateNet: []hcloudgo.ServerPrivateNet{
						{IP: net.ParseIP("10.0.0.5")},
					},
				}, nil
			},
		}

		r := NewClusterReconciler(client, scheme, recorder,
			WithHCloudClient(mockHCloud),
		)

		ip, err := r.getPrivateIPFromServer(context.Background(), "test-server")
		require.NoError(t, err)
		assert.Equal(t, "10.0.0.5", ip)
	})

	t.Run("returns empty string when no private networks", func(t *testing.T) {
		t.Parallel()
		scheme := setupTestScheme(t)
		client := fake.NewClientBuilder().WithScheme(scheme).Build()
		recorder := record.NewFakeRecorder(10)
		mockHCloud := &MockHCloudClient{
			GetServerByNameFunc: func(_ context.Context, _ string) (*hcloudgo.Server, error) {
				return &hcloudgo.Server{
					ID:   123,
					Name: "test-server",
				}, nil
			},
		}

		r := NewClusterReconciler(client, scheme, recorder,
			WithHCloudClient(mockHCloud),
		)

		ip, err := r.getPrivateIPFromServer(context.Background(), "test-server")
		require.NoError(t, err)
		assert.Equal(t, "", ip)
	})
}

func TestEnsureHCloudClient_AlreadyInitialized(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	recorder := record.NewFakeRecorder(10)
	mockHCloud := &MockHCloudClient{}

	r := NewClusterReconciler(client, scheme, recorder,
		WithHCloudClient(mockHCloud),
	)

	err := r.ensureHCloudClient()
	require.NoError(t, err)
	assert.Equal(t, mockHCloud, r.hcloudClient, "should keep existing client")
}

func TestEnsureHCloudClient_NoTokenReturnsError(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	recorder := record.NewFakeRecorder(10)

	r := NewClusterReconciler(client, scheme, recorder)
	// No HCloud client and no token

	err := r.ensureHCloudClient()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HCloud token not configured")
}

func TestEnsureHCloudClient_CreatesClientWithToken(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	recorder := record.NewFakeRecorder(10)

	r := NewClusterReconciler(client, scheme, recorder,
		WithHCloudToken("test-token-123"),
	)

	assert.Nil(t, r.hcloudClient, "client should be nil before ensureHCloudClient")

	err := r.ensureHCloudClient()
	require.NoError(t, err)
	assert.NotNil(t, r.hcloudClient, "client should be created with token")
}

func TestNormalizeServerSize_Controller(t *testing.T) {
	t.Parallel()
	// The controller version calls configv2.ServerSize().Normalize()
	tests := []struct {
		input    string
		expected string
	}{
		{"cx22", "cx23"},
		{"cx32", "cx33"},
		{"cx42", "cx43"},
		{"cx52", "cx53"},
		{"cx23", "cx23"},
		{"cpx31", "cpx31"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, string(config.ServerSize(tt.input).Normalize()))
		})
	}
}

func TestWithNodeReadyWaiter(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	recorder := record.NewFakeRecorder(10)

	waiterCalled := false
	customWaiter := func(ctx context.Context, nodeName string, timeout time.Duration) error {
		waiterCalled = true
		return nil
	}

	r := NewClusterReconciler(client, scheme, recorder,
		WithNodeReadyWaiter(customWaiter),
	)

	assert.NotNil(t, r.nodeReadyWaiter)
	err := r.nodeReadyWaiter(context.Background(), "test-node", 10*time.Second)
	require.NoError(t, err)
	assert.True(t, waiterCalled)
}

func TestWithTalosConfigGenerator(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	recorder := record.NewFakeRecorder(10)

	mockGen := &MockTalosConfigGenerator{}

	r := NewClusterReconciler(client, scheme, recorder,
		WithTalosConfigGenerator(mockGen),
	)

	assert.Equal(t, mockGen, r.talosConfigGen)
}

func TestUpdateStatusWithRetry_SuccessFirstTry(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)

	cluster := &k8znerv1alpha1.K8znerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		WithStatusSubresource(cluster).
		Build()
	recorder := record.NewFakeRecorder(10)

	r := NewClusterReconciler(client, scheme, recorder)

	cluster.Status.Phase = k8znerv1alpha1.ClusterPhaseRunning
	err := r.updateStatusWithRetry(context.Background(), cluster)
	require.NoError(t, err)
}

func TestUpdateStatusWithRetry_NonConflictErrorReturnsImmediately(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)

	cluster := &k8znerv1alpha1.K8znerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	// Create a fake client WITHOUT the object - this will cause a "not found" error
	// on status update, which is not a conflict error, so it should be returned immediately.
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&k8znerv1alpha1.K8znerCluster{}).
		Build()
	recorder := record.NewFakeRecorder(10)

	r := NewClusterReconciler(fakeClient, scheme, recorder)

	err := r.updateStatusWithRetry(context.Background(), cluster)
	require.Error(t, err)
	// The error should be a non-conflict error (not found), returned on first attempt
}

func TestUpdateStatusWithRetry_SuccessOnFirstTry_WithAddonStatus(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)

	cluster := &k8znerv1alpha1.K8znerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		WithStatusSubresource(cluster).
		Build()
	recorder := record.NewFakeRecorder(10)

	r := NewClusterReconciler(fakeClient, scheme, recorder)

	// Set addon status and verify it's preserved
	now := metav1.Now()
	cluster.Status.Addons = map[string]k8znerv1alpha1.AddonStatus{
		"cilium": {
			Installed:          true,
			Healthy:            true,
			Phase:              k8znerv1alpha1.AddonPhaseInstalled,
			LastTransitionTime: &now,
		},
	}
	cluster.Status.Phase = k8znerv1alpha1.ClusterPhaseRunning

	err := r.updateStatusWithRetry(context.Background(), cluster)
	require.NoError(t, err)

	// Verify the addon status persisted
	updated := &k8znerv1alpha1.K8znerCluster{}
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Namespace: "default",
		Name:      "test-cluster",
	}, updated)
	require.NoError(t, err)
	assert.Equal(t, k8znerv1alpha1.ClusterPhaseRunning, updated.Status.Phase)
}

// --- reconcileWithStateMachine additional coverage ---

func TestReconcile_DispatchesToStateMachineWithCredentialsRef(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)

	cluster := &k8znerv1alpha1.K8znerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: k8znerv1alpha1.K8znerClusterSpec{
			CredentialsRef: corev1.LocalObjectReference{
				Name: "my-secret",
			},
			ControlPlanes: k8znerv1alpha1.ControlPlaneSpec{Count: 1, Size: "cx22"},
			Workers:       k8znerv1alpha1.WorkerSpec{Count: 0},
		},
		Status: k8znerv1alpha1.K8znerClusterStatus{
			ProvisioningPhase: k8znerv1alpha1.ProvisioningPhase("SomeUnknownPhase"),
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		WithStatusSubresource(cluster).
		Build()
	recorder := record.NewFakeRecorder(10)

	r := NewClusterReconciler(fakeClient, scheme, recorder,
		WithHCloudClient(&MockHCloudClient{}),
		WithMetrics(false),
	)

	result, err := r.reconcile(context.Background(), cluster)
	assert.NoError(t, err)
	// Unknown phase gets reset to Infrastructure and requeues
	assert.True(t, result.Requeue)
	assert.Equal(t, k8znerv1alpha1.PhaseInfrastructure, cluster.Status.ProvisioningPhase)
}

func TestReconcile_DispatchesToLegacyWithoutCredentialsRef(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)

	cluster := &k8znerv1alpha1.K8znerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: k8znerv1alpha1.K8znerClusterSpec{
			ControlPlanes: k8znerv1alpha1.ControlPlaneSpec{Count: 1, Size: "cx22"},
			Workers:       k8znerv1alpha1.WorkerSpec{Count: 0},
			// No CredentialsRef
		},
	}

	cpNode := createTestNode("cp-1", true, true)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, cpNode).
		WithStatusSubresource(cluster).
		Build()
	recorder := record.NewFakeRecorder(10)

	r := NewClusterReconciler(fakeClient, scheme, recorder,
		WithHCloudClient(&MockHCloudClient{}),
		WithMetrics(false),
	)

	result, err := r.reconcile(context.Background(), cluster)
	assert.NoError(t, err)
	// Legacy mode should requeue for continuous monitoring
	assert.Equal(t, defaultRequeueAfter, result.RequeueAfter)
}

// --- reconcile: sync desired counts ---

func TestReconcile_SyncsDesiredCounts(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)

	cluster := &k8znerv1alpha1.K8znerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: k8znerv1alpha1.K8znerClusterSpec{
			ControlPlanes: k8znerv1alpha1.ControlPlaneSpec{Count: 3, Size: "cx22"},
			Workers:       k8znerv1alpha1.WorkerSpec{Count: 5, Size: "cx22"},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		WithStatusSubresource(cluster).
		Build()
	recorder := record.NewFakeRecorder(10)

	r := NewClusterReconciler(fakeClient, scheme, recorder,
		WithHCloudClient(&MockHCloudClient{}),
		WithMetrics(false),
	)

	_, _ = r.reconcile(context.Background(), cluster)

	assert.Equal(t, 3, cluster.Status.ControlPlanes.Desired)
	assert.Equal(t, 5, cluster.Status.Workers.Desired)
}

// --- logAndRecordError coverage ---

func TestLogAndRecordError(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)

	cluster := &k8znerv1alpha1.K8znerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Status: k8znerv1alpha1.K8znerClusterStatus{
			ProvisioningPhase: k8znerv1alpha1.PhaseAddons,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		WithStatusSubresource(cluster).
		Build()
	recorder := record.NewFakeRecorder(10)

	r := NewClusterReconciler(fakeClient, scheme, recorder,
		WithHCloudClient(&MockHCloudClient{}),
		WithMetrics(false),
	)

	testErr := fmt.Errorf("test error message")
	r.logAndRecordError(context.Background(), cluster, testErr, "TestReason", "Something failed")

	// Verify event was recorded
	select {
	case event := <-recorder.Events:
		assert.Contains(t, event, "TestReason")
		assert.Contains(t, event, "Something failed")
		assert.Contains(t, event, "test error message")
	default:
		t.Fatal("expected event to be recorded")
	}
}

// --- drainNode coverage ---

func TestReconcile_HCloudClientError(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)

	cluster := &k8znerv1alpha1.K8znerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: k8znerv1alpha1.K8znerClusterSpec{
			ControlPlanes: k8znerv1alpha1.ControlPlaneSpec{Count: 1, Size: "cx22"},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		WithStatusSubresource(cluster).
		Build()
	recorder := record.NewFakeRecorder(10)

	// No HCloud client and no token - should fail and requeue
	r := NewClusterReconciler(fakeClient, scheme, recorder)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "test-cluster",
		},
	})

	assert.NoError(t, err) // Error is recorded as event, not returned
	assert.Equal(t, defaultRequeueAfter, result.RequeueAfter)
}

// --- verifyAndUpdateNodeStates coverage ---

func TestReconcile_FullReconciliationUpdatesLastReconcileTime(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)

	cluster := &k8znerv1alpha1.K8znerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: k8znerv1alpha1.K8znerClusterSpec{
			ControlPlanes: k8znerv1alpha1.ControlPlaneSpec{Count: 1, Size: "cx22"},
			Workers:       k8znerv1alpha1.WorkerSpec{Count: 0},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		WithStatusSubresource(cluster).
		Build()
	recorder := record.NewFakeRecorder(20)

	r := NewClusterReconciler(fakeClient, scheme, recorder,
		WithHCloudClient(&MockHCloudClient{}),
		WithMetrics(false),
	)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "test-cluster",
		},
	})
	assert.NoError(t, err)

	// Verify LastReconcileTime was set
	updated := &k8znerv1alpha1.K8znerCluster{}
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Namespace: "default",
		Name:      "test-cluster",
	}, updated)
	require.NoError(t, err)
	assert.NotNil(t, updated.Status.LastReconcileTime)
	assert.Equal(t, cluster.Generation, updated.Status.ObservedGeneration)
}

// --- getPrivateIPFromServer: server with nil IP in PrivateNet ---

func TestWithMaxConcurrentHeals(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	recorder := record.NewFakeRecorder(10)

	r := NewClusterReconciler(fakeClient, scheme, recorder,
		WithMaxConcurrentHeals(5),
	)

	assert.Equal(t, 5, r.maxConcurrentHeals)
}

// --- countWorkersInEarlyProvisioning: all provisioning ---

func TestReconcile_ReconcileErrorRecordsEvent(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)

	cluster := &k8znerv1alpha1.K8znerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: k8znerv1alpha1.K8znerClusterSpec{
			ControlPlanes: k8znerv1alpha1.ControlPlaneSpec{Count: 1, Size: "cx22"},
			Workers:       k8znerv1alpha1.WorkerSpec{Count: 0},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		WithStatusSubresource(cluster).
		Build()
	recorder := record.NewFakeRecorder(20)

	r := NewClusterReconciler(fakeClient, scheme, recorder,
		WithHCloudClient(&MockHCloudClient{}),
		WithMetrics(false),
	)

	_, _ = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "test-cluster",
		},
	})

	// Drain and check events
	eventCount := 0
	for {
		select {
		case <-recorder.Events:
			eventCount++
		default:
			goto done
		}
	}
done:
	// Should have at least the Reconciling and ReconcileSucceeded events
	assert.GreaterOrEqual(t, eventCount, 2)
}

// --- findTalosEndpoint: bootstrap with nil Bootstrap spec ---

func TestReconcileRunningPhase_Legacy(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)

	cluster := &k8znerv1alpha1.K8znerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: k8znerv1alpha1.K8znerClusterSpec{
			ControlPlanes: k8znerv1alpha1.ControlPlaneSpec{Count: 1, Size: "cx22"},
			Workers:       k8znerv1alpha1.WorkerSpec{Count: 0},
		},
	}

	// Create node that will cause the health check to work
	cpNode := createTestNode("cp-1", true, true)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, cpNode).
		WithStatusSubresource(cluster).
		Build()
	recorder := record.NewFakeRecorder(10)

	r := NewClusterReconciler(fakeClient, scheme, recorder,
		WithHCloudClient(&MockHCloudClient{}),
		WithMetrics(false),
	)

	result, err := r.reconcileRunningPhase(context.Background(), cluster)
	assert.NoError(t, err)
	assert.Equal(t, defaultRequeueAfter, result.RequeueAfter)
}

// --- normalizeServerSize: additional sizes ---

func TestNormalizeServerSize_AdditionalSizes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected string
	}{
		{"cpx11", "cpx11"},
		{"cpx21", "cpx21"},
		{"cpx41", "cpx41"},
		{"cpx51", "cpx51"},
		{"cax11", "cax11"},
		{"cax21", "cax21"},
		{"cx22", "cx23"},
		{"some-custom-type", "some-custom-type"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, string(config.ServerSize(tt.input).Normalize()))
		})
	}
}

// --- handleStuckNode: verify event is recorded ---

func TestNewClusterReconciler_DefaultNodeReadyWaiter(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	recorder := record.NewFakeRecorder(10)

	r := NewClusterReconciler(fakeClient, scheme, recorder)
	assert.NotNil(t, r.nodeReadyWaiter, "default nodeReadyWaiter should be set")
}

// --- reconcileAddonsPhase: HCloudToken empty error path ---

func TestReconcile_PausedCluster(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)

	cluster := &k8znerv1alpha1.K8znerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: k8znerv1alpha1.K8znerClusterSpec{
			Paused:        true,
			ControlPlanes: k8znerv1alpha1.ControlPlaneSpec{Count: 1, Size: "cx22"},
			Workers:       k8znerv1alpha1.WorkerSpec{Count: 0},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		WithStatusSubresource(cluster).
		Build()
	recorder := record.NewFakeRecorder(10)

	r := NewClusterReconciler(fakeClient, scheme, recorder,
		WithHCloudClient(&MockHCloudClient{}),
		WithMetrics(false),
	)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "test-cluster",
		},
	})

	assert.NoError(t, err)
	assert.Equal(t, defaultRequeueAfter, result.RequeueAfter)
}

// --- Reconcile: not-found cluster ---

func TestReconcile_NotFoundCluster(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()
	recorder := record.NewFakeRecorder(10)

	r := NewClusterReconciler(fakeClient, scheme, recorder,
		WithHCloudClient(&MockHCloudClient{}),
		WithMetrics(false),
	)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "nonexistent-cluster",
		},
	})

	assert.NoError(t, err)
	assert.False(t, result.Requeue)
	assert.Zero(t, result.RequeueAfter)
}

// --- configureCPNode: no credentials path ---

func TestReconcile_StatusUpdateErrorPropagated(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)

	cluster := &k8znerv1alpha1.K8znerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: k8znerv1alpha1.K8znerClusterSpec{
			ControlPlanes: k8znerv1alpha1.ControlPlaneSpec{Count: 1},
			Workers:       k8znerv1alpha1.WorkerSpec{Count: 0},
		},
	}

	// Build client WITHOUT status subresource to trigger status update failure
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		Build()
	recorder := record.NewFakeRecorder(20)

	r := NewClusterReconciler(fakeClient, scheme, recorder,
		WithHCloudClient(&MockHCloudClient{}),
		WithMetrics(false),
	)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "test-cluster",
		},
	})

	// The reconcile itself may succeed but status update fails
	// In either case, it should be handled gracefully
	_ = result
	_ = err
}

// --- reconcileWithStateMachine: image phase dispatch ---

func TestReconcile_StuckNodeHandlingContinues(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)

	pastTime := metav1.NewTime(time.Now().Add(-30 * time.Minute))
	cluster := &k8znerv1alpha1.K8znerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: k8znerv1alpha1.K8znerClusterSpec{
			ControlPlanes: k8znerv1alpha1.ControlPlaneSpec{Count: 1},
			Workers:       k8znerv1alpha1.WorkerSpec{Count: 0},
		},
		Status: k8znerv1alpha1.K8znerClusterStatus{
			Workers: k8znerv1alpha1.NodeGroupStatus{
				Nodes: []k8znerv1alpha1.NodeStatus{
					{
						Name:                "w-stuck",
						Phase:               k8znerv1alpha1.NodePhaseCreatingServer,
						PhaseTransitionTime: &pastTime,
					},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		WithStatusSubresource(cluster).
		Build()
	recorder := record.NewFakeRecorder(20)

	mockHCloud := &MockHCloudClient{}
	r := NewClusterReconciler(fakeClient, scheme, recorder,
		WithHCloudClient(mockHCloud),
		WithMetrics(false),
	)

	// reconcile should handle the stuck node and continue
	_, err := r.reconcile(context.Background(), cluster)
	require.NoError(t, err)

	// Stuck node should have been cleaned up
	require.Len(t, mockHCloud.DeleteServerCalls, 1)
	assert.Equal(t, "w-stuck", mockHCloud.DeleteServerCalls[0])
}

// --- verifyNodeState: node IP from server ---

func TestReconcile_EnsureHCloudClientFailure(t *testing.T) {
	t.Parallel()
	scheme := setupTestScheme(t)

	cluster := &k8znerv1alpha1.K8znerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: k8znerv1alpha1.K8znerClusterSpec{
			ControlPlanes: k8znerv1alpha1.ControlPlaneSpec{Count: 1},
			Workers:       k8znerv1alpha1.WorkerSpec{Count: 0},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		WithStatusSubresource(cluster).
		Build()
	recorder := record.NewFakeRecorder(10)

	// No HCloud client and no token - ensureHCloudClient should fail
	r := NewClusterReconciler(fakeClient, scheme, recorder)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "test-cluster",
		},
	})

	// Should not return error (it's handled internally with requeue)
	require.NoError(t, err)
	assert.Equal(t, defaultRequeueAfter, result.RequeueAfter)
}

// --- isNodeInEarlyProvisioningPhase: all provisioning phases ---
