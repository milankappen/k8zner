package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// domainRegex is compiled once at package init for domain validation.
var domainRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)*\.[a-zA-Z]{2,}$`)

// Spec is the simplified, opinionated configuration for k8zner.
// It requires only 5 fields to deploy a production-ready Kubernetes cluster.
type Spec struct {
	// Name is the cluster name, used for resource naming and tagging.
	// Must be DNS-safe: lowercase alphanumeric and hyphens, must start with letter.
	Name string `yaml:"name"`

	// Region is the Hetzner datacenter location.
	Region Region `yaml:"region"`

	// Mode defines the cluster topology (dev or ha).
	Mode Mode `yaml:"mode"`

	// Workers defines the worker pool configuration.
	Workers WorkerSpec `yaml:"workers"`

	// ControlPlane defines optional control plane configuration.
	// If not specified, defaults to cx23 (2 dedicated vCPU, 4GB RAM).
	ControlPlane *ControlPlaneSpec `yaml:"control_plane,omitempty"`

	// Domain enables automatic DNS and TLS via Cloudflare.
	// Requires CF_API_TOKEN environment variable.
	Domain string `yaml:"domain,omitempty"`

	// WildcardCertificate additionally issues a "*.{domain}" certificate via
	// DNS01 and sets it as Traefik's default, so ingresses without an
	// explicit TLS secret are covered. Only used when Domain is set.
	WildcardCertificate bool `yaml:"wildcard_certificate,omitempty"`

	// ArgoSubdomain is the subdomain for ArgoCD dashboard (default: "argo").
	// When Domain is set, ArgoCD will be accessible at {ArgoSubdomain}.{Domain}.
	// Example: with Domain="example.com" and ArgoSubdomain="argo", ArgoCD is at argo.example.com
	ArgoSubdomain string `yaml:"argo_subdomain,omitempty"`

	// CertEmail is the email address for Let's Encrypt certificate notifications.
	// Let's Encrypt sends expiration warnings to this address.
	// If not set, defaults to "admin@{domain}".
	// Required for production use to receive renewal failure alerts.
	CertEmail string `yaml:"cert_email,omitempty"`

	// Backup enables automatic etcd backups to Hetzner Object Storage.
	// Requires HETZNER_S3_ACCESS_KEY and HETZNER_S3_SECRET_KEY environment variables.
	// Creates bucket "{cluster-name}-etcd-backups" automatically.
	Backup bool `yaml:"backup,omitempty"`

	// Monitoring enables the kube-prometheus-stack (Prometheus, Grafana, Alertmanager).
	// When Domain is set, Grafana will be accessible at {GrafanaSubdomain}.{Domain}.
	// Default: false
	Monitoring bool `yaml:"monitoring,omitempty"`

	// Logging enables the Loki + Alloy log aggregation stack.
	// Logs are kept for 168h by default and stored on a persistent volume.
	Logging bool `yaml:"logging,omitempty"`

	// GrafanaSubdomain is the subdomain for Grafana dashboard (default: "grafana").
	// Only used when both Monitoring and Domain are set.
	// Example: with Domain="example.com", Grafana is at grafana.example.com
	GrafanaSubdomain string `yaml:"grafana_subdomain,omitempty"`
}

// Region is a Hetzner datacenter location.
type Region string

const (
	// RegionNuremberg is the Nuremberg, Germany datacenter (nbg1).
	RegionNuremberg Region = "nbg1"
	// RegionFalkenstein is the Falkenstein, Germany datacenter (fsn1).
	RegionFalkenstein Region = "fsn1"
	// RegionHelsinki is the Helsinki, Finland datacenter (hel1).
	RegionHelsinki Region = "hel1"
)

// validRegions returns all valid regions.
func validRegions() []Region {
	return []Region{RegionNuremberg, RegionFalkenstein, RegionHelsinki}
}

// IsValid returns true if the region is a valid Hetzner location.
func (r Region) IsValid() bool {
	switch r {
	case RegionNuremberg, RegionFalkenstein, RegionHelsinki:
		return true
	default:
		return false
	}
}

// String returns a human-readable description of the region.
func (r Region) String() string {
	switch r {
	case RegionNuremberg:
		return "nbg1 (Nuremberg, Germany)"
	case RegionFalkenstein:
		return "fsn1 (Falkenstein, Germany)"
	case RegionHelsinki:
		return "hel1 (Helsinki, Finland)"
	default:
		return string(r)
	}
}

// Mode defines the cluster topology.
type Mode string

const (
	// ModeDev creates a development cluster with 1 control plane and 1 shared load balancer.
	// Best for: development, testing, side projects.
	// Cost: ~€15-25/mo depending on worker size.
	ModeDev Mode = "dev"

	// ModeHA creates a highly available cluster with 3 control planes and 2 separate load balancers.
	// Best for: production workloads requiring high availability.
	// Cost: ~€45-70/mo depending on worker size.
	ModeHA Mode = "ha"
)

// validModes returns all valid modes.
func validModes() []Mode {
	return []Mode{ModeDev, ModeHA}
}

// IsValid returns true if the mode is valid.
func (m Mode) IsValid() bool {
	switch m {
	case ModeDev, ModeHA:
		return true
	default:
		return false
	}
}

// ControlPlaneCount returns the number of control plane nodes for this mode.
func (m Mode) ControlPlaneCount() int {
	switch m {
	case ModeDev:
		return 1
	case ModeHA:
		return 3
	default:
		return 0
	}
}

// LoadBalancerCount returns the number of load balancers for this mode.
// Dev mode uses 1 shared LB (API on :6443, ingress on :80/:443).
// HA mode uses 2 separate LBs (dedicated API + dedicated ingress).
func (m Mode) LoadBalancerCount() int {
	switch m {
	case ModeDev:
		return 1
	case ModeHA:
		return 2
	default:
		return 0
	}
}

// String returns a human-readable description of the mode.
func (m Mode) String() string {
	switch m {
	case ModeDev:
		return "dev (1 control plane, 1 shared LB)"
	case ModeHA:
		return "ha (3 control planes, 2 separate LBs)"
	default:
		return string(m)
	}
}

// WorkerSpec defines the worker pool configuration.
type WorkerSpec struct {
	// Count is the number of worker nodes (1-5).
	Count int `yaml:"count"`

	// Size is the Hetzner server type for workers.
	Size ServerSize `yaml:"size"`
}

// ControlPlaneSpec defines the optional control plane configuration.
type ControlPlaneSpec struct {
	// Size is the Hetzner server type for control plane nodes.
	// Defaults to cx23 (2 dedicated vCPU, 4GB RAM) if not specified.
	Size ServerSize `yaml:"size,omitempty"`
}

// ServerSize is a Hetzner server type.
// Supports both shared vCPU (CPX) and dedicated vCPU (CX) types.
// Note: Hetzner renamed server types in 2024 (cx22 → cx23, etc.).
// Both old and new names are accepted for backwards compatibility.
type ServerSize string

const (
	// CPX series - Shared vCPU instances (better availability)
	// SizeCPX22 is 2 shared vCPU, 4GB RAM, 40GB disk (~€4.49/mo).
	SizeCPX22 ServerSize = "cpx22"
	// SizeCPX32 is 4 shared vCPU, 8GB RAM, 80GB disk (~€8.49/mo).
	SizeCPX32 ServerSize = "cpx32"
	// SizeCPX42 is 8 shared vCPU, 16GB RAM, 160GB disk (~€15.49/mo).
	SizeCPX42 ServerSize = "cpx42"
	// SizeCPX52 is 16 shared vCPU, 32GB RAM, 320GB disk (~€29.49/mo).
	SizeCPX52 ServerSize = "cpx52"

	// CX series - Dedicated vCPU instances (consistent performance)
	// SizeCX22 is kept for backwards compatibility, maps to cx23.
	//
	// Deprecated: Use SizeCX23 instead.
	SizeCX22 ServerSize = "cx22"
	// SizeCX23 is 2 vCPU, 4GB RAM, 40GB disk (~€4.35/mo).
	SizeCX23 ServerSize = "cx23"
	// SizeCX32 is kept for backwards compatibility, maps to cx33.
	//
	// Deprecated: Use SizeCX33 instead.
	SizeCX32 ServerSize = "cx32"
	// SizeCX33 is 4 vCPU, 8GB RAM, 80GB disk (~€8.09/mo).
	SizeCX33 ServerSize = "cx33"
	// SizeCX42 is kept for backwards compatibility, maps to cx43.
	//
	// Deprecated: Use SizeCX43 instead.
	SizeCX42 ServerSize = "cx42"
	// SizeCX43 is 8 vCPU, 16GB RAM, 160GB disk (~€15.59/mo).
	SizeCX43 ServerSize = "cx43"
	// SizeCX52 is kept for backwards compatibility, maps to cx53.
	//
	// Deprecated: Use SizeCX53 instead.
	SizeCX52 ServerSize = "cx52"
	// SizeCX53 is 16 vCPU, 32GB RAM, 320GB disk (~€29.59/mo).
	SizeCX53 ServerSize = "cx53"
)

// validServerSizes returns all valid server sizes (current names only).
func validServerSizes() []ServerSize {
	return []ServerSize{
		// CPX series (shared vCPU)
		SizeCPX22, SizeCPX32, SizeCPX42, SizeCPX52,
		// CX series (dedicated vCPU)
		SizeCX23, SizeCX33, SizeCX43, SizeCX53,
	}
}

// IsValid returns true if the server size is valid.
// Accepts CPX series, CX series, and legacy CX names (cx22, cx32, etc.).
func (s ServerSize) IsValid() bool {
	switch s {
	// CPX series (shared vCPU)
	case SizeCPX22, SizeCPX32, SizeCPX42, SizeCPX52:
		return true
	// CX series (dedicated vCPU) - includes legacy names
	case SizeCX22, SizeCX23, SizeCX32, SizeCX33, SizeCX42, SizeCX43, SizeCX52, SizeCX53:
		return true
	default:
		return false
	}
}

// Normalize returns the current Hetzner server type name.
// Converts old names (cx22) to new names (cx23) and lowercases the input.
func (s ServerSize) Normalize() ServerSize {
	switch ServerSize(strings.ToLower(string(s))) {
	case SizeCX22:
		return SizeCX23
	case SizeCX32:
		return SizeCX33
	case SizeCX42:
		return SizeCX43
	case SizeCX52:
		return SizeCX53
	default:
		return ServerSize(strings.ToLower(string(s)))
	}
}

// ServerSpecs contains the specifications for a server size.
type ServerSpecs struct {
	VCPU   int
	RAMGB  int
	DiskGB int
}

// Specs returns the specifications for this server size.
func (s ServerSize) Specs() ServerSpecs {
	// Normalize first to handle old server type names
	normalized := s.Normalize()
	switch normalized {
	// CPX series (shared vCPU) - same specs as CX series
	case SizeCPX22:
		return ServerSpecs{VCPU: 2, RAMGB: 4, DiskGB: 40}
	case SizeCPX32:
		return ServerSpecs{VCPU: 4, RAMGB: 8, DiskGB: 80}
	case SizeCPX42:
		return ServerSpecs{VCPU: 8, RAMGB: 16, DiskGB: 160}
	case SizeCPX52:
		return ServerSpecs{VCPU: 16, RAMGB: 32, DiskGB: 320}
	// CX series (dedicated vCPU)
	case SizeCX23:
		return ServerSpecs{VCPU: 2, RAMGB: 4, DiskGB: 40}
	case SizeCX33:
		return ServerSpecs{VCPU: 4, RAMGB: 8, DiskGB: 80}
	case SizeCX43:
		return ServerSpecs{VCPU: 8, RAMGB: 16, DiskGB: 160}
	case SizeCX53:
		return ServerSpecs{VCPU: 16, RAMGB: 32, DiskGB: 320}
	default:
		return ServerSpecs{}
	}
}

// String returns a human-readable description of the server size.
func (s ServerSize) String() string {
	specs := s.Specs()
	return fmt.Sprintf("%s (%d vCPU, %dGB RAM)", string(s), specs.VCPU, specs.RAMGB)
}

// Validate validates the configuration and returns an error if invalid.
func (c *Spec) Validate() error {
	var errs []error

	// Name: required, DNS-safe
	if c.Name == "" {
		errs = append(errs, errors.New("name is required"))
	} else if !isValidDNSName(c.Name) {
		errs = append(errs, errors.New("name must be DNS-safe (lowercase alphanumeric and hyphens, must start with letter)"))
	}

	// Region: must be valid
	if !c.Region.IsValid() {
		errs = append(errs, fmt.Errorf("region must be one of: %v", validRegions()))
	}

	// Mode: must be valid
	if !c.Mode.IsValid() {
		errs = append(errs, fmt.Errorf("mode must be one of: %v", validModes()))
	}

	// Workers: count 1-5, valid size
	if c.Workers.Count < 1 || c.Workers.Count > 5 {
		errs = append(errs, errors.New("workers.count must be 1-5"))
	}
	if !c.Workers.Size.IsValid() {
		errs = append(errs, fmt.Errorf("workers.size must be one of: %v", validServerSizes()))
	}

	// Domain: if set, validate and check for CF_API_TOKEN
	if c.Domain != "" {
		if !isValidDomain(c.Domain) {
			errs = append(errs, errors.New("domain must be a valid domain name"))
		}
		if os.Getenv("CF_API_TOKEN") == "" {
			errs = append(errs, errors.New("CF_API_TOKEN environment variable required when domain is set"))
		}
	}

	// Backup: if enabled, check for S3 credentials
	if c.Backup {
		if os.Getenv("HETZNER_S3_ACCESS_KEY") == "" {
			errs = append(errs, errors.New("HETZNER_S3_ACCESS_KEY environment variable required when backup is enabled"))
		}
		if os.Getenv("HETZNER_S3_SECRET_KEY") == "" {
			errs = append(errs, errors.New("HETZNER_S3_SECRET_KEY environment variable required when backup is enabled"))
		}
	}

	return errors.Join(errs...)
}

// ControlPlaneCount returns the number of control plane nodes.
func (c *Spec) ControlPlaneCount() int {
	return c.Mode.ControlPlaneCount()
}

// LoadBalancerCount returns the number of load balancers.
func (c *Spec) LoadBalancerCount() int {
	return c.Mode.LoadBalancerCount()
}

// HasDomain returns true if a domain is configured.
func (c *Spec) HasDomain() bool {
	return c.Domain != ""
}

// HasBackup returns true if backup is enabled.
func (c *Spec) HasBackup() bool {
	return c.Backup
}

// GetArgoSubdomain returns the ArgoCD subdomain (default: "argo").
func (c *Spec) GetArgoSubdomain() string {
	if c.ArgoSubdomain == "" {
		return "argo"
	}
	return c.ArgoSubdomain
}

// ArgoHost returns the full ArgoCD hostname (e.g., "argo.example.com").
// Returns empty string if no domain is configured.
func (c *Spec) ArgoHost() string {
	if c.Domain == "" {
		return ""
	}
	return c.GetArgoSubdomain() + "." + c.Domain
}

// GetGrafanaSubdomain returns the subdomain for Grafana (default: "grafana").
func (c *Spec) GetGrafanaSubdomain() string {
	if c.GrafanaSubdomain == "" {
		return "grafana"
	}
	return c.GrafanaSubdomain
}

// GrafanaHost returns the full Grafana hostname (e.g., "grafana.example.com").
// Returns empty string if no domain is configured.
func (c *Spec) GrafanaHost() string {
	if c.Domain == "" {
		return ""
	}
	return c.GetGrafanaSubdomain() + "." + c.Domain
}

// HasMonitoring returns true if monitoring is enabled.
func (c *Spec) HasMonitoring() bool {
	return c.Monitoring
}

// GetCertEmail returns the email address for Let's Encrypt certificates.
// If not set, defaults to "admin@{domain}".
func (c *Spec) GetCertEmail() string {
	if c.CertEmail != "" {
		return c.CertEmail
	}
	if c.Domain != "" {
		return "admin@" + c.Domain
	}
	return ""
}

// BackupBucketName returns the S3 bucket name for etcd backups.
func (c *Spec) BackupBucketName() string {
	return c.Name + "-etcd-backups"
}

// S3Endpoint returns the Hetzner S3 endpoint for the configured region.
func (c *Spec) S3Endpoint() string {
	return "https://" + string(c.Region) + ".your-objectstorage.com"
}

// ControlPlaneSize returns the server size for control planes.
// Returns the configured size or defaults to CX23 (2 dedicated vCPU, 4GB RAM).
func (c *Spec) ControlPlaneSize() ServerSize {
	if c.ControlPlane != nil && c.ControlPlane.Size != "" {
		return c.ControlPlane.Size.Normalize()
	}
	return SizeCX23 // Default: 2 dedicated vCPU, 4GB RAM - good for etcd + API server
}

// isValidDNSName checks if a string is a valid DNS name.
// Must be lowercase, alphanumeric with hyphens, start with a letter, max 63 chars.
func isValidDNSName(name string) bool {
	if len(name) == 0 || len(name) > 63 {
		return false
	}
	// Must start with lowercase letter
	if name[0] < 'a' || name[0] > 'z' {
		return false
	}
	// Must end with lowercase letter or digit
	last := name[len(name)-1]
	if (last < 'a' || last > 'z') && (last < '0' || last > '9') {
		return false
	}
	// Must contain only lowercase letters, digits, and hyphens
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	// Must not have consecutive hyphens
	if strings.Contains(name, "--") {
		return false
	}
	return true
}

// isValidDomain checks if a string is a valid domain name.
func isValidDomain(domain string) bool {
	if len(domain) == 0 || len(domain) > 253 {
		return false
	}
	return domainRegex.MatchString(domain)
}
