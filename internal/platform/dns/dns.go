// Package dns defines the provider-neutral interface for DNS backends.
//
// Cloudflare is the only implementation today; the interface exists so a
// second provider (Hetzner DNS, Route53, ...) is an additive change for
// consumers rather than surgery. Per repo convention it stays minimal: only
// the methods consumers actually call belong here.
package dns

import "context"

// Provider manages cluster-owned DNS records in a hosted zone.
type Provider interface {
	// GetZoneID resolves the hosted zone ID for a domain.
	GetZoneID(ctx context.Context, domain string) (string, error)

	// CleanupClusterRecords deletes all records owned by the given owner ID
	// (external-dns TXT ownership) and returns how many were removed.
	CleanupClusterRecords(ctx context.Context, zoneID, ownerID string) (int, error)
}
