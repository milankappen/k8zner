package handlers

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/milankappen/k8zner/internal/config"
	"github.com/milankappen/k8zner/internal/platform/dns"
)

// fakeDNSProvider records calls so tests can substitute any DNS backend.
type fakeDNSProvider struct {
	zoneIDByDomain map[string]string
	cleanedZoneID  string
	cleanedOwnerID string
	cleanupErr     error
}

func (f *fakeDNSProvider) GetZoneID(_ context.Context, domain string) (string, error) {
	id, ok := f.zoneIDByDomain[domain]
	if !ok {
		return "", fmt.Errorf("zone not found for %s", domain)
	}
	return id, nil
}

func (f *fakeDNSProvider) CleanupClusterRecords(_ context.Context, zoneID, ownerID string) (int, error) {
	f.cleanedZoneID = zoneID
	f.cleanedOwnerID = ownerID
	if f.cleanupErr != nil {
		return 0, f.cleanupErr
	}
	return 2, nil
}

func TestCleanupCloudflareDNS(t *testing.T) {
	// Serial: swaps the package-global provider factory shared with other tests.
	origProvider := newDNSProvider
	defer func() { newDNSProvider = origProvider }()

	newCfg := func() *config.Config {
		cfg := &config.Config{ClusterName: "my-cluster"}
		cfg.Addons.Cloudflare.APIToken = "token"
		cfg.Addons.Cloudflare.Domain = "example.com"
		return cfg
	}

	t.Run("resolves zone by domain and cleans records owned by the cluster", func(t *testing.T) {
		fake := &fakeDNSProvider{zoneIDByDomain: map[string]string{"example.com": "zone-123"}}
		newDNSProvider = func(apiToken string) dns.Provider {
			assert.Equal(t, "token", apiToken)
			return fake
		}

		err := cleanupCloudflareDNS(context.Background(), newCfg())

		require.NoError(t, err)
		assert.Equal(t, "zone-123", fake.cleanedZoneID)
		assert.Equal(t, "my-cluster", fake.cleanedOwnerID)
	})

	t.Run("prefers configured zone ID and TXT owner ID", func(t *testing.T) {
		fake := &fakeDNSProvider{}
		newDNSProvider = func(string) dns.Provider { return fake }

		cfg := newCfg()
		cfg.Addons.Cloudflare.ZoneID = "zone-explicit"
		cfg.Addons.ExternalDNS.TXTOwnerID = "owner-explicit"

		require.NoError(t, cleanupCloudflareDNS(context.Background(), cfg))
		assert.Equal(t, "zone-explicit", fake.cleanedZoneID)
		assert.Equal(t, "owner-explicit", fake.cleanedOwnerID)
	})

	t.Run("zone lookup failure surfaces as error", func(t *testing.T) {
		newDNSProvider = func(string) dns.Provider {
			return &fakeDNSProvider{zoneIDByDomain: map[string]string{}}
		}

		err := cleanupCloudflareDNS(context.Background(), newCfg())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "zone ID")
	})
}
