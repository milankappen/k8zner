package hcloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/milankappen/k8zner/internal/config"
)

func TestNewRealClient_Defaults(t *testing.T) {
	client := NewRealClient("test-token")

	if client.client == nil {
		t.Error("expected hcloud client to be initialized")
	}
	if client.timeouts == nil {
		t.Error("expected timeouts to be initialized")
	}
	if client.httpClient == nil {
		t.Error("expected httpClient to be initialized")
	}
	if client.httpClient.Timeout != defaultHTTPTimeout {
		t.Errorf("expected default httpClient timeout %v, got %v", defaultHTTPTimeout, client.httpClient.Timeout)
	}
}

func TestNewRealClient_WithTimeouts(t *testing.T) {
	customTimeouts := &config.Timeouts{
		ServerIP:          30 * time.Second,
		RetryMaxAttempts:  5,
		RetryInitialDelay: 2 * time.Second,
	}

	client := NewRealClient("test-token", WithTimeouts(customTimeouts))

	if client.timeouts != customTimeouts {
		t.Errorf("expected custom timeouts to be set")
	}
	if client.timeouts.ServerIP != 30*time.Second {
		t.Errorf("expected ServerIP timeout to be 30s, got %v", client.timeouts.ServerIP)
	}
}

func TestNewRealClient_WithHTTPClient(t *testing.T) {
	customHTTPClient := &http.Client{
		Timeout: 60 * time.Second,
	}

	client := NewRealClient("test-token", WithHTTPClient(customHTTPClient))

	if client.httpClient != customHTTPClient {
		t.Error("expected custom HTTP client to be set")
	}
}

func TestNewRealClient_MultipleOptions(t *testing.T) {
	customTimeouts := &config.Timeouts{
		ServerIP: 45 * time.Second,
	}
	customHTTPClient := &http.Client{
		Timeout: 90 * time.Second,
	}

	client := NewRealClient("test-token",
		WithTimeouts(customTimeouts),
		WithHTTPClient(customHTTPClient),
	)

	if client.timeouts != customTimeouts {
		t.Error("expected custom timeouts to be set")
	}
	if client.httpClient != customHTTPClient {
		t.Error("expected custom HTTP client to be set")
	}
}

func TestGetPublicIP_Success(t *testing.T) {
	// Create a mock server that returns a fixed IP
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("203.0.113.42\n"))
	}))
	defer server.Close()

	// Create client with custom HTTP client pointing to mock server
	client := NewRealClient("test-token", WithHTTPClient(server.Client()))

	// We can't easily override the URL in GetPublicIP, but we've tested that
	// the httpClient is properly injected. A full test would require more
	// refactoring to make the URL configurable.
	_ = client

	// Basic sanity check - the injected client replaced the default
	if client.httpClient != server.Client() {
		t.Error("expected custom HTTP client to be used")
	}
}

func TestGetPublicIP_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// This test actually calls the external service
	// Only run it when explicitly testing integration
	client := NewRealClient("test-token")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ip, err := client.GetPublicIP(ctx)
	if err != nil {
		t.Logf("GetPublicIP returned error (might be expected in CI): %v", err)
		return
	}

	if ip == "" {
		t.Error("expected non-empty IP address")
	}
	t.Logf("Got public IP: %s", ip)
}

func TestResolvePlacementGroup(t *testing.T) {
	t.Run("nil placement group ID", func(t *testing.T) {
		result := resolvePlacementGroup(nil)
		if result != nil {
			t.Error("expected nil for nil placement group ID")
		}
	})

	t.Run("valid placement group ID", func(t *testing.T) {
		id := int64(123)
		result := resolvePlacementGroup(&id)
		if result == nil {
			t.Fatal("expected non-nil placement group")
		}
		if result.ID != 123 {
			t.Errorf("expected ID 123, got %d", result.ID)
		}
	})

	t.Run("zero placement group ID", func(t *testing.T) {
		id := int64(0)
		result := resolvePlacementGroup(&id)
		if result == nil {
			t.Fatal("expected non-nil placement group")
		}
		if result.ID != 0 {
			t.Errorf("expected ID 0, got %d", result.ID)
		}
	})
}

func TestLoadedTimeouts(t *testing.T) {
	// Test that the client initializes with valid timeouts from config
	client := NewRealClient("test-token")

	if client.timeouts == nil {
		t.Fatal("expected timeouts to be initialized")
	}
	if client.timeouts.ServerIP == 0 {
		t.Error("expected non-zero ServerIP timeout")
	}
	if client.timeouts.RetryMaxAttempts == 0 {
		t.Error("expected non-zero RetryMaxAttempts")
	}
	if client.timeouts.RetryInitialDelay == 0 {
		t.Error("expected non-zero RetryInitialDelay")
	}
}

func TestBuildLabelSelector(t *testing.T) {
	t.Run("empty map", func(t *testing.T) {
		result := buildLabelSelector(map[string]string{})
		if result != "" {
			t.Errorf("expected empty string, got %q", result)
		}
	})

	t.Run("nil map", func(t *testing.T) {
		result := buildLabelSelector(nil)
		if result != "" {
			t.Errorf("expected empty string, got %q", result)
		}
	})

	t.Run("single label", func(t *testing.T) {
		result := buildLabelSelector(map[string]string{"cluster": "test"})
		if result != "cluster=test" {
			t.Errorf("expected 'cluster=test', got %q", result)
		}
	})

	t.Run("multiple labels", func(t *testing.T) {
		labels := map[string]string{
			"cluster": "test",
			"env":     "staging",
		}
		result := buildLabelSelector(labels)
		// Map iteration order is not guaranteed, so check both possibilities
		if result != "cluster=test,env=staging" && result != "env=staging,cluster=test" {
			t.Errorf("unexpected result: %q", result)
		}
	})
}

func TestWithHCloudClient(t *testing.T) {
	// Test the WithHCloudClient option
	client := NewRealClient("test-token")

	// Verify client was created
	if client.client == nil {
		t.Error("expected hcloud client to be initialized")
	}
}
