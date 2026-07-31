package helm

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/milankappen/k8zner/internal/config"
)

func TestGetChartSpec_KnownCharts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		chartName       string
		expectedRepo    string
		expectedChart   string
		expectedVersion string
	}{
		{
			name:            "hcloud-ccm",
			chartName:       "hcloud-ccm",
			expectedRepo:    "https://charts.hetzner.cloud",
			expectedChart:   "hcloud-cloud-controller-manager",
			expectedVersion: "1.29.0",
		},
		{
			name:            "hcloud-csi",
			chartName:       "hcloud-csi",
			expectedRepo:    "https://charts.hetzner.cloud",
			expectedChart:   "hcloud-csi",
			expectedVersion: "2.20.2",
		},
		{
			name:            "cilium",
			chartName:       "cilium",
			expectedRepo:    "https://helm.cilium.io",
			expectedChart:   "cilium",
			expectedVersion: "1.18.5",
		},
		{
			name:            "cert-manager",
			chartName:       "cert-manager",
			expectedRepo:    "https://charts.jetstack.io",
			expectedChart:   "cert-manager",
			expectedVersion: "v1.19.2",
		},
		{
			name:            "traefik",
			chartName:       "traefik",
			expectedRepo:    "https://traefik.github.io/charts",
			expectedChart:   "traefik",
			expectedVersion: "39.0.0",
		},
		{
			name:            "metrics-server",
			chartName:       "metrics-server",
			expectedRepo:    "https://kubernetes-sigs.github.io/metrics-server",
			expectedChart:   "metrics-server",
			expectedVersion: "3.12.2",
		},
		{
			name:            "argo-cd",
			chartName:       "argo-cd",
			expectedRepo:    "https://argoproj.github.io/argo-helm",
			expectedChart:   "argo-cd",
			expectedVersion: "9.3.5",
		},
		{
			name:            "external-dns",
			chartName:       "external-dns",
			expectedRepo:    "https://kubernetes-sigs.github.io/external-dns",
			expectedChart:   "external-dns",
			expectedVersion: "1.15.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec := GetChartSpec(tt.chartName, config.HelmChartConfig{})

			assert.Equal(t, tt.expectedRepo, spec.Repository)
			assert.Equal(t, tt.expectedChart, spec.Name)
			assert.Equal(t, tt.expectedVersion, spec.Version)
		})
	}
}

func TestGetChartSpec_UnknownChart(t *testing.T) {
	t.Parallel()
	spec := GetChartSpec("unknown-chart", config.HelmChartConfig{})

	// Should return empty spec
	assert.Empty(t, spec.Repository)
	assert.Empty(t, spec.Name)
	assert.Empty(t, spec.Version)
}

func TestGetChartSpec_WithRepositoryOverride(t *testing.T) {
	t.Parallel()
	helmCfg := config.HelmChartConfig{
		Repository: "https://custom.repo.io",
	}

	spec := GetChartSpec("cilium", helmCfg)

	assert.Equal(t, "https://custom.repo.io", spec.Repository)
	assert.Equal(t, "cilium", spec.Name)    // Default chart name
	assert.Equal(t, "1.18.5", spec.Version) // Default version
}

func TestGetChartSpec_WithChartOverride(t *testing.T) {
	t.Parallel()
	helmCfg := config.HelmChartConfig{
		Chart: "custom-cilium",
	}

	spec := GetChartSpec("cilium", helmCfg)

	assert.Equal(t, "https://helm.cilium.io", spec.Repository) // Default repo
	assert.Equal(t, "custom-cilium", spec.Name)
	assert.Equal(t, "1.18.5", spec.Version) // Default version
}

func TestGetChartSpec_WithVersionOverride(t *testing.T) {
	t.Parallel()
	helmCfg := config.HelmChartConfig{
		Version: "1.16.0",
	}

	spec := GetChartSpec("cilium", helmCfg)

	assert.Equal(t, "https://helm.cilium.io", spec.Repository) // Default repo
	assert.Equal(t, "cilium", spec.Name)                       // Default chart name
	assert.Equal(t, "1.16.0", spec.Version)
}

func TestGetChartSpec_WithAllOverrides(t *testing.T) {
	t.Parallel()
	helmCfg := config.HelmChartConfig{
		Repository: "https://my-private-repo.io",
		Chart:      "my-custom-chart",
		Version:    "2.0.0",
	}

	spec := GetChartSpec("cert-manager", helmCfg)

	assert.Equal(t, "https://my-private-repo.io", spec.Repository)
	assert.Equal(t, "my-custom-chart", spec.Name)
	assert.Equal(t, "2.0.0", spec.Version)
}

func TestDefaultChartSpecs_ContainsAllExpectedCharts(t *testing.T) {
	t.Parallel()
	expectedCharts := []string{
		"hcloud-ccm",
		"hcloud-csi",
		"cilium",
		"cert-manager",
		"traefik",
		"metrics-server",
		"argo-cd",
		"external-dns",
	}

	for _, chartName := range expectedCharts {
		_, exists := DefaultChartSpecs[chartName]
		assert.True(t, exists, "Expected chart %s to be in DefaultChartSpecs", chartName)
	}
}

func TestDefaultChartSpecs_AllHaveRequiredFields(t *testing.T) {
	t.Parallel()
	for name, spec := range DefaultChartSpecs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.NotEmpty(t, spec.Repository, "Repository should not be empty for %s", name)
			assert.NotEmpty(t, spec.Name, "Name should not be empty for %s", name)
			assert.NotEmpty(t, spec.Version, "Version should not be empty for %s", name)
		})
	}
}
