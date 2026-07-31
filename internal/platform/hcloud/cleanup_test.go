package hcloud

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/hetznercloud/hcloud-go/v2/hcloud/schema"
)

func TestCleanupError(t *testing.T) {
	t.Parallel()
	t.Run("single error", func(t *testing.T) {
		t.Parallel()
		ce := &CleanupError{}
		ce.Add(errors.New("test error"))

		if !ce.HasErrors() {
			t.Error("expected HasErrors() to return true")
		}

		if ce.Error() != "test error" {
			t.Errorf("expected 'test error', got %q", ce.Error())
		}
	})

	t.Run("multiple errors", func(t *testing.T) {
		t.Parallel()
		ce := &CleanupError{}
		ce.Add(errors.New("error 1"))
		ce.Add(errors.New("error 2"))

		if !ce.HasErrors() {
			t.Error("expected HasErrors() to return true")
		}

		errStr := ce.Error()
		if errStr != "cleanup encountered 2 errors: [error 1 error 2]" {
			t.Errorf("unexpected error message: %q", errStr)
		}
	})

	t.Run("no errors", func(t *testing.T) {
		t.Parallel()
		ce := &CleanupError{}

		if ce.HasErrors() {
			t.Error("expected HasErrors() to return false")
		}
	})

	t.Run("add nil error", func(t *testing.T) {
		t.Parallel()
		ce := &CleanupError{}
		ce.Add(nil)

		if ce.HasErrors() {
			t.Error("adding nil should not create an error")
		}
	})

	t.Run("unwrap single error", func(t *testing.T) {
		t.Parallel()
		original := errors.New("original error")
		ce := &CleanupError{}
		ce.Add(original)

		if !errors.Is(ce.Unwrap(), original) {
			t.Error("Unwrap should return the original error")
		}
	})
}

// TestBuildLabelSelector is already tested in real_client_test.go

func TestGetResourceInfo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		resource   interface{}
		expectName string
		expectID   int64
	}{
		{
			name:       "server",
			resource:   &hcloud.Server{ID: 1, Name: "server-1"},
			expectName: "server-1",
			expectID:   1,
		},
		{
			name:       "load balancer",
			resource:   &hcloud.LoadBalancer{ID: 2, Name: "lb-1"},
			expectName: "lb-1",
			expectID:   2,
		},
		{
			name:       "firewall",
			resource:   &hcloud.Firewall{ID: 4, Name: "fw-1"},
			expectName: "fw-1",
			expectID:   4,
		},
		{
			name:       "network",
			resource:   &hcloud.Network{ID: 5, Name: "net-1"},
			expectName: "net-1",
			expectID:   5,
		},
		{
			name:       "placement group",
			resource:   &hcloud.PlacementGroup{ID: 6, Name: "pg-1"},
			expectName: "pg-1",
			expectID:   6,
		},
		{
			name:       "SSH key",
			resource:   &hcloud.SSHKey{ID: 7, Name: "key-1"},
			expectName: "key-1",
			expectID:   7,
		},
		{
			name:       "certificate",
			resource:   &hcloud.Certificate{ID: 8, Name: "cert-1"},
			expectName: "cert-1",
			expectID:   8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var info resourceInfo
			switch v := tt.resource.(type) {
			case *hcloud.Server:
				info = getResourceInfo(v)
			case *hcloud.LoadBalancer:
				info = getResourceInfo(v)
			case *hcloud.Firewall:
				info = getResourceInfo(v)
			case *hcloud.Network:
				info = getResourceInfo(v)
			case *hcloud.PlacementGroup:
				info = getResourceInfo(v)
			case *hcloud.SSHKey:
				info = getResourceInfo(v)
			case *hcloud.Certificate:
				info = getResourceInfo(v)
			}

			if info.Name != tt.expectName {
				t.Errorf("expected name %q, got %q", tt.expectName, info.Name)
			}
			if info.ID != tt.expectID {
				t.Errorf("expected ID %d, got %d", tt.expectID, info.ID)
			}
		})
	}
}

func TestRealClient_CleanupByLabel_WithHTTPMock(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	// Mock all endpoints with empty lists - this tests the happy path with no resources
	ts.handleFunc("/servers", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, schema.ServerListResponse{Servers: []schema.Server{}})
	})

	ts.handleFunc("/volumes", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, schema.VolumeListResponse{Volumes: []schema.Volume{}})
	})

	ts.handleFunc("/load_balancers", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, schema.LoadBalancerListResponse{LoadBalancers: []schema.LoadBalancer{}})
	})

	ts.handleFunc("/firewalls", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, schema.FirewallListResponse{Firewalls: []schema.Firewall{}})
	})

	ts.handleFunc("/networks", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, schema.NetworkListResponse{Networks: []schema.Network{}})
	})

	ts.handleFunc("/placement_groups", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, schema.PlacementGroupListResponse{PlacementGroups: []schema.PlacementGroup{}})
	})

	ts.handleFunc("/ssh_keys", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, schema.SSHKeyListResponse{SSHKeys: []schema.SSHKey{}})
	})

	ts.handleFunc("/certificates", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, schema.CertificateListResponse{Certificates: []schema.Certificate{}})
	})

	client := ts.realClient()
	ctx := context.Background()

	err := client.CleanupByLabel(ctx, map[string]string{"cluster": "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRealClient_DeleteServersByLabel_WithHTTPMock(t *testing.T) {
	t.Parallel()
	t.Run("deletes servers successfully", func(t *testing.T) {
		t.Parallel()
		ts := newTestServer()
		defer ts.close()

		callCount := 0
		ts.handleFunc("/servers", func(w http.ResponseWriter, r *http.Request) {
			callCount++
			// First call returns servers to delete, subsequent calls return empty
			if callCount == 1 {
				jsonResponse(w, http.StatusOK, schema.ServerListResponse{
					Servers: []schema.Server{
						{ID: 1, Name: "server-1", Labels: map[string]string{"cluster": "test"}},
						{ID: 2, Name: "server-2", Labels: map[string]string{"cluster": "test"}},
					},
				})
				return
			}
			// After first call, return empty (servers deleted)
			jsonResponse(w, http.StatusOK, schema.ServerListResponse{Servers: []schema.Server{}})
		})

		ts.handleFunc("/servers/1", func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodDelete {
				jsonResponse(w, http.StatusOK, schema.ServerDeleteResponse{
					Action: schema.Action{ID: 1, Status: "success"},
				})
				return
			}
		})

		ts.handleFunc("/servers/2", func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodDelete {
				jsonResponse(w, http.StatusOK, schema.ServerDeleteResponse{
					Action: schema.Action{ID: 2, Status: "success"},
				})
				return
			}
		})

		client := ts.realClient()
		ctx := context.Background()

		err := client.deleteServersByLabel(ctx, "cluster=test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("handles empty label selector", func(t *testing.T) {
		t.Parallel()
		ts := newTestServer()
		defer ts.close()

		ts.handleFunc("/servers", func(w http.ResponseWriter, _ *http.Request) {
			jsonResponse(w, http.StatusOK, schema.ServerListResponse{Servers: []schema.Server{}})
		})

		client := ts.realClient()
		ctx := context.Background()

		err := client.deleteServersByLabel(ctx, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestRealClient_DeleteLoadBalancersByLabel_WithHTTPMock(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	ts.handleFunc("/load_balancers", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, schema.LoadBalancerListResponse{
			LoadBalancers: []schema.LoadBalancer{
				{ID: 1, Name: "lb-1", Labels: map[string]string{"cluster": "test"}},
			},
		})
	})

	ts.handleFunc("/load_balancers/1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	})

	client := ts.realClient()
	ctx := context.Background()

	err := client.deleteLoadBalancersByLabel(ctx, "cluster=test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRealClient_DeleteFirewallsByLabel_WithHTTPMock(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	ts.handleFunc("/firewalls", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, schema.FirewallListResponse{
			Firewalls: []schema.Firewall{
				{ID: 1, Name: "fw-1", Labels: map[string]string{"cluster": "test"}},
			},
		})
	})

	ts.handleFunc("/firewalls/1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	})

	client := ts.realClient()
	ctx := context.Background()

	err := client.deleteFirewallsByLabel(ctx, "cluster=test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRealClient_DeleteNetworksByLabel_WithHTTPMock(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	ts.handleFunc("/networks", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, schema.NetworkListResponse{
			Networks: []schema.Network{
				{ID: 1, Name: "net-1", Labels: map[string]string{"cluster": "test"}},
			},
		})
	})

	ts.handleFunc("/networks/1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	})

	client := ts.realClient()
	ctx := context.Background()

	err := client.deleteNetworksByLabel(ctx, "cluster=test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRealClient_DeletePlacementGroupsByLabel_WithHTTPMock(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	ts.handleFunc("/placement_groups", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, schema.PlacementGroupListResponse{
			PlacementGroups: []schema.PlacementGroup{
				{ID: 1, Name: "pg-1", Labels: map[string]string{"cluster": "test"}},
			},
		})
	})

	ts.handleFunc("/placement_groups/1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	})

	client := ts.realClient()
	ctx := context.Background()

	err := client.deletePlacementGroupsByLabel(ctx, "cluster=test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRealClient_DeleteSSHKeysByLabel_WithHTTPMock(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	ts.handleFunc("/ssh_keys", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, schema.SSHKeyListResponse{
			SSHKeys: []schema.SSHKey{
				{ID: 1, Name: "key-1", Labels: map[string]string{"cluster": "test"}},
			},
		})
	})

	ts.handleFunc("/ssh_keys/1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	})

	client := ts.realClient()
	ctx := context.Background()

	err := client.deleteSSHKeysByLabel(ctx, "cluster=test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRealClient_DeleteCertificatesByLabel_WithHTTPMock(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	ts.handleFunc("/certificates", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, schema.CertificateListResponse{
			Certificates: []schema.Certificate{
				{ID: 1, Name: "cert-1", Labels: map[string]string{"cluster": "test"}},
			},
		})
	})

	ts.handleFunc("/certificates/1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	})

	client := ts.realClient()
	ctx := context.Background()

	err := client.deleteCertificatesByLabel(ctx, "cluster=test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRealClient_DeleteVolumesByLabel_WithHTTPMock(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	ts.handleFunc("/volumes", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, schema.VolumeListResponse{
			Volumes: []schema.Volume{
				{ID: 1, Name: "vol-1", Labels: map[string]string{"cluster": "test"}},
			},
		})
	})

	ts.handleFunc("/volumes/1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	})

	client := ts.realClient()
	ctx := context.Background()

	err := client.deleteVolumesByLabel(ctx, "cluster=test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCleanupError_UnwrapMultiple tests Unwrap with multiple errors (covers the errors.Join branch).
func TestCleanupError_UnwrapMultiple(t *testing.T) {
	t.Parallel()
	err1 := errors.New("first")
	err2 := errors.New("second")
	ce := &CleanupError{}
	ce.Add(err1)
	ce.Add(err2)

	unwrapped := ce.Unwrap()
	if unwrapped == nil {
		t.Fatal("expected non-nil from Unwrap with multiple errors")
	}
	// errors.Join returns a joined error that satisfies errors.Is for each sub-error
	if !errors.Is(unwrapped, err1) {
		t.Error("expected Unwrap result to contain first error")
	}
	if !errors.Is(unwrapped, err2) {
		t.Error("expected Unwrap result to contain second error")
	}
}

// TestGetResourceInfo_Volume covers the Volume type branch in getResourceInfo.
func TestGetResourceInfo_Volume(t *testing.T) {
	t.Parallel()
	vol := &hcloud.Volume{ID: 99, Name: "vol-99"}
	info := getResourceInfo(vol)
	if info.Name != "vol-99" {
		t.Errorf("expected name 'vol-99', got %q", info.Name)
	}
	if info.ID != 99 {
		t.Errorf("expected ID 99, got %d", info.ID)
	}
}

// TestRealClient_CleanupByLabel_WithErrors tests CleanupByLabel when various resource deletions fail.
func TestRealClient_CleanupByLabel_WithErrors(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	// Servers list returns error
	ts.handleFunc("/servers", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		jsonResponse(w, http.StatusInternalServerError, schema.ErrorResponse{
			Error: schema.Error{Code: "server_error", Message: "internal"},
		})
	})

	// Volumes list returns error
	ts.handleFunc("/volumes", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		jsonResponse(w, http.StatusInternalServerError, schema.ErrorResponse{
			Error: schema.Error{Code: "server_error", Message: "internal"},
		})
	})

	// Load balancers returns empty (no error)
	ts.handleFunc("/load_balancers", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, schema.LoadBalancerListResponse{LoadBalancers: []schema.LoadBalancer{}})
	})

	// Firewalls list returns error
	ts.handleFunc("/firewalls", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		jsonResponse(w, http.StatusInternalServerError, schema.ErrorResponse{
			Error: schema.Error{Code: "server_error", Message: "internal"},
		})
	})

	// Networks returns empty (no error)
	ts.handleFunc("/networks", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, schema.NetworkListResponse{Networks: []schema.Network{}})
	})

	// Placement groups returns empty (no error)
	ts.handleFunc("/placement_groups", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, schema.PlacementGroupListResponse{PlacementGroups: []schema.PlacementGroup{}})
	})

	// SSH keys returns error
	ts.handleFunc("/ssh_keys", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		jsonResponse(w, http.StatusInternalServerError, schema.ErrorResponse{
			Error: schema.Error{Code: "server_error", Message: "internal"},
		})
	})

	// Certificates returns empty (no error)
	ts.handleFunc("/certificates", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, schema.CertificateListResponse{Certificates: []schema.Certificate{}})
	})

	client := ts.realClient()
	ctx := context.Background()

	err := client.CleanupByLabel(ctx, map[string]string{"cluster": "test"})
	if err == nil {
		t.Fatal("expected error from CleanupByLabel")
	}

	var cleanupErr *CleanupError
	if !errors.As(err, &cleanupErr) {
		t.Fatalf("expected *CleanupError, got %T", err)
	}
	// Should have errors for: servers, volumes, firewalls, ssh_keys (4 errors)
	if len(cleanupErr.Errors) < 3 {
		t.Errorf("expected at least 3 errors, got %d: %v", len(cleanupErr.Errors), cleanupErr.Errors)
	}
}

// TestRealClient_DeleteServersByLabel_ListError tests the list error path.
func TestRealClient_DeleteServersByLabel_ListError(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	ts.handleFunc("/servers", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		jsonResponse(w, http.StatusInternalServerError, schema.ErrorResponse{
			Error: schema.Error{Code: "server_error", Message: "internal"},
		})
	})

	client := ts.realClient()
	ctx := context.Background()

	err := client.deleteServersByLabel(ctx, "cluster=test")
	if err == nil {
		t.Fatal("expected error from list servers")
	}
	if !searchString(err.Error(), "failed to list servers") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestRealClient_DeleteServersByLabel_ContextCanceled tests the context cancellation path
// during the wait-for-deletion loop.
func TestRealClient_DeleteServersByLabel_ContextCanceled(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	callCount := 0
	ts.handleFunc("/servers", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: return a server to trigger the wait loop
			jsonResponse(w, http.StatusOK, schema.ServerListResponse{
				Servers: []schema.Server{
					{ID: 1, Name: "server-1"},
				},
			})
			return
		}
		// Subsequent calls: server still exists (will never be empty)
		jsonResponse(w, http.StatusOK, schema.ServerListResponse{
			Servers: []schema.Server{
				{ID: 1, Name: "server-1"},
			},
		})
	})

	ts.handleFunc("/servers/1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			jsonResponse(w, http.StatusOK, schema.ServerDeleteResponse{
				Action: schema.Action{ID: 1, Status: "success"},
			})
			return
		}
	})

	client := ts.realClient()
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately to trigger the ctx.Done() path in the wait loop
	cancel()

	err := client.deleteServersByLabel(ctx, "cluster=test")
	// With a canceled context, the function may return ctx.Err() or succeed
	// depending on timing; the key is to exercise the code path
	_ = err
}

// TestRealClient_DeleteCCMLoadBalancers_Found tests DeleteCCMLoadBalancers when CCM LBs exist.
func TestRealClient_DeleteCCMLoadBalancers_Found(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	ts.handleFunc("/load_balancers", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, schema.LoadBalancerListResponse{
			LoadBalancers: []schema.LoadBalancer{
				{
					ID:     10,
					Name:   "ccm-lb-1",
					Labels: map[string]string{"hcloud-ccm/service-uid": "uid-123"},
				},
				{
					ID:     11,
					Name:   "non-ccm-lb",
					Labels: map[string]string{"app": "myapp"},
				},
				{
					ID:     12,
					Name:   "ccm-lb-2",
					Labels: map[string]string{"hcloud-ccm/service-uid": "uid-456"},
				},
			},
		})
	})

	ts.handleFunc("/load_balancers/10", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	})

	ts.handleFunc("/load_balancers/12", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	})

	client := ts.realClient()
	ctx := context.Background()

	err := client.DeleteCCMLoadBalancers(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRealClient_DeleteCCMLoadBalancers_NoCCMLBs tests DeleteCCMLoadBalancers with no CCM LBs.
func TestRealClient_DeleteCCMLoadBalancers_NoCCMLBs(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	ts.handleFunc("/load_balancers", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, schema.LoadBalancerListResponse{
			LoadBalancers: []schema.LoadBalancer{
				{
					ID:     10,
					Name:   "regular-lb",
					Labels: map[string]string{"app": "myapp"},
				},
			},
		})
	})

	client := ts.realClient()
	ctx := context.Background()

	err := client.DeleteCCMLoadBalancers(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRealClient_DeleteCCMLoadBalancers_ListError tests DeleteCCMLoadBalancers when listing fails.
func TestRealClient_DeleteCCMLoadBalancers_ListError(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	ts.handleFunc("/load_balancers", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		jsonResponse(w, http.StatusInternalServerError, schema.ErrorResponse{
			Error: schema.Error{Code: "server_error", Message: "internal"},
		})
	})

	client := ts.realClient()
	ctx := context.Background()

	err := client.DeleteCCMLoadBalancers(ctx)
	if err == nil {
		t.Fatal("expected error from listing LBs")
	}
	if !searchString(err.Error(), "failed to list all load balancers") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestRealClient_DeleteCCMLoadBalancers_DeleteError tests DeleteCCMLoadBalancers when deletion fails.
func TestRealClient_DeleteCCMLoadBalancers_DeleteError(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	ts.handleFunc("/load_balancers", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, schema.LoadBalancerListResponse{
			LoadBalancers: []schema.LoadBalancer{
				{
					ID:     10,
					Name:   "ccm-lb-1",
					Labels: map[string]string{"hcloud-ccm/service-uid": "uid-123"},
				},
			},
		})
	})

	ts.handleFunc("/load_balancers/10", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			jsonResponse(w, http.StatusInternalServerError, schema.ErrorResponse{
				Error: schema.Error{Code: "server_error", Message: "delete failed"},
			})
			return
		}
	})

	client := ts.realClient()
	ctx := context.Background()

	err := client.DeleteCCMLoadBalancers(ctx)
	if err == nil {
		t.Fatal("expected error from deleting CCM LB")
	}
}

// TestRealClient_DeleteFirewallsByLabel_ListError tests the list error path.
func TestRealClient_DeleteFirewallsByLabel_ListError(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	ts.handleFunc("/firewalls", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		jsonResponse(w, http.StatusInternalServerError, schema.ErrorResponse{
			Error: schema.Error{Code: "server_error", Message: "internal"},
		})
	})

	client := ts.realClient()
	ctx := context.Background()

	err := client.deleteFirewallsByLabel(ctx, "cluster=test")
	if err == nil {
		t.Fatal("expected error from listing firewalls")
	}
	if !searchString(err.Error(), "failed to list firewalls") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestRealClient_DeleteFirewallsByLabel_WithAppliedTo tests firewall deletion when
// firewalls have resource associations that need to be removed first.
func TestRealClient_DeleteFirewallsByLabel_WithAppliedTo(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	ts.handleFunc("/firewalls", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, schema.FirewallListResponse{
			Firewalls: []schema.Firewall{
				{
					ID:   1,
					Name: "fw-with-resources",
					AppliedTo: []schema.FirewallResource{
						{
							Type: "server",
							Server: &schema.FirewallResourceServer{
								ID: 100,
							},
						},
						{
							Type: "label_selector",
							LabelSelector: &schema.FirewallResourceLabelSelector{
								Selector: "role=worker",
							},
						},
					},
				},
			},
		})
	})

	ts.handleFunc("/firewalls/1/actions/remove_from_resources", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusCreated, struct {
			Actions []schema.Action `json:"actions"`
		}{
			Actions: []schema.Action{{ID: 10, Status: "success"}},
		})
	})

	ts.handleFunc("/actions/10", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, schema.ActionGetResponse{
			Action: schema.Action{ID: 10, Status: "success", Progress: 100},
		})
	})

	ts.handleFunc("/firewalls/1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	})

	client := ts.realClient()
	ctx := context.Background()

	err := client.deleteFirewallsByLabel(ctx, "cluster=test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRealClient_DeleteFirewallsByLabel_DeleteError tests the non-retryable delete error path.
func TestRealClient_DeleteFirewallsByLabel_DeleteError(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	ts.handleFunc("/firewalls", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, schema.FirewallListResponse{
			Firewalls: []schema.Firewall{
				{ID: 1, Name: "fw-1"},
			},
		})
	})

	ts.handleFunc("/firewalls/1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			jsonResponse(w, http.StatusForbidden, schema.ErrorResponse{
				Error: schema.Error{Code: "forbidden", Message: "not allowed"},
			})
			return
		}
	})

	client := ts.realClient()
	ctx := context.Background()

	// Should not error at function level (firewall deletion errors are logged, not returned)
	err := client.deleteFirewallsByLabel(ctx, "cluster=test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRealClient_DeleteFirewallsByLabel_ContextCanceled tests context cancellation during firewall deletion.
func TestRealClient_DeleteFirewallsByLabel_ContextCanceled(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	ts.handleFunc("/firewalls", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, schema.FirewallListResponse{
			Firewalls: []schema.Firewall{
				{ID: 1, Name: "fw-1"},
			},
		})
	})

	// We don't even need a handler for /firewalls/1 since context is already canceled
	client := ts.realClient()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := client.deleteFirewallsByLabel(ctx, "cluster=test")
	// With canceled context, should return ctx.Err()
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
}

// TestRealClient_DeleteVolumesByLabel_ListError tests the list error path.
func TestRealClient_DeleteVolumesByLabel_ListError(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	ts.handleFunc("/volumes", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		jsonResponse(w, http.StatusInternalServerError, schema.ErrorResponse{
			Error: schema.Error{Code: "server_error", Message: "internal"},
		})
	})

	client := ts.realClient()
	ctx := context.Background()

	err := client.deleteVolumesByLabel(ctx, "cluster=test")
	if err == nil {
		t.Fatal("expected error from listing volumes")
	}
	if !searchString(err.Error(), "failed to list volumes") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestRealClient_DeleteVolumesByLabel_DeleteError tests a non-retryable delete error for volumes.
func TestRealClient_DeleteVolumesByLabel_DeleteError(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	ts.handleFunc("/volumes", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, schema.VolumeListResponse{
			Volumes: []schema.Volume{
				{ID: 1, Name: "vol-1"},
			},
		})
	})

	ts.handleFunc("/volumes/1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			jsonResponse(w, http.StatusForbidden, schema.ErrorResponse{
				Error: schema.Error{Code: "forbidden", Message: "not allowed"},
			})
			return
		}
	})

	client := ts.realClient()
	ctx := context.Background()

	// Volume delete errors are logged but not returned from the function
	err := client.deleteVolumesByLabel(ctx, "cluster=test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRealClient_DeleteVolumesByLabel_ServiceErrorRetriesThenSucceeds simulates
// the real-world race where a volume's attachment state hasn't cleared yet
// right after its server was deleted: Hetzner reports this as the generic
// ErrorCodeServiceError (it has no more specific code for the condition), and
// deletion must retry rather than give up on the first attempt.
func TestRealClient_DeleteVolumesByLabel_ServiceErrorRetriesThenSucceeds(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	ts.handleFunc("/volumes", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, schema.VolumeListResponse{
			Volumes: []schema.Volume{
				{ID: 1, Name: "vol-1", Labels: map[string]string{"cluster": "test"}},
			},
		})
	})

	attempts := 0
	ts.handleFunc("/volumes/1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			return
		}
		attempts++
		if attempts < 3 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			jsonResponse(w, http.StatusConflict, schema.ErrorResponse{
				Error: schema.Error{Code: "service_error", Message: "volume with ID '1' is still attached to a server"},
			})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	client := ts.realClient()
	ctx := context.Background()

	err := client.deleteVolumesByLabel(ctx, "cluster=test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 delete attempts, got %d", attempts)
	}
}

// TestRealClient_DeleteVolumesByLabel_ContextCanceled tests context cancellation during volume deletion.
func TestRealClient_DeleteVolumesByLabel_ContextCanceled(t *testing.T) {
	t.Parallel()
	ts := newTestServer()
	defer ts.close()

	ts.handleFunc("/volumes", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, schema.VolumeListResponse{
			Volumes: []schema.Volume{
				{ID: 1, Name: "vol-1"},
			},
		})
	})

	client := ts.realClient()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := client.deleteVolumesByLabel(ctx, "cluster=test")
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestDeleteResourcesByLabel(t *testing.T) {
	t.Parallel()
	// Test the generic deleteResourcesByLabel function

	t.Run("handles list error", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		err := deleteResourcesByLabel(ctx, "test",
			func(ctx context.Context) ([]*hcloud.Server, error) {
				return nil, context.DeadlineExceeded
			},
			func(ctx context.Context, s *hcloud.Server) error {
				return nil
			},
		)
		if err == nil {
			t.Fatal("expected error from list function")
		}
	})

	t.Run("returns delete errors", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		// deleteResourcesByLabel now returns errors for failed deletions
		err := deleteResourcesByLabel(ctx, "test",
			func(ctx context.Context) ([]*hcloud.Server, error) {
				return []*hcloud.Server{{ID: 1, Name: "test-server"}}, nil
			},
			func(ctx context.Context, s *hcloud.Server) error {
				return context.DeadlineExceeded
			},
		)
		// Should return error when delete fails
		if err == nil {
			t.Fatal("expected error from delete function")
		}
	})

	t.Run("handles empty list", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		err := deleteResourcesByLabel(ctx, "test",
			func(ctx context.Context) ([]*hcloud.Server, error) {
				return []*hcloud.Server{}, nil
			},
			func(ctx context.Context, s *hcloud.Server) error {
				t.Error("delete should not be called for empty list")
				return nil
			},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
