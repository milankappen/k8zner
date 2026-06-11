package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/milankappen/k8zner/internal/platform/dns"
)

func TestGetZoneID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") != "example.com" {
			t.Errorf("unexpected domain: %s", r.URL.Query().Get("name"))
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Result:  json.RawMessage(`[{"id":"zone-123"}]`),
		})
	}))
	defer srv.Close()

	c := NewClient("test-token")
	c.httpClient = srv.Client()
	// Override base URL by replacing the client's do method via a custom transport
	origBaseURL := baseURL
	defer func() { /* baseURL is const, we'll use the server URL approach */ }()
	_ = origBaseURL

	// Use a custom HTTP client that rewrites URLs
	c.httpClient = &http.Client{
		Transport: &rewriteTransport{base: srv.URL, wrapped: http.DefaultTransport},
	}

	id, err := c.GetZoneID(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "zone-123" {
		t.Errorf("expected zone-123, got %s", id)
	}
}

func TestGetZoneID_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Result:  json.RawMessage(`[]`),
		})
	}))
	defer srv.Close()

	c := NewClient("test-token")
	c.httpClient = &http.Client{
		Transport: &rewriteTransport{base: srv.URL, wrapped: http.DefaultTransport},
	}

	_, err := c.GetZoneID(context.Background(), "notfound.com")
	if err == nil {
		t.Fatal("expected error for missing zone")
	}
}

func TestCleanupClusterRecords(t *testing.T) {
	var deletedIDs []string

	records := []Record{
		{ID: "txt-1", Type: "TXT", Name: "a-app.example.com", Content: `"heritage=external-dns,external-dns/owner=my-cluster,external-dns/resource=ingress/default/app"`},
		{ID: "txt-2", Type: "TXT", Name: "a-api.example.com", Content: `"heritage=external-dns,external-dns/owner=other-cluster"`},
		{ID: "a-1", Type: "A", Name: "app.example.com", Content: "1.2.3.4"},
		{ID: "a-2", Type: "A", Name: "api.example.com", Content: "5.6.7.8"},
		{ID: "a-3", Type: "A", Name: "unrelated.example.com", Content: "9.9.9.9"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			resp := listResponse{
				Success:    true,
				Result:     records,
				ResultInfo: resultInfo{Page: 1, TotalPages: 1},
			}
			json.NewEncoder(w).Encode(resp)
		case http.MethodDelete:
			// Extract record ID from path: /zones/{zoneID}/dns_records/{recordID}
			parts := splitPath(r.URL.Path)
			deletedIDs = append(deletedIDs, parts[len(parts)-1])
			json.NewEncoder(w).Encode(apiResponse{Success: true, Result: json.RawMessage(`{}`)})
		}
	}))
	defer srv.Close()

	c := NewClient("test-token")
	c.httpClient = &http.Client{
		Transport: &rewriteTransport{base: srv.URL, wrapped: http.DefaultTransport},
	}

	count, err := c.CleanupClusterRecords(context.Background(), "zone-123", "my-cluster")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should delete: txt-1 (TXT ownership) + a-1 (A record for app.example.com)
	if count != 2 {
		t.Errorf("expected 2 deleted, got %d (IDs: %v)", count, deletedIDs)
	}

	expectedIDs := map[string]bool{"txt-1": true, "a-1": true}
	for _, id := range deletedIDs {
		if !expectedIDs[id] {
			t.Errorf("unexpected deletion of record %s", id)
		}
	}
}

func TestListDNSRecords_Pagination(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		page := r.URL.Query().Get("page")
		var records []Record
		totalPages := 2
		if page == "1" || page == "" {
			records = []Record{{ID: "r1", Type: "A", Name: "a.example.com"}}
		} else {
			records = []Record{{ID: "r2", Type: "A", Name: "b.example.com"}}
		}
		json.NewEncoder(w).Encode(listResponse{
			Success:    true,
			Result:     records,
			ResultInfo: resultInfo{Page: callCount, TotalPages: totalPages},
		})
	}))
	defer srv.Close()

	c := NewClient("test-token")
	c.httpClient = &http.Client{
		Transport: &rewriteTransport{base: srv.URL, wrapped: http.DefaultTransport},
	}

	records, err := c.ListDNSRecords(context.Background(), "zone-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 records, got %d", len(records))
	}
}

// rewriteTransport rewrites request URLs to point at the test server.
type rewriteTransport struct {
	base    string
	wrapped http.RoundTripper
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = t.base[len("http://"):]
	return t.wrapped.RoundTrip(req)
}

func splitPath(path string) []string {
	var parts []string
	for _, p := range split(path, '/') {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func split(s string, sep byte) []string {
	var result []string
	start := 0
	for i := range len(s) {
		if s[i] == sep {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}

// Client must satisfy the provider-neutral DNS interface.
var _ dns.Provider = (*Client)(nil)
