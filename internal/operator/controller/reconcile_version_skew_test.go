package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	k8znerv1alpha1 "github.com/milankappen/k8zner/api/v1alpha1"
)

func TestReconcileVersionSkew(t *testing.T) {
	t.Parallel()

	nodeWithKubelet := func(name, kubeletVersion string) *corev1.Node {
		node := createTestNode(name, false, true)
		node.Status.NodeInfo.KubeletVersion = kubeletVersion
		return node
	}

	newCluster := func(specVersion string) *k8znerv1alpha1.K8znerCluster {
		return &k8znerv1alpha1.K8znerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"},
			Spec: k8znerv1alpha1.K8znerClusterSpec{
				Kubernetes: k8znerv1alpha1.KubernetesSpec{Version: specVersion},
			},
		}
	}

	run := func(t *testing.T, cluster *k8znerv1alpha1.K8znerCluster, nodes ...*corev1.Node) {
		t.Helper()
		scheme := setupTestScheme(t)
		builder := fake.NewClientBuilder().WithScheme(scheme)
		for _, n := range nodes {
			builder = builder.WithObjects(n)
		}
		r := NewClusterReconciler(builder.Build(), scheme, record.NewFakeRecorder(10),
			WithHCloudClient(&MockHCloudClient{}))
		r.reconcileVersionSkew(context.Background(), cluster)
	}

	t.Run("matching versions set UpToDate true", func(t *testing.T) {
		t.Parallel()
		cluster := newCluster("1.32.0")
		run(t, cluster,
			nodeWithKubelet("node-1", "v1.32.0"),
			nodeWithKubelet("node-2", "v1.32.0"),
		)

		cond := meta.FindStatusCondition(cluster.Status.Conditions, k8znerv1alpha1.ConditionUpToDate)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
	})

	t.Run("outdated kubelet sets UpToDate false with node count", func(t *testing.T) {
		t.Parallel()
		cluster := newCluster("1.32.0")
		run(t, cluster,
			nodeWithKubelet("node-1", "v1.32.0"),
			nodeWithKubelet("node-2", "v1.31.4"),
		)

		cond := meta.FindStatusCondition(cluster.Status.Conditions, k8znerv1alpha1.ConditionUpToDate)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, "KubernetesUpgradePending", cond.Reason)
		assert.Contains(t, cond.Message, "1/2")
		assert.Contains(t, cond.Message, "1.32.0")
	})

	t.Run("no spec version means no condition", func(t *testing.T) {
		t.Parallel()
		cluster := newCluster("")
		run(t, cluster, nodeWithKubelet("node-1", "v1.32.0"))

		assert.Nil(t, meta.FindStatusCondition(cluster.Status.Conditions, k8znerv1alpha1.ConditionUpToDate))
	})

	t.Run("no nodes yet leaves condition untouched", func(t *testing.T) {
		t.Parallel()
		cluster := newCluster("1.32.0")
		run(t, cluster)

		assert.Nil(t, meta.FindStatusCondition(cluster.Status.Conditions, k8znerv1alpha1.ConditionUpToDate))
	})
}
