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

// TestOperatorChartNetworkPolicy asserts the chart ships a default-on
// NetworkPolicy locking the operator pod down to the ports it needs, and that
// the policy is skipped in hostNetwork mode where NetworkPolicies don't apply.
func TestOperatorChartNetworkPolicy(t *testing.T) {
	t.Parallel()

	chartPath, cleanup, err := extractOperatorChart()
	if err != nil {
		t.Fatalf("failed to extract embedded chart: %v", err)
	}
	defer cleanup()

	t.Run("rendered by default with expected ports", func(t *testing.T) {
		cfg := &config.Config{HCloudToken: "test-token"}
		cfg.Addons.Operator.Enabled = true
		cfg.Addons.Operator.HostNetwork = false

		out, err := helm.RenderFromPath(chartPath, "k8zner-operator", "k8zner-system", buildOperatorValues(cfg))
		if err != nil {
			t.Fatalf("failed to render operator chart: %v", err)
		}
		rendered := string(out)

		if !strings.Contains(rendered, "kind: NetworkPolicy") {
			t.Fatal("rendered chart missing NetworkPolicy")
		}

		for _, want := range []string{
			// Both Ingress and Egress are restricted.
			"- Ingress",
			"- Egress",
			// Ingress: metrics and health probe ports only.
			"port: 8080",
			"port: 8081",
			// Egress: DNS over UDP and TCP.
			"port: 53",
			"protocol: UDP",
			// Egress: HTTPS (Hetzner API, chart repos, ghcr, apiserver via LB).
			"port: 443",
			// Egress: Kubernetes apiserver.
			"port: 6443",
			// Egress: Talos API on cluster nodes.
			"port: 50000",
		} {
			if !strings.Contains(rendered, want) {
				t.Errorf("rendered chart missing %q", want)
			}
		}
	})

	t.Run("skipped when hostNetwork is enabled", func(t *testing.T) {
		cfg := &config.Config{HCloudToken: "test-token"}
		cfg.Addons.Operator.Enabled = true
		cfg.Addons.Operator.HostNetwork = true

		out, err := helm.RenderFromPath(chartPath, "k8zner-operator", "k8zner-system", buildOperatorValues(cfg))
		if err != nil {
			t.Fatalf("failed to render operator chart: %v", err)
		}

		if strings.Contains(string(out), "NetworkPolicy") {
			t.Error("NetworkPolicy must not render with hostNetwork: NetworkPolicies do not apply to hostNetwork pods")
		}
	})
}
