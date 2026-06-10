// Package handlers implements the business logic for CLI commands.
//
// This package contains handler functions that are called by command definitions
// in the commands package. Handlers are framework-agnostic and can be tested
// independently of the CLI framework.
package handlers

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/mattn/go-isatty"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"

	k8znerv1alpha1 "github.com/milankappen/k8zner/api/v1alpha1"
	"github.com/milankappen/k8zner/internal/config"
	hcloudInternal "github.com/milankappen/k8zner/internal/platform/hcloud"
	"github.com/milankappen/k8zner/internal/platform/talos"
	"github.com/milankappen/k8zner/internal/provisioning"
	"github.com/milankappen/k8zner/internal/ui/tui"
)

const (
	secretsFile     = "secrets.yaml"
	talosConfigPath = "talosconfig"
	kubeconfigPath  = "kubeconfig"

	// k8znerNamespace is the Kubernetes namespace for k8zner resources,
	// shared with the TUI layer so both look in the same place.
	k8znerNamespace = tui.Namespace

	// credentialsSecretName is the name of the secret containing Hetzner and Talos credentials.
	credentialsSecretName = "k8zner-credentials" //nolint:gosec // This is a secret name, not a credential value
)

// InfrastructureInfo contains information about created infrastructure.
type InfrastructureInfo struct {
	NetworkID             int64
	NetworkName           string
	FirewallID            int64
	FirewallName          string
	LoadBalancerID        int64
	LoadBalancerName      string
	LoadBalancerIP        string
	LoadBalancerPrivateIP string
	SSHKeyID              int64
}

// Factory function variables - can be replaced in tests for dependency injection.
var (
	// newInfraClient creates a new infrastructure client.
	newInfraClient = func(token string) hcloudInternal.InfrastructureManager {
		return hcloudInternal.NewRealClient(token)
	}

	// getOrGenerateSecrets loads or generates Talos secrets.
	getOrGenerateSecrets = talos.GetOrGenerateSecrets

	// newTalosGenerator creates a new Talos configuration generator.
	newTalosGenerator = func(clusterName, kubernetesVersion, talosVersion, endpoint string, sb *secrets.Bundle) provisioning.TalosConfigProducer {
		return talos.NewGenerator(clusterName, kubernetesVersion, talosVersion, endpoint, sb)
	}

	// writeFile writes data to a file (for testing injection).
	writeFile = os.WriteFile

	// loadV2ConfigFile loads v2 config from file (for testing injection).
	loadV2ConfigFile = config.LoadSpec

	// expandV2Config expands v2 config to internal format (for testing injection).
	expandV2Config = config.ExpandSpec

	// findV2ConfigFile finds the v2 config file (for testing injection).
	findV2ConfigFile = config.FindConfigFile

	newProvisioningContext = provisioning.NewContext
)

// IsCIMode returns true if TUI should be disabled (CI, non-TTY, or explicit flag).
func IsCIMode(ci bool) bool {
	if ci {
		return true
	}
	if os.Getenv("CI") != "" || os.Getenv("K8ZNER_NO_TUI") != "" {
		return true
	}
	return !isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsCygwinTerminal(os.Stdout.Fd())
}

// Apply creates or updates a Kubernetes cluster on Hetzner Cloud using Talos Linux.
// Checks for existing operator-managed clusters to update, otherwise bootstraps from scratch.
func Apply(ctx context.Context, configPath string, wait, ci bool) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	log.Printf("Applying configuration for cluster: %s", cfg.ClusterName)

	// Check if cluster already exists with operator management (short timeout to avoid slow startup)
	checkCtx, checkCancel := context.WithTimeout(ctx, 3*time.Second)
	isOperatorManaged, _ := checkOperatorManaged(checkCtx, cfg.ClusterName)
	checkCancel()
	if isOperatorManaged {
		log.Printf("Cluster %s is operator-managed, updating CRD spec", cfg.ClusterName)
		return updateExistingCluster(ctx, cfg)
	}

	// Use TUI for interactive terminals
	if !IsCIMode(ci) {
		return bootstrapNewClusterTUI(ctx, cfg, wait)
	}

	// No existing cluster — bootstrap from scratch (CI mode)
	return bootstrapNewCluster(ctx, cfg, wait)
}

// updateExistingCluster updates an existing operator-managed cluster's CRD spec.
func updateExistingCluster(ctx context.Context, cfg *config.Config) error {
	kubecfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	scheme := k8znerv1alpha1.Scheme
	k8sClient, err := client.New(kubecfg, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	k8zCluster := &k8znerv1alpha1.K8znerCluster{}
	key := client.ObjectKey{
		Namespace: k8znerNamespace,
		Name:      cfg.ClusterName,
	}

	if err := k8sClient.Get(ctx, key, k8zCluster); err != nil {
		return fmt.Errorf("failed to get K8znerCluster: %w", err)
	}

	updateClusterSpecFromConfig(k8zCluster, cfg)

	if err := k8sClient.Update(ctx, k8zCluster); err != nil {
		return fmt.Errorf("failed to update K8znerCluster: %w", err)
	}

	log.Printf("Updated K8znerCluster %s spec", cfg.ClusterName)
	log.Printf("\nThe operator will now reconcile the changes.")
	log.Printf("Monitor progress with:")
	log.Printf("  k8zner doctor --watch")

	return nil
}

// bootstrapNewCluster creates a new cluster from scratch (CI/non-interactive mode).
func bootstrapNewCluster(ctx context.Context, cfg *config.Config, wait bool) error {
	kubeconfig, err := runBootstrapPipeline(ctx, cfg, wait, nil)
	if err != nil {
		return err
	}

	printApplySuccess(cfg, wait)
	printOverallCostHint(ctx, cfg, "apply")

	if wait {
		return waitForOperatorComplete(ctx, cfg.ClusterName, kubeconfig)
	}
	return nil
}

// bootstrapNewClusterTUI creates a new cluster with a TUI dashboard.
func bootstrapNewClusterTUI(ctx context.Context, cfg *config.Config, wait bool) error {
	var kubeconfig []byte

	bootstrapFn := func(ch chan<- tui.BootstrapPhaseMsg) error {
		var err error
		kubeconfig, err = runBootstrapPipeline(ctx, cfg, wait, ch)
		return err
	}

	// Redirect log output during TUI to prevent bleed-through into alt-screen
	origLogOutput := log.Writer()
	log.SetOutput(io.Discard)
	err := tui.RunApplyTUI(ctx, bootstrapFn, cfg.ClusterName, cfg.Location, kubeconfig, wait)
	log.SetOutput(origLogOutput)
	if err != nil {
		return err
	}

	// Re-hydrate access-data after TUI completes (addons now installed, secrets available)
	if len(kubeconfig) > 0 {
		if rehydrateErr := persistAccessData(ctx, cfg, kubeconfig, true); rehydrateErr != nil {
			log.Printf("Warning: failed to re-hydrate access data: %v", rehydrateErr)
		}
		fmt.Println("Access credentials saved to: access-data.yaml")
	}

	printApplySuccess(cfg, wait)
	printOverallCostHint(ctx, cfg, "apply")
	return nil
}

// runBootstrapPipeline executes the shared bootstrap pipeline.
// Flow: Image -> Infrastructure -> 1 CP -> Bootstrap -> Install operator -> Create CRD.
// When ch is non-nil, phase progress messages are sent for TUI display.
func runBootstrapPipeline(ctx context.Context, cfg *config.Config, wait bool, ch chan<- tui.BootstrapPhaseMsg) (kubeconfig []byte, err error) {
	phase := func(name string, done bool) {
		if ch != nil {
			ch <- tui.BootstrapPhaseMsg{Phase: name, Done: done}
		}
	}

	token := os.Getenv("HCLOUD_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("HCLOUD_TOKEN environment variable is required")
	}
	infraClient := newInfraClient(token)

	talosGen, err := initializeTalosGenerator(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Talos generator: %w", err)
	}
	talosGen.SetMachineConfigOptions(talos.NewMachineConfigOptions(cfg))

	if err := writeTalosFiles(talosGen); err != nil {
		return nil, fmt.Errorf("failed to write Talos config files: %w", err)
	}

	pCtx := newProvisioningContext(ctx, cfg, infraClient, talosGen)

	var cleanupNeeded bool
	defer func() {
		if err != nil && cleanupNeeded {
			log.Println("Apply failed, cleaning up created resources...")
			cleanupCtx := context.Background()
			if cleanupErr := cleanupOnFailure(cleanupCtx, cfg, infraClient); cleanupErr != nil {
				log.Printf("Warning: cleanup failed: %v", cleanupErr)
			}
		}
	}()

	// Phase 1: Image
	phase("image:resolve", false)
	phase("image:resolve", true)
	phase("image:build", false)
	if err = provisionImage(pCtx); err != nil {
		return nil, err
	}
	phase("image:build", true)
	phase("image:snapshot", false)
	phase("image:snapshot", true)

	// Phase 2: Infrastructure
	phase("infrastructure", false)
	if err = provisionInfrastructure(pCtx); err != nil {
		return nil, err
	}
	cleanupNeeded = true
	phase("infrastructure", true)

	// Phase 3: Compute
	phase("compute", false)
	if err = provisionFirstControlPlane(cfg, pCtx); err != nil {
		return nil, err
	}
	phase("compute", true)

	// Phase 4: Bootstrap
	phase("bootstrap", false)
	if err = bootstrapCluster(pCtx); err != nil {
		return nil, err
	}
	phase("bootstrap", true)

	kubeconfig = pCtx.State.Kubeconfig
	if len(kubeconfig) == 0 {
		err = fmt.Errorf("kubeconfig not available after cluster bootstrap")
		return nil, err
	}
	if err = writeKubeconfig(kubeconfig); err != nil {
		return nil, err
	}

	if err = waitForLBHealth(ctx, infraClient, cfg.ClusterName); err != nil {
		return nil, err
	}

	// Phase 5: Operator
	phase("operator", false)
	if err = installOperator(ctx, cfg, kubeconfig, pCtx.State.Network.ID); err != nil {
		return nil, err
	}
	phase("operator", true)

	if err = persistAccessData(ctx, cfg, kubeconfig, wait); err != nil {
		return nil, err
	}

	// Phase 6: CRD
	phase("crd", false)
	log.Println("Phase 6/6: Creating K8znerCluster CRD...")
	infraInfo := buildInfraInfo(ctx, pCtx, infraClient, cfg)
	if crdErr := createClusterCRD(ctx, cfg, pCtx, infraInfo, kubeconfig, token); crdErr != nil {
		err = fmt.Errorf("CRD creation failed: %w", crdErr)
		return nil, err
	}
	phase("crd", true)

	return kubeconfig, nil
}

// checkOperatorManaged checks if a cluster is managed by the operator.
func checkOperatorManaged(ctx context.Context, clusterName string) (bool, error) {
	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		return false, nil
	}

	kubecfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return false, err
	}

	scheme := k8znerv1alpha1.Scheme
	k8sClient, err := client.New(kubecfg, client.Options{Scheme: scheme})
	if err != nil {
		return false, err
	}

	k8zCluster := &k8znerv1alpha1.K8znerCluster{}
	key := client.ObjectKey{
		Namespace: k8znerNamespace,
		Name:      clusterName,
	}

	if err := k8sClient.Get(ctx, key, k8zCluster); err != nil {
		return false, nil
	}

	return k8zCluster.Spec.CredentialsRef.Name != "", nil
}

// updateClusterSpecFromConfig updates the K8znerCluster spec from config.Config.
func updateClusterSpecFromConfig(k8zCluster *k8znerv1alpha1.K8znerCluster, cfg *config.Config) {
	if len(cfg.ControlPlane.NodePools) > 0 {
		pool := cfg.ControlPlane.NodePools[0]
		k8zCluster.Spec.ControlPlanes.Count = pool.Count
		k8zCluster.Spec.ControlPlanes.Size = pool.ServerType
	}

	if len(cfg.Workers) > 0 {
		pool := cfg.Workers[0]
		k8zCluster.Spec.Workers.Count = pool.Count
		k8zCluster.Spec.Workers.Size = pool.ServerType
	}

	k8zCluster.Spec.Talos.Version = cfg.Talos.Version
	k8zCluster.Spec.Talos.SchematicID = cfg.Talos.SchematicID
	k8zCluster.Spec.Talos.Extensions = cfg.Talos.Extensions
	k8zCluster.Spec.Kubernetes.Version = cfg.Kubernetes.Version

	k8zCluster.Spec.Network.IPv4CIDR = cfg.Network.IPv4CIDR
	k8zCluster.Spec.Network.PodCIDR = cfg.Network.PodIPv4CIDR
	k8zCluster.Spec.Network.ServiceCIDR = cfg.Network.ServiceIPv4CIDR

	if k8zCluster.Spec.Addons == nil {
		k8zCluster.Spec.Addons = &k8znerv1alpha1.AddonSpec{}
	}
	k8zCluster.Spec.Addons.MetricsServer = cfg.Addons.MetricsServer.Enabled
	k8zCluster.Spec.Addons.CertManager = cfg.Addons.CertManager.Enabled
	k8zCluster.Spec.Addons.Traefik = cfg.Addons.Traefik.Enabled
	k8zCluster.Spec.Addons.ArgoCD = cfg.Addons.ArgoCD.Enabled
	k8zCluster.Spec.Addons.Monitoring = cfg.Addons.KubePrometheusStack.Enabled

	if cfg.Addons.TalosBackup.Enabled && cfg.Addons.TalosBackup.S3AccessKey != "" {
		if k8zCluster.Spec.Backup == nil {
			k8zCluster.Spec.Backup = &k8znerv1alpha1.BackupSpec{}
		}
		k8zCluster.Spec.Backup.Enabled = true
		k8zCluster.Spec.Backup.Schedule = cfg.Addons.TalosBackup.Schedule
		if k8zCluster.Spec.Backup.S3SecretRef == nil {
			k8zCluster.Spec.Backup.S3SecretRef = &k8znerv1alpha1.SecretReference{
				Name: k8zCluster.Name + "-backup-s3",
			}
		}
	}

	if k8zCluster.Annotations == nil {
		k8zCluster.Annotations = make(map[string]string)
	}
	k8zCluster.Annotations["k8zner.io/last-applied"] = metav1.Now().Format(metav1.RFC3339Micro)
}

// loadConfig loads and validates cluster configuration.
func loadConfig(configPath string) (*config.Config, error) {
	if configPath == "" {
		path, err := findV2ConfigFile()
		if err != nil {
			return nil, fmt.Errorf("no config file found: %w\nRun 'k8zner init' to create one", err)
		}
		configPath = path
	}

	v2Cfg, err := loadV2ConfigFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config %s: %w", configPath, err)
	}

	log.Printf("Using config: %s", configPath)
	return expandV2Config(v2Cfg)
}
