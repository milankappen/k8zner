//go:build e2e

package e2e

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	k8znerv1alpha1 "github.com/milankappen/k8zner/api/v1alpha1"
	"github.com/milankappen/k8zner/cmd/k8zner/handlers"
)

// AddonVerificationContext holds parameters needed for addon verification.
type AddonVerificationContext struct {
	KubeconfigPath string
	Domain         string
	ArgoHost       string
	GrafanaHost    string
	// HasBackup indicates whether the cluster was configured with the TalosBackup
	// addon (requires HETZNER_S3_ACCESS_KEY/HETZNER_S3_SECRET_KEY). When false,
	// backup-specific checks are skipped.
	HasBackup bool
}

// hasDomain reports whether DNS/TLS integration (Cloudflare) is configured for this cluster.
func (v *AddonVerificationContext) hasDomain() bool {
	return v.Domain != ""
}

// =============================================================================
// Doctor-based E2E Helpers
// =============================================================================

// RunDoctorCheck shells out to k8zner doctor --json and parses the result.
func RunDoctorCheck(t *testing.T, configPath string) *handlers.DoctorStatus {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "k8zner", "doctor", "--json", "-c", configPath)
	output, err := cmd.Output()
	require.NoError(t, err, "k8zner doctor should succeed: %s", string(output))

	var status handlers.DoctorStatus
	require.NoError(t, json.Unmarshal(output, &status), "doctor output should be valid JSON")
	return &status
}

// WaitForDoctorHealthy polls k8zner doctor --json until the check function passes.
func WaitForDoctorHealthy(t *testing.T, configPath string, timeout time.Duration, check func(*handlers.DoctorStatus) error) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var lastErr error

	for time.Now().Before(deadline) {
		cmd := exec.CommandContext(context.Background(), "k8zner", "doctor", "--json", "-c", configPath)
		output, err := cmd.Output()
		if err != nil {
			lastErr = fmt.Errorf("doctor command failed: %w", err)
			time.Sleep(30 * time.Second)
			continue
		}

		var status handlers.DoctorStatus
		if err := json.Unmarshal(output, &status); err != nil {
			lastErr = fmt.Errorf("invalid JSON: %w", err)
			time.Sleep(30 * time.Second)
			continue
		}

		if err := check(&status); err != nil {
			lastErr = err
			t.Logf("  Doctor check not ready: %v", err)
			time.Sleep(30 * time.Second)
			continue
		}

		return
	}

	t.Fatalf("Timeout waiting for doctor healthy: %v", lastErr)
}

// AssertClusterRunning asserts the cluster is in Running phase with expected node counts.
func AssertClusterRunning(t *testing.T, status *handlers.DoctorStatus, expectedCPs, expectedWorkers int) {
	t.Helper()
	require.Equal(t, "Running", status.Phase, "cluster should be Running")
	require.GreaterOrEqual(t, status.ControlPlanes.Ready, expectedCPs, "expected %d CPs ready", expectedCPs)
	require.GreaterOrEqual(t, status.Workers.Ready, expectedWorkers, "expected %d workers ready", expectedWorkers)
}

// AssertInfraHealthy asserts all infrastructure components are healthy.
func AssertInfraHealthy(t *testing.T, status *handlers.DoctorStatus) {
	t.Helper()
	require.True(t, status.Infrastructure.Network, "network should be healthy")
	require.True(t, status.Infrastructure.Firewall, "firewall should be healthy")
	require.True(t, status.Infrastructure.LoadBalancer, "load balancer should be healthy")
}

// AssertAllAddonsHealthy asserts all expected addons are installed and healthy.
func AssertAllAddonsHealthy(t *testing.T, status *handlers.DoctorStatus, expectedAddons []string) {
	t.Helper()
	for _, name := range expectedAddons {
		addon, ok := status.Addons[name]
		require.True(t, ok, "addon %s should exist in status", name)
		require.True(t, addon.Installed, "addon %s should be installed", name)
		require.True(t, addon.Healthy, "addon %s should be healthy (message: %s)", name, addon.Message)
	}
}

// AssertAllAddonsInstalled asserts all expected addons are installed (health not required).
// Use this for post-operation checks where runtime health probes may lag.
func AssertAllAddonsInstalled(t *testing.T, status *handlers.DoctorStatus, expectedAddons []string) {
	t.Helper()
	for _, name := range expectedAddons {
		addon, ok := status.Addons[name]
		require.True(t, ok, "addon %s should exist in status", name)
		require.True(t, addon.Installed, "addon %s should be installed", name)
		if !addon.Healthy {
			t.Logf("  INFO: addon %s installed but not healthy: %s", name, addon.Message)
		}
	}
}

// AssertConnectivityHealthy asserts connectivity probes pass.
func AssertConnectivityHealthy(t *testing.T, status *handlers.DoctorStatus) {
	t.Helper()
	require.True(t, status.Connectivity.KubeAPI, "kube API should be reachable")
	require.True(t, status.Connectivity.MetricsAPI, "metrics API should be available")
	for _, ep := range status.Connectivity.Endpoints {
		require.True(t, ep.DNS, "endpoint %s DNS should resolve", ep.Host)
		require.True(t, ep.TLS, "endpoint %s TLS should work", ep.Host)
		require.True(t, ep.HTTP, "endpoint %s HTTP should respond", ep.Host)
	}
}

// VerifyAllAddonsCore performs core validation of all addons (pods running, basic functionality).
// Does NOT include DNS/TLS/HTTPS checks — those go in VerifyExternalConnectivity.
func VerifyAllAddonsCore(t *testing.T, ctx context.Context, vctx *AddonVerificationContext, state *OperatorTestContext) {
	t.Helper()

	cluster := GetClusterStatus(ctx, state)
	if cluster == nil {
		t.Fatal("Cluster CRD not found")
	}

	legacyState := state.ToE2EState()

	// Cilium
	t.Log("  [Core] Verifying Cilium...")
	cilium, ok := cluster.Status.Addons[k8znerv1alpha1.AddonNameCilium]
	if !ok || !cilium.Installed {
		t.Fatal("Cilium should be installed")
	}
	testCiliumNetworkConnectivity(t, legacyState)

	// CCM
	t.Log("  [Core] Verifying CCM...")
	if err := WaitForAddonInstalled(ctx, t, state, k8znerv1alpha1.AddonNameCCM, 2*time.Minute); err != nil {
		t.Fatalf("CCM should be installed: %v", err)
	}
	testCCMLoadBalancer(t, legacyState)

	// CSI
	t.Log("  [Core] Verifying CSI...")
	if err := WaitForAddonInstalled(ctx, t, state, k8znerv1alpha1.AddonNameCSI, 2*time.Minute); err != nil {
		t.Fatalf("CSI should be installed: %v", err)
	}
	testCSIVolume(t, legacyState)

	// Metrics Server
	t.Log("  [Core] Verifying Metrics Server...")
	testMetricsAPI(t, vctx.KubeconfigPath)

	// Traefik
	t.Log("  [Core] Verifying Traefik...")
	verifyIngressClassExists(t, vctx.KubeconfigPath, "traefik")
	waitForPod(t, vctx.KubeconfigPath, "traefik", "app.kubernetes.io/name=traefik", 5*time.Minute)
	verifyTraefikLoadBalancer(t, vctx.KubeconfigPath)

	// CertManager
	t.Log("  [Core] Verifying CertManager...")
	waitForPod(t, vctx.KubeconfigPath, "cert-manager", "app.kubernetes.io/name=cert-manager", 5*time.Minute)
	if vctx.hasDomain() {
		verifyClusterIssuerExists(t, vctx.KubeconfigPath, "letsencrypt-cloudflare-staging")
	} else {
		t.Log("  [Core] Skipping ClusterIssuer check (no domain configured, CF_API_TOKEN/CF_DOMAIN unset)")
	}

	// ExternalDNS (only installed when a domain is configured)
	if vctx.hasDomain() {
		t.Log("  [Core] Verifying ExternalDNS...")
		waitForPod(t, vctx.KubeconfigPath, "external-dns", "app.kubernetes.io/name=external-dns", 5*time.Minute)
	} else {
		t.Log("  [Core] Skipping ExternalDNS (no domain configured, CF_API_TOKEN/CF_DOMAIN unset)")
	}

	// ArgoCD (pod always; ingress only when a domain is configured)
	t.Log("  [Core] Verifying ArgoCD...")
	waitForPod(t, vctx.KubeconfigPath, "argocd", "app.kubernetes.io/name=argocd-server", 5*time.Minute)
	if vctx.hasDomain() {
		verifyArgoCDIngressConfigured(t, vctx.KubeconfigPath, vctx.ArgoHost)
	} else {
		t.Log("  [Core] Skipping ArgoCD ingress check (no domain configured)")
	}

	// Grafana (pod always; ingress only when a domain is configured)
	t.Log("  [Core] Verifying Grafana...")
	waitForPod(t, vctx.KubeconfigPath, "monitoring", "app.kubernetes.io/name=grafana", 5*time.Minute)
	if vctx.hasDomain() {
		verifyGrafanaIngressExists(t, vctx.KubeconfigPath, vctx.GrafanaHost)
	} else {
		t.Log("  [Core] Skipping Grafana ingress check (no domain configured)")
	}

	// Prometheus
	t.Log("  [Core] Verifying Prometheus...")
	waitForPrometheusReady(t, vctx.KubeconfigPath, 8*time.Minute)
	verifyPrometheusTargets(t, vctx.KubeconfigPath)
	verifyServiceMonitors(t, vctx.KubeconfigPath)

	// Alertmanager
	t.Log("  [Core] Verifying Alertmanager...")
	waitForAlertmanagerReady(t, vctx.KubeconfigPath, 5*time.Minute)

	// Backup (CronJob only, when configured)
	if vctx.HasBackup {
		t.Log("  [Core] Verifying Backup...")
		verifyBackupCronJob(t, vctx.KubeconfigPath, "0 * * * *")
	} else {
		t.Log("  [Core] Skipping Backup (HETZNER_S3_ACCESS_KEY/HETZNER_S3_SECRET_KEY unset)")
	}

	t.Log("  [Core] All addon core verification passed!")
}

// VerifyExternalConnectivity verifies DNS + TLS + HTTPS for ArgoCD and Grafana.
// This is separated from core addon checks because it depends on external services
// (DNS propagation, Let's Encrypt cert issuance) which can be slow/flaky.
func VerifyExternalConnectivity(t *testing.T, vctx *AddonVerificationContext, state *OperatorTestContext) {
	t.Helper()

	if !vctx.hasDomain() {
		t.Skip("Skipping external connectivity checks: no domain configured (CF_API_TOKEN/CF_DOMAIN unset)")
	}

	legacyState := state.ToE2EState()

	// ArgoCD: DNS -> TLS -> HTTPS
	t.Log("  [Connectivity] Verifying ArgoCD...")
	waitForDNSRecord(t, vctx.ArgoHost, 10*time.Minute)
	waitForArgoCDTLSCertificate(t, vctx.KubeconfigPath, 12*time.Minute)
	testArgoCDHTTPSAccess(t, vctx.ArgoHost, 5*time.Minute)
	t.Logf("  ArgoCD accessible at https://%s", vctx.ArgoHost)

	// Grafana: DNS -> TLS -> HTTPS
	t.Log("  [Connectivity] Verifying Grafana...")
	verifyGrafanaDNSRecord(t, legacyState, vctx.GrafanaHost, 10*time.Minute)
	verifyGrafanaCertificate(t, vctx.KubeconfigPath, 12*time.Minute)
	testGrafanaHTTPSConnectivity(t, vctx.GrafanaHost, 5*time.Minute)
	t.Logf("  Grafana accessible at https://%s", vctx.GrafanaHost)

	t.Log("  [Connectivity] All external connectivity checks passed!")
}

// VerifyAllAddonsDeep performs full deep validation (core + connectivity).
// Used by HA tests that need a single-call verification.
func VerifyAllAddonsDeep(t *testing.T, ctx context.Context, vctx *AddonVerificationContext, state *OperatorTestContext) {
	t.Helper()
	VerifyAllAddonsCore(t, ctx, vctx, state)
	VerifyExternalConnectivity(t, vctx, state)
}

// VerifyAllAddonsHealthy performs fast health checks on all addons.
// No DNS/TLS waits - just checks pods are running, services exist, basic functionality.
// Used after each HA operation (~5-8 min).
func VerifyAllAddonsHealthy(t *testing.T, vctx *AddonVerificationContext) {
	t.Helper()

	kc := vctx.KubeconfigPath

	// Cilium: DaemonSet has ready pods
	t.Log("  [Health] Checking Cilium...")
	verifyDaemonSetReady(t, kc, "kube-system", "k8s-app=cilium")

	// CCM: pod Running
	t.Log("  [Health] Checking CCM...")
	verifyPodRunning(t, kc, "kube-system", "app.kubernetes.io/name=hcloud-cloud-controller-manager")

	// CSI: controller deployment ready
	t.Log("  [Health] Checking CSI...")
	verifyDeploymentReady(t, kc, "kube-system", "hcloud-csi-controller")

	// Metrics Server: kubectl top nodes
	t.Log("  [Health] Checking Metrics Server...")
	verifyMetricsAvailable(t, kc)

	// Traefik: pod Running + service type=LoadBalancer
	t.Log("  [Health] Checking Traefik...")
	verifyPodRunning(t, kc, "traefik", "app.kubernetes.io/name=traefik")
	verifyServiceType(t, kc, "traefik", "app.kubernetes.io/name=traefik", "LoadBalancer")

	// CertManager: pod Running + ClusterIssuer exists
	t.Log("  [Health] Checking CertManager...")
	verifyPodRunning(t, kc, "cert-manager", "app.kubernetes.io/name=cert-manager")
	verifyClusterIssuerExists(t, kc, "letsencrypt-cloudflare-staging")

	// ExternalDNS: pod Running
	t.Log("  [Health] Checking ExternalDNS...")
	verifyPodRunning(t, kc, "external-dns", "app.kubernetes.io/name=external-dns")

	// ArgoCD: pod Running + ingress exists
	t.Log("  [Health] Checking ArgoCD...")
	verifyPodRunning(t, kc, "argocd", "app.kubernetes.io/name=argocd-server")
	verifyIngressExists(t, kc, "argocd", vctx.ArgoHost)

	// Prometheus: pod Running
	t.Log("  [Health] Checking Prometheus...")
	verifyPodRunning(t, kc, "monitoring", "app.kubernetes.io/name=prometheus")

	// Alertmanager: pod Running
	t.Log("  [Health] Checking Alertmanager...")
	verifyPodRunning(t, kc, "monitoring", "app.kubernetes.io/name=alertmanager")

	// Grafana: pod Running + ingress exists
	t.Log("  [Health] Checking Grafana...")
	verifyPodRunning(t, kc, "monitoring", "app.kubernetes.io/name=grafana")
	verifyIngressExists(t, kc, "monitoring", vctx.GrafanaHost)

	// Backup: CronJob exists
	t.Log("  [Health] Checking Backup...")
	verifyCronJobExists(t, kc, "kube-system", "talos-backup")

	t.Log("  [Health] All addon health checks passed!")
}

// =============================================================================
// Health Check Helpers (fast, single-attempt with retry)
// =============================================================================

// verifyPodRunning checks that at least one pod with the given selector is Running.
func verifyPodRunning(t *testing.T, kubeconfigPath, namespace, selector string) {
	t.Helper()

	for i := 0; i < 6; i++ {
		cmd := exec.CommandContext(context.Background(), "kubectl",
			"--kubeconfig", kubeconfigPath,
			"get", "pods", "-n", namespace, "-l", selector,
			"-o", "jsonpath={.items[0].status.phase}")
		output, err := cmd.CombinedOutput()
		if err == nil && string(output) == "Running" {
			return
		}
		if i < 5 {
			time.Sleep(10 * time.Second)
		}
	}
	t.Fatalf("Pod %s in %s is not Running after 60s", selector, namespace)
}

// verifyDaemonSetReady checks a DaemonSet has ready pods.
func verifyDaemonSetReady(t *testing.T, kubeconfigPath, namespace, selector string) {
	t.Helper()

	for i := 0; i < 6; i++ {
		cmd := exec.CommandContext(context.Background(), "kubectl",
			"--kubeconfig", kubeconfigPath,
			"get", "daemonset", "-n", namespace, "-l", selector,
			"-o", "jsonpath={.items[0].status.numberReady}")
		output, err := cmd.CombinedOutput()
		if err == nil && string(output) != "" && string(output) != "0" {
			return
		}
		if i < 5 {
			time.Sleep(10 * time.Second)
		}
	}
	t.Fatalf("DaemonSet %s in %s has no ready pods after 60s", selector, namespace)
}

// verifyDeploymentReady checks a deployment has ready replicas.
func verifyDeploymentReady(t *testing.T, kubeconfigPath, namespace, name string) {
	t.Helper()

	for i := 0; i < 6; i++ {
		cmd := exec.CommandContext(context.Background(), "kubectl",
			"--kubeconfig", kubeconfigPath,
			"get", "deployment", name, "-n", namespace,
			"-o", "jsonpath={.status.readyReplicas}")
		output, err := cmd.CombinedOutput()
		if err == nil && strings.TrimSpace(string(output)) != "" && strings.TrimSpace(string(output)) != "0" {
			return
		}
		if i < 5 {
			time.Sleep(10 * time.Second)
		}
	}
	t.Fatalf("Deployment %s in %s has no ready replicas after 60s", name, namespace)
}

// verifyServiceType checks a service has the expected type.
func verifyServiceType(t *testing.T, kubeconfigPath, namespace, selector, expectedType string) {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", kubeconfigPath,
		"get", "svc", "-n", namespace, "-l", selector,
		"-o", "jsonpath={.items[0].spec.type}")
	output, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != expectedType {
		t.Fatalf("Service %s in %s type is %q, expected %s", selector, namespace, string(output), expectedType)
	}
}

// verifyMetricsAvailable does a single kubectl top nodes with retries.
func verifyMetricsAvailable(t *testing.T, kubeconfigPath string) {
	t.Helper()

	for i := 0; i < 6; i++ {
		cmd := exec.CommandContext(context.Background(), "kubectl",
			"--kubeconfig", kubeconfigPath,
			"top", "nodes")
		if err := cmd.Run(); err == nil {
			return
		}
		if i < 5 {
			time.Sleep(10 * time.Second)
		}
	}
	t.Fatal("Metrics API not available after 60s")
}

// verifyIngressExists checks an ingress with the expected host exists.
func verifyIngressExists(t *testing.T, kubeconfigPath, namespace, expectedHost string) {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", kubeconfigPath,
		"get", "ingress", "-n", namespace,
		"-o", "jsonpath={.items[*].spec.rules[*].host}")
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), expectedHost) {
		t.Fatalf("Ingress with host %s not found in %s", expectedHost, namespace)
	}
}

// verifyCronJobExists checks a CronJob exists.
func verifyCronJobExists(t *testing.T, kubeconfigPath, namespace, name string) {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", kubeconfigPath,
		"get", "cronjob", name, "-n", namespace)
	if err := cmd.Run(); err != nil {
		t.Fatalf("CronJob %s not found in %s", name, namespace)
	}
}

// verifyHTTPSConnectivity does a single HTTPS request with retries (no DNS/TLS wait).
func verifyHTTPSConnectivity(t *testing.T, hostname string) {
	t.Helper()

	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	url := fmt.Sprintf("https://%s/", hostname)

	for i := 0; i < 6; i++ {
		resp, err := httpClient.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK ||
				resp.StatusCode == http.StatusFound ||
				resp.StatusCode == http.StatusTemporaryRedirect {
				return
			}
		}
		if i < 5 {
			time.Sleep(10 * time.Second)
		}
	}
	t.Fatalf("HTTPS connectivity to %s failed after 60s", hostname)
}
