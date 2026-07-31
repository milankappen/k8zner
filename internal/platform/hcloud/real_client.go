package hcloud

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/milankappen/k8zner/internal/config"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// defaultHTTPTimeout bounds individual HTTP requests (Hetzner API calls and
// public IP lookups) so a degraded network cannot hang provisioning or
// reconciliation indefinitely. Long-running operations poll with many short
// requests, so a per-request bound is safe.
const defaultHTTPTimeout = 30 * time.Second

// RealClient implements InfrastructureManager using the Hetzner Cloud API.
type RealClient struct {
	client     *hcloud.Client
	timeouts   *config.Timeouts
	httpClient *http.Client
}

// ClientOption configures a RealClient.
type ClientOption func(*RealClient)

// WithTimeouts sets custom timeouts for the client.
func WithTimeouts(t *config.Timeouts) ClientOption {
	return func(c *RealClient) {
		c.timeouts = t
	}
}

// WithHTTPClient sets a custom HTTP client for external requests.
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *RealClient) {
		c.httpClient = hc
	}
}

// WithHCloudClient sets a custom hcloud client (useful for testing).
func WithHCloudClient(hc *hcloud.Client) ClientOption {
	return func(c *RealClient) {
		c.client = hc
	}
}

// NewRealClient creates a new RealClient with optional configuration.
func NewRealClient(token string, opts ...ClientOption) *RealClient {
	c := &RealClient{
		client: hcloud.NewClient(
			hcloud.WithToken(token),
			hcloud.WithHTTPClient(&http.Client{Timeout: defaultHTTPTimeout}),
		),
		timeouts:   config.LoadTimeouts(),
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// GetPublicIP returns the public IPv4 address of the host.
func (c *RealClient) GetPublicIP(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://ipv4.icanhazip.com", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}
