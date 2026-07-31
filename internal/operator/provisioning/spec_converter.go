package provisioning

import (
	"fmt"
	"strings"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"gopkg.in/yaml.v3"

	k8znerv1alpha1 "github.com/milankappen/k8zner/api/v1alpha1"
	"github.com/milankappen/k8zner/internal/config"
	"github.com/milankappen/k8zner/internal/platform/talos"
	"github.com/milankappen/k8zner/internal/provisioning"
	"github.com/milankappen/k8zner/internal/util/ptr"
)

// SpecToConfig converts a K8znerCluster spec to the internal config.Config format.
func SpecToConfig(k8sCluster *k8znerv1alpha1.K8znerCluster, creds *Credentials) (*config.Config, error) {
	spec := &k8sCluster.Spec

	cfg := &config.Config{
		ClusterName: k8sCluster.Name,
		HCloudToken: creds.HCloudToken,
		Location:    spec.Region,

		// Firewall configuration
		// UseCurrentIPv4/IPv6 auto-detects operator's IP for API access rules.
		Firewall: expandFirewallFromSpec(spec),

		// Network configuration
		// NodeIPv4CIDR is critical for CCM subnet configuration - it determines
		// where load balancers are attached in the private network.
		Network: config.NetworkConfig{
			IPv4CIDR:           defaultString(spec.Network.IPv4CIDR, config.NetworkCIDR),
			NodeIPv4CIDR:       defaultString(spec.Network.NodeIPv4CIDR, config.NodeCIDR),
			NodeIPv4SubnetMask: 25, // /25 subnets for each role (126 IPs per subnet)
			PodIPv4CIDR:        defaultString(spec.Network.PodCIDR, config.PodCIDR),
			ServiceIPv4CIDR:    defaultString(spec.Network.ServiceCIDR, config.ServiceCIDR),
		},

		// Talos configuration
		Talos: config.TalosConfig{
			Version:     spec.Talos.Version,
			SchematicID: spec.Talos.SchematicID,
			Extensions:  spec.Talos.Extensions,
		},

		// Kubernetes configuration
		Kubernetes: config.KubernetesConfig{
			Version:                spec.Kubernetes.Version,
			Domain:                 "cluster.local",
			APILoadBalancerEnabled: true, // Always enable LB for operator-managed clusters
		},

		// Control plane configuration
		ControlPlane: config.ControlPlaneConfig{
			NodePools: []config.ControlPlaneNodePool{
				{
					Name:       "control-plane",
					Location:   spec.Region,
					ServerType: string(config.ServerSize(spec.ControlPlanes.Size).Normalize()),
					Count:      spec.ControlPlanes.Count,
				},
			},
		},

		// Worker configuration
		// IMPORTANT: Workers are created by the reconciliation loop (scaleUpWorkers),
		// NOT by the compute provisioner. Set Count=0 here to avoid duplicate workers.
		Workers: []config.WorkerNodePool{
			{
				Name:       "workers",
				Location:   spec.Region,
				ServerType: string(config.ServerSize(spec.Workers.Size).Normalize()),
				Count:      0, // Workers created by reconcileWorkers, not compute provisioner
			},
		},

		// Enable essential addons
		Addons: buildAddonsConfig(spec),
	}

	configureBackup(cfg, spec, creds)
	configureCloudflare(cfg, spec, creds, k8sCluster.Name)

	// Calculate derived network configuration (NodeIPv4CIDR, etc.)
	if err := cfg.CalculateSubnets(); err != nil {
		return nil, fmt.Errorf("failed to calculate network subnets: %w", err)
	}

	return cfg, nil
}

// buildAddonsConfig creates the addons configuration from the CRD spec.
func buildAddonsConfig(spec *k8znerv1alpha1.K8znerClusterSpec) config.AddonsConfig {
	return config.AddonsConfig{
		// CRDs - always enabled as dependencies for other addons
		GatewayAPICRDs:         config.DefaultGatewayAPICRDs(),
		PrometheusOperatorCRDs: config.DefaultPrometheusOperatorCRDs(),
		// Core addons
		TalosCCM: config.TalosCCMConfig{
			Enabled: true,
			Version: config.DefaultVersionMatrix().TalosCCM,
		},
		Cilium: config.DefaultCilium(),
		CCM:    config.DefaultCCM(),
		CSI:    config.DefaultCSI(),
		MetricsServer: config.MetricsServerConfig{
			Enabled: spec.Addons != nil && spec.Addons.MetricsServer,
		},
		CertManager: config.CertManagerConfig{
			Enabled: spec.Addons != nil && spec.Addons.CertManager,
		},
		Traefik:             config.DefaultTraefik(spec.Addons != nil && spec.Addons.Traefik),
		ExternalDNS:         expandExternalDNSFromSpec(spec),
		ArgoCD:              expandArgoCDFromSpec(spec),
		KubePrometheusStack: expandMonitoringFromSpec(spec),
		Logging: config.LoggingConfig{
			Enabled:   spec.Addons != nil && spec.Addons.Logging,
			Retention: config.LoggingDefaultRetention,
		},
	}
}

// configureBackup maps backup configuration from spec.Backup to cfg.Addons.TalosBackup.
func configureBackup(cfg *config.Config, spec *k8znerv1alpha1.K8znerClusterSpec, creds *Credentials) {
	if spec.Backup == nil || !spec.Backup.Enabled {
		return
	}
	if creds.BackupS3AccessKey == "" || creds.BackupS3SecretKey == "" {
		return
	}

	backup := config.DefaultTalosBackup()
	backup.Enabled = true
	backup.Schedule = spec.Backup.Schedule
	backup.S3AccessKey = creds.BackupS3AccessKey
	backup.S3SecretKey = creds.BackupS3SecretKey
	backup.S3Endpoint = creds.BackupS3Endpoint
	backup.S3Bucket = creds.BackupS3Bucket
	backup.S3Region = creds.BackupS3Region
	cfg.Addons.TalosBackup = backup
}

// configureCloudflare enables Cloudflare integration when ExternalDNS is active.
func configureCloudflare(cfg *config.Config, spec *k8znerv1alpha1.K8znerClusterSpec, creds *Credentials, clusterName string) {
	if !cfg.Addons.ExternalDNS.Enabled {
		return
	}

	cfg.Addons.Cloudflare = config.CloudflareConfig{
		Enabled:  true,
		APIToken: creds.CloudflareAPIToken,
		Domain:   spec.Domain,
	}
	cfg.Addons.ExternalDNS.TXTOwnerID = clusterName

	// Enable CertManager Cloudflare integration for DNS-01 challenge
	if cfg.Addons.CertManager.Enabled && spec.Domain != "" {
		cfg.Addons.CertManager.Cloudflare = config.CertManagerCloudflareConfig{
			Enabled:             true,
			Production:          true,
			Email:               "admin@" + spec.Domain,
			WildcardCertificate: spec.Addons != nil && spec.Addons.WildcardCertificate,
		}
	}
}

// expandFirewallFromSpec derives firewall config from the CRD spec.
func expandFirewallFromSpec(spec *k8znerv1alpha1.K8znerClusterSpec) config.FirewallConfig {
	return config.FirewallConfig{
		UseCurrentIPv4: ptr.Bool(true),
		UseCurrentIPv6: ptr.Bool(true),
	}
}

// expandArgoCDFromSpec derives ArgoCD config from the CRD spec.
func expandArgoCDFromSpec(spec *k8znerv1alpha1.K8znerClusterSpec) config.ArgoCDConfig {
	argoCfg := config.ArgoCDConfig{
		Enabled: spec.Addons != nil && spec.Addons.ArgoCD,
	}

	if spec.Domain != "" && argoCfg.Enabled {
		var customSub string
		if spec.Addons != nil {
			customSub = spec.Addons.ArgoSubdomain
		}
		argoCfg.IngressEnabled = true
		argoCfg.IngressHost = config.BuildIngressHost(spec.Domain, "argo", customSub)
		argoCfg.IngressClassName = "traefik"
		argoCfg.IngressTLS = true
	}

	return argoCfg
}

// expandMonitoringFromSpec derives kube-prometheus-stack config from the CRD spec.
func expandMonitoringFromSpec(spec *k8znerv1alpha1.K8znerClusterSpec) config.KubePrometheusStackConfig {
	promCfg := config.KubePrometheusStackConfig{
		Enabled: spec.Addons != nil && spec.Addons.Monitoring,
	}

	if !promCfg.Enabled {
		return promCfg
	}

	// Bug fix: Prometheus persistence was missing in operator path
	promCfg.Prometheus.Persistence = config.DefaultPrometheusPersistence()

	if spec.Domain != "" {
		var customSub string
		if spec.Addons != nil {
			customSub = spec.Addons.GrafanaSubdomain
		}
		promCfg.Grafana.IngressEnabled = true
		promCfg.Grafana.IngressHost = config.BuildIngressHost(spec.Domain, "grafana", customSub)
		promCfg.Grafana.IngressClassName = "traefik"
		promCfg.Grafana.IngressTLS = true
	}

	return promCfg
}

// expandExternalDNSFromSpec derives ExternalDNS config from the CRD spec.
func expandExternalDNSFromSpec(spec *k8znerv1alpha1.K8znerClusterSpec) config.ExternalDNSConfig {
	return config.DefaultExternalDNS(spec.Addons != nil && spec.Addons.ExternalDNS)
}

func defaultString(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

// parseSecretsFromBytes parses a Talos secrets bundle from YAML bytes.
func parseSecretsFromBytes(data []byte) (*secrets.Bundle, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty secrets data")
	}

	var sb secrets.Bundle
	if err := yaml.Unmarshal(data, &sb); err != nil {
		return nil, fmt.Errorf("failed to unmarshal secrets bundle: %w", err)
	}

	// Re-inject clock (required for certificate generation)
	sb.Clock = secrets.NewFixedClock(time.Now())

	return &sb, nil
}

// CreateTalosGenerator creates a TalosConfigProducer from the cluster spec and credentials.
func (a *PhaseAdapter) CreateTalosGenerator(
	k8sCluster *k8znerv1alpha1.K8znerCluster,
	creds *Credentials,
) (provisioning.TalosConfigProducer, error) {
	sb, err := parseSecretsFromBytes(creds.TalosSecrets)
	if err != nil {
		return nil, fmt.Errorf("failed to parse talos secrets: %w", err)
	}

	endpoint, err := resolveEndpoint(k8sCluster)
	if err != nil {
		return nil, err
	}

	generator := talos.NewGenerator(
		k8sCluster.Name,
		k8sCluster.Spec.Kubernetes.Version,
		k8sCluster.Spec.Talos.Version,
		endpoint,
		sb,
	)

	generator.SetMachineConfigOptions(buildMachineConfigOptions(k8sCluster))

	return generator, nil
}

// resolveEndpoint determines the control plane endpoint URL for Talos config generation.
func resolveEndpoint(k8sCluster *k8znerv1alpha1.K8znerCluster) (string, error) {
	var endpoint string
	var endpointIP string

	//nolint:gocritic // if-else chain is clearer here due to different condition types
	if k8sCluster.Status.Infrastructure.LoadBalancerPrivateIP != "" {
		endpointIP = k8sCluster.Status.Infrastructure.LoadBalancerPrivateIP
	} else if k8sCluster.Status.ControlPlaneEndpoint != "" {
		if strings.HasPrefix(k8sCluster.Status.ControlPlaneEndpoint, "https://") {
			endpoint = k8sCluster.Status.ControlPlaneEndpoint
		} else {
			endpointIP = k8sCluster.Status.ControlPlaneEndpoint
		}
	} else if k8sCluster.Status.Infrastructure.LoadBalancerIP != "" {
		endpointIP = k8sCluster.Status.Infrastructure.LoadBalancerIP
	} else if len(k8sCluster.Status.ControlPlanes.Nodes) > 0 {
		cp := k8sCluster.Status.ControlPlanes.Nodes[0]
		if cp.PrivateIP != "" {
			endpointIP = cp.PrivateIP
		} else if cp.PublicIP != "" {
			endpointIP = cp.PublicIP
		}
	}

	if endpoint == "" && endpointIP != "" {
		endpoint = fmt.Sprintf("https://%s:%d", endpointIP, config.KubeAPIPort)
	}

	if endpoint == "" {
		return "", fmt.Errorf("cannot create talos generator: no valid control plane endpoint found (LoadBalancerPrivateIP, ControlPlaneEndpoint, LoadBalancerIP, or CP node IP required)")
	}

	return endpoint, nil
}

// buildMachineConfigOptions creates MachineConfigOptions from the cluster spec.
func buildMachineConfigOptions(k8sCluster *k8znerv1alpha1.K8znerCluster) *talos.MachineConfigOptions {
	return &talos.MachineConfigOptions{
		SchematicID:             k8sCluster.Spec.Talos.SchematicID,
		StateEncryption:         true,
		EphemeralEncryption:     true,
		IPv6Enabled:             true,
		PublicIPv4Enabled:       true,
		PublicIPv6Enabled:       true,
		CoreDNSEnabled:          true,
		DiscoveryServiceEnabled: true,
		KubeProxyReplacement:    true, // Cilium replaces kube-proxy
		NodeIPv4CIDR:            defaultString(k8sCluster.Spec.Network.IPv4CIDR, config.NetworkCIDR),
		PodIPv4CIDR:             defaultString(k8sCluster.Spec.Network.PodCIDR, config.PodCIDR),
		ServiceIPv4CIDR:         defaultString(k8sCluster.Spec.Network.ServiceCIDR, config.ServiceCIDR),
		EtcdSubnet:              defaultString(k8sCluster.Spec.Network.IPv4CIDR, config.NetworkCIDR),
	}
}
