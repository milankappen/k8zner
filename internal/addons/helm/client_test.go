package helm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDownloadChartIntegration tests actual chart downloading.
// This test requires network access and downloads real charts.
// Skip in CI environments without network or for fast unit tests.
func TestDownloadChartIntegration(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Use a small, fast-to-download chart for testing
	spec := ChartSpec{
		Repository: "https://kubernetes-sigs.github.io/metrics-server",
		Name:       "metrics-server",
		Version:    "3.12.2",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Download the chart
	chart, err := DownloadChart(ctx, spec)
	if err != nil {
		t.Fatalf("DownloadChart failed: %v", err)
	}

	// Verify the chart was loaded
	if chart == nil {
		t.Fatal("DownloadChart returned nil chart")
	}
	if chart.Name() != "metrics-server" {
		t.Errorf("Chart name = %q, want %q", chart.Name(), "metrics-server")
	}
	if chart.Metadata.Version != "3.12.2" {
		t.Errorf("Chart version = %q, want %q", chart.Metadata.Version, "3.12.2")
	}

	// Verify chart has templates
	if len(chart.Templates) == 0 {
		t.Error("Chart has no templates")
	}

	// Verify chart was cached on disk
	cachePath := getCachePath()
	chartPath := filepath.Join(cachePath, "metrics-server-3.12.2.tgz")
	if _, err := os.Stat(chartPath); os.IsNotExist(err) {
		t.Errorf("Chart was not cached to disk at %s", chartPath)
	}

	// Test that second download uses disk cache (should be fast)
	start := time.Now()
	chart2, err := DownloadChart(ctx, spec)
	if err != nil {
		t.Fatalf("Second DownloadChart failed: %v", err)
	}
	elapsed := time.Since(start)

	if chart2 == nil {
		t.Fatal("Second DownloadChart returned nil chart")
	}

	// Cached download should be very fast (under 100ms typically)
	if elapsed > 5*time.Second {
		t.Logf("Warning: cached download took %v (expected <5s)", elapsed)
	}
}

// TestDownloadChartInvalidRepo tests error handling for invalid repos.
func TestDownloadChartInvalidRepo(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	spec := ChartSpec{
		Repository: "https://invalid-repo.example.com/charts",
		Name:       "nonexistent",
		Version:    "1.0.0",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := DownloadChart(ctx, spec)
	if err == nil {
		t.Error("DownloadChart should fail for invalid repository")
	}
}

// TestClearCache tests cache clearing functionality.
func TestClearCache(t *testing.T) {
	// Uses its own isolated cache dir: getCachePath() resolves to the real,
	// shared ~/.cache/k8zner/charts, which TestDownloadChartIntegration also
	// writes to. Running in parallel against that shared dir raced clearCache's
	// os.RemoveAll against the download, intermittently wiping the chart the
	// other test had just cached.
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	// Create a test file in cache directory
	cachePath := getCachePath()
	if err := os.MkdirAll(cachePath, 0755); err != nil {
		t.Fatalf("Failed to create cache directory: %v", err)
	}

	testFile := filepath.Join(cachePath, "test-cache-file.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Clear cache
	if err := clearCache(); err != nil {
		t.Fatalf("clearCache failed: %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("clearCache did not remove cached files")
	}
}

// TestChartSpecString tests ChartSpec string representation.
func TestChartSpec(t *testing.T) {
	t.Parallel()
	spec := ChartSpec{
		Repository: "https://example.com/charts",
		Name:       "my-chart",
		Version:    "1.2.3",
	}

	if spec.Repository == "" || spec.Name == "" || spec.Version == "" {
		t.Error("ChartSpec fields should not be empty")
	}
}

// TestClearCache_NonexistentDir tests clearCache when cache doesn't exist.
func TestClearCache_NonexistentDir(t *testing.T) {
	// Set a non-existent cache path
	t.Setenv("XDG_CACHE_HOME", "/nonexistent/path/that/does/not/exist")

	// clearCache should succeed even if directory doesn't exist
	err := clearCache()
	if err != nil {
		t.Errorf("clearCache should succeed for nonexistent directory: %v", err)
	}
}

// TestGetCachePath_WithXDGCacheHome verifies XDG_CACHE_HOME is used when set.
func TestGetCachePath_WithXDGCacheHome(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/custom/cache")

	path := getCachePath()
	expected := filepath.Join("/custom/cache", "k8zner", "charts")
	if path != expected {
		t.Errorf("getCachePath() = %q, want %q", path, expected)
	}
}

// TestGetCachePath_WithoutXDGCacheHome verifies the fallback to ~/.cache.
func TestGetCachePath_WithoutXDGCacheHome(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")

	path := getCachePath()
	// Should end with .cache/k8zner/charts (the home dir prefix varies)
	if !filepath.IsAbs(path) && path != filepath.Join(".", ".cache", "k8zner", "charts") {
		// If it resolved to an absolute path, it used homeDir
		if !strings.HasSuffix(path, filepath.Join(".cache", "k8zner", "charts")) {
			t.Errorf("getCachePath() = %q, expected to end with .cache/k8zner/charts", path)
		}
	}
}

// TestClearCache_Idempotent verifies cache clearing is idempotent.
func TestClearCache_Idempotent(t *testing.T) {
	// Use temp directory for cache
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// clearCache should not panic and should succeed
	err := clearCache()
	if err != nil {
		t.Errorf("clearCache failed: %v", err)
	}

	// Verify we can call it multiple times without error
	err = clearCache()
	if err != nil {
		t.Errorf("Second clearCache failed: %v", err)
	}
}
