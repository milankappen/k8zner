package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	k8znerv1alpha1 "github.com/milankappen/k8zner/api/v1alpha1"
	"github.com/milankappen/k8zner/internal/addons"
	"github.com/milankappen/k8zner/internal/config"
)

func TestPlanAddonReconcile(t *testing.T) {
	t.Parallel()

	installed := func(version string) k8znerv1alpha1.AddonStatus {
		return k8znerv1alpha1.AddonStatus{
			Installed: true,
			Phase:     k8znerv1alpha1.AddonPhaseInstalled,
			Version:   version,
		}
	}

	steps := []addons.AddonStep{
		{Name: addons.StepCCM, Order: 2, Version: "1.29.0"},
		{Name: addons.StepTraefik, Order: 6, Version: "39.0.0"},
	}

	t.Run("everything current means nothing to do", func(t *testing.T) {
		t.Parallel()
		statuses := map[string]k8znerv1alpha1.AddonStatus{
			addons.StepCCM:     installed("1.29.0"),
			addons.StepTraefik: installed("39.0.0"),
		}

		backfills, next := planAddonReconcile(steps, statuses)
		assert.Empty(t, backfills)
		assert.Nil(t, next)
	})

	t.Run("empty recorded version is backfilled without reinstall", func(t *testing.T) {
		t.Parallel()
		statuses := map[string]k8znerv1alpha1.AddonStatus{
			addons.StepCCM:     installed(""),
			addons.StepTraefik: installed("39.0.0"),
		}

		backfills, next := planAddonReconcile(steps, statuses)
		assert.Equal(t, map[string]string{addons.StepCCM: "1.29.0"}, backfills)
		assert.Nil(t, next)
	})

	t.Run("version mismatch selects the addon for upgrade", func(t *testing.T) {
		t.Parallel()
		statuses := map[string]k8znerv1alpha1.AddonStatus{
			addons.StepCCM:     installed("1.29.0"),
			addons.StepTraefik: installed("38.0.0"),
		}

		backfills, next := planAddonReconcile(steps, statuses)
		assert.Empty(t, backfills)
		require.NotNil(t, next)
		assert.Equal(t, addons.StepTraefik, next.Name)
	})

	t.Run("only the first outdated addon is selected per reconcile", func(t *testing.T) {
		t.Parallel()
		statuses := map[string]k8znerv1alpha1.AddonStatus{
			addons.StepCCM:     installed("1.0.0"),
			addons.StepTraefik: installed("38.0.0"),
		}

		_, next := planAddonReconcile(steps, statuses)
		require.NotNil(t, next)
		assert.Equal(t, addons.StepCCM, next.Name, "steps are ordered; CCM upgrades first")
	})

	t.Run("newly enabled addon with no status entry is installed", func(t *testing.T) {
		t.Parallel()
		statuses := map[string]k8znerv1alpha1.AddonStatus{
			addons.StepCCM: installed("1.29.0"),
		}

		backfills, next := planAddonReconcile(steps, statuses)
		assert.Empty(t, backfills)
		require.NotNil(t, next)
		assert.Equal(t, addons.StepTraefik, next.Name)
	})

	t.Run("previously failed upgrade is retried", func(t *testing.T) {
		t.Parallel()
		statuses := map[string]k8znerv1alpha1.AddonStatus{
			addons.StepCCM: installed("1.29.0"),
			addons.StepTraefik: {
				Installed:  true,
				Phase:      k8znerv1alpha1.AddonPhaseFailed,
				Version:    "38.0.0",
				RetryCount: 2,
			},
		}

		_, next := planAddonReconcile(steps, statuses)
		require.NotNil(t, next)
		assert.Equal(t, addons.StepTraefik, next.Name)
	})

	t.Run("failed addon at the desired version is still repaired", func(t *testing.T) {
		t.Parallel()
		// A half-applied upgrade can leave Phase=Failed with the recorded
		// version equal to the (reverted) desired version; re-applying the
		// manifests is the only way back to a converged state.
		statuses := map[string]k8znerv1alpha1.AddonStatus{
			addons.StepCCM: installed("1.29.0"),
			addons.StepTraefik: {
				Installed: true,
				Phase:     k8znerv1alpha1.AddonPhaseFailed,
				Version:   "39.0.0",
			},
		}

		_, next := planAddonReconcile(steps, statuses)
		require.NotNil(t, next)
		assert.Equal(t, addons.StepTraefik, next.Name)
	})

	t.Run("addon mid-install during provisioning is left alone", func(t *testing.T) {
		t.Parallel()
		statuses := map[string]k8znerv1alpha1.AddonStatus{
			addons.StepCCM: {
				Phase:   k8znerv1alpha1.AddonPhaseInstalling,
				Version: "",
			},
			addons.StepTraefik: installed("39.0.0"),
		}

		backfills, next := planAddonReconcile(steps, statuses)
		assert.Empty(t, backfills)
		assert.Nil(t, next)
	})
}

func TestApplyAddonPlan(t *testing.T) {
	t.Parallel()

	newReconciler := func(t *testing.T, installer func(ctx context.Context, name string, cfg *config.Config, kubeconfig []byte, networkID int64) error) (*ClusterReconciler, *k8znerv1alpha1.K8znerCluster) {
		t.Helper()
		scheme := setupTestScheme(t)
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := NewClusterReconciler(c, scheme, record.NewFakeRecorder(20),
			WithHCloudClient(&MockHCloudClient{}),
			WithAddonInstaller(installer),
		)
		cluster := &k8znerv1alpha1.K8znerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"},
			Status: k8znerv1alpha1.K8znerClusterStatus{
				Addons: map[string]k8znerv1alpha1.AddonStatus{},
			},
		}
		return r, cluster
	}

	steps := []addons.AddonStep{
		{Name: addons.StepTraefik, Order: 6, Version: "39.0.0"},
	}

	t.Run("backfills versions without invoking the installer", func(t *testing.T) {
		t.Parallel()
		installs := 0
		r, cluster := newReconciler(t, func(context.Context, string, *config.Config, []byte, int64) error {
			installs++
			return nil
		})
		cluster.Status.Addons[addons.StepTraefik] = k8znerv1alpha1.AddonStatus{
			Installed: true,
			Phase:     k8znerv1alpha1.AddonPhaseInstalled,
		}

		backfills := map[string]string{addons.StepTraefik: "39.0.0"}
		_, handled := r.applyAddonPlan(context.Background(), cluster, backfills, nil, &config.Config{}, nil, 0)

		assert.True(t, handled)
		assert.Zero(t, installs)
		assert.Equal(t, "39.0.0", cluster.Status.Addons[addons.StepTraefik].Version)
	})

	t.Run("successful upgrade records new version and resets retries", func(t *testing.T) {
		t.Parallel()
		var installedName string
		r, cluster := newReconciler(t, func(_ context.Context, name string, _ *config.Config, _ []byte, _ int64) error {
			installedName = name
			return nil
		})
		cluster.Status.Addons[addons.StepTraefik] = k8znerv1alpha1.AddonStatus{
			Installed:  true,
			Phase:      k8znerv1alpha1.AddonPhaseFailed,
			Version:    "38.0.0",
			RetryCount: 3,
		}

		result, handled := r.applyAddonPlan(context.Background(), cluster, nil, &steps[0], &config.Config{}, nil, 0)

		assert.True(t, handled)
		assert.Equal(t, addons.StepTraefik, installedName)
		got := cluster.Status.Addons[addons.StepTraefik]
		assert.Equal(t, k8znerv1alpha1.AddonPhaseInstalled, got.Phase)
		assert.Equal(t, "39.0.0", got.Version)
		assert.Zero(t, got.RetryCount)
		assert.True(t, got.Installed)
		assert.True(t, result.Requeue || result.RequeueAfter > 0)
	})

	t.Run("failed upgrade marks addon failed with backoff", func(t *testing.T) {
		t.Parallel()
		r, cluster := newReconciler(t, func(context.Context, string, *config.Config, []byte, int64) error {
			return fmt.Errorf("chart render exploded")
		})
		cluster.Status.Addons[addons.StepTraefik] = k8znerv1alpha1.AddonStatus{
			Installed: true,
			Phase:     k8znerv1alpha1.AddonPhaseInstalled,
			Version:   "38.0.0",
		}

		result, handled := r.applyAddonPlan(context.Background(), cluster, nil, &steps[0], &config.Config{}, nil, 0)

		assert.True(t, handled)
		got := cluster.Status.Addons[addons.StepTraefik]
		assert.Equal(t, k8znerv1alpha1.AddonPhaseFailed, got.Phase)
		assert.Equal(t, "38.0.0", got.Version, "version must reflect what is actually running")
		assert.Equal(t, 1, got.RetryCount)
		assert.Contains(t, got.Message, "chart render exploded")
		assert.Greater(t, result.RequeueAfter, time.Duration(0))
	})

	t.Run("nothing to do returns handled=false", func(t *testing.T) {
		t.Parallel()
		r, cluster := newReconciler(t, func(context.Context, string, *config.Config, []byte, int64) error {
			t.Error("installer must not be called")
			return nil
		})
		cluster.Status.Addons[addons.StepTraefik] = k8znerv1alpha1.AddonStatus{
			Installed: true,
			Phase:     k8znerv1alpha1.AddonPhaseInstalled,
			Version:   "39.0.0",
		}

		_, handled := r.applyAddonPlan(context.Background(), cluster, nil, nil, &config.Config{}, nil, 0)
		assert.False(t, handled)
	})
}

func TestAddonUpgradesGate(t *testing.T) {
	t.Parallel()

	t.Run("upgrades only proceed when all nodes are ready", func(t *testing.T) {
		t.Parallel()
		cluster := &k8znerv1alpha1.K8znerCluster{
			Status: k8znerv1alpha1.K8znerClusterStatus{
				ControlPlanes: k8znerv1alpha1.NodeGroupStatus{Desired: 3, Ready: 2},
				Workers:       k8znerv1alpha1.NodeGroupStatus{Desired: 2, Ready: 2},
			},
		}
		assert.False(t, clusterStableForUpgrades(cluster))

		cluster.Status.ControlPlanes.Ready = 3
		assert.True(t, clusterStableForUpgrades(cluster))
	})
}
