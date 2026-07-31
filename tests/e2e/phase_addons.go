//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// This file contains verification functions for addons used by operator-centric E2E tests.

// mockTalosGenerator is a mock for E2E testing
type mockTalosGenerator struct{}

func (m *mockTalosGenerator) GenerateControlPlaneConfig(san []string, hostname string) ([]byte, error) {
	return []byte("mock-cp-config"), nil
}

func (m *mockTalosGenerator) GenerateWorkerConfig(hostname string) ([]byte, error) {
	return []byte("mock-worker-config"), nil
}

func (m *mockTalosGenerator) GetClientConfig() ([]byte, error) {
	return []byte("mock-client-config"), nil
}

func (m *mockTalosGenerator) SetEndpoint(endpoint string) {
}

// =============================================================================
// VERIFICATION FUNCTIONS - Used by operator-centric tests
// =============================================================================

// waitForPod waits for a pod with the given selector to be running.
func waitForPod(t *testing.T, kubeconfigPath, namespace, selector string, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	startTime := time.Now()
	lastProgressLog := time.Now()

	for {
		select {
		case <-ctx.Done():
			// Timeout - collect comprehensive diagnostics
			t.Logf("Timeout waiting for pod - collecting comprehensive diagnostics...")

			diag := NewDiagnosticCollector(t, kubeconfigPath, namespace, selector)
			diag.WithComponentName(selector)
			diag.Collect()
			diag.Report()

			t.Fatalf("Timeout waiting for pod with selector %s in namespace %s", selector, namespace)
		case <-ticker.C:
			cmd := exec.CommandContext(context.Background(), "kubectl",
				"--kubeconfig", kubeconfigPath,
				"get", "pods", "-n", namespace, "-l", selector,
				"-o", "jsonpath={.items[0].status.phase}")
			output, err := cmd.CombinedOutput()
			if err == nil && string(output) == "Running" {
				t.Logf("  Pod %s is Running", selector)
				return
			}

			// Log progress every 30 seconds during long waits
			if time.Since(lastProgressLog) >= 30*time.Second {
				elapsed := time.Since(startTime).Round(time.Second)
				t.Logf("  [%s] Waiting for pod %s (phase: %s)...", elapsed, selector, string(output))
				lastProgressLog = time.Now()
			}
		}
	}
}

// waitForDaemonSet waits for a DaemonSet with the given selector to have ready pods.
func waitForDaemonSet(t *testing.T, kubeconfigPath, namespace, selector string, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	startTime := time.Now()
	lastProgressLog := time.Now()

	for {
		select {
		case <-ctx.Done():
			// Timeout - collect comprehensive diagnostics
			t.Logf("Timeout waiting for daemonset - collecting comprehensive diagnostics...")

			diag := NewDiagnosticCollector(t, kubeconfigPath, namespace, selector)
			diag.WithComponentName("DaemonSet: " + selector)
			diag.Collect()
			diag.Report()

			t.Fatalf("Timeout waiting for daemonset with selector %s in namespace %s", selector, namespace)
		case <-ticker.C:
			cmd := exec.CommandContext(context.Background(), "kubectl",
				"--kubeconfig", kubeconfigPath,
				"get", "daemonset", "-n", namespace, "-l", selector,
				"-o", "jsonpath={.items[0].status.numberReady}")
			output, err := cmd.CombinedOutput()
			if err == nil && string(output) != "" && string(output) != "0" {
				t.Logf("  DaemonSet %s has ready pods", selector)
				return
			}

			if time.Since(lastProgressLog) >= 30*time.Second {
				elapsed := time.Since(startTime).Round(time.Second)
				t.Logf("  [%s] Waiting for daemonset %s...", elapsed, selector)
				lastProgressLog = time.Now()
			}
		}
	}
}

// verifyProviderIDs verifies all nodes have Hetzner provider IDs set.
func verifyProviderIDs(t *testing.T, kubeconfigPath string, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatal("Timeout waiting for provider IDs to be set")
		case <-ticker.C:
			cmd := exec.CommandContext(context.Background(), "kubectl",
				"--kubeconfig", kubeconfigPath,
				"get", "nodes", "-o", "json")
			output, err := cmd.CombinedOutput()
			if err != nil {
				continue
			}

			var nodes struct {
				Items []struct {
					Spec struct {
						ProviderID string `json:"providerID"`
					} `json:"spec"`
				} `json:"items"`
			}
			if json.Unmarshal(output, &nodes) == nil {
				allSet := true
				for _, node := range nodes.Items {
					if !strings.HasPrefix(node.Spec.ProviderID, "hcloud://") {
						allSet = false
						break
					}
				}
				if allSet && len(nodes.Items) > 0 {
					t.Log("  All nodes have provider IDs")
					return
				}
			}
		}
	}
}

// verifyStorageClass verifies the hcloud-volumes StorageClass exists.
func verifyStorageClass(t *testing.T, kubeconfigPath string) {
	cmd := exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", kubeconfigPath,
		"get", "storageclass", "hcloud-volumes")
	if err := cmd.Run(); err != nil {
		t.Fatal("StorageClass hcloud-volumes not found")
	}
	t.Log("  StorageClass hcloud-volumes exists")
}

// verifyCRDExists verifies a CRD exists.
func verifyCRDExists(t *testing.T, kubeconfigPath, crdName string) {
	cmd := exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", kubeconfigPath,
		"get", "crd", crdName)
	if err := cmd.Run(); err != nil {
		t.Fatalf("CRD %s not found", crdName)
	}
	t.Logf("  CRD %s exists", crdName)
}

// verifyIngressClassExists verifies an IngressClass exists.
func verifyIngressClassExists(t *testing.T, kubeconfigPath, name string) {
	cmd := exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", kubeconfigPath,
		"get", "ingressclass", name)
	if err := cmd.Run(); err != nil {
		t.Fatalf("IngressClass %s not found", name)
	}
	t.Logf("  IngressClass %s exists", name)
}

// verifyTraefikLoadBalancer checks that Traefik is deployed as a Deployment with LoadBalancer service.
func verifyTraefikLoadBalancer(t *testing.T, kubeconfigPath string) {
	// Verify Traefik is a Deployment (not DaemonSet)
	cmd := exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", kubeconfigPath,
		"-n", "traefik",
		"get", "deployment", "-l", "app.kubernetes.io/name=traefik",
		"-o", "jsonpath={.items[0].metadata.name}")
	output, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) == "" {
		t.Fatalf("Traefik Deployment not found: %v (output: %s)", err, string(output))
	}
	t.Logf("  Traefik Deployment: %s", strings.TrimSpace(string(output)))

	// Verify service type is LoadBalancer
	cmd = exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", kubeconfigPath,
		"-n", "traefik",
		"get", "svc", "-l", "app.kubernetes.io/name=traefik",
		"-o", "jsonpath={.items[0].spec.type}")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to get Traefik service type: %v", err)
	}
	svcType := strings.TrimSpace(string(output))
	if svcType != "LoadBalancer" {
		t.Fatalf("Traefik service type is %q, expected LoadBalancer", svcType)
	}
	t.Logf("  Traefik service type: %s", svcType)

	// Verify LB annotation exists
	cmd = exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", kubeconfigPath,
		"-n", "traefik",
		"get", "svc", "-l", "app.kubernetes.io/name=traefik",
		"-o", "jsonpath={.items[0].metadata.annotations.load-balancer\\.hetzner\\.cloud/name}")
	output, err = cmd.CombinedOutput()
	if err == nil && len(output) > 0 {
		t.Logf("  Hetzner LB name: %s", string(output))
	}
}

// verifyClusterIssuerExists verifies a ClusterIssuer exists.
func verifyClusterIssuerExists(t *testing.T, kubeconfigPath, name string) {
	cmd := exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", kubeconfigPath,
		"get", "clusterissuer", name)
	if err := cmd.Run(); err != nil {
		t.Fatalf("ClusterIssuer %s not found", name)
	}
	t.Logf("  ClusterIssuer %s exists", name)
}

// testMetricsAPI verifies the metrics API is working.
func testMetricsAPI(t *testing.T, kubeconfigPath string) {
	// Wait up to 3 minutes for metrics to be available
	for i := 0; i < 18; i++ {
		cmd := exec.CommandContext(context.Background(), "kubectl",
			"--kubeconfig", kubeconfigPath,
			"top", "nodes")
		output, err := cmd.CombinedOutput()
		if err == nil {
			t.Log("  Metrics API working")
			return
		}
		if i%6 == 5 {
			t.Logf("  Waiting for metrics API (attempt %d/18)... Error: %s", i+1, string(output))
		}
		time.Sleep(10 * time.Second)
	}
	t.Fatal("Metrics API not available after 3 minutes")
}

// testCCMLoadBalancer tests CCM's ability to provision load balancers.
func testCCMLoadBalancer(t *testing.T, state *E2EState) {
	t.Log("  Testing CCM LB provisioning...")

	testLBName := "e2e-ccm-test-lb"
	manifest := fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: default
  annotations:
    load-balancer.hetzner.cloud/location: nbg1
spec:
  type: LoadBalancer
  ports:
    - port: 80
  selector:
    app: nonexistent
`, testLBName)

	// Apply manifest
	cmd := exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", state.KubeconfigPath,
		"apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to create test LB service: %v\nOutput: %s", err, string(output))
	}

	// Wait for external IP (max 6 minutes)
	externalIP := ""
	maxAttempts := 72
	for i := 0; i < maxAttempts; i++ {
		cmd = exec.CommandContext(context.Background(), "kubectl",
			"--kubeconfig", state.KubeconfigPath,
			"get", "svc", testLBName, "-o", "jsonpath={.status.loadBalancer.ingress[0].ip}")
		output, err := cmd.CombinedOutput()
		if err == nil && len(output) > 0 {
			externalIP = string(output)
			break
		}

		if i > 0 && i%6 == 0 {
			elapsed := i * 5
			t.Logf("  [%ds] Waiting for LB external IP...", elapsed)
		}

		time.Sleep(5 * time.Second)
	}

	if externalIP == "" {
		t.Fatal("  CCM failed to provision load balancer")
	}

	t.Logf("  CCM provisioned LB with IP: %s", externalIP)

	// Cleanup
	_ = exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", state.KubeconfigPath,
		"delete", "svc", testLBName).Run()

	time.Sleep(30 * time.Second)
	t.Log("  CCM LB test complete")
}

// testCSIVolume tests CSI's ability to provision and mount volumes.
// waitForCSIReady waits for the CSI controller deployment to be ready.
// This ensures the CSI driver can provision volumes before running volume tests.
func waitForCSIReady(t *testing.T, kubeconfigPath string, timeout time.Duration) {
	t.Log("  Waiting for CSI controller to be ready...")
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	startTime := time.Now()
	lastProgressLog := time.Now()

	for {
		select {
		case <-ctx.Done():
			// Timeout - collect comprehensive diagnostics
			t.Logf("Timeout waiting for CSI controller - collecting comprehensive diagnostics...")

			diag := NewDiagnosticCollector(t, kubeconfigPath, "kube-system",
				"app.kubernetes.io/name=hcloud-csi,app.kubernetes.io/component=controller")
			diag.WithComponentName("hcloud-csi-controller")
			diag.Collect()
			diag.Report()

			t.Fatalf("Timeout waiting for CSI controller to be ready after %v", timeout)
		case <-ticker.C:
			// Check if hcloud-csi-controller deployment has ready replicas
			cmd := exec.CommandContext(context.Background(), "kubectl",
				"--kubeconfig", kubeconfigPath,
				"get", "deployment", "hcloud-csi-controller",
				"-n", "kube-system",
				"-o", "jsonpath={.status.readyReplicas}")
			output, err := cmd.CombinedOutput()
			if err == nil && strings.TrimSpace(string(output)) != "" && strings.TrimSpace(string(output)) != "0" {
				elapsed := time.Since(startTime).Round(time.Second)
				t.Logf("  CSI controller ready (after %v)", elapsed)
				return
			}

			// Log progress every 30 seconds
			if time.Since(lastProgressLog) >= 30*time.Second {
				elapsed := time.Since(startTime).Round(time.Second)
				t.Logf("  [%s] Waiting for CSI controller (readyReplicas: %s)...", elapsed, strings.TrimSpace(string(output)))
				lastProgressLog = time.Now()
			}
		}
	}
}

func testCSIVolume(t *testing.T, state *E2EState) {
	t.Log("  Testing CSI volume provisioning...")

	// Wait for CSI controller to be ready before testing volume provisioning
	waitForCSIReady(t, state.KubeconfigPath, 8*time.Minute)

	pvcName := "e2e-csi-test-pvc"
	podName := "e2e-csi-test-pod"

	// Create PVC
	pvcManifest := fmt.Sprintf(`
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: %s
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 10Gi
  storageClassName: hcloud-volumes
`, pvcName)

	cmd := exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", state.KubeconfigPath,
		"apply", "-f", "-")
	cmd.Stdin = strings.NewReader(pvcManifest)
	if err := cmd.Run(); err != nil {
		t.Fatal("  Failed to create PVC")
	}

	// Create pod to trigger binding
	podManifest := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: %s
spec:
  containers:
  - name: test
    image: busybox:latest
    command: ["sleep", "3600"]
    volumeMounts:
    - name: test-volume
      mountPath: /data
  volumes:
  - name: test-volume
    persistentVolumeClaim:
      claimName: %s
  tolerations:
  - operator: Exists
`, podName, pvcName)

	cmd = exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", state.KubeconfigPath,
		"apply", "-f", "-")
	cmd.Stdin = strings.NewReader(podManifest)
	if err := cmd.Run(); err != nil {
		t.Fatal("  Failed to create test pod")
	}

	// Wait for pod to be running (confirms volume attached)
	for i := 0; i < 48; i++ {
		cmd = exec.CommandContext(context.Background(), "kubectl",
			"--kubeconfig", state.KubeconfigPath,
			"get", "pod", podName, "-o", "jsonpath={.status.phase}")
		output, _ := cmd.CombinedOutput()
		if string(output) == "Running" {
			t.Log("  CSI volume provisioned and mounted")
			break
		}
		if i == 47 {
			t.Fatal("  Timeout waiting for pod with CSI volume (4 minutes)")
		}
		if i > 0 && i%12 == 0 {
			t.Logf("  Waiting for CSI volume (attempt %d/48, phase: %s)...", i+1, string(output))
		}
		time.Sleep(5 * time.Second)
	}

	// Cleanup
	_ = exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", state.KubeconfigPath,
		"delete", "pod", podName, "--force", "--grace-period=0").Run()
	_ = exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", state.KubeconfigPath,
		"delete", "pvc", pvcName).Run()

	// Wait for the PVC to actually disappear before returning: deleting the
	// PVC only starts CSI's DeleteVolume call asynchronously. If this test
	// fails elsewhere in the same subtest, the enclosing test destroys the
	// cluster (and its CSI controller) right after, which can leak the
	// underlying Hetzner volume if it hasn't been deleted yet.
	waitForPVCDeleted(t, state.KubeconfigPath, pvcName, 90*time.Second)
	t.Log("  CSI volume test complete")
}

// waitForPVCDeleted polls until the named PVC in the default namespace no
// longer exists, so its backing Hetzner volume has been released before the
// caller proceeds (e.g. into cluster teardown).
func waitForPVCDeleted(t *testing.T, kubeconfigPath, pvcName string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cmd := exec.CommandContext(context.Background(), "kubectl",
			"--kubeconfig", kubeconfigPath,
			"get", "pvc", pvcName)
		if err := cmd.Run(); err != nil {
			return
		}
		time.Sleep(5 * time.Second)
	}
	t.Logf("  Warning: PVC %s still present after %v, its Hetzner volume may need manual cleanup", pvcName, timeout)
}

// testCiliumNetworkConnectivity tests Cilium network connectivity.
func testCiliumNetworkConnectivity(t *testing.T, state *E2EState) {
	t.Log("  Testing Cilium network connectivity...")

	testPodName := "cilium-test-pod"
	manifest := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: default
spec:
  containers:
  - name: test
    image: busybox:latest
    command: ["sleep", "3600"]
  tolerations:
  - operator: Exists
`, testPodName)

	cmd := exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", state.KubeconfigPath,
		"apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	if err := cmd.Run(); err != nil {
		t.Fatal("  Failed to create test pod")
	}

	// Wait for pod to be running
	for i := 0; i < 24; i++ {
		cmd = exec.CommandContext(context.Background(), "kubectl",
			"--kubeconfig", state.KubeconfigPath,
			"get", "pod", testPodName, "-o", "jsonpath={.status.phase}")
		output, _ := cmd.CombinedOutput()
		if string(output) == "Running" {
			t.Log("  Test pod running with Cilium networking")
			break
		}
		if i == 23 {
			t.Fatal("  Timeout waiting for test pod")
		}
		time.Sleep(5 * time.Second)
	}

	// Cleanup
	_ = exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", state.KubeconfigPath,
		"delete", "pod", testPodName, "--force", "--grace-period=0").Run()

	t.Log("  Cilium network connectivity test complete")
}

// showExternalDNSLogs shows external-dns logs for debugging.
func showExternalDNSLogs(t *testing.T, kubeconfigPath string) {
	cmd := exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", kubeconfigPath,
		"logs", "-n", "external-dns", "-l", "app.kubernetes.io/name=external-dns",
		"--tail=30")
	output, _ := cmd.CombinedOutput()
	t.Logf("External-DNS logs:\n%s", string(output))
}
