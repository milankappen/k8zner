package controller

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	k8znerv1alpha1 "github.com/milankappen/k8zner/api/v1alpha1"
	"github.com/milankappen/k8zner/internal/addons"
	"github.com/milankappen/k8zner/internal/config"
)

// Addon upgrade event reasons.
const (
	EventReasonAddonUpgrading     = "AddonUpgrading"
	EventReasonAddonUpgraded      = "AddonUpgraded"
	EventReasonAddonUpgradeFailed = "AddonUpgradeFailed"
)

// reconcileAddonUpgrades keeps installed addons converged on their desired
// versions while the cluster is running. It backfills missing version records,
// upgrades one outdated addon per reconcile (re-rendering and re-applying its
// manifests), and installs addons that were enabled in the spec after
// provisioning completed.
//
// Cilium is deliberately excluded: CNI upgrades affect every pod on the
// cluster and are not attempted automatically.
//
// Returns handled=true when it acted (the caller should requeue with the
// returned result instead of continuing).
func (r *ClusterReconciler) reconcileAddonUpgrades(ctx context.Context, cluster *k8znerv1alpha1.K8znerCluster) (ctrl.Result, bool) {
	logger := log.FromContext(ctx)

	// Never upgrade a cluster that is scaling, healing, or otherwise not at
	// full strength: converging versions can wait, quorum cannot.
	if !clusterStableForUpgrades(cluster) {
		return ctrl.Result{}, false
	}

	cfg, kubeconfig, networkID, err := r.prepareAddonInputs(ctx, cluster)
	if err != nil {
		// Non-fatal: upgrades are retried on the next periodic reconcile.
		logger.V(1).Info("skipping addon upgrade check", "reason", err.Error())
		return ctrl.Result{}, false
	}

	return r.applyAddonPlan(ctx, cluster, addons.EnabledSteps(cfg), cfg, kubeconfig, networkID)
}

// clusterStableForUpgrades reports whether all desired nodes are ready.
func clusterStableForUpgrades(cluster *k8znerv1alpha1.K8znerCluster) bool {
	return cluster.Status.ControlPlanes.Ready >= cluster.Status.ControlPlanes.Desired &&
		cluster.Status.Workers.Ready >= cluster.Status.Workers.Desired
}

// planAddonReconcile compares desired steps against recorded addon statuses.
// It returns version backfills (addons installed before version tracking
// existed: recorded as installed but with no version) and the first step that
// needs an install or upgrade. Backfills never trigger a reinstall — we assume
// the version that installed them is the one currently pinned.
func planAddonReconcile(steps []addons.AddonStep, statuses map[string]k8znerv1alpha1.AddonStatus) (map[string]string, *addons.AddonStep) {
	backfills := make(map[string]string)

	for i := range steps {
		step := steps[i]
		status, exists := statuses[step.Name]

		if !exists {
			// Enabled after provisioning completed — install it.
			return backfills, &step
		}

		switch status.Phase {
		case k8znerv1alpha1.AddonPhaseInstalled:
			if status.Version == "" {
				backfills[step.Name] = step.Version
				continue
			}
			if status.Version != step.Version {
				return backfills, &step
			}
		case k8znerv1alpha1.AddonPhaseFailed, k8znerv1alpha1.AddonPhaseUpgrading:
			if status.Version != step.Version {
				return backfills, &step
			}
		default:
			// Pending/Installing belong to the provisioning state machine.
		}
	}

	return backfills, nil
}

// applyAddonPlan executes the result of planAddonReconcile: it records version
// backfills and performs at most one addon install/upgrade per reconcile.
func (r *ClusterReconciler) applyAddonPlan(ctx context.Context, cluster *k8znerv1alpha1.K8znerCluster, steps []addons.AddonStep, cfg *config.Config, kubeconfig []byte, networkID int64) (ctrl.Result, bool) {
	logger := log.FromContext(ctx)

	if cluster.Status.Addons == nil {
		cluster.Status.Addons = make(map[string]k8znerv1alpha1.AddonStatus)
	}

	backfills, next := planAddonReconcile(steps, cluster.Status.Addons)

	for name, version := range backfills {
		status := cluster.Status.Addons[name]
		status.Version = version
		cluster.Status.Addons[name] = status
		logger.Info("backfilled addon version", "addon", name, "version", version)
	}

	if next == nil {
		if len(backfills) > 0 {
			return ctrl.Result{Requeue: true}, true
		}
		return ctrl.Result{}, false
	}

	previous := cluster.Status.Addons[next.Name]

	logger.Info("upgrading addon", "addon", next.Name,
		"from", previous.Version, "to", next.Version)
	r.Recorder.Eventf(cluster, corev1.EventTypeNormal, EventReasonAddonUpgrading,
		"Upgrading addon %s from %q to %q", next.Name, previous.Version, next.Version)

	now := metav1.Now()
	upgrading := previous
	upgrading.Phase = k8znerv1alpha1.AddonPhaseUpgrading
	upgrading.LastTransitionTime = &now
	cluster.Status.Addons[next.Name] = upgrading

	startedAt := metav1.Now()
	if err := r.addonInstaller(ctx, next.Name, cfg, kubeconfig, networkID); err != nil {
		r.logAndRecordError(ctx, cluster, err, EventReasonAddonUpgradeFailed,
			"Failed to upgrade addon: "+next.Name)
		recordPhaseError(cluster, next.Name, err.Error())

		failed := previous
		failed.Phase = k8znerv1alpha1.AddonPhaseFailed
		failed.RetryCount = previous.RetryCount + 1
		failed.Message = err.Error()
		failureTime := metav1.Now()
		failed.LastTransitionTime = &failureTime
		cluster.Status.Addons[next.Name] = failed

		return ctrl.Result{RequeueAfter: addonRetryBackoff(failed.RetryCount)}, true
	}

	finished := metav1.Now()
	cluster.Status.Addons[next.Name] = k8znerv1alpha1.AddonStatus{
		Installed:          true,
		Healthy:            true,
		Phase:              k8znerv1alpha1.AddonPhaseInstalled,
		Version:            next.Version,
		LastTransitionTime: &finished,
		InstallOrder:       next.Order,
		StartedAt:          &startedAt,
		Duration:           finished.Sub(startedAt.Time).Round(time.Second).String(),
	}

	logger.Info("addon upgraded", "addon", next.Name, "version", next.Version)
	r.Recorder.Eventf(cluster, corev1.EventTypeNormal, EventReasonAddonUpgraded,
		"Addon %s upgraded to %q", next.Name, next.Version)

	return ctrl.Result{Requeue: true}, true
}
