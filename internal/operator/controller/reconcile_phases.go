package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	k8znerv1alpha1 "github.com/milankappen/k8zner/api/v1alpha1"
	"github.com/milankappen/k8zner/internal/addons"
	operatorprov "github.com/milankappen/k8zner/internal/operator/provisioning"
	"github.com/milankappen/k8zner/internal/platform/hcloud"
	"github.com/milankappen/k8zner/internal/provisioning"
	"github.com/milankappen/k8zner/internal/util/naming"
)

// recordPhaseTransition records a phase transition in the cluster's PhaseHistory.
// It closes the previous phase record (if open) and opens a new one.
func recordPhaseTransition(cluster *k8znerv1alpha1.K8znerCluster, newPhase k8znerv1alpha1.ProvisioningPhase) {
	now := metav1.Now()

	// Close the previous open phase record
	for i := range cluster.Status.PhaseHistory {
		if cluster.Status.PhaseHistory[i].EndedAt == nil {
			cluster.Status.PhaseHistory[i].EndedAt = &now
			d := now.Sub(cluster.Status.PhaseHistory[i].StartedAt.Time)
			cluster.Status.PhaseHistory[i].Duration = d.Round(time.Second).String()
		}
	}

	// Open a new record for the new phase
	cluster.Status.PhaseHistory = append(cluster.Status.PhaseHistory, k8znerv1alpha1.PhaseRecord{
		Phase:     newPhase,
		StartedAt: now,
	})

	// Update PhaseStartedAt for timeout detection
	cluster.Status.PhaseStartedAt = &now
}

// recordPhaseError records an error on the current open phase record and appends to LastErrors.
func recordPhaseError(cluster *k8znerv1alpha1.K8znerCluster, component, message string) {
	now := metav1.Now()

	// Set error on current open phase
	for i := range cluster.Status.PhaseHistory {
		if cluster.Status.PhaseHistory[i].EndedAt == nil {
			cluster.Status.PhaseHistory[i].Error = message
		}
	}

	// Append to LastErrors ring buffer
	cluster.Status.LastErrors = append(cluster.Status.LastErrors, k8znerv1alpha1.ErrorRecord{
		Time:      now,
		Phase:     string(cluster.Status.ProvisioningPhase),
		Component: component,
		Message:   message,
	})
	if len(cluster.Status.LastErrors) > k8znerv1alpha1.MaxLastErrors {
		cluster.Status.LastErrors = cluster.Status.LastErrors[len(cluster.Status.LastErrors)-k8znerv1alpha1.MaxLastErrors:]
	}
}

// reconcileInfrastructurePhase creates network, firewall, and load balancer.
// If infrastructure already exists (from CLI bootstrap), it skips creation and proceeds to the next phase.
func (r *ClusterReconciler) reconcileInfrastructurePhase(ctx context.Context, cluster *k8znerv1alpha1.K8znerCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("reconciling infrastructure phase")

	cluster.Status.Phase = k8znerv1alpha1.ClusterPhaseProvisioning
	cluster.Status.ProvisioningPhase = k8znerv1alpha1.PhaseInfrastructure
	if len(cluster.Status.PhaseHistory) == 0 {
		recordPhaseTransition(cluster, k8znerv1alpha1.PhaseInfrastructure)
	}

	// Check if infrastructure already exists (from CLI bootstrap)
	infra := cluster.Status.Infrastructure
	if infra.NetworkID != 0 && infra.LoadBalancerID != 0 && infra.FirewallID != 0 {
		logger.Info("infrastructure already exists from CLI bootstrap, skipping creation",
			"networkID", infra.NetworkID,
			"loadBalancerID", infra.LoadBalancerID,
			"firewallID", infra.FirewallID,
		)

		if cluster.Status.ControlPlaneEndpoint == "" && infra.LoadBalancerIP != "" {
			cluster.Status.ControlPlaneEndpoint = infra.LoadBalancerIP
		}

		r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonInfrastructureCreated,
			"Using existing infrastructure from CLI bootstrap")

		recordPhaseTransition(cluster, k8znerv1alpha1.PhaseImage)
		cluster.Status.ProvisioningPhase = k8znerv1alpha1.PhaseImage
		return ctrl.Result{Requeue: true}, nil
	}

	// Infrastructure doesn't exist - need to create it
	r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonProvisioningPhase,
		"Starting infrastructure provisioning")

	creds, err := r.phaseAdapter.LoadCredentials(ctx, cluster)
	if err != nil {
		r.logAndRecordError(ctx, cluster, err, EventReasonCredentialsError, "Failed to load credentials")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	pCtx, err := r.buildProvisioningContext(ctx, cluster, creds)
	if err != nil {
		r.logAndRecordError(ctx, cluster, err, EventReasonInfrastructureFailed, "Failed to build provisioning context")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	if err := r.phaseAdapter.ReconcileInfrastructure(pCtx, cluster); err != nil {
		r.logAndRecordError(ctx, cluster, err, EventReasonInfrastructureFailed, "Infrastructure provisioning failed")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	// If bootstrap node exists, attach it to infrastructure
	if cluster.Spec.Bootstrap != nil && cluster.Spec.Bootstrap.Completed {
		if err := r.phaseAdapter.AttachBootstrapNodeToInfrastructure(pCtx, cluster); err != nil {
			logger.Error(err, "failed to attach bootstrap node to infrastructure")
		}
	}

	r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonInfrastructureCreated,
		"Infrastructure provisioned successfully")

	recordPhaseTransition(cluster, k8znerv1alpha1.PhaseImage)
	cluster.Status.ProvisioningPhase = k8znerv1alpha1.PhaseImage
	return ctrl.Result{Requeue: true}, nil
}

// reconcileImagePhase ensures the Talos image snapshot exists.
func (r *ClusterReconciler) reconcileImagePhase(ctx context.Context, cluster *k8znerv1alpha1.K8znerCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("reconciling image phase")

	r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonProvisioningPhase,
		"Ensuring Talos image is available")

	creds, err := r.phaseAdapter.LoadCredentials(ctx, cluster)
	if err != nil {
		r.logAndRecordError(ctx, cluster, err, EventReasonCredentialsError, "Failed to load credentials")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	pCtx, err := r.buildProvisioningContext(ctx, cluster, creds)
	if err != nil {
		r.logAndRecordError(ctx, cluster, err, EventReasonImageFailed, "Failed to build provisioning context")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	if err := r.phaseAdapter.ReconcileImage(pCtx, cluster); err != nil {
		r.logAndRecordError(ctx, cluster, err, EventReasonImageFailed, "Image provisioning failed")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonImageReady,
		"Talos image is available")

	recordPhaseTransition(cluster, k8znerv1alpha1.PhaseCompute)
	cluster.Status.ProvisioningPhase = k8znerv1alpha1.PhaseCompute
	return ctrl.Result{Requeue: true}, nil
}

// reconcileComputePhase provisions control plane and worker servers.
func (r *ClusterReconciler) reconcileComputePhase(ctx context.Context, cluster *k8znerv1alpha1.K8znerCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("reconciling compute phase")

	r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonProvisioningPhase,
		"Provisioning compute resources")

	creds, err := r.phaseAdapter.LoadCredentials(ctx, cluster)
	if err != nil {
		r.logAndRecordError(ctx, cluster, err, EventReasonCredentialsError, "Failed to load credentials")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	pCtx, err := r.buildProvisioningContext(ctx, cluster, creds)
	if err != nil {
		r.logAndRecordError(ctx, cluster, err, EventReasonComputeFailed, "Failed to build provisioning context")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	if err := r.phaseAdapter.ReconcileCompute(pCtx, cluster); err != nil {
		r.logAndRecordError(ctx, cluster, err, EventReasonComputeFailed, "Compute provisioning failed")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonComputeProvisioned,
		"Compute resources provisioned")

	// For CLI-bootstrapped clusters, skip Bootstrap phase (can't run from inside the cluster)
	if cluster.Spec.Bootstrap != nil && cluster.Spec.Bootstrap.Completed {
		recordPhaseTransition(cluster, k8znerv1alpha1.PhaseAddons)
		cluster.Status.ProvisioningPhase = k8znerv1alpha1.PhaseAddons
	} else {
		recordPhaseTransition(cluster, k8znerv1alpha1.PhaseBootstrap)
		cluster.Status.ProvisioningPhase = k8znerv1alpha1.PhaseBootstrap
	}
	return ctrl.Result{Requeue: true}, nil
}

// reconcileBootstrapPhase applies Talos configs and bootstraps the cluster.
func (r *ClusterReconciler) reconcileBootstrapPhase(ctx context.Context, cluster *k8znerv1alpha1.K8znerCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("reconciling bootstrap phase")

	r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonProvisioningPhase,
		"Bootstrapping cluster")

	creds, err := r.phaseAdapter.LoadCredentials(ctx, cluster)
	if err != nil {
		r.logAndRecordError(ctx, cluster, err, EventReasonCredentialsError, "Failed to load credentials")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	pCtx, err := r.buildProvisioningContext(ctx, cluster, creds)
	if err != nil {
		r.logAndRecordError(ctx, cluster, err, EventReasonBootstrapFailed, "Failed to build provisioning context")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	if err := r.phaseAdapter.ReconcileBootstrap(pCtx, cluster); err != nil {
		r.logAndRecordError(ctx, cluster, err, EventReasonBootstrapFailed, "Cluster bootstrap failed")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonBootstrapComplete,
		"Cluster bootstrapped successfully")

	recordPhaseTransition(cluster, k8znerv1alpha1.PhaseCNI)
	cluster.Status.ProvisioningPhase = k8znerv1alpha1.PhaseCNI
	return ctrl.Result{Requeue: true}, nil
}

// reconcileConfiguringPhase installs addons and finalizes cluster configuration (legacy phase).
func (r *ClusterReconciler) reconcileConfiguringPhase(ctx context.Context, cluster *k8znerv1alpha1.K8znerCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("reconciling configuring phase")

	r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonProvisioningPhase,
		"Configuring cluster (installing addons)")

	creds, err := r.phaseAdapter.LoadCredentials(ctx, cluster)
	if err != nil {
		r.logAndRecordError(ctx, cluster, err, EventReasonCredentialsError, "Failed to load credentials")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	cfg, err := operatorprov.SpecToConfig(cluster, creds)
	if err != nil {
		r.logAndRecordError(ctx, cluster, err, EventReasonConfiguringFailed, "Failed to convert spec to config")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}
	cfg.HCloudToken = creds.HCloudToken

	kubeconfig, err := r.getKubeconfigFromTalos(ctx, cluster, creds)
	if err != nil {
		r.logAndRecordError(ctx, cluster, err, EventReasonConfiguringFailed, "Failed to get kubeconfig")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	networkID, err := r.resolveNetworkID(ctx, cluster)
	if err != nil {
		r.logAndRecordError(ctx, cluster, err, EventReasonConfiguringFailed, "Failed to resolve network ID")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	logger.Info("installing addons", "networkID", networkID)
	if err := addons.Apply(ctx, cfg, kubeconfig, networkID); err != nil {
		r.logAndRecordError(ctx, cluster, err, EventReasonConfiguringFailed, "Failed to install addons")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonConfiguringComplete,
		"Cluster configuration complete")

	recordPhaseTransition(cluster, k8znerv1alpha1.PhaseComplete)
	cluster.Status.ProvisioningPhase = k8znerv1alpha1.PhaseComplete
	cluster.Status.Phase = k8znerv1alpha1.ClusterPhaseRunning

	r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonProvisioningComplete,
		"Cluster provisioning complete")

	return ctrl.Result{Requeue: true}, nil
}

// reconcileRunningPhase handles health monitoring and scaling for a running cluster.
func (r *ClusterReconciler) reconcileRunningPhase(ctx context.Context, cluster *k8znerv1alpha1.K8znerCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("reconciling running phase (health monitoring)")

	if err := r.reconcileHealthCheck(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("health check failed: %w", err)
	}

	// Handle scaling first — health probes have network timeouts that slow the reconcile loop
	if result, err := r.reconcileControlPlanes(ctx, cluster); err != nil || result.RequeueAfter > 0 {
		return result, err
	}

	if result, err := r.reconcileWorkers(ctx, cluster); err != nil || result.RequeueAfter > 0 {
		return result, err
	}

	// Keep addons converged on their desired versions (one upgrade per
	// reconcile). Only operator-managed clusters carry credentials to do so.
	if cluster.Spec.CredentialsRef.Name != "" {
		if result, handled := r.reconcileAddonUpgrades(ctx, cluster); handled {
			return result, nil
		}
	}

	// Non-fatal health probes: only run when cluster is stable (no scaling in progress)
	r.reconcileInfraHealth(ctx, cluster)
	r.reconcileAddonHealth(ctx, cluster)
	r.reconcileConnectivityHealth(ctx, cluster)
	r.reconcileVersionSkew(ctx, cluster)

	return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
}

// buildProvisioningContext creates a provisioning context for phase adapter methods.
func (r *ClusterReconciler) buildProvisioningContext(ctx context.Context, cluster *k8znerv1alpha1.K8znerCluster, creds *operatorprov.Credentials) (*provisioning.Context, error) {
	infraManager := hcloud.NewRealClient(creds.HCloudToken)

	// Discover infrastructure from HCloud BEFORE creating Talos generator.
	r.discoverInfrastructure(ctx, cluster, infraManager)

	talosProducer, err := r.phaseAdapter.CreateTalosGenerator(cluster, creds)
	if err != nil {
		return nil, fmt.Errorf("failed to create talos generator: %w", err)
	}

	pCtx, err := r.phaseAdapter.BuildProvisioningContext(ctx, cluster, creds, infraManager, talosProducer)
	if err != nil {
		return nil, err
	}

	// Populate network state for CLI bootstrap clusters
	if pCtx.State.Network == nil {
		network, err := infraManager.GetNetwork(ctx, cluster.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to get network: %w", err)
		}
		pCtx.State.Network = network
		if cluster.Status.Infrastructure.NetworkID == 0 && network != nil {
			cluster.Status.Infrastructure.NetworkID = network.ID
		}
	}

	return pCtx, nil
}

// discoverInfrastructure populates missing infrastructure IDs by querying HCloud.
func (r *ClusterReconciler) discoverInfrastructure(ctx context.Context, cluster *k8znerv1alpha1.K8znerCluster, infraManager *hcloud.RealClient) {
	if cluster.Status.Infrastructure.LoadBalancerID == 0 {
		lbName := naming.KubeAPILoadBalancer(cluster.Name)
		lb, err := infraManager.GetLoadBalancer(ctx, lbName)
		if err == nil && lb != nil {
			cluster.Status.Infrastructure.LoadBalancerID = lb.ID
			if lb.PublicNet.Enabled && lb.PublicNet.IPv4.IP.String() != "<nil>" {
				cluster.Status.Infrastructure.LoadBalancerIP = lb.PublicNet.IPv4.IP.String()
			}
			if len(lb.PrivateNet) > 0 && lb.PrivateNet[0].IP != nil {
				cluster.Status.Infrastructure.LoadBalancerPrivateIP = lb.PrivateNet[0].IP.String()
			}
		}
	}

	if cluster.Status.Infrastructure.FirewallID == 0 {
		// CLI creates firewall with cluster name directly (no suffix)
		fw, err := infraManager.GetFirewall(ctx, cluster.Name)
		if err == nil && fw != nil {
			cluster.Status.Infrastructure.FirewallID = fw.ID
		}
	}
}
