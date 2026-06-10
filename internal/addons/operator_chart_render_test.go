package addons

import (
	"strings"
	"testing"

	"github.com/milankappen/k8zner/internal/addons/helm"
	"github.com/milankappen/k8zner/internal/config"
)

// TestOperatorChartRenders renders the embedded operator chart with the same
// values the CLI uses and asserts the security-relevant invariants of the
// resulting manifests, so chart edits that weaken them fail fast.
func TestOperatorChartRenders(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{HCloudToken: "test-token"}
	cfg.Addons.Operator.Enabled = true

	chartPath, cleanup, err := extractOperatorChart()
	if err != nil {
		t.Fatalf("failed to extract embedded chart: %v", err)
	}
	defer cleanup()

	out, err := helm.RenderFromPath(chartPath, "k8zner-operator", "k8zner-system", buildOperatorValues(cfg))
	if err != nil {
		t.Fatalf("failed to render operator chart: %v", err)
	}
	rendered := string(out)

	for _, want := range []string{
		// Token reaches the operator as a mounted file, not an env var.
		"HCLOUD_TOKEN_FILE",
		"/var/run/secrets/k8zner/hcloud-token",
		"defaultMode: 288", // 0440 secret volume
		"fsGroup: 65532",
		// Default replicaCount is 2, so the PDB must render.
		"PodDisruptionBudget",
		"minAvailable: 1",
		// Log level wired through as a flag.
		"--log-level=info",
		"readOnlyRootFilesystem: true",
		"runAsNonRoot: true",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered chart missing %q", want)
		}
	}

	if strings.Contains(rendered, "name: HCLOUD_TOKEN\n") {
		t.Error("token must not be injected as the HCLOUD_TOKEN env var")
	}
}
