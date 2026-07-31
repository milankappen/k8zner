package handlers

import (
	"context"
	"errors"
	"testing"

	hcloudgo "github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	k8znerv1alpha1 "github.com/milankappen/k8zner/api/v1alpha1"
	"github.com/milankappen/k8zner/internal/config"
	"github.com/milankappen/k8zner/internal/platform/hcloud"
	"github.com/milankappen/k8zner/internal/provisioning"
)

func TestBuildK8znerCluster(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		ClusterName: "prod",
		Location:    "nbg1",
		Network: config.NetworkConfig{
			IPv4CIDR:        "10.0.0.0/16",
			NodeIPv4CIDR:    "10.0.0.0/20",
			PodIPv4CIDR:     "10.244.0.0/16",
			ServiceIPv4CIDR: "10.96.0.0/16",
		},
		ControlPlane: config.ControlPlaneConfig{
			NodePools: []config.ControlPlaneNodePool{
				{Name: "cp", Count: 3, ServerType: "cx32"},
			},
		},
		Workers: []config.WorkerNodePool{
			{Name: "workers", Count: 2, ServerType: "cx22"},
		},
		Kubernetes: config.KubernetesConfig{Version: "1.30.0"},
		Talos:      config.TalosConfig{Version: "1.7.0", SchematicID: "abc123"},
	}

	infraInfo := &InfrastructureInfo{
		NetworkID:             100,
		FirewallID:            200,
		LoadBalancerID:        300,
		LoadBalancerIP:        "1.2.3.4",
		LoadBalancerPrivateIP: "10.0.0.254",
	}

	cluster := buildK8znerCluster(cfg, infraInfo, "cp-1", 12345, "5.6.7.8")

	assert.Equal(t, "prod", cluster.Name)
	assert.Equal(t, k8znerNamespace, cluster.Namespace)
	assert.Equal(t, "prod", cluster.Labels["cluster"])

	// Spec
	assert.Equal(t, "nbg1", cluster.Spec.Region)
	assert.Equal(t, 3, cluster.Spec.ControlPlanes.Count)
	assert.Equal(t, "cx32", cluster.Spec.ControlPlanes.Size)
	assert.Equal(t, 2, cluster.Spec.Workers.Count)
	assert.Equal(t, "cx22", cluster.Spec.Workers.Size)
	assert.Equal(t, "10.0.0.0/16", cluster.Spec.Network.IPv4CIDR)
	assert.Equal(t, "1.30.0", cluster.Spec.Kubernetes.Version)
	assert.Equal(t, "1.7.0", cluster.Spec.Talos.Version)
	assert.Equal(t, "abc123", cluster.Spec.Talos.SchematicID)
	assert.Equal(t, credentialsSecretName, cluster.Spec.CredentialsRef.Name)

	// Bootstrap state
	require.NotNil(t, cluster.Spec.Bootstrap)
	assert.True(t, cluster.Spec.Bootstrap.Completed)
	assert.Equal(t, "cp-1", cluster.Spec.Bootstrap.BootstrapNode)
	assert.Equal(t, int64(12345), cluster.Spec.Bootstrap.BootstrapNodeID)
	assert.Equal(t, "5.6.7.8", cluster.Spec.Bootstrap.PublicIP)

	// Placement group
	require.NotNil(t, cluster.Spec.PlacementGroup)
	assert.True(t, cluster.Spec.PlacementGroup.Enabled)
	assert.Equal(t, "spread", cluster.Spec.PlacementGroup.Type)

	// Status
	assert.Equal(t, k8znerv1alpha1.ClusterPhaseProvisioning, cluster.Status.Phase)
	assert.Equal(t, k8znerv1alpha1.PhaseCNI, cluster.Status.ProvisioningPhase)
	assert.Equal(t, "1.2.3.4", cluster.Status.ControlPlaneEndpoint)
	assert.Equal(t, int64(100), cluster.Status.Infrastructure.NetworkID)
	assert.Equal(t, int64(200), cluster.Status.Infrastructure.FirewallID)
	assert.Equal(t, int64(300), cluster.Status.Infrastructure.LoadBalancerID)
	require.Len(t, cluster.Status.ControlPlanes.Nodes, 1)
	assert.Equal(t, "cp-1", cluster.Status.ControlPlanes.Nodes[0].Name)
	assert.Equal(t, int64(12345), cluster.Status.ControlPlanes.Nodes[0].ServerID)
}

func TestBuildClusterSpec(t *testing.T) {
	t.Parallel()

	t.Run("maps all config fields", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			ClusterName: "test",
			Location:    "fsn1",
			ControlPlane: config.ControlPlaneConfig{
				NodePools: []config.ControlPlaneNodePool{
					{Name: "cp", Count: 1, ServerType: "cx21"},
				},
			},
			Network: config.NetworkConfig{
				IPv4CIDR:        "10.0.0.0/16",
				NodeIPv4CIDR:    "10.0.0.0/20",
				PodIPv4CIDR:     "10.244.0.0/16",
				ServiceIPv4CIDR: "10.96.0.0/16",
			},
			Kubernetes: config.KubernetesConfig{Version: "1.30.0"},
			Talos:      config.TalosConfig{Version: "1.7.0"},
			Addons: config.AddonsConfig{
				Cloudflare: config.CloudflareConfig{
					Domain: "example.com",
				},
			},
		}

		infraInfo := &InfrastructureInfo{}
		spec := buildClusterSpec(cfg, infraInfo, "cp-1", 1, "1.1.1.1", nil)

		assert.Equal(t, "fsn1", spec.Region)
		assert.Equal(t, "example.com", spec.Domain)
		assert.Equal(t, "10.0.0.0/16", spec.Network.IPv4CIDR)
		assert.Equal(t, "10.0.0.0/20", spec.Network.NodeIPv4CIDR)
		assert.Equal(t, "10.244.0.0/16", spec.Network.PodCIDR)
		assert.Equal(t, "10.96.0.0/16", spec.Network.ServiceCIDR)
		assert.True(t, spec.Firewall.Enabled)
	})
}

func TestBuildAddonSpec(t *testing.T) {
	t.Parallel()

	t.Run("maps enabled addons", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				Traefik:             config.TraefikConfig{Enabled: true},
				CertManager:         config.CertManagerConfig{Enabled: true},
				ExternalDNS:         config.ExternalDNSConfig{Enabled: true},
				ArgoCD:              config.ArgoCDConfig{Enabled: true},
				MetricsServer:       config.MetricsServerConfig{Enabled: true},
				KubePrometheusStack: config.KubePrometheusStackConfig{Enabled: true},
				Logging:             config.LoggingConfig{Enabled: true},
			},
		}
		cfg.Addons.CertManager.Cloudflare.WildcardCertificate = true

		spec := buildAddonSpec(cfg)
		assert.True(t, spec.Traefik)
		assert.True(t, spec.CertManager)
		assert.True(t, spec.ExternalDNS)
		assert.True(t, spec.ArgoCD)
		assert.True(t, spec.MetricsServer)
		assert.True(t, spec.Monitoring)
		assert.True(t, spec.Logging)
		assert.True(t, spec.WildcardCertificate)
	})

	t.Run("all disabled by default", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}

		spec := buildAddonSpec(cfg)
		assert.False(t, spec.Traefik)
		assert.False(t, spec.CertManager)
		assert.False(t, spec.ExternalDNS)
		assert.False(t, spec.ArgoCD)
		assert.False(t, spec.MetricsServer)
		assert.False(t, spec.Monitoring)
		assert.False(t, spec.Logging)
		assert.False(t, spec.WildcardCertificate)
	})

	t.Run("extracts custom argo subdomain", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				ArgoCD: config.ArgoCDConfig{
					Enabled:     true,
					IngressHost: "cd.example.com",
				},
				Cloudflare: config.CloudflareConfig{
					Domain: "example.com",
				},
			},
		}

		spec := buildAddonSpec(cfg)
		assert.Equal(t, "cd", spec.ArgoSubdomain)
	})

	t.Run("skips default argo subdomain", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				ArgoCD: config.ArgoCDConfig{
					Enabled:     true,
					IngressHost: "argo.example.com",
				},
				Cloudflare: config.CloudflareConfig{
					Domain: "example.com",
				},
			},
		}

		spec := buildAddonSpec(cfg)
		assert.Empty(t, spec.ArgoSubdomain) // "argo" is the default
	})

	t.Run("extracts custom grafana subdomain", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				KubePrometheusStack: config.KubePrometheusStackConfig{
					Enabled: true,
					Grafana: config.KubePrometheusGrafanaConfig{
						IngressHost: "monitoring.example.com",
					},
				},
				Cloudflare: config.CloudflareConfig{
					Domain: "example.com",
				},
			},
		}

		spec := buildAddonSpec(cfg)
		assert.Equal(t, "monitoring", spec.GrafanaSubdomain)
	})
}

func TestBuildBackupSpec(t *testing.T) {
	t.Parallel()

	t.Run("returns nil when backup disabled", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		assert.Nil(t, buildBackupSpec(cfg, "test"))
	})

	t.Run("returns nil when S3 keys missing", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				TalosBackup: config.TalosBackupConfig{
					Enabled: true,
					// Missing S3AccessKey and S3SecretKey
				},
			},
		}
		assert.Nil(t, buildBackupSpec(cfg, "test"))
	})

	t.Run("builds spec with S3 credentials", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				TalosBackup: config.TalosBackupConfig{
					Enabled:     true,
					Schedule:    "0 */6 * * *",
					S3AccessKey: "AKIA...",
					S3SecretKey: "secret",
				},
			},
		}

		spec := buildBackupSpec(cfg, "prod")
		require.NotNil(t, spec)
		assert.True(t, spec.Enabled)
		assert.Equal(t, "0 */6 * * *", spec.Schedule)
		assert.Equal(t, "168h", spec.Retention)
		assert.Equal(t, "prod-backup-s3", spec.S3SecretRef.Name)
	})
}

func TestCreateBackupS3Secret(t *testing.T) {
	t.Parallel()

	t.Run("returns nil when disabled", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		assert.Nil(t, createBackupS3Secret(cfg, "test"))
	})

	t.Run("returns nil when keys missing", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				TalosBackup: config.TalosBackupConfig{
					Enabled: true,
				},
			},
		}
		assert.Nil(t, createBackupS3Secret(cfg, "test"))
	})

	t.Run("creates secret with all fields", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				TalosBackup: config.TalosBackupConfig{
					Enabled:     true,
					S3AccessKey: "AKIA-KEY",
					S3SecretKey: "secret-key",
					S3Endpoint:  "s3.eu-central.wasabisys.com",
					S3Bucket:    "my-backup",
					S3Region:    "eu-central-1",
				},
			},
		}

		secret := createBackupS3Secret(cfg, "prod")
		require.NotNil(t, secret)
		assert.Equal(t, "prod-backup-s3", secret.Name)
		assert.Equal(t, k8znerNamespace, secret.Namespace)
		assert.Equal(t, "AKIA-KEY", secret.StringData["access-key"])
		assert.Equal(t, "secret-key", secret.StringData["secret-key"])
		assert.Equal(t, "s3.eu-central.wasabisys.com", secret.StringData["endpoint"])
		assert.Equal(t, "my-backup", secret.StringData["bucket"])
		assert.Equal(t, "eu-central-1", secret.StringData["region"])
	})
}

func TestGetWorkerCount(t *testing.T) {
	t.Parallel()

	t.Run("returns zero for no workers", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		assert.Equal(t, 0, cfg.WorkerCount())
	})

	t.Run("returns first pool count", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Workers: []config.WorkerNodePool{
				{Name: "pool1", Count: 3},
			},
		}
		assert.Equal(t, 3, cfg.WorkerCount())
	})
}

func TestGetWorkerSize(t *testing.T) {
	t.Parallel()

	t.Run("returns default for no workers", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		assert.Equal(t, config.DefaultWorkerServerType, getWorkerSize(cfg))
	})

	t.Run("returns first pool server type", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Workers: []config.WorkerNodePool{
				{Name: "pool1", ServerType: "cx42"},
			},
		}
		assert.Equal(t, "cx42", getWorkerSize(cfg))
	})
}

func TestGetBootstrapNode(t *testing.T) {
	t.Parallel()

	t.Run("returns empty for no CPs", func(t *testing.T) {
		t.Parallel()
		pCtx := &provisioning.Context{
			State: &provisioning.State{},
		}
		name, id, ip := getBootstrapNode(pCtx)
		assert.Empty(t, name)
		assert.Equal(t, int64(0), id)
		assert.Empty(t, ip)
	})

	t.Run("returns first sorted CP", func(t *testing.T) {
		t.Parallel()
		pCtx := &provisioning.Context{
			State: &provisioning.State{
				ControlPlaneIPs: map[string]string{
					"cp-z": "10.0.0.3",
					"cp-a": "10.0.0.1",
					"cp-m": "10.0.0.2",
				},
				ControlPlaneServerIDs: map[string]int64{
					"cp-z": 3,
					"cp-a": 1,
					"cp-m": 2,
				},
			},
		}
		name, id, ip := getBootstrapNode(pCtx)
		assert.Equal(t, "cp-a", name) // Sorted alphabetically
		assert.Equal(t, int64(1), id)
		assert.Equal(t, "10.0.0.1", ip)
	})
}

func TestBackupS3SecretName(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "prod-backup-s3", backupS3SecretName("prod"))
	assert.Equal(t, "test-backup-s3", backupS3SecretName("test"))
}

// --- buildClusterStatus tests ---

func TestBuildClusterStatus(t *testing.T) {
	t.Parallel()

	t.Run("maps all fields", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			ControlPlane: config.ControlPlaneConfig{
				NodePools: []config.ControlPlaneNodePool{
					{Count: 3, ServerType: "cx32"},
				},
			},
			Workers: []config.WorkerNodePool{
				{Count: 2, ServerType: "cx22"},
			},
		}

		infraInfo := &InfrastructureInfo{
			NetworkID:             100,
			FirewallID:            200,
			LoadBalancerID:        300,
			LoadBalancerIP:        "1.2.3.4",
			LoadBalancerPrivateIP: "10.0.0.254",
			SSHKeyID:              400,
		}

		status := buildClusterStatus(cfg, infraInfo, "cp-1", 12345, "5.6.7.8")

		assert.Equal(t, k8znerv1alpha1.ClusterPhaseProvisioning, status.Phase)
		assert.Equal(t, k8znerv1alpha1.PhaseCNI, status.ProvisioningPhase)
		assert.Equal(t, 3, status.ControlPlanes.Desired)
		assert.Equal(t, 1, status.ControlPlanes.Ready)
		require.Len(t, status.ControlPlanes.Nodes, 1)
		assert.Equal(t, "cp-1", status.ControlPlanes.Nodes[0].Name)
		assert.Equal(t, int64(12345), status.ControlPlanes.Nodes[0].ServerID)
		assert.Equal(t, "5.6.7.8", status.ControlPlanes.Nodes[0].PublicIP)
		assert.True(t, status.ControlPlanes.Nodes[0].Healthy)
		assert.Equal(t, 2, status.Workers.Desired)
		assert.Equal(t, int64(100), status.Infrastructure.NetworkID)
		assert.Equal(t, int64(200), status.Infrastructure.FirewallID)
		assert.Equal(t, int64(300), status.Infrastructure.LoadBalancerID)
		assert.Equal(t, "1.2.3.4", status.Infrastructure.LoadBalancerIP)
		assert.Equal(t, "10.0.0.254", status.Infrastructure.LoadBalancerPrivateIP)
		assert.Equal(t, int64(400), status.Infrastructure.SSHKeyID)
		assert.Equal(t, "1.2.3.4", status.ControlPlaneEndpoint)
	})

	t.Run("no workers", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			ControlPlane: config.ControlPlaneConfig{
				NodePools: []config.ControlPlaneNodePool{
					{Count: 1},
				},
			},
		}
		infraInfo := &InfrastructureInfo{}
		status := buildClusterStatus(cfg, infraInfo, "cp-0", 1, "1.1.1.1")
		assert.Equal(t, 0, status.Workers.Desired)
	})
}

// --- buildInfraInfo tests ---

func TestBuildInfraInfo(t *testing.T) {
	t.Parallel()

	t.Run("from state with LB", func(t *testing.T) {
		t.Parallel()
		lb := &hcloudgo.LoadBalancer{
			ID:   300,
			Name: "test-lb",
		}

		pCtx := &provisioning.Context{
			State: &provisioning.State{
				Network:      &hcloudgo.Network{ID: 100, Name: "test-net"},
				Firewall:     &hcloudgo.Firewall{ID: 200, Name: "test-fw"},
				SSHKeyID:     400,
				LoadBalancer: lb,
			},
		}
		cfg := &config.Config{ClusterName: "test"}
		mockClient := &hcloud.MockClient{}

		info := buildInfraInfo(context.Background(), pCtx, mockClient, cfg)

		assert.Equal(t, int64(100), info.NetworkID)
		assert.Equal(t, "test-net", info.NetworkName)
		assert.Equal(t, int64(200), info.FirewallID)
		assert.Equal(t, "test-fw", info.FirewallName)
		assert.Equal(t, int64(300), info.LoadBalancerID)
		assert.Equal(t, "test-lb", info.LoadBalancerName)
		assert.Equal(t, int64(400), info.SSHKeyID)
	})

	t.Run("nil firewall", func(t *testing.T) {
		t.Parallel()
		pCtx := &provisioning.Context{
			State: &provisioning.State{
				Network: &hcloudgo.Network{ID: 100},
			},
		}
		cfg := &config.Config{ClusterName: "test"}
		mockClient := &hcloud.MockClient{}

		info := buildInfraInfo(context.Background(), pCtx, mockClient, cfg)

		assert.Equal(t, int64(100), info.NetworkID)
		assert.Equal(t, int64(0), info.FirewallID)
		assert.Equal(t, int64(0), info.LoadBalancerID)
	})

	t.Run("fetches LB from API when not in state", func(t *testing.T) {
		t.Parallel()
		pCtx := &provisioning.Context{
			State: &provisioning.State{
				Network: &hcloudgo.Network{ID: 100},
			},
		}
		cfg := &config.Config{ClusterName: "test"}
		mockClient := &hcloud.MockClient{
			GetLoadBalancerFunc: func(_ context.Context, _ string) (*hcloudgo.LoadBalancer, error) {
				return &hcloudgo.LoadBalancer{ID: 500, Name: "api-lb"}, nil
			},
		}

		info := buildInfraInfo(context.Background(), pCtx, mockClient, cfg)

		assert.Equal(t, int64(500), info.LoadBalancerID)
		assert.Equal(t, "api-lb", info.LoadBalancerName)
	})

	t.Run("LB API error - graceful degradation", func(t *testing.T) {
		t.Parallel()
		pCtx := &provisioning.Context{
			State: &provisioning.State{
				Network: &hcloudgo.Network{ID: 100},
			},
		}
		cfg := &config.Config{ClusterName: "test"}
		mockClient := &hcloud.MockClient{
			GetLoadBalancerFunc: func(_ context.Context, _ string) (*hcloudgo.LoadBalancer, error) {
				return nil, errors.New("API unavailable")
			},
		}

		info := buildInfraInfo(context.Background(), pCtx, mockClient, cfg)

		assert.Equal(t, int64(100), info.NetworkID)
		assert.Equal(t, int64(0), info.LoadBalancerID) // Graceful - no LB info
	})
}
