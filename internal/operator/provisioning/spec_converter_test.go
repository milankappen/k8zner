package provisioning

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	k8znerv1alpha1 "github.com/milankappen/k8zner/api/v1alpha1"
	"github.com/milankappen/k8zner/internal/config"
	"github.com/milankappen/k8zner/internal/util/ptr"
)

func baseCreds() *Credentials {
	return &Credentials{
		HCloudToken:        "test-token",
		CloudflareAPIToken: "cf-token",
	}
}

// --- SpecToConfig basic field mapping ---

func TestSpecToConfig_BasicFields(t *testing.T) {
	t.Parallel()
	cluster := newTestCluster("my-cluster", "example.com", &k8znerv1alpha1.AddonSpec{})
	cluster.Spec.Region = "nbg1"
	cluster.Spec.ControlPlanes = k8znerv1alpha1.ControlPlaneSpec{Count: 3, Size: "cx33"}
	cluster.Spec.Workers = k8znerv1alpha1.WorkerSpec{Count: 5, Size: "cpx31"}
	cluster.Spec.Kubernetes.Version = "1.32.2"
	cluster.Spec.Talos.Version = "v1.10.2"

	cfg, err := SpecToConfig(cluster, baseCreds())
	require.NoError(t, err)

	assert.Equal(t, "my-cluster", cfg.ClusterName)
	assert.Equal(t, "test-token", cfg.HCloudToken)
	assert.Equal(t, "nbg1", cfg.Location)

	// Control plane mapping
	require.Len(t, cfg.ControlPlane.NodePools, 1)
	assert.Equal(t, "control-plane", cfg.ControlPlane.NodePools[0].Name)
	assert.Equal(t, 3, cfg.ControlPlane.NodePools[0].Count)
	assert.Equal(t, "cx33", cfg.ControlPlane.NodePools[0].ServerType)
	assert.Equal(t, "nbg1", cfg.ControlPlane.NodePools[0].Location)

	// Kubernetes/Talos
	assert.Equal(t, "1.32.2", cfg.Kubernetes.Version)
	assert.Equal(t, "cluster.local", cfg.Kubernetes.Domain)
	assert.True(t, cfg.Kubernetes.APILoadBalancerEnabled)
	assert.Equal(t, "v1.10.2", cfg.Talos.Version)
}

func TestSpecToConfig_WorkerCountZero(t *testing.T) {
	t.Parallel()
	// Workers are created by the reconciliation loop, NOT the compute provisioner
	cluster := newTestCluster("test", "", &k8znerv1alpha1.AddonSpec{})
	cluster.Spec.Workers = k8znerv1alpha1.WorkerSpec{Count: 5, Size: "cx23"}

	cfg, err := SpecToConfig(cluster, baseCreds())
	require.NoError(t, err)

	require.Len(t, cfg.Workers, 1)
	assert.Equal(t, 0, cfg.Workers[0].Count, "worker count must be 0 — reconciler creates workers")
	assert.Equal(t, "cx23", cfg.Workers[0].ServerType)
	assert.Equal(t, "workers", cfg.Workers[0].Name)
}

func TestSpecToConfig_TalosConfig(t *testing.T) {
	t.Parallel()
	cluster := newTestCluster("test", "", &k8znerv1alpha1.AddonSpec{})
	cluster.Spec.Talos.SchematicID = "abc123"
	cluster.Spec.Talos.Extensions = []string{"siderolabs/qemu-guest-agent", "siderolabs/iscsi-tools"}

	cfg, err := SpecToConfig(cluster, baseCreds())
	require.NoError(t, err)

	assert.Equal(t, "abc123", cfg.Talos.SchematicID)
	assert.Equal(t, []string{"siderolabs/qemu-guest-agent", "siderolabs/iscsi-tools"}, cfg.Talos.Extensions)
}

// --- Network defaults ---

func TestSpecToConfig_NetworkDefaults(t *testing.T) {
	t.Parallel()
	cluster := newTestCluster("test", "", &k8znerv1alpha1.AddonSpec{})
	// Leave network spec empty to test defaults

	cfg, err := SpecToConfig(cluster, baseCreds())
	require.NoError(t, err)

	assert.Equal(t, "10.0.0.0/16", cfg.Network.IPv4CIDR)
	assert.Equal(t, "10.0.0.0/17", cfg.Network.NodeIPv4CIDR)
	assert.Equal(t, 25, cfg.Network.NodeIPv4SubnetMask)
	assert.Equal(t, "10.0.128.0/17", cfg.Network.PodIPv4CIDR)
	assert.Equal(t, "10.96.0.0/12", cfg.Network.ServiceIPv4CIDR)
}

func TestSpecToConfig_NetworkCustom(t *testing.T) {
	t.Parallel()
	cluster := newTestCluster("test", "", &k8znerv1alpha1.AddonSpec{})
	cluster.Spec.Network = k8znerv1alpha1.NetworkSpec{
		IPv4CIDR:     "172.16.0.0/16",
		NodeIPv4CIDR: "172.16.0.0/17",
		PodCIDR:      "172.16.128.0/17",
		ServiceCIDR:  "10.100.0.0/16",
	}

	cfg, err := SpecToConfig(cluster, baseCreds())
	require.NoError(t, err)

	assert.Equal(t, "172.16.0.0/16", cfg.Network.IPv4CIDR)
	assert.Equal(t, "172.16.0.0/17", cfg.Network.NodeIPv4CIDR)
	assert.Equal(t, "172.16.128.0/17", cfg.Network.PodIPv4CIDR)
	assert.Equal(t, "10.100.0.0/16", cfg.Network.ServiceIPv4CIDR)
}

// --- Addon config ---

func TestBuildAddonsConfig_AlwaysEnabled(t *testing.T) {
	t.Parallel()
	// Even with nil Addons, core addons must be enabled
	spec := &k8znerv1alpha1.K8znerClusterSpec{Addons: nil}
	addons := buildAddonsConfig(spec)

	assert.Equal(t, config.DefaultGatewayAPICRDs(), addons.GatewayAPICRDs, "GatewayAPICRDs uses shared default")
	assert.Equal(t, config.DefaultPrometheusOperatorCRDs(), addons.PrometheusOperatorCRDs, "PrometheusOperatorCRDs uses shared default")
	assert.True(t, addons.TalosCCM.Enabled, "TalosCCM always enabled")
	assert.Equal(t, config.DefaultVersionMatrix().TalosCCM, addons.TalosCCM.Version, "TalosCCM version from DefaultVersionMatrix")
	assert.Equal(t, config.DefaultCilium(), addons.Cilium, "Cilium uses shared default")
	assert.Equal(t, config.DefaultCCM(), addons.CCM, "CCM uses shared default")
	assert.Equal(t, config.DefaultCSI(), addons.CSI, "CSI uses shared default (includes DefaultStorageClass)")
	assert.True(t, addons.CSI.DefaultStorageClass, "CSI.DefaultStorageClass must be true")
}

func TestBuildAddonsConfig_CiliumDefaults(t *testing.T) {
	t.Parallel()
	spec := &k8znerv1alpha1.K8znerClusterSpec{Addons: nil}
	addons := buildAddonsConfig(spec)

	// Must exactly equal shared default
	assert.Equal(t, config.DefaultCilium(), addons.Cilium)
}

func TestBuildAddonsConfig_TraefikDefaults(t *testing.T) {
	t.Parallel()
	spec := &k8znerv1alpha1.K8znerClusterSpec{
		Addons: &k8znerv1alpha1.AddonSpec{Traefik: true},
	}
	addons := buildAddonsConfig(spec)

	// Must exactly equal shared default with enabled=true
	assert.Equal(t, config.DefaultTraefik(true), addons.Traefik)
}

func TestBuildAddonsConfig_ConditionalAddons(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		addons *k8znerv1alpha1.AddonSpec
		check  func(t *testing.T, a config.AddonsConfig)
	}{
		{
			name:   "nil addons disables optional addons",
			addons: nil,
			check: func(t *testing.T, a config.AddonsConfig) {
				assert.False(t, a.MetricsServer.Enabled)
				assert.False(t, a.CertManager.Enabled)
				assert.False(t, a.Traefik.Enabled)
				assert.False(t, a.ExternalDNS.Enabled)
				assert.False(t, a.ArgoCD.Enabled)
				assert.False(t, a.KubePrometheusStack.Enabled)
			},
		},
		{
			name:   "all optional addons enabled",
			addons: &k8znerv1alpha1.AddonSpec{MetricsServer: true, CertManager: true, Traefik: true, ExternalDNS: true, ArgoCD: true, Monitoring: true},
			check: func(t *testing.T, a config.AddonsConfig) {
				assert.True(t, a.MetricsServer.Enabled)
				assert.True(t, a.CertManager.Enabled)
				assert.True(t, a.Traefik.Enabled)
				assert.True(t, a.ExternalDNS.Enabled)
				assert.True(t, a.ArgoCD.Enabled)
				assert.True(t, a.KubePrometheusStack.Enabled)
			},
		},
		{
			name:   "selective enablement",
			addons: &k8znerv1alpha1.AddonSpec{MetricsServer: true, Traefik: true},
			check: func(t *testing.T, a config.AddonsConfig) {
				assert.True(t, a.MetricsServer.Enabled)
				assert.True(t, a.Traefik.Enabled)
				assert.False(t, a.CertManager.Enabled)
				assert.False(t, a.ExternalDNS.Enabled)
				assert.False(t, a.ArgoCD.Enabled)
				assert.False(t, a.KubePrometheusStack.Enabled)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec := &k8znerv1alpha1.K8znerClusterSpec{Addons: tt.addons}
			tt.check(t, buildAddonsConfig(spec))
		})
	}
}

// --- Backup configuration ---

func TestConfigureBackup_Disabled(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	spec := &k8znerv1alpha1.K8znerClusterSpec{Backup: nil}

	configureBackup(cfg, spec, baseCreds())
	assert.False(t, cfg.Addons.TalosBackup.Enabled)
}

func TestConfigureBackup_EnabledNoCreds(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	spec := &k8znerv1alpha1.K8znerClusterSpec{
		Backup: &k8znerv1alpha1.BackupSpec{Enabled: true, Schedule: "0 * * * *"},
	}

	configureBackup(cfg, spec, &Credentials{})
	assert.False(t, cfg.Addons.TalosBackup.Enabled, "backup requires S3 credentials")
}

func TestConfigureBackup_EnabledWithCreds(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	spec := &k8znerv1alpha1.K8znerClusterSpec{
		Backup: &k8znerv1alpha1.BackupSpec{Enabled: true, Schedule: "0 */6 * * *"},
	}
	creds := &Credentials{
		BackupS3AccessKey: "ak",
		BackupS3SecretKey: "sk",
		BackupS3Endpoint:  "s3.example.com",
		BackupS3Bucket:    "my-backups",
		BackupS3Region:    "eu-central-1",
	}

	configureBackup(cfg, spec, creds)

	assert.True(t, cfg.Addons.TalosBackup.Enabled)
	assert.Equal(t, "0 */6 * * *", cfg.Addons.TalosBackup.Schedule)
	assert.Equal(t, "ak", cfg.Addons.TalosBackup.S3AccessKey)
	assert.Equal(t, "sk", cfg.Addons.TalosBackup.S3SecretKey)
	assert.Equal(t, "s3.example.com", cfg.Addons.TalosBackup.S3Endpoint)
	assert.Equal(t, "my-backups", cfg.Addons.TalosBackup.S3Bucket)
	assert.Equal(t, "eu-central-1", cfg.Addons.TalosBackup.S3Region)
	assert.True(t, cfg.Addons.TalosBackup.EncryptionDisabled, "no age key in operator path")
	assert.Equal(t, "etcd-backups", cfg.Addons.TalosBackup.S3Prefix, "S3Prefix from DefaultTalosBackup")
	assert.True(t, cfg.Addons.TalosBackup.EnableCompression, "EnableCompression from DefaultTalosBackup")
}

// --- Cloudflare configuration ---

func TestConfigureCloudflare_ExternalDNSDisabled(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{} // ExternalDNS not enabled
	spec := &k8znerv1alpha1.K8znerClusterSpec{Domain: "example.com"}

	configureCloudflare(cfg, spec, baseCreds(), "my-cluster")
	assert.False(t, cfg.Addons.Cloudflare.Enabled)
}

func TestConfigureCloudflare_Enabled(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Addons.ExternalDNS.Enabled = true
	spec := &k8znerv1alpha1.K8znerClusterSpec{Domain: "example.com"}

	configureCloudflare(cfg, spec, baseCreds(), "my-cluster")

	assert.True(t, cfg.Addons.Cloudflare.Enabled)
	assert.Equal(t, "cf-token", cfg.Addons.Cloudflare.APIToken)
	assert.Equal(t, "example.com", cfg.Addons.Cloudflare.Domain)
	assert.Equal(t, "my-cluster", cfg.Addons.ExternalDNS.TXTOwnerID)
}

func TestConfigureCloudflare_CertManagerDNS01(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Addons.ExternalDNS.Enabled = true
	cfg.Addons.CertManager.Enabled = true
	spec := &k8znerv1alpha1.K8znerClusterSpec{Domain: "example.com"}

	configureCloudflare(cfg, spec, baseCreds(), "test")

	assert.True(t, cfg.Addons.CertManager.Cloudflare.Enabled)
	assert.True(t, cfg.Addons.CertManager.Cloudflare.Production)
	assert.Equal(t, "admin@example.com", cfg.Addons.CertManager.Cloudflare.Email)
}

func TestConfigureCloudflare_CertManagerNoDomain(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Addons.ExternalDNS.Enabled = true
	cfg.Addons.CertManager.Enabled = true
	spec := &k8znerv1alpha1.K8znerClusterSpec{Domain: ""} // no domain

	configureCloudflare(cfg, spec, baseCreds(), "test")

	assert.False(t, cfg.Addons.CertManager.Cloudflare.Enabled, "DNS-01 requires domain")
}

// --- normalizeServerSize ---

func TestNormalizeServerSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected string
	}{
		{"cx22", "cx23"},
		{"cx32", "cx33"},
		{"cx42", "cx43"},
		{"cx52", "cx53"},
		{"CX22", "cx23"},   // case insensitive
		{"cx23", "cx23"},   // current size passes through
		{"cpx31", "cpx31"}, // non-legacy passes through
		{"cx33", "cx33"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, string(config.ServerSize(tt.input).Normalize()))
		})
	}
}

// --- expandFirewallFromSpec ---

func TestExpandFirewallFromSpec(t *testing.T) {
	t.Parallel()
	spec := &k8znerv1alpha1.K8znerClusterSpec{}
	fw := expandFirewallFromSpec(spec)

	require.NotNil(t, fw.UseCurrentIPv4)
	assert.True(t, *fw.UseCurrentIPv4)
	require.NotNil(t, fw.UseCurrentIPv6)
	assert.True(t, *fw.UseCurrentIPv6)
}

// --- expandArgoCDFromSpec ---

func TestExpandArgoCDFromSpec(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		spec          *k8znerv1alpha1.K8znerClusterSpec
		expectEnabled bool
		expectIngress bool
		expectHost    string
	}{
		{
			name:          "disabled when addon nil",
			spec:          &k8znerv1alpha1.K8znerClusterSpec{},
			expectEnabled: false,
		},
		{
			name:          "disabled when argocd false",
			spec:          &k8znerv1alpha1.K8znerClusterSpec{Addons: &k8znerv1alpha1.AddonSpec{ArgoCD: false}},
			expectEnabled: false,
		},
		{
			name:          "enabled no domain means no ingress",
			spec:          &k8znerv1alpha1.K8znerClusterSpec{Addons: &k8znerv1alpha1.AddonSpec{ArgoCD: true}},
			expectEnabled: true,
			expectIngress: false,
		},
		{
			name:          "enabled with domain uses default subdomain",
			spec:          &k8znerv1alpha1.K8znerClusterSpec{Domain: "example.com", Addons: &k8znerv1alpha1.AddonSpec{ArgoCD: true}},
			expectEnabled: true,
			expectIngress: true,
			expectHost:    "argo.example.com",
		},
		{
			name:          "custom subdomain",
			spec:          &k8znerv1alpha1.K8znerClusterSpec{Domain: "example.com", Addons: &k8znerv1alpha1.AddonSpec{ArgoCD: true, ArgoSubdomain: "gitops"}},
			expectEnabled: true,
			expectIngress: true,
			expectHost:    "gitops.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := expandArgoCDFromSpec(tt.spec)
			assert.Equal(t, tt.expectEnabled, result.Enabled)
			assert.Equal(t, tt.expectIngress, result.IngressEnabled)
			if tt.expectHost != "" {
				assert.Equal(t, tt.expectHost, result.IngressHost)
				assert.Equal(t, "traefik", result.IngressClassName)
				assert.True(t, result.IngressTLS)
			}
		})
	}
}

// --- expandMonitoringFromSpec ---

func TestExpandMonitoringFromSpec(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		spec          *k8znerv1alpha1.K8znerClusterSpec
		expectEnabled bool
		expectIngress bool
		expectHost    string
	}{
		{
			name:          "disabled",
			spec:          &k8znerv1alpha1.K8znerClusterSpec{},
			expectEnabled: false,
		},
		{
			name:          "enabled no domain",
			spec:          &k8znerv1alpha1.K8znerClusterSpec{Addons: &k8znerv1alpha1.AddonSpec{Monitoring: true}},
			expectEnabled: true,
			expectIngress: false,
		},
		{
			name:          "enabled with domain uses default subdomain",
			spec:          &k8znerv1alpha1.K8znerClusterSpec{Domain: "example.com", Addons: &k8znerv1alpha1.AddonSpec{Monitoring: true}},
			expectEnabled: true,
			expectIngress: true,
			expectHost:    "grafana.example.com",
		},
		{
			name:          "custom subdomain",
			spec:          &k8znerv1alpha1.K8znerClusterSpec{Domain: "example.com", Addons: &k8znerv1alpha1.AddonSpec{Monitoring: true, GrafanaSubdomain: "metrics"}},
			expectEnabled: true,
			expectIngress: true,
			expectHost:    "metrics.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := expandMonitoringFromSpec(tt.spec)
			assert.Equal(t, tt.expectEnabled, result.Enabled)
			assert.Equal(t, tt.expectIngress, result.Grafana.IngressEnabled)
			if tt.expectHost != "" {
				assert.Equal(t, tt.expectHost, result.Grafana.IngressHost)
				assert.Equal(t, "traefik", result.Grafana.IngressClassName)
				assert.True(t, result.Grafana.IngressTLS)
			}
			if tt.expectEnabled {
				assert.Equal(t, config.DefaultPrometheusPersistence(), result.Prometheus.Persistence, "Prometheus persistence from shared default")
			}
		})
	}
}

// --- expandExternalDNSFromSpec ---

func TestExpandExternalDNSFromSpec(t *testing.T) {
	t.Parallel()

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()
		result := expandExternalDNSFromSpec(&k8znerv1alpha1.K8znerClusterSpec{})
		assert.False(t, result.Enabled)
		assert.Empty(t, result.Policy)
		assert.Empty(t, result.Sources)
	})

	t.Run("enabled", func(t *testing.T) {
		t.Parallel()
		result := expandExternalDNSFromSpec(&k8znerv1alpha1.K8znerClusterSpec{
			Addons: &k8znerv1alpha1.AddonSpec{ExternalDNS: true},
		})
		assert.True(t, result.Enabled)
		assert.Equal(t, "sync", result.Policy)
		assert.Equal(t, []string{"ingress"}, result.Sources)
	})
}

// --- resolveEndpoint ---

func TestResolveEndpoint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		cluster        *k8znerv1alpha1.K8znerCluster
		expectEndpoint string
		expectError    bool
	}{
		{
			name: "LoadBalancerPrivateIP takes precedence",
			cluster: &k8znerv1alpha1.K8znerCluster{
				Status: k8znerv1alpha1.K8znerClusterStatus{
					ControlPlaneEndpoint: "1.2.3.4",
					Infrastructure: k8znerv1alpha1.InfrastructureStatus{
						LoadBalancerIP:        "5.6.7.8",
						LoadBalancerPrivateIP: "10.0.0.2",
					},
				},
			},
			expectEndpoint: "https://10.0.0.2:6443",
		},
		{
			name: "ControlPlaneEndpoint as IP",
			cluster: &k8znerv1alpha1.K8znerCluster{
				Status: k8znerv1alpha1.K8znerClusterStatus{
					ControlPlaneEndpoint: "1.2.3.4",
				},
			},
			expectEndpoint: "https://1.2.3.4:6443",
		},
		{
			name: "ControlPlaneEndpoint as full URL",
			cluster: &k8znerv1alpha1.K8znerCluster{
				Status: k8znerv1alpha1.K8znerClusterStatus{
					ControlPlaneEndpoint: "https://my-lb.example.com:6443",
				},
			},
			expectEndpoint: "https://my-lb.example.com:6443",
		},
		{
			name: "LoadBalancerIP fallback",
			cluster: &k8znerv1alpha1.K8znerCluster{
				Status: k8znerv1alpha1.K8znerClusterStatus{
					Infrastructure: k8znerv1alpha1.InfrastructureStatus{
						LoadBalancerIP: "5.6.7.8",
					},
				},
			},
			expectEndpoint: "https://5.6.7.8:6443",
		},
		{
			name: "CP node private IP fallback",
			cluster: &k8znerv1alpha1.K8znerCluster{
				Status: k8znerv1alpha1.K8znerClusterStatus{
					ControlPlanes: k8znerv1alpha1.NodeGroupStatus{
						Nodes: []k8znerv1alpha1.NodeStatus{
							{Name: "cp-1", PrivateIP: "10.0.0.5", PublicIP: "1.2.3.4"},
						},
					},
				},
			},
			expectEndpoint: "https://10.0.0.5:6443",
		},
		{
			name: "CP node public IP fallback",
			cluster: &k8znerv1alpha1.K8znerCluster{
				Status: k8znerv1alpha1.K8znerClusterStatus{
					ControlPlanes: k8znerv1alpha1.NodeGroupStatus{
						Nodes: []k8znerv1alpha1.NodeStatus{
							{Name: "cp-1", PublicIP: "1.2.3.4"},
						},
					},
				},
			},
			expectEndpoint: "https://1.2.3.4:6443",
		},
		{
			name:        "no endpoint available",
			cluster:     &k8znerv1alpha1.K8znerCluster{},
			expectError: true,
		},
		{
			name: "CP node with no IPs",
			cluster: &k8znerv1alpha1.K8znerCluster{
				Status: k8znerv1alpha1.K8znerClusterStatus{
					ControlPlanes: k8znerv1alpha1.NodeGroupStatus{
						Nodes: []k8znerv1alpha1.NodeStatus{
							{Name: "cp-1"},
						},
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			endpoint, err := resolveEndpoint(tt.cluster)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectEndpoint, endpoint)
		})
	}
}

// --- buildMachineConfigOptions ---

func TestBuildMachineConfigOptions(t *testing.T) {
	t.Parallel()
	cluster := &k8znerv1alpha1.K8znerCluster{
		Spec: k8znerv1alpha1.K8znerClusterSpec{
			Talos: k8znerv1alpha1.TalosSpec{SchematicID: "schematic-123"},
		},
	}

	opts := buildMachineConfigOptions(cluster)

	assert.Equal(t, "schematic-123", opts.SchematicID)
	assert.True(t, opts.StateEncryption)
	assert.True(t, opts.EphemeralEncryption)
	assert.True(t, opts.IPv6Enabled)
	assert.True(t, opts.PublicIPv4Enabled)
	assert.True(t, opts.PublicIPv6Enabled)
	assert.True(t, opts.CoreDNSEnabled)
	assert.True(t, opts.DiscoveryServiceEnabled)
	assert.True(t, opts.KubeProxyReplacement, "Cilium replaces kube-proxy")
}

func TestBuildMachineConfigOptions_NetworkDefaults(t *testing.T) {
	t.Parallel()
	cluster := &k8znerv1alpha1.K8znerCluster{
		Spec: k8znerv1alpha1.K8znerClusterSpec{},
	}

	opts := buildMachineConfigOptions(cluster)

	assert.Equal(t, "10.0.0.0/16", opts.NodeIPv4CIDR)
	assert.Equal(t, "10.0.128.0/17", opts.PodIPv4CIDR)
	assert.Equal(t, "10.96.0.0/12", opts.ServiceIPv4CIDR)
	assert.Equal(t, "10.0.0.0/16", opts.EtcdSubnet)
}

func TestBuildMachineConfigOptions_NetworkCustom(t *testing.T) {
	t.Parallel()
	cluster := &k8znerv1alpha1.K8znerCluster{
		Spec: k8znerv1alpha1.K8znerClusterSpec{
			Network: k8znerv1alpha1.NetworkSpec{
				IPv4CIDR:    "172.16.0.0/16",
				PodCIDR:     "172.16.128.0/17",
				ServiceCIDR: "10.100.0.0/16",
			},
		},
	}

	opts := buildMachineConfigOptions(cluster)

	assert.Equal(t, "172.16.0.0/16", opts.NodeIPv4CIDR)
	assert.Equal(t, "172.16.128.0/17", opts.PodIPv4CIDR)
	assert.Equal(t, "10.100.0.0/16", opts.ServiceIPv4CIDR)
	assert.Equal(t, "172.16.0.0/16", opts.EtcdSubnet)
}

// --- parseSecretsFromBytes ---

func TestParseSecretsFromBytes_Empty(t *testing.T) {
	t.Parallel()
	_, err := parseSecretsFromBytes([]byte{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty secrets data")
}

func TestParseSecretsFromBytes_InvalidYAML(t *testing.T) {
	t.Parallel()
	_, err := parseSecretsFromBytes([]byte("not: valid: yaml: {{{"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal")
}

func TestParseSecretsFromBytes_NilInput(t *testing.T) {
	t.Parallel()
	_, err := parseSecretsFromBytes(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty secrets data")
}

func TestParseSecretsFromBytes_ValidYAML(t *testing.T) {
	t.Parallel()
	// A minimal YAML that will unmarshal into a secrets.Bundle without error.
	// The Bundle struct has pointer fields so empty YAML {} is fine.
	sb, err := parseSecretsFromBytes([]byte("{}"))
	require.NoError(t, err)
	require.NotNil(t, sb)
	assert.NotNil(t, sb.Clock, "clock should be re-injected")
}

func TestParseSecretsFromBytes_ValidYAMLWithFields(t *testing.T) {
	t.Parallel()
	// Provide a YAML with the Cluster key set to verify parsing
	yamlData := `
Cluster:
  ID: "test-cluster-id"
  Secret: "test-cluster-secret"
`
	sb, err := parseSecretsFromBytes([]byte(yamlData))
	require.NoError(t, err)
	require.NotNil(t, sb)
	assert.NotNil(t, sb.Clock, "clock should be re-injected")
	if sb.Cluster != nil {
		assert.Equal(t, "test-cluster-id", sb.Cluster.ID)
		assert.Equal(t, "test-cluster-secret", sb.Cluster.Secret)
	}
}

// --- defaultString helper ---

func TestDefaultString(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "custom", defaultString("custom", "default"))
	assert.Equal(t, "default", defaultString("", "default"))
}

func TestDefaultString_BothEmpty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", defaultString("", ""))
}

func TestDefaultString_WhitespaceNotEmpty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, " ", defaultString(" ", "default"), "whitespace is not empty")
}

// --- boolPtr helper ---

func TestPtrBool(t *testing.T) {
	t.Parallel()

	trueVal := ptr.Bool(true)
	require.NotNil(t, trueVal)
	assert.True(t, *trueVal)

	falseVal := ptr.Bool(false)
	require.NotNil(t, falseVal)
	assert.False(t, *falseVal)
}

// --- Integration: SpecToConfig end-to-end ---

func TestSpecToConfig_FullRoundTrip(t *testing.T) {
	t.Parallel()
	// Simulate a realistic production cluster spec
	cluster := &k8znerv1alpha1.K8znerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "prod-cluster"},
		Spec: k8znerv1alpha1.K8znerClusterSpec{
			Region:     "fsn1",
			Domain:     "k8s.example.com",
			Kubernetes: k8znerv1alpha1.KubernetesSpec{Version: "1.32.2"},
			Talos:      k8znerv1alpha1.TalosSpec{Version: "v1.10.2", SchematicID: "abc123"},
			ControlPlanes: k8znerv1alpha1.ControlPlaneSpec{
				Count: 3, Size: "cx33",
			},
			Workers: k8znerv1alpha1.WorkerSpec{
				Count: 5, Size: "cpx31",
			},
			Addons: &k8znerv1alpha1.AddonSpec{
				Traefik:       true,
				CertManager:   true,
				ExternalDNS:   true,
				ArgoCD:        true,
				Monitoring:    true,
				MetricsServer: true,
			},
			Backup: &k8znerv1alpha1.BackupSpec{
				Enabled:  true,
				Schedule: "0 */6 * * *",
			},
		},
	}
	creds := &Credentials{
		HCloudToken:        "hcloud-test",
		CloudflareAPIToken: "cf-test",
		BackupS3AccessKey:  "s3-ak",
		BackupS3SecretKey:  "s3-sk",
		BackupS3Endpoint:   "s3.eu-central.example.com",
		BackupS3Bucket:     "backups",
		BackupS3Region:     "eu-central-1",
	}

	cfg, err := SpecToConfig(cluster, creds)
	require.NoError(t, err)

	// Core fields
	assert.Equal(t, "prod-cluster", cfg.ClusterName)
	assert.Equal(t, "hcloud-test", cfg.HCloudToken)
	assert.Equal(t, "fsn1", cfg.Location)

	// CP
	require.Len(t, cfg.ControlPlane.NodePools, 1)
	assert.Equal(t, 3, cfg.ControlPlane.NodePools[0].Count)
	assert.Equal(t, "cx33", cfg.ControlPlane.NodePools[0].ServerType)

	// Workers (count must be 0)
	require.Len(t, cfg.Workers, 1)
	assert.Equal(t, 0, cfg.Workers[0].Count)
	assert.Equal(t, "cpx31", cfg.Workers[0].ServerType)

	// Always-on addons
	assert.True(t, cfg.Addons.Cilium.Enabled)
	assert.True(t, cfg.Addons.Cilium.KubeProxyReplacementEnabled)
	assert.Equal(t, "tunnel", cfg.Addons.Cilium.RoutingMode)
	assert.True(t, cfg.Addons.CCM.Enabled)
	assert.True(t, cfg.Addons.CSI.Enabled)
	assert.True(t, cfg.Addons.GatewayAPICRDs.Enabled)

	// Optional addons
	assert.True(t, cfg.Addons.Traefik.Enabled)
	assert.True(t, cfg.Addons.CertManager.Enabled)
	assert.True(t, cfg.Addons.MetricsServer.Enabled)

	// ArgoCD ingress
	assert.True(t, cfg.Addons.ArgoCD.Enabled)
	assert.True(t, cfg.Addons.ArgoCD.IngressEnabled)
	assert.Equal(t, "argo.k8s.example.com", cfg.Addons.ArgoCD.IngressHost)

	// Monitoring ingress
	assert.True(t, cfg.Addons.KubePrometheusStack.Enabled)
	assert.True(t, cfg.Addons.KubePrometheusStack.Grafana.IngressEnabled)
	assert.Equal(t, "grafana.k8s.example.com", cfg.Addons.KubePrometheusStack.Grafana.IngressHost)

	// Cloudflare
	assert.True(t, cfg.Addons.Cloudflare.Enabled)
	assert.Equal(t, "k8s.example.com", cfg.Addons.Cloudflare.Domain)
	assert.Equal(t, "prod-cluster", cfg.Addons.ExternalDNS.TXTOwnerID)

	// CertManager DNS-01
	assert.True(t, cfg.Addons.CertManager.Cloudflare.Enabled)
	assert.Equal(t, "admin@k8s.example.com", cfg.Addons.CertManager.Cloudflare.Email)

	// Backup
	assert.True(t, cfg.Addons.TalosBackup.Enabled)
	assert.Equal(t, "0 */6 * * *", cfg.Addons.TalosBackup.Schedule)
	assert.True(t, cfg.Addons.TalosBackup.EncryptionDisabled)
	assert.Equal(t, "etcd-backups", cfg.Addons.TalosBackup.S3Prefix, "S3Prefix from DefaultTalosBackup")
	assert.True(t, cfg.Addons.TalosBackup.EnableCompression, "EnableCompression from DefaultTalosBackup")

	// Prometheus persistence (bug fix)
	assert.Equal(t, config.DefaultPrometheusPersistence(), cfg.Addons.KubePrometheusStack.Prometheus.Persistence, "Prometheus persistence from shared default")

	// Network defaults applied
	assert.Equal(t, "10.0.0.0/16", cfg.Network.IPv4CIDR)

	// Firewall
	require.NotNil(t, cfg.Firewall.UseCurrentIPv4)
	assert.True(t, *cfg.Firewall.UseCurrentIPv4)
}

// --- SpecToConfig additional coverage ---

func TestSpecToConfig_AllNetworkDefaultsPreFilled(t *testing.T) {
	t.Parallel()
	// When all network CIDRs are empty, SpecToConfig uses configv2 defaults
	cluster := newTestCluster("test", "", nil)

	cfg, err := SpecToConfig(cluster, baseCreds())
	require.NoError(t, err)

	// Verify the defaults are applied via defaultString
	assert.NotEmpty(t, cfg.Network.IPv4CIDR)
	assert.NotEmpty(t, cfg.Network.NodeIPv4CIDR)
	assert.NotEmpty(t, cfg.Network.PodIPv4CIDR)
	assert.NotEmpty(t, cfg.Network.ServiceIPv4CIDR)
	assert.Equal(t, 25, cfg.Network.NodeIPv4SubnetMask)
}

// --- configureBackup edge cases ---

func TestConfigureBackup_EnabledFalse(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	spec := &k8znerv1alpha1.K8znerClusterSpec{
		Backup: &k8znerv1alpha1.BackupSpec{Enabled: false, Schedule: "0 * * * *"},
	}
	creds := &Credentials{
		BackupS3AccessKey: "ak",
		BackupS3SecretKey: "sk",
	}

	configureBackup(cfg, spec, creds)
	assert.False(t, cfg.Addons.TalosBackup.Enabled, "backup disabled overrides having creds")
}

func TestConfigureBackup_OnlyAccessKey(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	spec := &k8znerv1alpha1.K8znerClusterSpec{
		Backup: &k8znerv1alpha1.BackupSpec{Enabled: true, Schedule: "0 * * * *"},
	}
	creds := &Credentials{
		BackupS3AccessKey: "ak",
		// BackupS3SecretKey is missing
	}

	configureBackup(cfg, spec, creds)
	assert.False(t, cfg.Addons.TalosBackup.Enabled, "needs both access key and secret key")
}

func TestConfigureBackup_OnlySecretKey(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	spec := &k8znerv1alpha1.K8znerClusterSpec{
		Backup: &k8znerv1alpha1.BackupSpec{Enabled: true, Schedule: "0 * * * *"},
	}
	creds := &Credentials{
		// BackupS3AccessKey is missing
		BackupS3SecretKey: "sk",
	}

	configureBackup(cfg, spec, creds)
	assert.False(t, cfg.Addons.TalosBackup.Enabled, "needs both access key and secret key")
}

// --- configureCloudflare edge cases ---

func TestConfigureCloudflare_CertManagerDisabled(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Addons.ExternalDNS.Enabled = true
	cfg.Addons.CertManager.Enabled = false // CertManager disabled
	spec := &k8znerv1alpha1.K8znerClusterSpec{Domain: "example.com"}

	configureCloudflare(cfg, spec, baseCreds(), "my-cluster")

	assert.True(t, cfg.Addons.Cloudflare.Enabled)
	assert.False(t, cfg.Addons.CertManager.Cloudflare.Enabled, "DNS-01 requires CertManager enabled")
}

// --- expandArgoCDFromSpec edge cases ---

func TestExpandArgoCDFromSpec_EmptySubdomainField(t *testing.T) {
	t.Parallel()
	// ArgoSubdomain is empty string, should use default "argo"
	spec := &k8znerv1alpha1.K8znerClusterSpec{
		Domain: "example.com",
		Addons: &k8znerv1alpha1.AddonSpec{ArgoCD: true, ArgoSubdomain: ""},
	}

	result := expandArgoCDFromSpec(spec)
	assert.Equal(t, "argo.example.com", result.IngressHost, "empty subdomain should use default 'argo'")
}

// --- expandMonitoringFromSpec edge cases ---

func TestExpandMonitoringFromSpec_EmptyGrafanaSubdomain(t *testing.T) {
	t.Parallel()
	// GrafanaSubdomain is empty string, should use default "grafana"
	spec := &k8znerv1alpha1.K8znerClusterSpec{
		Domain: "example.com",
		Addons: &k8znerv1alpha1.AddonSpec{Monitoring: true, GrafanaSubdomain: ""},
	}

	result := expandMonitoringFromSpec(spec)
	assert.Equal(t, "grafana.example.com", result.Grafana.IngressHost, "empty subdomain should use default 'grafana'")
}

func TestExpandMonitoringFromSpec_DisabledWithDomain(t *testing.T) {
	t.Parallel()
	spec := &k8znerv1alpha1.K8znerClusterSpec{
		Domain: "example.com",
		Addons: &k8znerv1alpha1.AddonSpec{Monitoring: false},
	}

	result := expandMonitoringFromSpec(spec)
	assert.False(t, result.Enabled)
	assert.False(t, result.Grafana.IngressEnabled, "disabled monitoring should not set up ingress")
	assert.Empty(t, result.Grafana.IngressHost)
}

// --- expandExternalDNSFromSpec edge cases ---

func TestExpandExternalDNSFromSpec_NilAddons(t *testing.T) {
	t.Parallel()
	result := expandExternalDNSFromSpec(&k8znerv1alpha1.K8znerClusterSpec{Addons: nil})
	assert.False(t, result.Enabled)
	assert.Empty(t, result.Policy)
	assert.Empty(t, result.Sources)
}

// --- resolveEndpoint edge cases ---

func TestResolveEndpoint_EmptyNodes(t *testing.T) {
	t.Parallel()
	cluster := &k8znerv1alpha1.K8znerCluster{
		Status: k8znerv1alpha1.K8znerClusterStatus{
			ControlPlanes: k8znerv1alpha1.NodeGroupStatus{
				Nodes: []k8znerv1alpha1.NodeStatus{}, // empty slice
			},
		},
	}
	_, err := resolveEndpoint(cluster)
	assert.Error(t, err)
}

// --- SpecToConfig with minimal spec ---

func TestSpecToConfig_MinimalSpec(t *testing.T) {
	t.Parallel()
	cluster := &k8znerv1alpha1.K8znerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "min"},
		Spec: k8znerv1alpha1.K8znerClusterSpec{
			Region:         "fsn1",
			ControlPlanes:  k8znerv1alpha1.ControlPlaneSpec{Count: 1, Size: "cx23"},
			Workers:        k8znerv1alpha1.WorkerSpec{Count: 1, Size: "cx23"},
			Kubernetes:     k8znerv1alpha1.KubernetesSpec{Version: "1.32.0"},
			Talos:          k8znerv1alpha1.TalosSpec{Version: "v1.10.0"},
			CredentialsRef: corev1.LocalObjectReference{Name: "creds"},
			// No Addons, no Domain, no Backup
		},
	}

	cfg, err := SpecToConfig(cluster, baseCreds())
	require.NoError(t, err)

	// Core always enabled
	assert.True(t, cfg.Addons.Cilium.Enabled)
	assert.True(t, cfg.Addons.CCM.Enabled)
	assert.True(t, cfg.Addons.CSI.Enabled)

	// Optional addons disabled
	assert.False(t, cfg.Addons.MetricsServer.Enabled)
	assert.False(t, cfg.Addons.CertManager.Enabled)
	assert.False(t, cfg.Addons.Traefik.Enabled)
	assert.False(t, cfg.Addons.ExternalDNS.Enabled)
	assert.False(t, cfg.Addons.ArgoCD.Enabled)
	assert.False(t, cfg.Addons.KubePrometheusStack.Enabled)
	assert.False(t, cfg.Addons.TalosBackup.Enabled)

	// No cloudflare
	assert.False(t, cfg.Addons.Cloudflare.Enabled)
}

func TestConfigureCloudflare_WildcardCertificate(t *testing.T) {
	t.Parallel()

	newCfg := func() *config.Config {
		cfg := &config.Config{}
		cfg.Addons.ExternalDNS.Enabled = true
		cfg.Addons.CertManager.Enabled = true
		return cfg
	}

	t.Run("disabled by default", func(t *testing.T) {
		t.Parallel()
		cfg := newCfg()
		spec := &k8znerv1alpha1.K8znerClusterSpec{Domain: "example.com"}

		configureCloudflare(cfg, spec, baseCreds(), "test")
		assert.False(t, cfg.Addons.CertManager.Cloudflare.WildcardCertificate)
	})

	t.Run("enabled from CRD spec", func(t *testing.T) {
		t.Parallel()
		cfg := newCfg()
		spec := &k8znerv1alpha1.K8znerClusterSpec{
			Domain: "example.com",
			Addons: &k8znerv1alpha1.AddonSpec{WildcardCertificate: true},
		}

		configureCloudflare(cfg, spec, baseCreds(), "test")
		assert.True(t, cfg.Addons.CertManager.Cloudflare.WildcardCertificate)
	})
}

func TestSpecToConfig_Logging(t *testing.T) {
	t.Parallel()

	t.Run("disabled by default", func(t *testing.T) {
		t.Parallel()
		spec := &k8znerv1alpha1.K8znerClusterSpec{}
		cluster := &k8znerv1alpha1.K8znerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
			Spec:       *spec,
		}

		cfg, err := SpecToConfig(cluster, baseCreds())
		require.NoError(t, err)
		assert.False(t, cfg.Addons.Logging.Enabled)
	})

	t.Run("enabled from CRD spec with default retention", func(t *testing.T) {
		t.Parallel()
		cluster := &k8znerv1alpha1.K8znerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
			Spec: k8znerv1alpha1.K8znerClusterSpec{
				Addons: &k8znerv1alpha1.AddonSpec{Logging: true},
			},
		}

		cfg, err := SpecToConfig(cluster, baseCreds())
		require.NoError(t, err)
		assert.True(t, cfg.Addons.Logging.Enabled)
		assert.Equal(t, "168h", cfg.Addons.Logging.Retention)
	})
}
