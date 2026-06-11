package config

import (
	"os"
	"testing"
)

func TestExpandSpec_BasicConfig(t *testing.T) {
	t.Parallel()
	cfg := &Spec{
		Name:   "test-cluster",
		Region: RegionFalkenstein,
		Mode:   ModeHA,
		Workers: WorkerSpec{
			Count: 3,
			Size:  SizeCX32,
		},
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	// Verify basic fields
	if expanded.ClusterName != "test-cluster" {
		t.Errorf("ClusterName = %q, want %q", expanded.ClusterName, "test-cluster")
	}
	if expanded.Location != "fsn1" {
		t.Errorf("Location = %q, want %q", expanded.Location, "fsn1")
	}
}

func TestExpandSpec_ControlPlane_DevMode(t *testing.T) {
	t.Parallel()
	cfg := &Spec{
		Name:   "dev-cluster",
		Region: RegionNuremberg,
		Mode:   ModeDev,
		Workers: WorkerSpec{
			Count: 1,
			Size:  SizeCX22,
		},
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	// Dev mode should have 1 control plane
	if len(expanded.ControlPlane.NodePools) != 1 {
		t.Errorf("ControlPlane.NodePools length = %d, want 1", len(expanded.ControlPlane.NodePools))
	}

	cp := expanded.ControlPlane.NodePools[0]
	if cp.Count != 1 {
		t.Errorf("ControlPlane count = %d, want 1", cp.Count)
	}
	if cp.ServerType != "cx23" {
		t.Errorf("ControlPlane type = %q, want %q", cp.ServerType, "cx23")
	}
	if cp.Location != "nbg1" {
		t.Errorf("ControlPlane location = %q, want %q", cp.Location, "nbg1")
	}
}

func TestExpandSpec_ControlPlane_HAMode(t *testing.T) {
	t.Parallel()
	cfg := &Spec{
		Name:   "ha-cluster",
		Region: RegionHelsinki,
		Mode:   ModeHA,
		Workers: WorkerSpec{
			Count: 3,
			Size:  SizeCX32,
		},
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	// HA mode should have 3 control planes
	if len(expanded.ControlPlane.NodePools) != 1 {
		t.Errorf("ControlPlane.NodePools length = %d, want 1", len(expanded.ControlPlane.NodePools))
	}

	cp := expanded.ControlPlane.NodePools[0]
	if cp.Count != 3 {
		t.Errorf("ControlPlane count = %d, want 3", cp.Count)
	}
}

func TestExpandSpec_Workers(t *testing.T) {
	t.Parallel()
	cfg := &Spec{
		Name:   "worker-test",
		Region: RegionFalkenstein,
		Mode:   ModeHA,
		Workers: WorkerSpec{
			Count: 5,
			Size:  SizeCX52,
		},
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	// Should have exactly 1 worker pool
	if len(expanded.Workers) != 1 {
		t.Errorf("Workers length = %d, want 1", len(expanded.Workers))
	}

	workers := expanded.Workers[0]
	if workers.Count != 5 {
		t.Errorf("Workers count = %d, want 5", workers.Count)
	}
	// Note: cx52 is normalized to cx53 (Hetzner renamed types in 2024)
	if workers.ServerType != "cx53" {
		t.Errorf("Workers type = %q, want %q", workers.ServerType, "cx53")
	}
	if workers.Name != "workers" {
		t.Errorf("Workers name = %q, want %q", workers.Name, "workers")
	}
	// Workers should have placement groups enabled by default
	if !workers.PlacementGroup {
		t.Error("Workers PlacementGroup should be enabled by default")
	}
}

func TestExpandSpec_Network(t *testing.T) {
	t.Parallel()
	cfg := &Spec{
		Name:   "network-test",
		Region: RegionFalkenstein,
		Mode:   ModeHA,
		Workers: WorkerSpec{
			Count: 3,
			Size:  SizeCX32,
		},
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	// Verify network CIDRs
	if expanded.Network.IPv4CIDR != NetworkCIDR {
		t.Errorf("Network.IPv4CIDR = %q, want %q", expanded.Network.IPv4CIDR, NetworkCIDR)
	}
	if expanded.Network.PodIPv4CIDR != PodCIDR {
		t.Errorf("Network.PodIPv4CIDR = %q, want %q", expanded.Network.PodIPv4CIDR, PodCIDR)
	}
	if expanded.Network.ServiceIPv4CIDR != ServiceCIDR {
		t.Errorf("Network.ServiceIPv4CIDR = %q, want %q", expanded.Network.ServiceIPv4CIDR, ServiceCIDR)
	}
	if expanded.Network.Zone != "eu-central" {
		t.Errorf("Network.Zone = %q, want %q", expanded.Network.Zone, "eu-central")
	}
}

func TestExpandSpec_Talos(t *testing.T) {
	t.Parallel()
	cfg := &Spec{
		Name:   "talos-test",
		Region: RegionFalkenstein,
		Mode:   ModeHA,
		Workers: WorkerSpec{
			Count: 3,
			Size:  SizeCX32,
		},
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	vm := DefaultVersionMatrix()

	if expanded.Talos.Version != vm.Talos {
		t.Errorf("Talos.Version = %q, want %q", expanded.Talos.Version, vm.Talos)
	}

	// IPv6-only nodes should not have public IPv4
	if expanded.Talos.Machine.PublicIPv4Enabled == nil || *expanded.Talos.Machine.PublicIPv4Enabled {
		t.Error("Talos.Machine.PublicIPv4Enabled should be false for IPv6-only")
	}
}

func TestExpandSpec_Addons(t *testing.T) {
	t.Parallel()
	cfg := &Spec{
		Name:   "addon-test",
		Region: RegionFalkenstein,
		Mode:   ModeHA,
		Workers: WorkerSpec{
			Count: 3,
			Size:  SizeCX32,
		},
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	// All addons should be enabled
	if !expanded.Addons.Cilium.Enabled {
		t.Error("Cilium should be enabled")
	}
	if !expanded.Addons.Traefik.Enabled {
		t.Error("Traefik should be enabled")
	}
	if !expanded.Addons.CertManager.Enabled {
		t.Error("CertManager should be enabled")
	}
	if !expanded.Addons.MetricsServer.Enabled {
		t.Error("MetricsServer should be enabled")
	}
	if !expanded.Addons.CCM.Enabled {
		t.Error("CCM should be enabled")
	}
	if !expanded.Addons.CSI.Enabled {
		t.Error("CSI should be enabled")
	}
	if !expanded.Addons.TalosCCM.Enabled {
		t.Error("TalosCCM should be enabled")
	}
	if expanded.Addons.TalosCCM.Version == "" {
		t.Error("TalosCCM version should be set")
	}
	vm := DefaultVersionMatrix()
	if expanded.Addons.TalosCCM.Version != vm.TalosCCM {
		t.Errorf("TalosCCM version = %q, want %q", expanded.Addons.TalosCCM.Version, vm.TalosCCM)
	}

}

func TestExpandSpec_WithDomain(t *testing.T) {
	os.Setenv("CF_API_TOKEN", "test-token")
	defer os.Unsetenv("CF_API_TOKEN")

	cfg := &Spec{
		Name:   "domain-test",
		Region: RegionFalkenstein,
		Mode:   ModeHA,
		Workers: WorkerSpec{
			Count: 3,
			Size:  SizeCX32,
		},
		Domain: "example.com",
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	// Cloudflare should be enabled when domain is set
	if !expanded.Addons.Cloudflare.Enabled {
		t.Error("Cloudflare should be enabled when domain is set")
	}
	if expanded.Addons.Cloudflare.Domain != "example.com" {
		t.Errorf("Cloudflare.Domain = %q, want %q", expanded.Addons.Cloudflare.Domain, "example.com")
	}

	// External DNS should be enabled
	if !expanded.Addons.ExternalDNS.Enabled {
		t.Error("ExternalDNS should be enabled when domain is set")
	}

	// cert-manager Cloudflare should be enabled
	if !expanded.Addons.CertManager.Cloudflare.Enabled {
		t.Error("CertManager Cloudflare should be enabled when domain is set")
	}
}

func TestExpandSpec_WithoutDomain(t *testing.T) {
	t.Parallel()
	cfg := &Spec{
		Name:   "no-domain",
		Region: RegionFalkenstein,
		Mode:   ModeHA,
		Workers: WorkerSpec{
			Count: 3,
			Size:  SizeCX32,
		},
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	// Cloudflare should be disabled when no domain
	if expanded.Addons.Cloudflare.Enabled {
		t.Error("Cloudflare should be disabled when no domain")
	}

	// External DNS should be disabled
	if expanded.Addons.ExternalDNS.Enabled {
		t.Error("ExternalDNS should be disabled when no domain")
	}
}

func TestExpandSpec_ArgoCDIngressWithDomain(t *testing.T) {
	os.Setenv("CF_API_TOKEN", "test-token")
	defer os.Unsetenv("CF_API_TOKEN")

	cfg := &Spec{
		Name:   "argocd-test",
		Region: RegionFalkenstein,
		Mode:   ModeHA,
		Workers: WorkerSpec{
			Count: 2,
			Size:  SizeCX33,
		},
		Domain: "example.com",
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	if !expanded.Addons.ArgoCD.Enabled {
		t.Error("ArgoCD should be enabled")
	}
	if !expanded.Addons.ArgoCD.IngressEnabled {
		t.Error("ArgoCD IngressEnabled should be true when domain is set")
	}
	expectedHost := "argo.example.com"
	if expanded.Addons.ArgoCD.IngressHost != expectedHost {
		t.Errorf("ArgoCD IngressHost = %q, want %q", expanded.Addons.ArgoCD.IngressHost, expectedHost)
	}
	if expanded.Addons.ArgoCD.IngressClassName != "traefik" {
		t.Errorf("ArgoCD IngressClassName = %q, want %q", expanded.Addons.ArgoCD.IngressClassName, "traefik")
	}
	if !expanded.Addons.ArgoCD.IngressTLS {
		t.Error("ArgoCD IngressTLS should be true when domain is set")
	}
}

func TestExpandSpec_ArgoCDCustomSubdomain(t *testing.T) {
	os.Setenv("CF_API_TOKEN", "test-token")
	defer os.Unsetenv("CF_API_TOKEN")

	cfg := &Spec{
		Name:   "argocd-custom",
		Region: RegionFalkenstein,
		Mode:   ModeDev,
		Workers: WorkerSpec{
			Count: 1,
			Size:  SizeCX23,
		},
		Domain:        "mycompany.com",
		ArgoSubdomain: "gitops",
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	expectedHost := "gitops.mycompany.com"
	if expanded.Addons.ArgoCD.IngressHost != expectedHost {
		t.Errorf("ArgoCD IngressHost = %q, want %q", expanded.Addons.ArgoCD.IngressHost, expectedHost)
	}
}

func TestExpandSpec_ArgoCDNoIngressWithoutDomain(t *testing.T) {
	t.Parallel()
	cfg := &Spec{
		Name:   "argocd-nodomain",
		Region: RegionFalkenstein,
		Mode:   ModeHA,
		Workers: WorkerSpec{
			Count: 2,
			Size:  SizeCX33,
		},
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	if !expanded.Addons.ArgoCD.Enabled {
		t.Error("ArgoCD should be enabled even without domain")
	}
	if expanded.Addons.ArgoCD.IngressEnabled {
		t.Error("ArgoCD IngressEnabled should be false when no domain is set")
	}
	if expanded.Addons.ArgoCD.IngressHost != "" {
		t.Errorf("ArgoCD IngressHost should be empty when no domain, got %q", expanded.Addons.ArgoCD.IngressHost)
	}
}

func TestExpandSpec_Traefik_DevMode(t *testing.T) {
	t.Parallel()
	cfg := &Spec{
		Name:   "dev-traefik",
		Region: RegionFalkenstein,
		Mode:   ModeDev,
		Workers: WorkerSpec{
			Count: 1,
			Size:  SizeCX22,
		},
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	if !expanded.Addons.Traefik.Enabled {
		t.Errorf("Traefik.Enabled = false, want true")
	}
}

func TestExpandSpec_Traefik_HAMode(t *testing.T) {
	t.Parallel()
	cfg := &Spec{
		Name:   "ha-traefik",
		Region: RegionFalkenstein,
		Mode:   ModeHA,
		Workers: WorkerSpec{
			Count: 3,
			Size:  SizeCX32,
		},
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	if !expanded.Addons.Traefik.Enabled {
		t.Errorf("Traefik.Enabled = false, want true")
	}
}

func TestExpandSpec_Kubernetes(t *testing.T) {
	t.Parallel()
	cfg := &Spec{
		Name:   "k8s-test",
		Region: RegionFalkenstein,
		Mode:   ModeHA,
		Workers: WorkerSpec{
			Count: 3,
			Size:  SizeCX32,
		},
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	vm := DefaultVersionMatrix()

	if expanded.Kubernetes.Version != vm.Kubernetes {
		t.Errorf("Kubernetes.Version = %q, want %q", expanded.Kubernetes.Version, vm.Kubernetes)
	}

	if !expanded.Kubernetes.APILoadBalancerEnabled {
		t.Error("APILoadBalancerEnabled should be true for HA mode")
	}
}

func TestExpandSpec_IPv6Only(t *testing.T) {
	t.Parallel()
	cfg := &Spec{
		Name:   "ipv6-test",
		Region: RegionFalkenstein,
		Mode:   ModeHA,
		Workers: WorkerSpec{
			Count: 3,
			Size:  SizeCX32,
		},
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	if expanded.Talos.Machine.PublicIPv4Enabled == nil || *expanded.Talos.Machine.PublicIPv4Enabled {
		t.Error("PublicIPv4Enabled should be false for IPv6-only")
	}
	if expanded.Talos.Machine.IPv6Enabled == nil || !*expanded.Talos.Machine.IPv6Enabled {
		t.Error("IPv6Enabled should be true")
	}
}

func TestExpandSpec_ReturnsValidConfig(t *testing.T) {
	t.Parallel()
	cfg := &Spec{
		Name:   "valid-test",
		Region: RegionFalkenstein,
		Mode:   ModeHA,
		Workers: WorkerSpec{
			Count: 3,
			Size:  SizeCX32,
		},
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	var _ *Config = expanded
}

func TestExpandSpec_WithBackupEnabled(t *testing.T) {
	os.Setenv("HETZNER_S3_ACCESS_KEY", "test-access-key")
	os.Setenv("HETZNER_S3_SECRET_KEY", "test-secret-key")
	defer os.Unsetenv("HETZNER_S3_ACCESS_KEY")
	defer os.Unsetenv("HETZNER_S3_SECRET_KEY")

	cfg := &Spec{
		Name:   "backup-test",
		Region: RegionFalkenstein,
		Mode:   ModeHA,
		Workers: WorkerSpec{
			Count: 3,
			Size:  SizeCX32,
		},
		Backup: true,
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	if !expanded.Addons.TalosBackup.Enabled {
		t.Error("TalosBackup should be enabled when backup is true")
	}
	expectedBucket := "backup-test-etcd-backups"
	if expanded.Addons.TalosBackup.S3Bucket != expectedBucket {
		t.Errorf("TalosBackup.S3Bucket = %q, want %q", expanded.Addons.TalosBackup.S3Bucket, expectedBucket)
	}
	if expanded.Addons.TalosBackup.S3Region != "fsn1" {
		t.Errorf("TalosBackup.S3Region = %q, want %q", expanded.Addons.TalosBackup.S3Region, "fsn1")
	}
	expectedEndpoint := "https://fsn1.your-objectstorage.com"
	if expanded.Addons.TalosBackup.S3Endpoint != expectedEndpoint {
		t.Errorf("TalosBackup.S3Endpoint = %q, want %q", expanded.Addons.TalosBackup.S3Endpoint, expectedEndpoint)
	}
	if expanded.Addons.TalosBackup.S3AccessKey != "test-access-key" {
		t.Errorf("TalosBackup.S3AccessKey = %q, want %q", expanded.Addons.TalosBackup.S3AccessKey, "test-access-key")
	}
	if expanded.Addons.TalosBackup.S3SecretKey != "test-secret-key" {
		t.Errorf("TalosBackup.S3SecretKey = %q, want %q", expanded.Addons.TalosBackup.S3SecretKey, "test-secret-key")
	}
	if expanded.Addons.TalosBackup.Schedule != "0 * * * *" {
		t.Errorf("TalosBackup.Schedule = %q, want %q", expanded.Addons.TalosBackup.Schedule, "0 * * * *")
	}
	if !expanded.Addons.TalosBackup.EnableCompression {
		t.Error("TalosBackup.EnableCompression should be true")
	}
}

func TestExpandSpec_WithBackupDisabled(t *testing.T) {
	t.Parallel()
	cfg := &Spec{
		Name:   "no-backup",
		Region: RegionFalkenstein,
		Mode:   ModeHA,
		Workers: WorkerSpec{
			Count: 3,
			Size:  SizeCX32,
		},
		Backup: false,
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	if expanded.Addons.TalosBackup.Enabled {
		t.Error("TalosBackup should be disabled when backup is false")
	}
}

func TestExpandSpec_BackupDifferentRegions(t *testing.T) {
	os.Setenv("HETZNER_S3_ACCESS_KEY", "test-key")
	os.Setenv("HETZNER_S3_SECRET_KEY", "test-secret")
	defer os.Unsetenv("HETZNER_S3_ACCESS_KEY")
	defer os.Unsetenv("HETZNER_S3_SECRET_KEY")

	tests := []struct {
		region           Region
		expectedEndpoint string
	}{
		{RegionNuremberg, "https://nbg1.your-objectstorage.com"},
		{RegionFalkenstein, "https://fsn1.your-objectstorage.com"},
		{RegionHelsinki, "https://hel1.your-objectstorage.com"},
	}

	for _, tt := range tests {
		t.Run(string(tt.region), func(t *testing.T) {
			cfg := &Spec{
				Name:   "region-test",
				Region: tt.region,
				Mode:   ModeDev,
				Workers: WorkerSpec{
					Count: 1,
					Size:  SizeCX22,
				},
				Backup: true,
			}

			expanded, err := ExpandSpec(cfg)
			if err != nil {
				t.Fatalf("ExpandSpec() error = %v", err)
			}

			if expanded.Addons.TalosBackup.S3Endpoint != tt.expectedEndpoint {
				t.Errorf("S3Endpoint = %q, want %q", expanded.Addons.TalosBackup.S3Endpoint, tt.expectedEndpoint)
			}
		})
	}
}

func TestExpandSpec_BackupEncryptionDisabled(t *testing.T) {
	os.Setenv("HETZNER_S3_ACCESS_KEY", "test-access-key")
	os.Setenv("HETZNER_S3_SECRET_KEY", "test-secret-key")
	defer os.Unsetenv("HETZNER_S3_ACCESS_KEY")
	defer os.Unsetenv("HETZNER_S3_SECRET_KEY")

	cfg := &Spec{
		Name:   "encryption-test",
		Region: RegionFalkenstein,
		Mode:   ModeHA,
		Workers: WorkerSpec{
			Count: 3,
			Size:  SizeCX32,
		},
		Backup: true,
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	if !expanded.Addons.TalosBackup.EncryptionDisabled {
		t.Error("TalosBackup.EncryptionDisabled should be true for spec config")
	}
}

func TestExpandSpec_MonitoringDisabledByDefault(t *testing.T) {
	t.Parallel()
	cfg := &Spec{
		Name:   "monitoring-test",
		Region: RegionNuremberg,
		Mode:   ModeDev,
		Workers: WorkerSpec{
			Count: 2,
			Size:  SizeCX22,
		},
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	if expanded.Addons.KubePrometheusStack.Enabled {
		t.Error("KubePrometheusStack should be disabled by default")
	}
}

func TestExpandSpec_MonitoringEnabledWithoutDomain(t *testing.T) {
	t.Parallel()
	cfg := &Spec{
		Name:   "monitoring-test",
		Region: RegionNuremberg,
		Mode:   ModeDev,
		Workers: WorkerSpec{
			Count: 2,
			Size:  SizeCX22,
		},
		Monitoring: true,
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	if !expanded.Addons.KubePrometheusStack.Enabled {
		t.Error("KubePrometheusStack should be enabled when Monitoring is true")
	}
	if expanded.Addons.KubePrometheusStack.Grafana.IngressEnabled {
		t.Error("Grafana ingress should not be enabled without domain")
	}
	if !expanded.Addons.KubePrometheusStack.Prometheus.Persistence.Enabled {
		t.Error("Prometheus persistence should be enabled by default")
	}
	if expanded.Addons.KubePrometheusStack.Prometheus.Persistence.Size != "50Gi" {
		t.Errorf("Prometheus persistence size = %s, want 50Gi", expanded.Addons.KubePrometheusStack.Prometheus.Persistence.Size)
	}
}

func TestExpandSpec_MonitoringEnabledWithDomain(t *testing.T) {
	os.Setenv("CF_API_TOKEN", "test-cf-token")
	defer os.Unsetenv("CF_API_TOKEN")

	cfg := &Spec{
		Name:   "monitoring-test",
		Region: RegionNuremberg,
		Mode:   ModeHA,
		Workers: WorkerSpec{
			Count: 3,
			Size:  SizeCX32,
		},
		Domain:     "example.com",
		Monitoring: true,
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	if !expanded.Addons.KubePrometheusStack.Enabled {
		t.Error("KubePrometheusStack should be enabled")
	}

	grafana := expanded.Addons.KubePrometheusStack.Grafana
	if !grafana.IngressEnabled {
		t.Error("Grafana ingress should be enabled with domain")
	}
	if grafana.IngressHost != "grafana.example.com" {
		t.Errorf("Grafana ingress host = %s, want grafana.example.com", grafana.IngressHost)
	}
	if grafana.IngressClassName != "traefik" {
		t.Errorf("Grafana ingress class = %s, want traefik", grafana.IngressClassName)
	}
	if !grafana.IngressTLS {
		t.Error("Grafana ingress TLS should be enabled")
	}
}

func TestExpandSpec_MonitoringCustomGrafanaSubdomain(t *testing.T) {
	os.Setenv("CF_API_TOKEN", "test-cf-token")
	defer os.Unsetenv("CF_API_TOKEN")

	cfg := &Spec{
		Name:   "monitoring-test",
		Region: RegionNuremberg,
		Mode:   ModeDev,
		Workers: WorkerSpec{
			Count: 2,
			Size:  SizeCX22,
		},
		Domain:           "example.com",
		GrafanaSubdomain: "metrics",
		Monitoring:       true,
	}

	expanded, err := ExpandSpec(cfg)
	if err != nil {
		t.Fatalf("ExpandSpec() error = %v", err)
	}

	if expanded.Addons.KubePrometheusStack.Grafana.IngressHost != "metrics.example.com" {
		t.Errorf("Grafana ingress host = %s, want metrics.example.com",
			expanded.Addons.KubePrometheusStack.Grafana.IngressHost)
	}
}

func TestExpandSpec_WildcardCertificate(t *testing.T) {
	t.Parallel()

	base := func() *Spec {
		return &Spec{
			Name:    "test",
			Region:  RegionFalkenstein,
			Mode:    ModeDev,
			Workers: WorkerSpec{Count: 1, Size: SizeCX22},
		}
	}

	t.Run("disabled by default even with domain", func(t *testing.T) {
		t.Parallel()
		spec := base()
		spec.Domain = "example.com"

		expanded, err := ExpandSpec(spec)
		if err != nil {
			t.Fatalf("ExpandSpec failed: %v", err)
		}
		if expanded.Addons.CertManager.Cloudflare.WildcardCertificate {
			t.Error("wildcard certificate should be opt-in")
		}
	})

	t.Run("enabled with domain", func(t *testing.T) {
		t.Parallel()
		spec := base()
		spec.Domain = "example.com"
		spec.WildcardCertificate = true

		expanded, err := ExpandSpec(spec)
		if err != nil {
			t.Fatalf("ExpandSpec failed: %v", err)
		}
		if !expanded.Addons.CertManager.Cloudflare.WildcardCertificate {
			t.Error("wildcard certificate should be enabled when requested with a domain")
		}
	})

	t.Run("ignored without domain", func(t *testing.T) {
		t.Parallel()
		spec := base()
		spec.WildcardCertificate = true

		expanded, err := ExpandSpec(spec)
		if err != nil {
			t.Fatalf("ExpandSpec failed: %v", err)
		}
		if expanded.Addons.CertManager.Cloudflare.WildcardCertificate {
			t.Error("wildcard needs a domain for DNS01")
		}
	})
}
