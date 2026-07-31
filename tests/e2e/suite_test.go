//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/milankappen/k8zner/internal/config"
	hcloud_client "github.com/milankappen/k8zner/internal/platform/hcloud"
	"github.com/milankappen/k8zner/internal/provisioning/image"
)

// Test result tracking for skip logic between tests
var (
	testResultsLock sync.Mutex
	fullStackPassed bool
)

// SetFullStackPassed marks the full stack test as passed.
// This is called by TestE2EFullStackDev after ALL subtests pass.
func SetFullStackPassed() {
	testResultsLock.Lock()
	defer testResultsLock.Unlock()
	fullStackPassed = true
}

// IsFullStackPassed returns whether TestE2EFullStackDev passed.
// There is NO override - HA test will NEVER run if FullStack failed.
func IsFullStackPassed() bool {
	testResultsLock.Lock()
	defer testResultsLock.Unlock()
	return fullStackPassed
}

// TestMain orchestrates E2E tests to run sequentially and manage shared resources.
func TestMain(m *testing.M) {
	token := os.Getenv("HCLOUD_TOKEN")
	if token == "" {
		log.Println("HCLOUD_TOKEN not set, skipping E2E tests")
		os.Exit(0)
	}

	client := hcloud_client.NewRealClient(token)
	sharedCtx = &SharedTestContext{
		Client: client,
	}

	// Tests shell out to the k8zner CLI (e.g. `k8zner doctor`) via $PATH.
	// Nothing else in the local `make e2e`/`make e2e-fast` targets or the
	// CI workflow builds/installs that binary first, so build it here and
	// prepend it to PATH for this process and its children.
	if err := buildK8znerCLI(); err != nil {
		log.Printf("Failed to build k8zner CLI for tests: %v", err)
		os.Exit(1)
	}

	// Build Talos snapshots before running tests
	log.Println("=== Building Talos snapshots for E2E tests ===")
	if err := buildSharedSnapshots(client); err != nil {
		log.Printf("Failed to build snapshots: %v", err)
		os.Exit(1)
	}

	// Run tests
	code := m.Run()

	// Cleanup snapshots after all tests
	log.Println("=== Cleaning up shared snapshots ===")
	cleanupSharedSnapshots(client)

	os.Exit(code)
}

// buildK8znerCLI builds the k8zner CLI binary and prepends its directory to
// PATH so exec.Command("k8zner", ...) resolves it for the rest of this run.
func buildK8znerCLI() error {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("failed to determine module root: runtime.Caller failed")
	}
	moduleRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	binDir, err := os.MkdirTemp("", "k8zner-e2e-bin-")
	if err != nil {
		return fmt.Errorf("failed to create temp bin dir: %w", err)
	}
	binName := "k8zner"
	if runtime.GOOS == "windows" {
		binName = "k8zner.exe"
	}
	binPath := filepath.Join(binDir, binName)

	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/k8zner")
	cmd.Dir = moduleRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build k8zner CLI: %w\n%s", err, output)
	}

	if err := os.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		return fmt.Errorf("failed to update PATH: %w", err)
	}
	log.Printf("Built k8zner CLI at %s and added to PATH", binPath)
	return nil
}

// buildSharedSnapshots builds Talos snapshot for amd64 (ARM64 not used).
func buildSharedSnapshots(client *hcloud_client.RealClient) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	builder := image.NewBuilder(client)

	// Use versions from the default version matrix for consistency
	// Note: Kubernetes version is used WITHOUT 'v' prefix to match provisioning code labels
	vm := config.DefaultVersionMatrix()
	talosVer := vm.Talos
	k8sVer := vm.Kubernetes // NO 'v' prefix - must match provisioning labels

	// Only build AMD64 - ARM64 is not used (see config.Architecture constant)
	labelsAMD64 := map[string]string{
		"os":            "talos",
		"talos-version": talosVer,
		"k8s-version":   k8sVer,
		"arch":          "amd64",
		"e2e-shared":    "true",
	}

	// Check for existing snapshot FIRST
	log.Println("Checking for existing amd64 snapshot...")
	existingAMD64, err := client.GetSnapshotByLabels(ctx, labelsAMD64)
	if err != nil {
		return fmt.Errorf("failed to check for existing amd64 snapshot: %w", err)
	}

	// Report existing snapshot
	if existingAMD64 != nil {
		log.Printf("Found existing amd64 snapshot: %s (ID: %d)", existingAMD64.Description, existingAMD64.ID)
		sharedCtx.SnapshotAMD64 = fmt.Sprintf("%d", existingAMD64.ID)
		log.Println("Snapshot already exists, skipping build")
		return nil
	}

	// Build AMD64 snapshot
	log.Println("=== BUILDING AMD64 SNAPSHOT ===")
	log.Printf("[amd64] Starting build at %s", time.Now().Format("15:04:05"))

	snapshotID, err := builder.Build(ctx, talosVer, k8sVer, "amd64", "", "nbg1", labelsAMD64)
	if err != nil {
		log.Printf("[amd64] Build failed at %s: %v", time.Now().Format("15:04:05"), err)
		return fmt.Errorf("failed to build amd64 snapshot: %w", err)
	}

	log.Printf("[amd64] Build completed at %s: %s", time.Now().Format("15:04:05"), snapshotID)
	sharedCtx.SnapshotAMD64 = snapshotID

	log.Println("=== SNAPSHOT BUILT SUCCESSFULLY ===")
	return nil
}

// cleanupSharedSnapshots removes the shared snapshot after all tests complete.
// Set E2E_KEEP_SNAPSHOTS=true to skip cleanup for faster local development.
func cleanupSharedSnapshots(client *hcloud_client.RealClient) {
	// Check if snapshots should be kept for faster subsequent runs
	if os.Getenv("E2E_KEEP_SNAPSHOTS") == "true" {
		log.Println("=== Skipping snapshot cleanup (E2E_KEEP_SNAPSHOTS=true) ===")
		log.Printf("Keeping snapshot: amd64=%s", sharedCtx.SnapshotAMD64)
		log.Println("Note: Snapshot will be reused in subsequent test runs")
		return
	}

	ctx := context.Background()

	if sharedCtx.SnapshotAMD64 != "" {
		log.Printf("Deleting amd64 snapshot %s...", sharedCtx.SnapshotAMD64)
		if err := client.DeleteImage(ctx, sharedCtx.SnapshotAMD64); err != nil {
			log.Printf("Failed to delete amd64 snapshot: %v", err)
		}
	}
}
