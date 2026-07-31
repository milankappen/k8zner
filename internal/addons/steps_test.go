package addons

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/milankappen/k8zner/internal/addons/helm"
	"github.com/milankappen/k8zner/internal/config"
)

func TestEnabledSteps(t *testing.T) {
	t.Parallel()
	t.Run("all addons enabled", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				CCM:                 config.CCMConfig{Enabled: true},
				CSI:                 config.CSIConfig{Enabled: true},
				MetricsServer:       config.MetricsServerConfig{Enabled: true},
				CertManager:         config.CertManagerConfig{Enabled: true},
				Traefik:             config.TraefikConfig{Enabled: true},
				ExternalDNS:         config.ExternalDNSConfig{Enabled: true},
				ArgoCD:              config.ArgoCDConfig{Enabled: true},
				KubePrometheusStack: config.KubePrometheusStackConfig{Enabled: true},
				TalosBackup:         config.TalosBackupConfig{Enabled: true},
			},
		}

		steps := EnabledSteps(cfg)

		require.Len(t, steps, 9)

		// Verify names
		names := make([]string, len(steps))
		for i, s := range steps {
			names[i] = s.Name
		}
		assert.Equal(t, []string{
			StepCCM,
			StepCSI,
			StepMetricsServer,
			StepCertManager,
			StepTraefik,
			StepExternalDNS,
			StepArgoCD,
			StepMonitoring,
			StepTalosBackup,
		}, names)
	})

	t.Run("minimal config - only CCM and CSI", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				CCM: config.CCMConfig{Enabled: true},
				CSI: config.CSIConfig{Enabled: true},
			},
		}

		steps := EnabledSteps(cfg)

		require.Len(t, steps, 2)
		assert.Equal(t, StepCCM, steps[0].Name)
		assert.Equal(t, StepCSI, steps[1].Name)
	})

	t.Run("no addons enabled", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}

		steps := EnabledSteps(cfg)

		assert.Empty(t, steps)
	})

	t.Run("order values are ascending", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				CCM:                 config.CCMConfig{Enabled: true},
				CSI:                 config.CSIConfig{Enabled: true},
				MetricsServer:       config.MetricsServerConfig{Enabled: true},
				CertManager:         config.CertManagerConfig{Enabled: true},
				Traefik:             config.TraefikConfig{Enabled: true},
				ExternalDNS:         config.ExternalDNSConfig{Enabled: true},
				ArgoCD:              config.ArgoCDConfig{Enabled: true},
				KubePrometheusStack: config.KubePrometheusStackConfig{Enabled: true},
				TalosBackup:         config.TalosBackupConfig{Enabled: true},
			},
		}

		steps := EnabledSteps(cfg)

		for i := 1; i < len(steps); i++ {
			assert.Greater(t, steps[i].Order, steps[i-1].Order,
				"step %s (order %d) should be after step %s (order %d)",
				steps[i].Name, steps[i].Order, steps[i-1].Name, steps[i-1].Order)
		}
	})

	t.Run("CCM is always first when enabled", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				CCM:     config.CCMConfig{Enabled: true},
				ArgoCD:  config.ArgoCDConfig{Enabled: true},
				Traefik: config.TraefikConfig{Enabled: true},
			},
		}

		steps := EnabledSteps(cfg)

		require.Len(t, steps, 3)
		assert.Equal(t, StepCCM, steps[0].Name)
	})

	t.Run("ArgoCD and monitoring are last", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				CCM:                 config.CCMConfig{Enabled: true},
				ArgoCD:              config.ArgoCDConfig{Enabled: true},
				KubePrometheusStack: config.KubePrometheusStackConfig{Enabled: true},
				TalosBackup:         config.TalosBackupConfig{Enabled: true},
			},
		}

		steps := EnabledSteps(cfg)

		require.Len(t, steps, 4)
		assert.Equal(t, StepCCM, steps[0].Name)
		assert.Equal(t, StepArgoCD, steps[1].Name)
		assert.Equal(t, StepMonitoring, steps[2].Name)
		assert.Equal(t, StepTalosBackup, steps[3].Name)
	})
}

func TestInstallStep_UnknownStep(t *testing.T) {
	t.Parallel()
	err := InstallStep(t.Context(), "nonexistent-addon", &config.Config{}, []byte("fake-kubeconfig"), 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create kubernetes client")
}

func TestEnabledSteps_IndividualAddons(t *testing.T) {
	t.Parallel()

	// Each subtest enables a single addon and verifies exactly one step is returned
	// with the correct name and order.
	tests := []struct {
		name          string
		configure     func(cfg *config.Config)
		expectedName  string
		expectedOrder int
	}{
		{
			name:          "only CCM",
			configure:     func(cfg *config.Config) { cfg.Addons.CCM.Enabled = true },
			expectedName:  StepCCM,
			expectedOrder: 2,
		},
		{
			name:          "only CSI",
			configure:     func(cfg *config.Config) { cfg.Addons.CSI.Enabled = true },
			expectedName:  StepCSI,
			expectedOrder: 3,
		},
		{
			name:          "only MetricsServer",
			configure:     func(cfg *config.Config) { cfg.Addons.MetricsServer.Enabled = true },
			expectedName:  StepMetricsServer,
			expectedOrder: 4,
		},
		{
			name:          "only CertManager",
			configure:     func(cfg *config.Config) { cfg.Addons.CertManager.Enabled = true },
			expectedName:  StepCertManager,
			expectedOrder: 5,
		},
		{
			name:          "only Traefik",
			configure:     func(cfg *config.Config) { cfg.Addons.Traefik.Enabled = true },
			expectedName:  StepTraefik,
			expectedOrder: 6,
		},
		{
			name:          "only ExternalDNS",
			configure:     func(cfg *config.Config) { cfg.Addons.ExternalDNS.Enabled = true },
			expectedName:  StepExternalDNS,
			expectedOrder: 7,
		},
		{
			name:          "only ArgoCD",
			configure:     func(cfg *config.Config) { cfg.Addons.ArgoCD.Enabled = true },
			expectedName:  StepArgoCD,
			expectedOrder: 8,
		},
		{
			name:          "only KubePrometheusStack",
			configure:     func(cfg *config.Config) { cfg.Addons.KubePrometheusStack.Enabled = true },
			expectedName:  StepMonitoring,
			expectedOrder: 9,
		},
		{
			name:          "only TalosBackup",
			configure:     func(cfg *config.Config) { cfg.Addons.TalosBackup.Enabled = true },
			expectedName:  StepTalosBackup,
			expectedOrder: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &config.Config{}
			tt.configure(cfg)

			steps := EnabledSteps(cfg)

			require.Len(t, steps, 1)
			assert.Equal(t, tt.expectedName, steps[0].Name)
			assert.Equal(t, tt.expectedOrder, steps[0].Order)
		})
	}
}

func TestEnabledSteps_PartialAddons(t *testing.T) {
	t.Parallel()

	t.Run("ingress stack without monitoring", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				CCM:         config.CCMConfig{Enabled: true},
				CSI:         config.CSIConfig{Enabled: true},
				CertManager: config.CertManagerConfig{Enabled: true},
				Traefik:     config.TraefikConfig{Enabled: true},
				ExternalDNS: config.ExternalDNSConfig{Enabled: true},
			},
		}

		steps := EnabledSteps(cfg)

		require.Len(t, steps, 5)
		names := make([]string, len(steps))
		for i, s := range steps {
			names[i] = s.Name
		}
		assert.Equal(t, []string{
			StepCCM,
			StepCSI,
			StepCertManager,
			StepTraefik,
			StepExternalDNS,
		}, names)
	})

	t.Run("monitoring only - no ingress", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				CCM:                 config.CCMConfig{Enabled: true},
				MetricsServer:       config.MetricsServerConfig{Enabled: true},
				KubePrometheusStack: config.KubePrometheusStackConfig{Enabled: true},
			},
		}

		steps := EnabledSteps(cfg)

		require.Len(t, steps, 3)
		assert.Equal(t, StepCCM, steps[0].Name)
		assert.Equal(t, StepMetricsServer, steps[1].Name)
		assert.Equal(t, StepMonitoring, steps[2].Name)
	})

	t.Run("backup with minimal infra", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				CCM:         config.CCMConfig{Enabled: true},
				TalosBackup: config.TalosBackupConfig{Enabled: true},
			},
		}

		steps := EnabledSteps(cfg)

		require.Len(t, steps, 2)
		assert.Equal(t, StepCCM, steps[0].Name)
		assert.Equal(t, StepTalosBackup, steps[1].Name)
	})
}

func TestEnabledSteps_OrderGaps(t *testing.T) {
	t.Parallel()
	// When addons in the middle are disabled, remaining steps should
	// preserve their original order values (they are not renumbered).
	cfg := &config.Config{
		Addons: config.AddonsConfig{
			CCM:     config.CCMConfig{Enabled: true},     // order 2
			Traefik: config.TraefikConfig{Enabled: true}, // order 6
			ArgoCD:  config.ArgoCDConfig{Enabled: true},  // order 8
		},
	}

	steps := EnabledSteps(cfg)

	require.Len(t, steps, 3)
	assert.Equal(t, 2, steps[0].Order)
	assert.Equal(t, 6, steps[1].Order)
	assert.Equal(t, 8, steps[2].Order)

	// Verify ascending even with gaps
	for i := 1; i < len(steps); i++ {
		assert.Greater(t, steps[i].Order, steps[i-1].Order)
	}
}

func TestStepConstants(t *testing.T) {
	t.Parallel()
	// Verify step constants match expected addon names

	assert.Equal(t, "hcloud-ccm", StepCCM)
	assert.Equal(t, "hcloud-csi", StepCSI)
	assert.Equal(t, "metrics-server", StepMetricsServer)
	assert.Equal(t, "cert-manager", StepCertManager)
	assert.Equal(t, "traefik", StepTraefik)
	assert.Equal(t, "external-dns", StepExternalDNS)
	assert.Equal(t, "argocd", StepArgoCD)
	assert.Equal(t, "monitoring", StepMonitoring)
	assert.Equal(t, "talos-backup", StepTalosBackup)
}

func TestEnabledSteps_Versions(t *testing.T) {
	t.Parallel()

	t.Run("steps carry the resolved default chart versions", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				CCM:                 config.CCMConfig{Enabled: true},
				CSI:                 config.CSIConfig{Enabled: true},
				MetricsServer:       config.MetricsServerConfig{Enabled: true},
				CertManager:         config.CertManagerConfig{Enabled: true},
				Traefik:             config.TraefikConfig{Enabled: true},
				ExternalDNS:         config.ExternalDNSConfig{Enabled: true},
				ArgoCD:              config.ArgoCDConfig{Enabled: true},
				KubePrometheusStack: config.KubePrometheusStackConfig{Enabled: true},
				TalosBackup:         config.TalosBackupConfig{Enabled: true},
			},
		}

		versions := make(map[string]string)
		for _, s := range EnabledSteps(cfg) {
			versions[s.Name] = s.Version
		}

		assert.Equal(t, helm.DefaultChartSpecs["hcloud-ccm"].Version, versions[StepCCM])
		assert.Equal(t, helm.DefaultChartSpecs["hcloud-csi"].Version, versions[StepCSI])
		assert.Equal(t, helm.DefaultChartSpecs["metrics-server"].Version, versions[StepMetricsServer])
		assert.Equal(t, helm.DefaultChartSpecs["cert-manager"].Version, versions[StepCertManager])
		assert.Equal(t, helm.DefaultChartSpecs["traefik"].Version, versions[StepTraefik])
		assert.Equal(t, helm.DefaultChartSpecs["external-dns"].Version, versions[StepExternalDNS])
		assert.Equal(t, helm.DefaultChartSpecs["argo-cd"].Version, versions[StepArgoCD])
		assert.Equal(t, helm.DefaultChartSpecs["kube-prometheus-stack"].Version, versions[StepMonitoring])
		assert.Equal(t, config.DefaultVersionMatrix().TalosBackup, versions[StepTalosBackup])
	})

	t.Run("helm version overrides are respected", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				Traefik:     config.TraefikConfig{Enabled: true, Helm: config.HelmChartConfig{Version: "99.0.0"}},
				TalosBackup: config.TalosBackupConfig{Enabled: true, Version: "v9.9.9"},
			},
		}

		versions := make(map[string]string)
		for _, s := range EnabledSteps(cfg) {
			versions[s.Name] = s.Version
		}

		assert.Equal(t, "99.0.0", versions[StepTraefik])
		assert.Equal(t, "v9.9.9", versions[StepTalosBackup])
	})
}

func TestCNIVersion(t *testing.T) {
	t.Parallel()

	t.Run("defaults to registry version", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		assert.Equal(t, helm.DefaultChartSpecs["cilium"].Version, CNIVersion(cfg))
	})

	t.Run("respects helm override", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		cfg.Addons.Cilium.Helm.Version = "1.99.0"
		assert.Equal(t, "1.99.0", CNIVersion(cfg))
	})
}
