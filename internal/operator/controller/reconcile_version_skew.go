package controller

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	k8znerv1alpha1 "github.com/milankappen/k8zner/api/v1alpha1"
)

// reconcileVersionSkew surfaces pending Kubernetes upgrades as the UpToDate
// condition by comparing each node's kubelet version against the spec. It is
// detection only: node upgrades are rolled out explicitly via `k8zner apply`
// (rebuilding the image and replacing nodes), never by the operator on its own.
func (r *ClusterReconciler) reconcileVersionSkew(ctx context.Context, cluster *k8znerv1alpha1.K8znerCluster) {
	desired := cluster.Spec.Kubernetes.Version
	if desired == "" {
		return
	}

	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes); err != nil {
		log.FromContext(ctx).V(1).Info("skipping version skew check", "reason", err.Error())
		return
	}
	if len(nodes.Items) == 0 {
		return
	}

	outdated := 0
	for _, node := range nodes.Items {
		// Kubelet reports "v1.32.0"; the spec stores "1.32.0".
		if strings.TrimPrefix(node.Status.NodeInfo.KubeletVersion, "v") != desired {
			outdated++
		}
	}

	if outdated == 0 {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    k8znerv1alpha1.ConditionUpToDate,
			Status:  metav1.ConditionTrue,
			Reason:  "VersionsMatch",
			Message: fmt.Sprintf("All %d nodes run Kubernetes %s", len(nodes.Items), desired),
		})
		return
	}

	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:   k8znerv1alpha1.ConditionUpToDate,
		Status: metav1.ConditionFalse,
		Reason: "KubernetesUpgradePending",
		Message: fmt.Sprintf("%d/%d nodes are not on Kubernetes %s; run `k8zner apply` to upgrade",
			outdated, len(nodes.Items), desired),
	})
	r.Recorder.Eventf(cluster, corev1.EventTypeNormal, "UpgradePending",
		"%d/%d nodes need upgrading to Kubernetes %s", outdated, len(nodes.Items), desired)
}
