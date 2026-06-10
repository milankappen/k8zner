// Package controller contains the Kubernetes controllers for the k8zner operator.
package controller

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	k8znerv1alpha1 "github.com/milankappen/k8zner/api/v1alpha1"
	operatorprov "github.com/milankappen/k8zner/internal/operator/provisioning"
	"github.com/milankappen/k8zner/internal/platform/hcloud"
)

const (
	// Default reconciliation interval.
	defaultRequeueAfter = 30 * time.Second

	// Default health check thresholds.
	defaultNodeNotReadyThreshold  = 3 * time.Minute
	defaultEtcdUnhealthyThreshold = 2 * time.Minute

	// Fast requeue for in-progress operations (CNI readiness checks, provisioning waits).
	fastRequeueAfter = 10 * time.Second

	// Requeue for waiting on external dependencies (worker readiness before addons).
	workerReadyRequeueAfter = 15 * time.Second

	// Server creation timeouts.
	serverIPTimeout  = 2 * time.Minute
	nodeReadyTimeout = 5 * time.Minute

	// Cilium readiness check settings.
	ciliumReadyTimeout  = 5 * time.Minute
	ciliumCheckInterval = 10 * time.Second

	// Kubeconfig retrieval timeout.
	kubeconfigTimeout = 2 * time.Minute

	// Status update retry settings.
	statusUpdateRetries = 3
	statusRetryInterval = 100 * time.Millisecond
	serverIPRetryDelay  = 5 * time.Second

	// Event reasons.
	EventReasonReconciling         = "Reconciling"
	EventReasonReconcileSucceeded  = "ReconcileSucceeded"
	EventReasonReconcileFailed     = "ReconcileFailed"
	EventReasonNodeUnhealthy       = "NodeUnhealthy"
	EventReasonNodeReplacing       = "NodeReplacing"
	EventReasonNodeReplaced        = "NodeReplaced"
	EventReasonQuorumLost          = "QuorumLost"
	EventReasonScalingUp           = "ScalingUp"
	EventReasonScalingDown         = "ScalingDown"
	EventReasonServerCreationError = "ServerCreationError"
	EventReasonConfigApplyError    = "ConfigApplyError"
	EventReasonNodeReadyTimeout    = "NodeReadyTimeout"

	// Provisioning event reasons.
	EventReasonProvisioningPhase     = "ProvisioningPhase"
	EventReasonInfrastructureCreated = "InfrastructureCreated"
	EventReasonInfrastructureFailed  = "InfrastructureFailed"
	EventReasonImageReady            = "ImageReady"
	EventReasonImageFailed           = "ImageFailed"
	EventReasonComputeProvisioned    = "ComputeProvisioned"
	EventReasonComputeFailed         = "ComputeFailed"
	EventReasonBootstrapComplete     = "BootstrapComplete"
	EventReasonBootstrapFailed       = "BootstrapFailed"
	EventReasonCNIInstalling         = "CNIInstalling"
	EventReasonCNIReady              = "CNIReady"
	EventReasonCNIFailed             = "CNIFailed"
	EventReasonAddonsInstalling      = "AddonsInstalling"
	EventReasonAddonsReady           = "AddonsReady"
	EventReasonAddonsFailed          = "AddonsFailed"
	EventReasonConfiguringComplete   = "ConfiguringComplete"
	EventReasonConfiguringFailed     = "ConfiguringFailed"
	EventReasonProvisioningComplete  = "ProvisioningComplete"
	EventReasonCredentialsError      = "CredentialsError"
)

// ClusterReconciler reconciles a K8znerCluster object.
type ClusterReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	// Dependencies (injected via options).
	hcloudClient       hcloudClient
	talosClient        talosClient
	talosConfigGen     talosConfigGenerator
	hcloudToken        string
	enableMetrics      bool
	maxConcurrentHeals int

	// nodeReadyWaiter is called to wait for a node to become ready after config is applied.
	// Defaults to waitForK8sNodeReady. Can be overridden in tests.
	nodeReadyWaiter func(ctx context.Context, nodeName string, timeout time.Duration) error

	// Provisioning adapter for operator-driven provisioning.
	phaseAdapter *operatorprov.PhaseAdapter

	// statusMu protects concurrent access to cluster status during parallel provisioning.
	statusMu sync.Mutex
}

// Option configures a ClusterReconciler.
type Option func(*ClusterReconciler)

// logAndRecordError logs an error and records a Kubernetes event.
// This ensures errors are visible in both operator logs (for kubectl logs) and
// Kubernetes events (for kubectl describe/get events).
func (r *ClusterReconciler) logAndRecordError(ctx context.Context, cluster *k8znerv1alpha1.K8znerCluster, err error, reason, message string) {
	logger := log.FromContext(ctx)
	logger.Error(err, message,
		"cluster", cluster.Name,
		"phase", cluster.Status.ProvisioningPhase,
		"reason", reason,
	)
	r.Recorder.Eventf(cluster, corev1.EventTypeWarning, reason, "%s: %v", message, err)
}

// WithHCloudClient sets a custom HCloud client.
func WithHCloudClient(c hcloudClient) Option {
	return func(r *ClusterReconciler) {
		r.hcloudClient = c
	}
}

// WithTalosClient sets a custom Talos client.
func WithTalosClient(c talosClient) Option {
	return func(r *ClusterReconciler) {
		r.talosClient = c
	}
}

// WithTalosConfigGenerator sets a custom Talos config generator.
func WithTalosConfigGenerator(g talosConfigGenerator) Option {
	return func(r *ClusterReconciler) {
		r.talosConfigGen = g
	}
}

// WithHCloudToken sets the Hetzner Cloud API token for lazy client creation.
func WithHCloudToken(token string) Option {
	return func(r *ClusterReconciler) {
		r.hcloudToken = token
	}
}

// WithMetrics enables Prometheus metrics.
func WithMetrics(enable bool) Option {
	return func(r *ClusterReconciler) {
		r.enableMetrics = enable
	}
}

// WithMaxConcurrentHeals sets the maximum concurrent healing operations.
func WithMaxConcurrentHeals(maxHeals int) Option {
	return func(r *ClusterReconciler) {
		r.maxConcurrentHeals = maxHeals
	}
}

// WithNodeReadyWaiter sets a custom function for waiting for nodes to become ready.
// This is primarily used for testing to avoid waiting for actual Kubernetes nodes.
func WithNodeReadyWaiter(waiter func(ctx context.Context, nodeName string, timeout time.Duration) error) Option {
	return func(r *ClusterReconciler) {
		r.nodeReadyWaiter = waiter
	}
}

// NewClusterReconciler creates a new ClusterReconciler with the given options.
func NewClusterReconciler(c client.Client, scheme *runtime.Scheme, recorder record.EventRecorder, opts ...Option) *ClusterReconciler {
	r := &ClusterReconciler{
		Client:             c,
		Scheme:             scheme,
		Recorder:           recorder,
		enableMetrics:      true,
		maxConcurrentHeals: 1,
		phaseAdapter:       operatorprov.NewPhaseAdapter(c),
	}

	for _, opt := range opts {
		opt(r)
	}

	// Set default nodeReadyWaiter if not overridden
	if r.nodeReadyWaiter == nil {
		r.nodeReadyWaiter = r.waitForK8sNodeReady
	}

	return r
}

// ensureHCloudClient ensures the HCloud client is initialized.
func (r *ClusterReconciler) ensureHCloudClient() error {
	if r.hcloudClient != nil {
		return nil
	}
	if r.hcloudToken == "" {
		return fmt.Errorf("HCloud token not configured")
	}
	r.hcloudClient = hcloud.NewRealClient(r.hcloudToken)
	return nil
}

// +kubebuilder:rbac:groups=k8zner.io,resources=k8znerclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=k8zner.io,resources=k8znerclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=k8zner.io,resources=k8znerclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups="",resources=pods/eviction,verbs=create
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;create;update
// +kubebuilder:rbac:groups=apps,resources=deployments;daemonsets;statefulsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch
// +kubebuilder:rbac:groups=apiregistration.k8s.io,resources=apiservices,verbs=get;list;watch

// Reconcile handles the reconciliation loop for K8znerCluster resources.
func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("cluster", req.Name)
	ctx = log.IntoContext(ctx, logger)

	startTime := time.Now()
	defer func() {
		r.recordReconcile(req.Name, "completed", time.Since(startTime).Seconds())
	}()

	// Fetch the K8znerCluster
	cluster := &k8znerv1alpha1.K8znerCluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("cluster resource not found, likely deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "unable to fetch K8znerCluster")
		return ctrl.Result{}, err
	}

	// The operator does not register a finalizer: deleting a K8znerCluster
	// intentionally leaves cloud infrastructure intact (tear-down is an
	// explicit action via `k8zner destroy` or the cleanup utility). Skip
	// reconciliation so a deleting cluster is not provisioned or healed
	// while the object disappears.
	if !cluster.DeletionTimestamp.IsZero() {
		logger.Info("cluster is being deleted, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	// Check if paused
	if cluster.Spec.Paused {
		logger.Info("cluster is paused, skipping reconciliation")
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	// Ensure HCloud client is initialized
	if err := r.ensureHCloudClient(); err != nil {
		logger.Error(err, "failed to initialize HCloud client")
		r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonReconcileFailed, err.Error())
		return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
	}

	// Record reconciliation start
	r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonReconciling, "Starting reconciliation")

	// Run the reconciliation phases
	result, err := r.reconcile(ctx, cluster)

	// Update status with retry for conflict handling
	cluster.Status.LastReconcileTime = &metav1.Time{Time: time.Now()}
	cluster.Status.ObservedGeneration = cluster.Generation

	if statusErr := r.updateStatusWithRetry(ctx, cluster); statusErr != nil {
		logger.Error(statusErr, "failed to update status")
		if err == nil {
			err = statusErr
		}
	}

	if err != nil {
		r.Recorder.Eventf(cluster, corev1.EventTypeWarning, EventReasonReconcileFailed,
			"Reconciliation failed: %v", err)
		r.recordReconcile(req.Name, "error", time.Since(startTime).Seconds())
	} else {
		r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonReconcileSucceeded,
			"Reconciliation completed successfully")
	}

	return result, err
}

// updateStatusWithRetry updates the cluster status with retry on conflict.
// This handles the case where another reconcile has modified the object.
func (r *ClusterReconciler) updateStatusWithRetry(ctx context.Context, cluster *k8znerv1alpha1.K8znerCluster) error {
	logger := log.FromContext(ctx)

	for i := 0; i < statusUpdateRetries; i++ {
		err := r.Status().Update(ctx, cluster)
		if err == nil {
			return nil
		}

		if !apierrors.IsConflict(err) {
			return err
		}

		// On conflict, re-fetch the latest version and re-apply our status changes
		logger.V(1).Info("status update conflict, retrying", "attempt", i+1)

		latest := &k8znerv1alpha1.K8znerCluster{}
		if getErr := r.Get(ctx, client.ObjectKeyFromObject(cluster), latest); getErr != nil {
			return fmt.Errorf("failed to get latest cluster for retry: %w", getErr)
		}

		// Preserve our status changes on the latest version, but keep
		// addon statuses that may have been set by a prior reconcile.
		savedAddons := latest.Status.Addons
		latest.Status = cluster.Status
		if latest.Status.Addons == nil && savedAddons != nil {
			latest.Status.Addons = savedAddons
		}
		cluster = latest

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(statusRetryInterval):
		}
	}

	return fmt.Errorf("failed to update status after %d retries", statusUpdateRetries)
}

// reconcile runs the main reconciliation logic.
// It uses a state machine based on ProvisioningPhase for new clusters,
// and falls back to health-check-only mode for existing clusters without provisioning state.
func (r *ClusterReconciler) reconcile(ctx context.Context, cluster *k8znerv1alpha1.K8znerCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Always update the cluster phase before returning
	defer r.updateClusterPhase(cluster)

	// Always keep status.Desired in sync with spec counts
	// This ensures status reflects the desired state for both legacy and state-machine modes
	cluster.Status.ControlPlanes.Desired = cluster.Spec.ControlPlanes.Count
	cluster.Status.Workers.Desired = cluster.Spec.Workers.Count

	// Step 1: Check for stuck nodes and clean them up
	stuckNodes := r.checkStuckNodes(ctx, cluster)
	for _, stuck := range stuckNodes {
		if err := r.handleStuckNode(ctx, cluster, stuck); err != nil {
			logger.Error(err, "failed to handle stuck node", "node", stuck.Name)
		}
		r.Recorder.Eventf(cluster, corev1.EventTypeWarning, "NodeStuck",
			"Node %s stuck in %s phase for %s, cleaning up", stuck.Name, stuck.Phase, stuck.Elapsed.Round(time.Second))
	}

	// Step 2: Verify and update node states from external APIs
	// This catches nodes that have progressed without operator involvement
	if err := r.verifyAndUpdateNodeStates(ctx, cluster); err != nil {
		logger.Error(err, "failed to verify node states")
		// Continue with reconciliation - this is not fatal
	}

	// Step 3: Run health check to update Ready counts
	// This ensures cluster.Status.ControlPlanes.Ready and cluster.Status.Workers.Ready
	// are always current, regardless of which provisioning phase we're in.
	// Without this, Ready counts stay at 0 during CNI/Addons/Compute phases.
	if err := r.reconcileHealthCheck(ctx, cluster); err != nil {
		logger.V(1).Info("health check failed during reconciliation", "error", err)
		// Continue with reconciliation - health check failures shouldn't block provisioning
	} else {
		logger.V(1).Info("health check complete",
			"cpReady", cluster.Status.ControlPlanes.Ready,
			"cpDesired", cluster.Status.ControlPlanes.Desired,
			"workersReady", cluster.Status.Workers.Ready,
			"workersDesired", cluster.Status.Workers.Desired,
		)
	}

	// Check if this cluster needs provisioning (has credentialsRef set)
	if cluster.Spec.CredentialsRef.Name != "" {
		// This is an operator-managed cluster - use state machine
		return r.reconcileWithStateMachine(ctx, cluster)
	}

	// Legacy mode: same as running phase (health check + healing + non-fatal health probes)
	return r.reconcileRunningPhase(ctx, cluster)
}

// reconcileWithStateMachine handles provisioning and ongoing management using a phase-based state machine.
func (r *ClusterReconciler) reconcileWithStateMachine(ctx context.Context, cluster *k8znerv1alpha1.K8znerCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Debug: Log cluster state at start of reconciliation
	logger.V(1).Info("state machine reconciliation starting",
		"currentPhase", cluster.Status.ProvisioningPhase,
		"clusterPhase", cluster.Status.Phase,
		"cpReady", cluster.Status.ControlPlanes.Ready,
		"cpDesired", cluster.Status.ControlPlanes.Desired,
		"workersReady", cluster.Status.Workers.Ready,
		"workersDesired", cluster.Status.Workers.Desired,
		"bootstrapCompleted", cluster.Spec.Bootstrap != nil && cluster.Spec.Bootstrap.Completed,
	)

	// Determine current phase (empty means start from beginning)
	currentPhase := cluster.Status.ProvisioningPhase
	if currentPhase == "" {
		// Check if CLI bootstrap completed - if so, start from CNI phase
		if cluster.Spec.Bootstrap.Completed {
			currentPhase = k8znerv1alpha1.PhaseCNI
			cluster.Status.ProvisioningPhase = k8znerv1alpha1.PhaseCNI
			logger.Info("bootstrap completed by CLI, starting from CNI phase")
		} else {
			// New cluster - start with infrastructure phase
			currentPhase = k8znerv1alpha1.PhaseInfrastructure
			logger.Info("new cluster, starting from infrastructure phase")
		}
	}

	// Check for phase timeout: emit warning event if phase exceeds 2x expected duration
	r.checkPhaseTimeout(ctx, cluster)

	logger.Info("reconciling with state machine", "phase", currentPhase)

	switch currentPhase {
	case k8znerv1alpha1.PhaseInfrastructure:
		return r.reconcileInfrastructurePhase(ctx, cluster)

	case k8znerv1alpha1.PhaseImage:
		return r.reconcileImagePhase(ctx, cluster)

	case k8znerv1alpha1.PhaseCompute:
		return r.reconcileComputePhase(ctx, cluster)

	case k8znerv1alpha1.PhaseBootstrap:
		return r.reconcileBootstrapPhase(ctx, cluster)

	case k8znerv1alpha1.PhaseCNI:
		return r.reconcileCNIPhase(ctx, cluster)

	case k8znerv1alpha1.PhaseAddons:
		return r.reconcileAddonsPhase(ctx, cluster)

	case k8znerv1alpha1.PhaseConfiguring:
		// Legacy phase - redirect to CNI for new operator-centric flow
		return r.reconcileConfiguringPhase(ctx, cluster)

	case k8znerv1alpha1.PhaseComplete:
		// Provisioning complete - switch to health monitoring mode
		return r.reconcileRunningPhase(ctx, cluster)

	default:
		logger.Info("unknown provisioning phase, resetting to infrastructure", "phase", currentPhase)
		cluster.Status.ProvisioningPhase = k8znerv1alpha1.PhaseInfrastructure
		return ctrl.Result{Requeue: true}, nil
	}
}

// phaseExpectedDurations maps provisioning phases to expected durations.
// Used for timeout detection: warning emitted at 2x expected duration.
var phaseExpectedDurations = map[k8znerv1alpha1.ProvisioningPhase]time.Duration{
	k8znerv1alpha1.PhaseInfrastructure: 30 * time.Second,
	k8znerv1alpha1.PhaseImage:          2 * time.Minute,
	k8znerv1alpha1.PhaseCompute:        1 * time.Minute,
	k8znerv1alpha1.PhaseBootstrap:      3 * time.Minute,
	k8znerv1alpha1.PhaseCNI:            2 * time.Minute,
	k8znerv1alpha1.PhaseAddons:         5 * time.Minute,
}

// checkPhaseTimeout emits a warning event if the current phase exceeds 2x expected duration.
func (r *ClusterReconciler) checkPhaseTimeout(ctx context.Context, cluster *k8znerv1alpha1.K8znerCluster) {
	if cluster.Status.PhaseStartedAt == nil {
		return
	}

	expected, ok := phaseExpectedDurations[cluster.Status.ProvisioningPhase]
	if !ok {
		return
	}

	elapsed := time.Since(cluster.Status.PhaseStartedAt.Time)
	threshold := 2 * expected

	if elapsed > threshold {
		logger := log.FromContext(ctx)
		logger.Info("phase is taking longer than expected",
			"phase", cluster.Status.ProvisioningPhase,
			"elapsed", elapsed.Round(time.Second),
			"expected", expected)

		r.Recorder.Eventf(cluster, corev1.EventTypeWarning, "PhaseTimeout",
			"Phase %s has been running for %s (expected %s)",
			cluster.Status.ProvisioningPhase, elapsed.Round(time.Second), expected)

		recordPhaseError(cluster, string(cluster.Status.ProvisioningPhase),
			fmt.Sprintf("phase exceeds expected duration: %s > %s", elapsed.Round(time.Second), expected))
	}
}

// drainNode evicts all pods from a node.
func (r *ClusterReconciler) drainNode(ctx context.Context, nodeName string) error {
	logger := log.FromContext(ctx)

	// List pods on this node
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, client.MatchingFields{"spec.nodeName": nodeName}); err != nil {
		return fmt.Errorf("failed to list pods on node: %w", err)
	}

	// Evict each pod (skip DaemonSet pods and mirror pods)
	for _, pod := range podList.Items {
		// Skip mirror pods (static pods)
		if _, isMirror := pod.Annotations[corev1.MirrorPodAnnotationKey]; isMirror {
			continue
		}

		// Skip DaemonSet pods
		isDaemonSet := false
		for _, ref := range pod.OwnerReferences {
			if ref.Kind == "DaemonSet" {
				isDaemonSet = true
				break
			}
		}
		if isDaemonSet {
			continue
		}

		// Create eviction
		eviction := &policyv1.Eviction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pod.Name,
				Namespace: pod.Namespace,
			},
		}
		if err := r.SubResource("eviction").Create(ctx, &pod, eviction); err != nil {
			if !apierrors.IsNotFound(err) && !apierrors.IsTooManyRequests(err) {
				logger.Error(err, "failed to evict pod", "pod", pod.Name, "namespace", pod.Namespace)
			}
		} else {
			logger.V(1).Info("evicted pod", "pod", pod.Name, "namespace", pod.Namespace)
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Index pods by node name for efficient drain operations
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.Pod{}, "spec.nodeName", func(o client.Object) []string {
		pod := o.(*corev1.Pod)
		return []string{pod.Spec.NodeName}
	}); err != nil {
		return fmt.Errorf("failed to create pod node name index: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&k8znerv1alpha1.K8znerCluster{}).
		Watches(&corev1.Node{}, &nodeEventHandler{client: mgr.GetClient()},
			builder.WithPredicates(nodeChangePredicate())).
		Complete(r)
}

// nodeChangePredicate filters node update events down to changes that can
// affect reconciliation: readiness transitions, schedulability, labels, and
// deletion. Without it, every kubelet heartbeat (one status update per node
// every ~10s) re-enqueues the cluster and floods the workqueue.
func nodeChangePredicate() predicate.Funcs {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldNode, okOld := e.ObjectOld.(*corev1.Node)
			newNode, okNew := e.ObjectNew.(*corev1.Node)
			if !okOld || !okNew {
				return true
			}
			return nodeReadyStatus(oldNode) != nodeReadyStatus(newNode) ||
				oldNode.Spec.Unschedulable != newNode.Spec.Unschedulable ||
				oldNode.DeletionTimestamp.IsZero() != newNode.DeletionTimestamp.IsZero() ||
				!maps.Equal(oldNode.Labels, newNode.Labels)
		},
	}
}

// nodeReadyStatus returns the status of the node's Ready condition.
func nodeReadyStatus(node *corev1.Node) corev1.ConditionStatus {
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady {
			return cond.Status
		}
	}
	return corev1.ConditionUnknown
}

// nodeEventHandler handles node events and triggers reconciliation.
type nodeEventHandler struct {
	client client.Client
}

// Create enqueues the cluster for reconciliation on node creation.
func (h *nodeEventHandler) Create(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.enqueueCluster(ctx, q)
}

// Update enqueues the cluster for reconciliation on node update.
func (h *nodeEventHandler) Update(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.enqueueCluster(ctx, q)
}

// Delete enqueues the cluster for reconciliation on node deletion.
func (h *nodeEventHandler) Delete(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.enqueueCluster(ctx, q)
}

// Generic enqueues the cluster for reconciliation on generic events.
func (h *nodeEventHandler) Generic(ctx context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.enqueueCluster(ctx, q)
}

func (h *nodeEventHandler) enqueueCluster(ctx context.Context, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	// Node objects carry no owner reference back to a K8znerCluster, so
	// enqueue every cluster and let each reconcile sort out relevance.
	// In practice a management cluster hosts a single K8znerCluster.
	clusters := &k8znerv1alpha1.K8znerClusterList{}
	if err := h.client.List(ctx, clusters); err != nil {
		log.FromContext(ctx).Error(err, "failed to list K8znerClusters for node event")
		return
	}
	for i := range clusters.Items {
		q.Add(reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: clusters.Items[i].Namespace,
				Name:      clusters.Items[i].Name,
			},
		})
	}
}
