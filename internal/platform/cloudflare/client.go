package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const baseURL = "https://api.cloudflare.com/client/v4"

// requestTimeout bounds each Cloudflare API call so a degraded network
// cannot hang DNS reconciliation indefinitely.
const requestTimeout = 30 * time.Second

// Client is a minimal Cloudflare API client for DNS record management.
type Client struct {
	apiToken   string
	httpClient *http.Client
}

// Record represents a Cloudflare DNS record.
type Record struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

type apiResponse struct {
	Success bool            `json:"success"`
	Errors  []apiError      `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type zoneResult struct {
	ID string `json:"id"`
}

type resultInfo struct {
	Page       int `json:"page"`
	TotalPages int `json:"total_pages"`
}

type listResponse struct {
	Success    bool       `json:"success"`
	Errors     []apiError `json:"errors"`
	Result     []Record   `json:"result"`
	ResultInfo resultInfo `json:"result_info"`
}

// NewClient creates a new Cloudflare API client.
func NewClient(apiToken string) *Client {
	return &Client{
		apiToken:   apiToken,
		httpClient: &http.Client{Timeout: requestTimeout},
	}
}

// GetZoneID returns the zone ID for the given domain.
func (c *Client) GetZoneID(ctx context.Context, domain string) (string, error) {
	req, err := c.newRequest(ctx, http.MethodGet, fmt.Sprintf("/zones?name=%s", domain), nil)
	if err != nil {
		return "", err
	}

	var resp apiResponse
	if err := c.do(req, &resp); err != nil {
		return "", fmt.Errorf("get zone ID: %w", err)
	}

	var zones []zoneResult
	if err := json.Unmarshal(resp.Result, &zones); err != nil {
		return "", fmt.Errorf("parse zones: %w", err)
	}

	if len(zones) == 0 {
		return "", fmt.Errorf("no zone found for domain %s", domain)
	}

	return zones[0].ID, nil
}

// ListDNSRecords returns all DNS records in the zone.
func (c *Client) ListDNSRecords(ctx context.Context, zoneID string) ([]Record, error) {
	var all []Record
	page := 1

	for {
		req, err := c.newRequest(ctx, http.MethodGet,
			fmt.Sprintf("/zones/%s/dns_records?per_page=100&page=%d", zoneID, page), nil)
		if err != nil {
			return nil, err
		}

		var resp listResponse
		if err := c.do(req, &resp); err != nil {
			return nil, fmt.Errorf("list DNS records page %d: %w", page, err)
		}

		all = append(all, resp.Result...)

		if page >= resp.ResultInfo.TotalPages {
			break
		}
		page++
	}

	return all, nil
}

// DeleteDNSRecord deletes a DNS record by ID.
func (c *Client) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error {
	req, err := c.newRequest(ctx, http.MethodDelete,
		fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID), nil)
	if err != nil {
		return err
	}

	var resp apiResponse
	if err := c.do(req, &resp); err != nil {
		return fmt.Errorf("delete DNS record %s: %w", recordID, err)
	}

	return nil
}

// CleanupClusterRecords finds and deletes all DNS records owned by the given cluster.
// It identifies ownership via TXT records created by external-dns.
// Returns the number of records deleted.
func (c *Client) CleanupClusterRecords(ctx context.Context, zoneID, clusterName string) (int, error) {
	records, err := c.ListDNSRecords(ctx, zoneID)
	if err != nil {
		return 0, fmt.Errorf("list records: %w", err)
	}

	ownerMarker := "external-dns/owner=" + clusterName

	// First pass: find TXT ownership records and collect owned record names.
	ownedNames := make(map[string]bool)
	var toDelete []Record

	for _, r := range records {
		if r.Type != "TXT" {
			continue
		}
		if !strings.Contains(r.Content, ownerMarker) {
			continue
		}

		toDelete = append(toDelete, r)

		// TXT ownership records use prefixed names like "a-<name>" for A records,
		// "aaaa-<name>" for AAAA records, "cname-<name>" for CNAME records.
		name := r.Name
		for _, prefix := range []string{"a-", "aaaa-", "cname-"} {
			if strings.HasPrefix(name, prefix) {
				ownedNames[strings.TrimPrefix(name, prefix)] = true
				break
			}
		}
	}

	// Second pass: find the actual A/AAAA/CNAME records matching owned names.
	for _, r := range records {
		switch r.Type {
		case "A", "AAAA", "CNAME":
			if ownedNames[r.Name] {
				toDelete = append(toDelete, r)
			}
		}
	}

	// Delete all identified records.
	deleted := 0
	for _, r := range toDelete {
		if err := c.DeleteDNSRecord(ctx, zoneID, r.ID); err != nil {
			return deleted, fmt.Errorf("delete record %s (%s %s): %w", r.ID, r.Type, r.Name, err)
		}
		deleted++
	}

	return deleted, nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req) //nolint:gosec // G704: URL comes from validated API endpoint
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("parse response: %w (status %d)", err, resp.StatusCode)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}
