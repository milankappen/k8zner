package addons

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/milankappen/k8zner/internal/addons/helm"
	"github.com/milankappen/k8zner/internal/config"
)

func loggingTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Addons.Logging.Enabled = true
	cfg.Addons.Logging.Retention = "168h"
	return cfg
}

func TestBuildLokiValues(t *testing.T) {
	t.Parallel()

	values := buildLokiValues(loggingTestConfig())

	assert.Equal(t, "SingleBinary", values["deploymentMode"])

	loki := values["loki"].(helm.Values)
	assert.Equal(t, false, loki["auth_enabled"])

	limits := loki["limits_config"].(helm.Values)
	assert.Equal(t, "168h", limits["retention_period"])

	single := values["singleBinary"].(helm.Values)
	assert.Equal(t, 1, single["replicas"])
	persistence := single["persistence"].(helm.Values)
	assert.Equal(t, true, persistence["enabled"])

	// The simple scalable targets must be off in single binary mode.
	for _, target := range []string{"backend", "read", "write"} {
		section := values[target].(helm.Values)
		assert.Equal(t, 0, section["replicas"], "%s replicas", target)
	}

	// No caches or canaries on small clusters.
	assert.Equal(t, false, values["chunksCache"].(helm.Values)["enabled"])
	assert.Equal(t, false, values["resultsCache"].(helm.Values)["enabled"])
	assert.Equal(t, false, values["lokiCanary"].(helm.Values)["enabled"])
}

func TestBuildLokiValues_RetentionDefault(t *testing.T) {
	t.Parallel()

	cfg := loggingTestConfig()
	cfg.Addons.Logging.Retention = ""

	values := buildLokiValues(cfg)
	limits := values["loki"].(helm.Values)["limits_config"].(helm.Values)
	assert.Equal(t, config.LoggingDefaultRetention, limits["retention_period"])
}

func TestBuildAlloyValues(t *testing.T) {
	t.Parallel()

	values := buildAlloyValues()

	alloy := values["alloy"].(helm.Values)
	configMap := alloy["configMap"].(helm.Values)
	content := configMap["content"].(string)

	// Logs are tailed via the Kubernetes API (no hostPath mounts), so the
	// collector stays compatible with baseline pod security.
	assert.Contains(t, content, "loki.source.kubernetes")
	assert.Contains(t, content, "http://loki.logging.svc.cluster.local:3100/loki/api/v1/push")
	assert.NotContains(t, content, "/var/log")

	controller := values["controller"].(helm.Values)
	assert.Equal(t, "statefulset", controller["type"])
	assert.Equal(t, 1, controller["replicas"])
}

func TestBuildLokiDatasourceManifest(t *testing.T) {
	t.Parallel()

	manifest := buildLokiDatasourceManifest()
	rendered := string(manifest)

	// The grafana sidecar from kube-prometheus-stack discovers datasources
	// by this label.
	assert.Contains(t, rendered, "grafana_datasource")
	assert.Contains(t, rendered, "namespace: monitoring")
	assert.Contains(t, rendered, "http://loki.logging.svc.cluster.local:3100")
	require.True(t, strings.Contains(rendered, "type: loki"))
}

func TestEnabledSteps_IncludesLogging(t *testing.T) {
	t.Parallel()

	cfg := loggingTestConfig()
	steps := EnabledSteps(cfg)

	require.Len(t, steps, 1)
	assert.Equal(t, StepLogging, steps[0].Name)
	assert.Equal(t, 11, steps[0].Order)
	assert.Equal(t, helm.DefaultChartSpecs["loki"].Version, steps[0].Version)
}
