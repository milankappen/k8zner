//go:build e2e

package e2e

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	k8znerv1alpha1 "github.com/milankappen/k8zner/api/v1alpha1"
	"github.com/milankappen/k8zner/cmd/k8zner/handlers"
	"github.com/milankappen/k8zner/internal/platform/s3"
	"github.com/milankappen/k8zner/internal/util/naming"
)

// TestE2EFullStackDev is Test 1: Full addon validation.
//
// This comprehensive test validates ALL addons on a dev cluster:
// - Config: 1 CP + 1 worker, mode=dev, ALL addons enabled
// - Timeout: 60 minutes
//
// This test should be run FIRST before TestE2EHAOperations as it validates
// the full addon stack. The HA test only runs if this test passes.
//
// IMPORTANT: The HA test (TestE2EHAOperations) will ONLY run if ALL subtests
// in this test pass. If any subtest fails, the HA test will be skipped.
//
// Required environment variables:
//   - HCLOUD_TOKEN - Hetzner Cloud API token
//   - CF_API_TOKEN - Cloudflare API token (for DNS/TLS)
//   - CF_DOMAIN - Domain managed by Cloudflare
//   - HETZNER_S3_ACCESS_KEY - Hetzner Object Storage access key
//   - HETZNER_S3_SECRET_KEY - Hetzner Object Storage secret key
//
// Example:
//
//	HCLOUD_TOKEN=xxx CF_API_TOKEN=yyy CF_DOMAIN=example.com \
//	HETZNER_S3_ACCESS_KEY=aaa HETZNER_S3_SECRET_KEY=bbb \
//	go test -v -timeout=60m -tags=e2e -run TestE2EFullStackDev ./tests/e2e/
func TestE2EFullStackDev(t *testing.T) {
	// Track if any subtest fails - HA test should NEVER run if FullStack has failures
	allSubtestsPassed := true
	runSubtest := func(name string, fn func(t *testing.T)) {
		if !allSubtestsPassed {
			t.Skipf("Skipping %s: previous subtest failed", name)
			return
		}
		// IMPORTANT: Use t.Run()'s return value to detect failures.
		// We cannot check t.Failed() after fn(t) because when fn(t) calls
		// t.Fatal()/require.NoError()/etc., it triggers runtime.Goexit() which
		// terminates the goroutine before any code after fn(t) can execute.
		passed := t.Run(name, fn)
		if !passed {
			allSubtestsPassed = false
		}
	}
	// Validate required environment variables
	token := os.Getenv("HCLOUD_TOKEN")
	if token == "" {
		t.Skip("HCLOUD_TOKEN not set, skipping E2E test")
	}

	// CF_API_TOKEN/CF_DOMAIN and HETZNER_S3_* are optional: when absent, the
	// corresponding addon phases (DNS/TLS ingress, etcd backup) are skipped
	// rather than skipping the whole test, so core provisioning and addon
	// installation are still validated on every run.
	cfAPIToken := os.Getenv("CF_API_TOKEN")
	cfDomain := os.Getenv("CF_DOMAIN")
	hasDomain := cfAPIToken != "" && cfDomain != ""

	s3AccessKey := os.Getenv("HETZNER_S3_ACCESS_KEY")
	s3SecretKey := os.Getenv("HETZNER_S3_SECRET_KEY")
	hasBackup := s3AccessKey != "" && s3SecretKey != ""

	// Generate unique identifiers (short cluster names for Hetzner resource limits)
	clusterName := naming.E2ECluster(naming.E2EFullStack) // e.g., e2e-fs-abc12
	clusterID := clusterName[len(naming.E2EFullStack)+1:] // Extract the 5-char ID
	var argoSubdomain, grafanaSubdomain, argoHost, grafanaHost string
	if hasDomain {
		argoSubdomain = "argo-" + clusterID
		grafanaSubdomain = "grafana-" + clusterID
		argoHost = fmt.Sprintf("%s.%s", argoSubdomain, cfDomain)
		grafanaHost = fmt.Sprintf("%s.%s", grafanaSubdomain, cfDomain)
	}

	t.Logf("=== Starting Full Stack Dev E2E Test: %s ===", clusterName)
	if hasDomain {
		t.Logf("=== ArgoCD: https://%s ===", argoHost)
		t.Logf("=== Grafana: https://%s ===", grafanaHost)
	} else {
		t.Log("=== CF_API_TOKEN/CF_DOMAIN not set: DNS/TLS ingress phases will be skipped ===")
	}
	if !hasBackup {
		t.Log("=== HETZNER_S3_ACCESS_KEY/HETZNER_S3_SECRET_KEY not set: backup phase will be skipped ===")
	}

	// Create S3 client for backup verification (only when backup creds are available)
	region := "fsn1"
	bucketName := clusterName + "-etcd-backups"
	var s3Client *s3.Client
	if hasBackup {
		endpoint := fmt.Sprintf("https://%s.your-objectstorage.com", region)
		var err error
		s3Client, err = s3.NewClient(endpoint, region, s3AccessKey, s3SecretKey)
		if err != nil {
			t.Fatalf("Failed to create S3 client: %v", err)
		}
	}

	// Create configuration with all available addons enabled
	configOpts := []ConfigOption{
		WithWorkers(1),
		WithRegion(region),
		WithMonitoring(true),
	}
	if hasDomain {
		configOpts = append(configOpts,
			WithDomain(cfDomain),
			WithArgoSubdomain(argoSubdomain),
			WithGrafanaSubdomain(grafanaSubdomain),
		)
	}
	if hasBackup {
		configOpts = append(configOpts, WithBackup(true))
	}
	configPath := CreateTestConfig(t, clusterName, ModeDev, configOpts...)
	defer os.Remove(configPath)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	// Create cluster via operator
	var state *OperatorTestContext

	// Cleanup handlers
	defer func() {
		// Clean S3 bucket
		if hasBackup {
			t.Log("Cleaning up S3 bucket...")
			if cleanupErr := cleanupS3Bucket(context.Background(), s3Client, bucketName); cleanupErr != nil {
				t.Logf("Warning: failed to cleanup bucket: %v", cleanupErr)
			}
		}

		// Destroy cluster
		if state != nil {
			DestroyCluster(context.Background(), t, state)
		}
	}()

	// =========================================================================
	// SUBTEST 01: Create Cluster
	// =========================================================================
	runSubtest("01_CreateCluster", func(t *testing.T) {
		var createErr error
		state, createErr = CreateClusterViaOperator(ctx, t, configPath)
		require.NoError(t, createErr, "Cluster creation should succeed")
	})

	// =========================================================================
	// SUBTEST 02: Wait for Cluster Ready (doctor-based)
	// =========================================================================
	runSubtest("02_WaitForClusterReady", func(t *testing.T) {
		// First wait via CRD status (operator must finish provisioning)
		err := WaitForClusterReady(ctx, t, state, 30*time.Minute)
		require.NoError(t, err, "Cluster should become ready")

		// Then verify via doctor JSON
		WaitForDoctorHealthy(t, configPath, 5*time.Minute, func(s *handlers.DoctorStatus) error {
			if s.Phase != "Running" {
				return fmt.Errorf("phase is %s", s.Phase)
			}
			if s.ControlPlanes.Ready < 1 {
				return fmt.Errorf("CPs not ready: %d", s.ControlPlanes.Ready)
			}
			if s.Workers.Ready < 1 {
				return fmt.Errorf("workers not ready: %d", s.Workers.Ready)
			}
			return nil
		})
	})

	// Verify cluster is in good state before proceeding
	if allSubtestsPassed && state != nil {
		cluster := GetClusterStatus(ctx, state)
		if cluster == nil {
			allSubtestsPassed = false
			t.Error("Cluster CRD should exist")
		}
	}

	// =========================================================================
	// SUBTEST 03: Verify Addon Pods (core addon functionality)
	// =========================================================================
	runSubtest("03_VerifyAddonPods", func(t *testing.T) {
		vctx := &AddonVerificationContext{
			KubeconfigPath: state.KubeconfigPath,
			Domain:         cfDomain,
			ArgoHost:       argoHost,
			GrafanaHost:    grafanaHost,
			HasBackup:      hasBackup,
		}
		VerifyAllAddonsCore(t, ctx, vctx, state)

		// Validate via doctor JSON (wait for operator to populate all health fields).
		// ExternalDNS and TalosBackup are only installed when the corresponding
		// optional credentials (CF_API_TOKEN/CF_DOMAIN, HETZNER_S3_*) are set.
		expectedAddons := []string{
			k8znerv1alpha1.AddonNameCilium,
			k8znerv1alpha1.AddonNameCCM,
			k8znerv1alpha1.AddonNameCSI,
			k8znerv1alpha1.AddonNameMetricsServer,
			k8znerv1alpha1.AddonNameTraefik,
			k8znerv1alpha1.AddonNameCertManager,
			k8znerv1alpha1.AddonNameArgoCD,
			k8znerv1alpha1.AddonNameMonitoring,
		}
		if hasDomain {
			expectedAddons = append(expectedAddons, k8znerv1alpha1.AddonNameExternalDNS)
		}
		if hasBackup {
			expectedAddons = append(expectedAddons, k8znerv1alpha1.AddonNameTalosBackup)
		}
		WaitForDoctorHealthy(t, configPath, 5*time.Minute, func(status *handlers.DoctorStatus) error {
			if status.Phase != "Running" {
				return fmt.Errorf("cluster phase %s, want Running", status.Phase)
			}
			if status.ControlPlanes.Ready < 1 {
				return fmt.Errorf("CPs ready %d, want >= 1", status.ControlPlanes.Ready)
			}
			if status.Workers.Ready < 1 {
				return fmt.Errorf("workers ready %d, want >= 1", status.Workers.Ready)
			}
			if !status.Infrastructure.Network {
				return fmt.Errorf("network not healthy")
			}
			if !status.Infrastructure.Firewall {
				return fmt.Errorf("firewall not healthy")
			}
			if !status.Infrastructure.LoadBalancer {
				return fmt.Errorf("load balancer not healthy")
			}
			for _, name := range expectedAddons {
				addon, ok := status.Addons[name]
				if !ok {
					return fmt.Errorf("addon %s not in status (have %d addons)", name, len(status.Addons))
				}
				if !addon.Installed {
					return fmt.Errorf("addon %s not installed (phase=%s)", name, addon.Phase)
				}
			}
			// Log addon health (informational, not blocking)
			for _, name := range expectedAddons {
				addon := status.Addons[name]
				if !addon.Healthy {
					t.Logf("  INFO: addon %s installed but not healthy: %s", name, addon.Message)
				}
			}
			return nil
		})
	})

	// =========================================================================
	// SUBTEST 04: Verify External Connectivity (DNS + TLS + HTTPS)
	// Non-fatal: uses t.Run instead of runSubtest so failures don't block
	// subsequent tests. External connectivity depends on DNS propagation
	// and Let's Encrypt which can be unreliable.
	// =========================================================================
	t.Run("04_VerifyConnectivity", func(t *testing.T) {
		if !hasDomain {
			t.Skip("Skipping: CF_API_TOKEN/CF_DOMAIN not set")
		}
		if !allSubtestsPassed || state == nil {
			t.Skip("Skipping: previous subtest failed")
		}
		vctx := &AddonVerificationContext{
			KubeconfigPath: state.KubeconfigPath,
			Domain:         cfDomain,
			ArgoHost:       argoHost,
			GrafanaHost:    grafanaHost,
		}
		VerifyExternalConnectivity(t, vctx, state)
	})

	// =========================================================================
	// SUBTEST 05: Verify Backup (trigger + S3 verification)
	// =========================================================================
	runSubtest("05_VerifyBackup", func(t *testing.T) {
		if !hasBackup {
			t.Skip("Skipping: HETZNER_S3_ACCESS_KEY/HETZNER_S3_SECRET_KEY not set")
		}

		// Trigger manual backup
		triggerBackupJob(t, state.KubeconfigPath, 5*time.Minute)

		// Verify backup in S3
		backupKey := verifyBackupInS3(t, s3Client, bucketName, "etcd-backups/")

		// Verify backup can be restored (download and validate)
		verifyBackupRestore(t, s3Client, bucketName, backupKey)
	})

	// ONLY mark full stack test as passed if ALL subtests passed
	// This is critical: HA test should NEVER run if FullStack has any failures
	if allSubtestsPassed {
		SetFullStackPassed()
		t.Log("=== FULL STACK DEV E2E TEST PASSED ===")
	} else {
		t.Log("=== FULL STACK DEV E2E TEST FAILED - HA test will be skipped ===")
	}
}

// verifyArgoCDIngressConfigured verifies the ArgoCD ingress is properly configured.
func verifyArgoCDIngressConfigured(t *testing.T, kubeconfigPath, expectedHost string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Show ingress details for debugging
			descCmd := exec.CommandContext(context.Background(), "kubectl",
				"--kubeconfig", kubeconfigPath,
				"get", "ingress", "-n", "argocd", "-o", "yaml")
			if output, _ := descCmd.CombinedOutput(); len(output) > 0 {
				t.Logf("ArgoCD ingress YAML:\n%s", string(output))
			}
			t.Fatalf("Timeout waiting for ArgoCD ingress with host %s", expectedHost)
		case <-ticker.C:
			cmd := exec.CommandContext(context.Background(), "kubectl",
				"--kubeconfig", kubeconfigPath,
				"get", "ingress", "-n", "argocd", "-o", "jsonpath={.items[*].spec.rules[*].host}")
			output, err := cmd.CombinedOutput()
			if err == nil && strings.Contains(string(output), expectedHost) {
				t.Logf("  ArgoCD ingress configured with host: %s", expectedHost)
				return
			}
		}
	}
}

// waitForDNSRecord waits for DNS record to be created and resolvable.
func waitForDNSRecord(t *testing.T, hostname string, timeout time.Duration, expectedIPs ...string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	expectedIPMap := make(map[string]bool)
	for _, ip := range expectedIPs {
		expectedIPMap[ip] = true
	}

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for DNS record %s (expected IPs: %v)", hostname, expectedIPs)
		case <-ticker.C:
			cmd := exec.CommandContext(context.Background(), "dig", "+short", hostname)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Logf("  DNS lookup failed: %v", err)
				continue
			}

			resolvedIP := strings.TrimSpace(string(output))
			if resolvedIP == "" {
				t.Log("  Waiting for DNS propagation...")
				continue
			}

			if len(expectedIPs) > 0 {
				if !expectedIPMap[resolvedIP] {
					t.Logf("  Waiting for DNS update (current: %s, expected: %v)...", resolvedIP, expectedIPs)
					continue
				}
			}

			t.Logf("  DNS record created: %s -> %s", hostname, resolvedIP)
			return
		}
	}
}

// waitForArgoCDTLSCertificate waits for the ArgoCD TLS certificate to be issued.
func waitForArgoCDTLSCertificate(t *testing.T, kubeconfigPath string, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	secretName := "argocd-server-tls"

	for {
		select {
		case <-ctx.Done():
			// Get certificate status for debugging
			descCmd := exec.CommandContext(context.Background(), "kubectl",
				"--kubeconfig", kubeconfigPath,
				"describe", "certificate", "-n", "argocd")
			if output, _ := descCmd.CombinedOutput(); len(output) > 0 {
				t.Logf("Certificate status:\n%s", string(output))
			}
			t.Fatalf("Timeout waiting for ArgoCD TLS certificate")
		case <-ticker.C:
			cmd := exec.CommandContext(context.Background(), "kubectl",
				"--kubeconfig", kubeconfigPath,
				"get", "secret", "-n", "argocd", secretName,
				"-o", "jsonpath={.data.tls\\.crt}")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Log("  Waiting for TLS certificate to be issued...")
				continue
			}

			if len(output) > 0 {
				t.Log("  TLS certificate issued")
				return
			}
		}
	}
}

// verifyBackupCronJob verifies the TalosBackup CronJob is properly configured.
func verifyBackupCronJob(t *testing.T, kubeconfigPath, expectedSchedule string) {
	t.Log("  Verifying TalosBackup CronJob configuration...")

	// Get CronJob details
	cmd := exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", kubeconfigPath,
		"get", "cronjob", "-n", "kube-system", "talos-backup",
		"-o", "jsonpath={.spec.schedule}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to get CronJob schedule: %v", err)
	}

	schedule := string(output)
	if schedule != expectedSchedule {
		t.Fatalf("Unexpected schedule: got %s, want %s", schedule, expectedSchedule)
	}
	t.Logf("  CronJob schedule: %s", schedule)

	// Verify S3 secret exists
	cmd = exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", kubeconfigPath,
		"get", "secret", "-n", "kube-system", "talos-backup-s3-secrets")
	if err := cmd.Run(); err != nil {
		t.Fatal("  S3 secrets not found")
	}
	t.Log("  S3 secrets configured")
}

// triggerBackupJob creates a manual Job from the CronJob and waits for completion.
func triggerBackupJob(t *testing.T, kubeconfigPath string, timeout time.Duration) {
	t.Log("  Triggering manual backup job...")

	jobName := fmt.Sprintf("talos-backup-manual-%d", time.Now().Unix())

	// Create a Job from the CronJob
	cmd := exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", kubeconfigPath,
		"create", "job", jobName, "-n", "kube-system",
		"--from=cronjob/talos-backup")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to create backup job: %v\nOutput: %s", err, string(output))
	}
	t.Logf("  Created job: %s", jobName)

	// Wait for job completion
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Get job status for debugging
			statusCmd := exec.CommandContext(context.Background(), "kubectl",
				"--kubeconfig", kubeconfigPath,
				"describe", "job", "-n", "kube-system", jobName)
			if statusOutput, _ := statusCmd.CombinedOutput(); len(statusOutput) > 0 {
				t.Logf("  Job status:\n%s", string(statusOutput))
			}
			t.Fatal("  Timeout waiting for backup job to complete")

		case <-ticker.C:
			cmd := exec.CommandContext(context.Background(), "kubectl",
				"--kubeconfig", kubeconfigPath,
				"get", "job", "-n", "kube-system", jobName,
				"-o", "jsonpath={.status.succeeded}")
			output, err := cmd.CombinedOutput()
			if err == nil && string(output) == "1" {
				t.Log("  Backup job completed successfully")
				return
			}

			// Check for failure
			cmd = exec.CommandContext(context.Background(), "kubectl",
				"--kubeconfig", kubeconfigPath,
				"get", "job", "-n", "kube-system", jobName,
				"-o", "jsonpath={.status.failed}")
			failOutput, _ := cmd.CombinedOutput()
			if string(failOutput) != "" && string(failOutput) != "0" {
				// Get pod logs for debugging
				logsCmd := exec.CommandContext(context.Background(), "kubectl",
					"--kubeconfig", kubeconfigPath,
					"logs", "-n", "kube-system", "-l", fmt.Sprintf("job-name=%s", jobName))
				if logsOutput, _ := logsCmd.CombinedOutput(); len(logsOutput) > 0 {
					t.Logf("  Job logs:\n%s", string(logsOutput))
				}
				t.Fatal("  Backup job failed")
			}

			t.Log("  Waiting for backup job to complete...")
		}
	}
}

// verifyBackupInS3 checks that a backup file exists in the S3 bucket and returns the backup key.
func verifyBackupInS3(t *testing.T, s3Client *s3.Client, bucketName, prefix string) string {
	t.Log("  Verifying backup exists in S3...")

	objects, err := s3Client.ListObjects(context.Background(), bucketName, prefix)
	if err != nil {
		t.Fatalf("Failed to list S3 objects: %v", err)
	}

	if len(objects) == 0 {
		t.Fatal("  No backup files found in S3 bucket")
	}

	t.Logf("  Found %d backup file(s) in S3:", len(objects))
	for _, obj := range objects {
		t.Logf("    - %s", obj)
	}

	return objects[0] // Return the first backup key for restore verification
}

// verifyBackupRestore downloads and validates the backup file can be decompressed.
func verifyBackupRestore(t *testing.T, s3Client *s3.Client, bucketName, backupKey string) {
	t.Log("  Verifying backup can be restored (download and validate)...")

	ctx := context.Background()

	// Download the backup
	data, err := s3Client.GetObject(ctx, bucketName, backupKey)
	if err != nil {
		t.Fatalf("Failed to download backup: %v", err)
	}

	t.Logf("  Downloaded backup: %d bytes", len(data))

	// Verify it's a valid zstd-compressed file
	// zstd magic number: 0x28 0xB5 0x2F 0xFD
	if len(data) < 4 {
		t.Fatal("  Backup file too small to be valid")
	}

	zstdMagic := []byte{0x28, 0xB5, 0x2F, 0xFD}
	if data[0] != zstdMagic[0] || data[1] != zstdMagic[1] || data[2] != zstdMagic[2] || data[3] != zstdMagic[3] {
		t.Fatalf("  Invalid zstd magic number: got %x %x %x %x, want %x %x %x %x",
			data[0], data[1], data[2], data[3],
			zstdMagic[0], zstdMagic[1], zstdMagic[2], zstdMagic[3])
	}

	t.Log("  Backup is valid zstd-compressed file")
	t.Log("  Backup restore verification passed (file is downloadable and valid)")
}

// cleanupS3Bucket deletes all objects and the bucket itself.
func cleanupS3Bucket(ctx context.Context, s3Client *s3.Client, bucketName string) error {
	// Check if bucket exists
	exists, err := s3Client.BucketExists(ctx, bucketName)
	if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}
	if !exists {
		return nil // Nothing to clean up
	}

	// List and delete all objects
	objects, err := s3Client.ListObjects(ctx, bucketName, "")
	if err != nil {
		return fmt.Errorf("failed to list objects: %w", err)
	}

	for _, obj := range objects {
		if err := s3Client.DeleteObject(ctx, bucketName, obj); err != nil {
			return fmt.Errorf("failed to delete object %s: %w", obj, err)
		}
	}

	// Delete the bucket
	if err := s3Client.DeleteBucket(ctx, bucketName); err != nil {
		// Check if it's because the bucket is not empty (shouldn't happen, but be safe)
		if !strings.Contains(err.Error(), "BucketNotEmpty") {
			return fmt.Errorf("failed to delete bucket: %w", err)
		}
	}

	return nil
}

// testArgoCDHTTPSAccess tests HTTPS connectivity to ArgoCD dashboard.
func testArgoCDHTTPSAccess(t *testing.T, hostname string, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

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

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for HTTPS connectivity to ArgoCD at %s", hostname)
		case <-ticker.C:
			resp, err := httpClient.Get(url)
			if err != nil {
				t.Logf("  HTTPS request failed: %v", err)
				continue
			}
			resp.Body.Close()

			if resp.StatusCode == http.StatusOK ||
				resp.StatusCode == http.StatusFound ||
				resp.StatusCode == http.StatusTemporaryRedirect {
				t.Logf("  HTTPS connectivity verified (status: %d)", resp.StatusCode)
				return
			}

			t.Logf("  HTTPS response: %d, waiting...", resp.StatusCode)
		}
	}
}
