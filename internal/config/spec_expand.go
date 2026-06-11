package config

import (
	"os"

	"github.com/milankappen/k8zner/internal/util/ptr"
)

// ExpandSpec converts a simplified Spec to the full internal Config
// that the provisioning layer expects.
//
// This is where all the opinionated defaults are applied:
// - IPv6-only nodes
// - Hardcoded addon stack
// - Pinned versions
// - Secure network configuration
func ExpandSpec(cfg *Spec) (*Config, error) {
	vm := DefaultVersionMatrix()

	// Create the full internal config
	internal := &Config{
		// Basic cluster info
		ClusterName: cfg.Name,
		Location:    string(cfg.Region),
		HCloudToken: os.Getenv("HCLOUD_TOKEN"),

		// Access configuration
		ClusterAccess:      "public", // LB is public, nodes are IPv6-only
		GracefulDestroy:    true,
		HealthcheckEnabled: ptr.Bool(true),
		DeleteProtection:   false,

		// Config output paths
		KubeconfigPath:  "./secrets/kubeconfig",
		TalosconfigPath: "./secrets/talosconfig",

		// Talosctl settings
		TalosctlVersionCheckEnabled: ptr.Bool(true),
		TalosctlRetryCount:          5,

		// Network configuration
		Network: expandNetwork(cfg),

		// Firewall configuration
		Firewall: expandFirewall(cfg),

		// Control plane
		ControlPlane: expandControlPlane(cfg),

		// Workers
		Workers: expandWorkers(cfg),

		// Talos configuration
		Talos: expandTalos(cfg, vm),

		// Kubernetes configuration
		Kubernetes: expandKubernetes(cfg, vm),

		// Addons
		Addons: expandAddons(cfg, vm),
	}

	return internal, nil
}

func expandNetwork(cfg *Spec) NetworkConfig {
	return NetworkConfig{
		IPv4CIDR:           NetworkCIDR,
		NodeIPv4CIDR:       NodeCIDR,
		ServiceIPv4CIDR:    ServiceCIDR,
		PodIPv4CIDR:        PodCIDR,
		Zone:               NetworkZone(cfg.Region),
		NodeIPv4SubnetMask: 25, // /25 subnets for each role (126 IPs per subnet)
	}
}

func expandFirewall(cfg *Spec) FirewallConfig {
	return FirewallConfig{
		// Auto-detect current IP for API access
		UseCurrentIPv4: ptr.Bool(true),
		UseCurrentIPv6: ptr.Bool(true),
		// No ExtraRules needed: Traefik uses LoadBalancer service,
		// so the Hetzner LB handles ingress traffic (not node ports 80/443).
	}
}

func expandControlPlane(cfg *Spec) ControlPlaneConfig {
	cpCount := cfg.ControlPlaneCount()

	return ControlPlaneConfig{
		NodePools: []ControlPlaneNodePool{
			{
				Name:       "control-plane",
				Location:   string(cfg.Region),
				ServerType: string(cfg.ControlPlaneSize()), // Use configured or default size
				Count:      cpCount,
				Labels: map[string]string{
					"node.kubernetes.io/role": "control-plane",
				},
			},
		},
	}
}

func expandWorkers(cfg *Spec) []WorkerNodePool {
	return []WorkerNodePool{
		{
			Name:           "workers",
			Location:       string(cfg.Region),
			ServerType:     string(cfg.Workers.Size.Normalize()), // Convert old cx22 to cx23 etc.
			Count:          cfg.Workers.Count,
			PlacementGroup: true, // Spread workers across different physical hosts
			Labels: map[string]string{
				"node.kubernetes.io/role": "worker",
			},
		},
	}
}

func expandTalos(cfg *Spec, vm VersionMatrix) TalosConfig {
	return TalosConfig{
		Version: vm.Talos,
		Machine: TalosMachineConfig{
			// IPv6-only configuration
			IPv6Enabled:       ptr.Bool(true),
			PublicIPv4Enabled: ptr.Bool(false), // No public IPv4!
			PublicIPv6Enabled: ptr.Bool(true),

			// Disk encryption
			StateEncryption:     ptr.Bool(true),
			EphemeralEncryption: ptr.Bool(true),

			// CoreDNS
			CoreDNSEnabled: ptr.Bool(true),

			// Discovery
			DiscoveryKubernetesEnabled: ptr.Bool(true),
			DiscoveryServiceEnabled:    ptr.Bool(true),

			// Config apply mode
			ConfigApplyMode: "auto",
		},
	}
}

func expandKubernetes(cfg *Spec, vm VersionMatrix) KubernetesConfig {
	return KubernetesConfig{
		Version: vm.Kubernetes,
		Domain:  "cluster.local",

		// Enable API load balancer for HA mode
		APILoadBalancerEnabled:       cfg.Mode == ModeHA,
		APILoadBalancerPublicNetwork: ptr.Bool(true),

		// Allow scheduling on control plane only in dev mode
		AllowSchedulingOnCP: ptr.Bool(cfg.Mode == ModeDev),
	}
}

func expandAddons(cfg *Spec, vm VersionMatrix) AddonsConfig {
	hasDomain := cfg.HasDomain()

	return AddonsConfig{
		// Hetzner Cloud Controller Manager - always enabled
		CCM: DefaultCCM(),

		// Hetzner CSI Driver - always enabled
		CSI: DefaultCSI(),

		// Cilium CNI - always enabled with kube-proxy replacement
		Cilium: DefaultCilium(),

		// Traefik ingress - always enabled
		Traefik: DefaultTraefik(true),

		// cert-manager - always enabled
		CertManager: CertManagerConfig{
			Enabled: true,
			Cloudflare: CertManagerCloudflareConfig{
				Enabled:             hasDomain,
				Email:               cfg.GetCertEmail(),
				Production:          true, // Use production Let's Encrypt
				WildcardCertificate: hasDomain && cfg.WildcardCertificate,
			},
		},

		// Metrics server - always enabled
		MetricsServer: MetricsServerConfig{
			Enabled: true,
		},

		// ArgoCD - always enabled, with ingress when domain is set
		ArgoCD: expandArgoCD(cfg),

		// Gateway API CRDs - always enabled
		GatewayAPICRDs: DefaultGatewayAPICRDs(),

		// Prometheus Operator CRDs - always enabled
		PrometheusOperatorCRDs: DefaultPrometheusOperatorCRDs(),

		// Kube Prometheus Stack - enabled only when monitoring is set
		KubePrometheusStack: expandKubePrometheusStack(cfg),

		// Talos CCM - always enabled
		TalosCCM: TalosCCMConfig{
			Enabled: true,
			Version: vm.TalosCCM,
		},

		// Cloudflare - enabled only when domain is set
		// API token is read from CF_API_TOKEN environment variable
		Cloudflare: CloudflareConfig{
			Enabled:  hasDomain,
			Domain:   cfg.Domain,
			APIToken: os.Getenv("CF_API_TOKEN"),
		},

		// External DNS - enabled only when domain is set
		ExternalDNS: expandExternalDNS(cfg),

		// Talos Backup - enabled only when backup is set
		TalosBackup: expandTalosBackup(cfg),
	}
}

func expandTalosBackup(cfg *Spec) TalosBackupConfig {
	if !cfg.HasBackup() {
		return TalosBackupConfig{Enabled: false}
	}

	backup := DefaultTalosBackup()
	backup.Enabled = true
	backup.Schedule = "0 * * * *" // Hourly
	backup.S3Bucket = cfg.BackupBucketName()
	backup.S3Region = string(cfg.Region)
	backup.S3Endpoint = cfg.S3Endpoint()
	backup.S3AccessKey = os.Getenv("HETZNER_S3_ACCESS_KEY")
	backup.S3SecretKey = os.Getenv("HETZNER_S3_SECRET_KEY")
	return backup
}

func expandArgoCD(cfg *Spec) ArgoCDConfig {
	argoCfg := ArgoCDConfig{
		Enabled: true,
		HA:      cfg.Mode == ModeHA,
	}

	// Enable ingress with TLS when domain is configured
	if cfg.HasDomain() {
		argoCfg.IngressEnabled = true
		argoCfg.IngressHost = cfg.ArgoHost()
		argoCfg.IngressClassName = "traefik"
		argoCfg.IngressTLS = true
	}

	return argoCfg
}

func expandExternalDNS(cfg *Spec) ExternalDNSConfig {
	dns := DefaultExternalDNS(cfg.HasDomain())
	if dns.Enabled {
		dns.TXTOwnerID = cfg.Name
	}
	return dns
}

func expandKubePrometheusStack(cfg *Spec) KubePrometheusStackConfig {
	if !cfg.HasMonitoring() {
		return KubePrometheusStackConfig{Enabled: false}
	}

	promCfg := KubePrometheusStackConfig{
		Enabled: true,
		Grafana: KubePrometheusGrafanaConfig{},
		Prometheus: KubePrometheusPrometheusConfig{
			Persistence: DefaultPrometheusPersistence(),
		},
		Alertmanager: KubePrometheusAlertmanagerConfig{},
	}

	// Enable Grafana ingress with TLS when domain is configured
	if cfg.HasDomain() {
		promCfg.Grafana.IngressEnabled = true
		promCfg.Grafana.IngressHost = cfg.GrafanaHost()
		promCfg.Grafana.IngressClassName = "traefik"
		promCfg.Grafana.IngressTLS = true
	}

	return promCfg
}
