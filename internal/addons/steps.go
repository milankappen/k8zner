// Package addons provides functionality for installing cluster addons.
package addons

import (
	"context"
	"fmt"
	"log"

	"github.com/milankappen/k8zner/internal/addons/helm"
	"github.com/milankappen/k8zner/internal/addons/k8sclient"
	"github.com/milankappen/k8zner/internal/config"
)

// Step names match the addon name constants in api/v1alpha1/types.go.
const (
	StepCCM           = "hcloud-ccm"
	StepCSI           = "hcloud-csi"
	StepMetricsServer = "metrics-server"
	StepCertManager   = "cert-manager"
	StepTraefik       = "traefik"
	StepExternalDNS   = "external-dns"
	StepArgoCD        = "argocd"
	StepMonitoring    = "monitoring"
	StepTalosBackup   = "talos-backup"
)

// AddonStep defines a single installable addon with its install order and
// the desired (chart) version resolved from configuration. The version is
// recorded in cluster status on install and compared on later reconciles to
// detect addons that need an upgrade.
type AddonStep struct {
	Name    string
	Order   int
	Version string
}

// stepChartNames maps step names to their helm chart registry keys.
var stepChartNames = map[string]string{
	StepCCM:           "hcloud-ccm",
	StepCSI:           "hcloud-csi",
	StepMetricsServer: "metrics-server",
	StepCertManager:   "cert-manager",
	StepTraefik:       "traefik",
	StepExternalDNS:   "external-dns",
	StepArgoCD:        "argo-cd",
	StepMonitoring:    "kube-prometheus-stack",
}

// stepVersion returns the desired version for a step: the resolved helm chart
// version for chart-backed addons, or the pinned tool version for talos-backup.
func stepVersion(name string, cfg *config.Config) string {
	if name == StepTalosBackup {
		if v := cfg.Addons.TalosBackup.Version; v != "" {
			return v
		}
		return talosBackupVersion()
	}
	return helm.GetChartSpec(stepChartNames[name], stepHelmConfig(name, cfg)).Version
}

// stepHelmConfig returns the user-supplied helm overrides for a step.
func stepHelmConfig(name string, cfg *config.Config) config.HelmChartConfig {
	switch name {
	case StepCCM:
		return cfg.Addons.CCM.Helm
	case StepCSI:
		return cfg.Addons.CSI.Helm
	case StepMetricsServer:
		return cfg.Addons.MetricsServer.Helm
	case StepCertManager:
		return cfg.Addons.CertManager.Helm
	case StepTraefik:
		return cfg.Addons.Traefik.Helm
	case StepExternalDNS:
		return cfg.Addons.ExternalDNS.Helm
	case StepArgoCD:
		return cfg.Addons.ArgoCD.Helm
	case StepMonitoring:
		return cfg.Addons.KubePrometheusStack.Helm
	default:
		return config.HelmChartConfig{}
	}
}

// CNIVersion returns the desired Cilium chart version. Cilium is installed in
// the CNI phase (not via EnabledSteps) but its version is tracked the same way.
func CNIVersion(cfg *config.Config) string {
	return helm.GetChartSpec("cilium", cfg.Addons.Cilium.Helm).Version
}

// EnabledSteps returns the ordered list of addon steps that should be installed
// based on the provided configuration. Cilium is excluded (installed in CNI phase).
func EnabledSteps(cfg *config.Config) []AddonStep {
	var steps []AddonStep

	add := func(name string, order int) {
		steps = append(steps, AddonStep{Name: name, Order: order, Version: stepVersion(name, cfg)})
	}

	if cfg.Addons.CCM.Enabled {
		add(StepCCM, 2)
	}
	if cfg.Addons.CSI.Enabled {
		add(StepCSI, 3)
	}
	if cfg.Addons.MetricsServer.Enabled {
		add(StepMetricsServer, 4)
	}
	if cfg.Addons.CertManager.Enabled {
		add(StepCertManager, 5)
	}
	if cfg.Addons.Traefik.Enabled {
		add(StepTraefik, 6)
	}
	if cfg.Addons.ExternalDNS.Enabled {
		add(StepExternalDNS, 7)
	}
	if cfg.Addons.ArgoCD.Enabled {
		add(StepArgoCD, 8)
	}
	if cfg.Addons.KubePrometheusStack.Enabled {
		add(StepMonitoring, 9)
	}
	if cfg.Addons.TalosBackup.Enabled {
		add(StepTalosBackup, 10)
	}

	return steps
}

// InstallStep installs a single addon by name. Prerequisites (secrets, CRDs)
// for the addon are handled automatically within each step.
// The kubeconfig and networkID are used to create a Kubernetes client and
// configure network-dependent addons.
func InstallStep(ctx context.Context, stepName string, cfg *config.Config, kubeconfig []byte, networkID int64) error {
	client, err := k8sclient.NewFromKubeconfig(kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	switch stepName {
	case StepCCM:
		return installCCMStep(ctx, client, cfg, networkID)
	case StepCSI:
		return installCSIStep(ctx, client, cfg)
	case StepMetricsServer:
		return applyMetricsServer(ctx, client, cfg)
	case StepCertManager:
		return installCertManagerStep(ctx, client, cfg)
	case StepTraefik:
		return applyTraefik(ctx, client, cfg)
	case StepExternalDNS:
		return applyExternalDNS(ctx, client, cfg)
	case StepArgoCD:
		return installArgoCDStep(ctx, client, cfg)
	case StepMonitoring:
		return installMonitoringStep(ctx, client, cfg)
	case StepTalosBackup:
		return installTalosBackupStep(ctx, client, cfg)
	default:
		return fmt.Errorf("unknown addon step: %s", stepName)
	}
}

// installCCMStep installs CCM and its prerequisites (CRDs, secrets, Talos CCM).
func installCCMStep(ctx context.Context, client k8sclient.Client, cfg *config.Config, networkID int64) error {
	// Install prerequisite CRDs (idempotent via Server-Side Apply)
	if cfg.Addons.GatewayAPICRDs.Enabled {
		log.Printf("[addons] Installing Gateway API CRDs...")
		if err := applyGatewayAPICRDs(ctx, client, cfg); err != nil {
			return fmt.Errorf("failed to install Gateway API CRDs: %w", err)
		}
	}
	if cfg.Addons.PrometheusOperatorCRDs.Enabled {
		log.Printf("[addons] Installing Prometheus Operator CRDs...")
		if err := applyPrometheusOperatorCRDs(ctx, client, cfg); err != nil {
			return fmt.Errorf("failed to install Prometheus Operator CRDs: %w", err)
		}
	}
	if cfg.Addons.TalosCCM.Enabled {
		log.Printf("[addons] Installing Talos CCM...")
		if err := applyTalosCCM(ctx, client, cfg); err != nil {
			return fmt.Errorf("failed to install Talos CCM: %w", err)
		}
	}

	// Create HCloud secret (needed by CCM and CSI)
	log.Printf("[addons] Creating HCloud secret...")
	if err := createHCloudSecret(ctx, client, cfg.HCloudToken, networkID); err != nil {
		return fmt.Errorf("failed to create hcloud secret: %w", err)
	}

	log.Printf("[addons] Installing Hetzner Cloud Controller Manager...")
	if err := applyCCM(ctx, client, cfg, networkID); err != nil {
		return fmt.Errorf("failed to install CCM: %w", err)
	}
	log.Printf("[addons] CCM installed successfully")
	return nil
}

// installCSIStep installs CSI (HCloud secret should already exist from CCM step).
func installCSIStep(ctx context.Context, client k8sclient.Client, cfg *config.Config) error {
	log.Printf("[addons] Installing Hetzner CSI Driver...")
	if err := applyCSI(ctx, client, cfg); err != nil {
		return fmt.Errorf("failed to install CSI: %w", err)
	}
	log.Printf("[addons] CSI installed successfully")
	return nil
}

// installCertManagerStep installs cert-manager and optionally the Cloudflare ClusterIssuer.
func installCertManagerStep(ctx context.Context, client k8sclient.Client, cfg *config.Config) error {
	log.Printf("[addons] Installing Cert Manager...")
	if err := applyCertManager(ctx, client, cfg); err != nil {
		return fmt.Errorf("failed to install Cert Manager: %w", err)
	}

	// Create Cloudflare secrets if enabled
	if cfg.Addons.Cloudflare.Enabled {
		log.Printf("[addons] Creating Cloudflare secrets...")
		if err := createCloudflareSecrets(ctx, client, cfg); err != nil {
			return fmt.Errorf("failed to create Cloudflare secrets: %w", err)
		}
	}

	// Configure Cloudflare DNS01 issuer if enabled
	if cfg.Addons.CertManager.Cloudflare.Enabled {
		log.Printf("[addons] Configuring Cloudflare DNS01 issuer...")
		if err := applyCertManagerCloudflare(ctx, client, cfg); err != nil {
			return fmt.Errorf("failed to configure Cloudflare DNS01 issuer: %w", err)
		}
	}

	log.Printf("[addons] Cert Manager installed successfully")
	return nil
}

// installArgoCDStep installs ArgoCD (includes waiting for IngressClass if ingress enabled).
func installArgoCDStep(ctx context.Context, client k8sclient.Client, cfg *config.Config) error {
	log.Printf("[addons] Installing ArgoCD...")
	if err := applyArgoCD(ctx, client, cfg); err != nil {
		return fmt.Errorf("failed to install ArgoCD: %w", err)
	}
	log.Printf("[addons] ArgoCD installed successfully")
	return nil
}

// installMonitoringStep installs kube-prometheus-stack.
func installMonitoringStep(ctx context.Context, client k8sclient.Client, cfg *config.Config) error {
	log.Printf("[addons] Installing kube-prometheus-stack (Prometheus, Grafana, Alertmanager)...")
	if err := applyKubePrometheusStack(ctx, client, cfg); err != nil {
		return fmt.Errorf("failed to install kube-prometheus-stack: %w", err)
	}
	log.Printf("[addons] kube-prometheus-stack installed successfully")
	return nil
}

// installTalosBackupStep waits for Talos CRD and installs Talos Backup.
func installTalosBackupStep(ctx context.Context, client k8sclient.Client, cfg *config.Config) error {
	if err := waitForTalosCRD(ctx, client); err != nil {
		log.Printf("[addons] WARNING: Talos Backup SKIPPED - %v", err)
		log.Printf("[addons] To enable backup later, ensure kubernetesTalosAPIAccess is enabled")
		// Return nil so we don't block other addons - backup can be retried later
		return nil
	}

	log.Printf("[addons] Installing Talos Backup (schedule=%s, bucket=%s)...",
		cfg.Addons.TalosBackup.Schedule, cfg.Addons.TalosBackup.S3Bucket)
	if err := applyTalosBackup(ctx, client, cfg); err != nil {
		return fmt.Errorf("failed to install Talos Backup: %w", err)
	}
	log.Printf("[addons] Talos Backup installed successfully")
	return nil
}
