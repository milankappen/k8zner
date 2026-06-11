package config

// HelmChartConfig defines custom Helm chart configuration for addons.
// This allows overriding the default repository, chart, version, and values.
type HelmChartConfig struct {
	// Repository specifies a custom Helm repository URL.
	Repository string `mapstructure:"repository" yaml:"repository"`

	// Chart specifies a custom chart name.
	Chart string `mapstructure:"chart" yaml:"chart"`

	// Version specifies a custom chart version.
	Version string `mapstructure:"version" yaml:"version"`

	// Values specifies custom Helm values to merge with defaults.
	Values map[string]any `mapstructure:"values" yaml:"values"`
}

// AddonsConfig defines the addon-related configuration.
type AddonsConfig struct {
	Cilium                 CiliumConfig                 `mapstructure:"cilium" yaml:"cilium"`
	CCM                    CCMConfig                    `mapstructure:"ccm" yaml:"ccm"`
	CSI                    CSIConfig                    `mapstructure:"csi" yaml:"csi"`
	MetricsServer          MetricsServerConfig          `mapstructure:"metrics_server" yaml:"metrics_server"`
	CertManager            CertManagerConfig            `mapstructure:"cert_manager" yaml:"cert_manager"`
	Traefik                TraefikConfig                `mapstructure:"traefik" yaml:"traefik"`
	ArgoCD                 ArgoCDConfig                 `mapstructure:"argocd" yaml:"argocd"`
	TalosBackup            TalosBackupConfig            `mapstructure:"talos_backup" yaml:"talos_backup"`
	GatewayAPICRDs         GatewayAPICRDsConfig         `mapstructure:"gateway_api_crds" yaml:"gateway_api_crds"`
	PrometheusOperatorCRDs PrometheusOperatorCRDsConfig `mapstructure:"prometheus_operator_crds" yaml:"prometheus_operator_crds"`
	KubePrometheusStack    KubePrometheusStackConfig    `mapstructure:"kube_prometheus_stack" yaml:"kube_prometheus_stack"`
	Logging                LoggingConfig                `mapstructure:"logging" yaml:"logging"`
	TalosCCM               TalosCCMConfig               `mapstructure:"talos_ccm" yaml:"talos_ccm"`
	Cloudflare             CloudflareConfig             `mapstructure:"cloudflare" yaml:"cloudflare"`
	ExternalDNS            ExternalDNSConfig            `mapstructure:"external_dns" yaml:"external_dns"`
	Operator               OperatorConfig               `mapstructure:"operator" yaml:"operator"`
}

// CCMConfig defines the Hetzner Cloud Controller Manager configuration.
type CCMConfig struct {
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Helm allows customizing the Helm chart repository, version, and values.
	Helm HelmChartConfig `mapstructure:"helm" yaml:"helm"`

	// LoadBalancers configures the CCM Load Balancer controller.
	// These settings control the default behavior for CCM-managed load balancers.
	LoadBalancers CCMLoadBalancerConfig `mapstructure:"load_balancers" yaml:"load_balancers"`

	// NetworkRoutesEnabled enables or disables the CCM Route Controller.
	// When enabled, CCM manages routes for pod networking.
	// Default: true
	NetworkRoutesEnabled *bool `mapstructure:"network_routes_enabled" yaml:"network_routes_enabled"`
}

// CCMLoadBalancerConfig defines the CCM Load Balancer controller configuration.
type CCMLoadBalancerConfig struct {
	// Enabled enables or disables the CCM Service Controller (load balancer management).
	// Default: true
	Enabled *bool `mapstructure:"enabled" yaml:"enabled"`

	// Location sets the default Hetzner location for CCM-managed Load Balancers.
	// If not set, uses the cluster's default location.
	Location string `mapstructure:"location" yaml:"location"`

	// Type sets the default Load Balancer type (e.g., lb11, lb21, lb31).
	// Default: "lb11"
	Type string `mapstructure:"type" yaml:"type"`

	// Algorithm sets the default load balancing algorithm.
	// Valid values: "round_robin", "least_connections"
	// Default: "least_connections"
	Algorithm string `mapstructure:"algorithm" yaml:"algorithm"`

	// UsePrivateIP configures Load Balancer server targets to use the private IP by default.
	// Default: true
	UsePrivateIP *bool `mapstructure:"use_private_ip" yaml:"use_private_ip"`

	// DisablePrivateIngress disables the use of the private network for ingress by default.
	// Default: true
	DisablePrivateIngress *bool `mapstructure:"disable_private_ingress" yaml:"disable_private_ingress"`

	// DisablePublicNetwork disables the public interface of CCM-managed Load Balancers by default.
	// Default: false
	DisablePublicNetwork *bool `mapstructure:"disable_public_network" yaml:"disable_public_network"`

	// DisableIPv6 disables the use of IPv6 for Load Balancers by default.
	// Default: false
	DisableIPv6 *bool `mapstructure:"disable_ipv6" yaml:"disable_ipv6"`

	// UsesProxyProtocol enables the PROXY protocol for CCM-managed Load Balancers by default.
	// Default: false
	UsesProxyProtocol *bool `mapstructure:"uses_proxy_protocol" yaml:"uses_proxy_protocol"`

	// HealthCheck configures default health check settings for load balancers.
	HealthCheck CCMHealthCheckConfig `mapstructure:"health_check" yaml:"health_check"`
}

// CCMHealthCheckConfig defines health check settings for CCM-managed load balancers.
type CCMHealthCheckConfig struct {
	// Interval is the time interval in seconds between health checks.
	// Valid range: 3-60 seconds
	// Default: 3
	Interval int `mapstructure:"interval" yaml:"interval"`

	// Timeout is the time in seconds after which a health check is considered failed.
	// Valid range: 1-60 seconds
	// Default: 3
	Timeout int `mapstructure:"timeout" yaml:"timeout"`

	// Retries is the number of unsuccessful retries before a target is considered unhealthy.
	// Valid range: 1-10
	// Default: 3
	Retries int `mapstructure:"retries" yaml:"retries"`
}

// CSIConfig defines the Hetzner Cloud CSI driver configuration.
type CSIConfig struct {
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Helm allows customizing the Helm chart repository, version, and values.
	Helm HelmChartConfig `mapstructure:"helm" yaml:"helm"`

	DefaultStorageClass  bool           `mapstructure:"default_storage_class" yaml:"default_storage_class"`
	EncryptionPassphrase string         `mapstructure:"encryption_passphrase" yaml:"encryption_passphrase"`
	StorageClasses       []StorageClass `mapstructure:"storage_classes" yaml:"storage_classes"`

	// VolumeExtraLabels specifies additional labels to apply to Hetzner volumes.
	VolumeExtraLabels map[string]string `mapstructure:"volume_extra_labels" yaml:"volume_extra_labels"`
}

// MetricsServerConfig defines the Kubernetes Metrics Server configuration.
type MetricsServerConfig struct {
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Helm allows customizing the Helm chart repository, version, and values.
	Helm HelmChartConfig `mapstructure:"helm" yaml:"helm"`

	// ScheduleOnControlPlane determines whether to schedule the Metrics Server on control plane nodes.
	// If nil, defaults to true when there are no worker nodes.
	ScheduleOnControlPlane *bool `mapstructure:"schedule_on_control_plane" yaml:"schedule_on_control_plane"`

	// Replicas specifies the number of replicas for the Metrics Server.
	// If nil, auto-calculated: 2 for clusters with >1 schedulable nodes, 1 otherwise.
	Replicas *int `mapstructure:"replicas" yaml:"replicas"`
}

// CertManagerConfig defines the cert-manager configuration.
type CertManagerConfig struct {
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Helm allows customizing the Helm chart repository, version, and values.
	Helm HelmChartConfig `mapstructure:"helm" yaml:"helm"`

	// Cloudflare configures Cloudflare DNS01 solver for cert-manager.
	Cloudflare CertManagerCloudflareConfig `mapstructure:"cloudflare" yaml:"cloudflare"`
}

// CertManagerCloudflareConfig extends cert-manager with Cloudflare DNS01 solver.
type CertManagerCloudflareConfig struct {
	// Enabled creates ClusterIssuers using Cloudflare DNS01 solver.
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Email for Let's Encrypt account registration.
	Email string `mapstructure:"email" yaml:"email"`

	// Production uses Let's Encrypt production server (default: false = staging).
	// Set to true only after testing with staging to avoid rate limits.
	Production bool `mapstructure:"production" yaml:"production"`

	// WildcardCertificate additionally issues a "*.{domain}" certificate and
	// sets it as the Traefik default, so ingresses without an explicit TLS
	// secret are covered without per-host certificates.
	WildcardCertificate bool `mapstructure:"wildcard_certificate" yaml:"wildcard_certificate"`
}

// LoggingDefaultRetention is how long Loki keeps logs unless overridden.
// Both expansion paths (CLI spec and operator CRD) and the addon installer
// reference this single constant.
const LoggingDefaultRetention = "168h"

// LoggingConfig defines the Loki + Alloy log aggregation stack.
// Loki runs as a single binary with persistent filesystem storage; Alloy
// tails pod logs via the Kubernetes API and pushes them to Loki.
type LoggingConfig struct {
	// Enabled installs the logging stack.
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Retention is how long Loki keeps logs (default: 168h).
	Retention string `mapstructure:"retention" yaml:"retention"`

	// LokiHelm customizes the Loki chart.
	LokiHelm HelmChartConfig `mapstructure:"loki_helm" yaml:"loki_helm"`

	// AlloyHelm customizes the Alloy chart.
	AlloyHelm HelmChartConfig `mapstructure:"alloy_helm" yaml:"alloy_helm"`
}

// TraefikConfig defines the Traefik Proxy ingress controller configuration.
// Traefik provides the cluster's ingress with automatic service discovery
// and a LoadBalancer service managed by CCM.
type TraefikConfig struct {
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Helm allows customizing the Helm chart repository, version, and values.
	Helm HelmChartConfig `mapstructure:"helm" yaml:"helm"`

	// Replicas specifies the number of controller replicas.
	// If nil, auto-calculated: 2 for <3 workers, 3 for >=3 workers.
	Replicas *int `mapstructure:"replicas" yaml:"replicas"`

	// ExternalTrafficPolicy controls how external traffic is routed.
	// Valid values: "Cluster" (cluster-wide) or "Local" (node-local).
	// Default: "Cluster"
	ExternalTrafficPolicy string `mapstructure:"external_traffic_policy" yaml:"external_traffic_policy"`

	// IngressClass specifies the IngressClass name for Traefik.
	// Default: "traefik"
	IngressClass string `mapstructure:"ingress_class" yaml:"ingress_class"`
}

// ArgoCDConfig defines the ArgoCD GitOps configuration.
// ArgoCD is a declarative, GitOps continuous delivery tool for Kubernetes.
// See: https://argo-cd.readthedocs.io/
type ArgoCDConfig struct {
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Helm allows customizing the Helm chart repository, version, and values.
	Helm HelmChartConfig `mapstructure:"helm" yaml:"helm"`

	// HA enables high availability mode with multiple replicas.
	// When enabled, ArgoCD components run with multiple replicas for fault tolerance.
	// Default: false
	HA bool `mapstructure:"ha" yaml:"ha"`

	// ServerReplicas sets the number of ArgoCD server replicas.
	// Only used when HA is enabled. Default: 2
	ServerReplicas *int `mapstructure:"server_replicas" yaml:"server_replicas"`

	// ControllerReplicas sets the number of application controller replicas.
	// Only used when HA is enabled. Default: 1
	ControllerReplicas *int `mapstructure:"controller_replicas" yaml:"controller_replicas"`

	// RepoServerReplicas sets the number of repo server replicas.
	// Only used when HA is enabled. Default: 2
	RepoServerReplicas *int `mapstructure:"repo_server_replicas" yaml:"repo_server_replicas"`

	// IngressEnabled enables Ingress for the ArgoCD server.
	// Requires Traefik ingress controller to be installed.
	IngressEnabled bool `mapstructure:"ingress_enabled" yaml:"ingress_enabled"`

	// IngressHost is the hostname for the ArgoCD server Ingress.
	// Required when IngressEnabled is true.
	IngressHost string `mapstructure:"ingress_host" yaml:"ingress_host"`

	// IngressClassName specifies the IngressClass to use.
	// Default: "traefik"
	IngressClassName string `mapstructure:"ingress_class_name" yaml:"ingress_class_name"`

	// IngressTLS enables TLS for the Ingress.
	// When enabled with cert-manager, certificates are automatically provisioned.
	IngressTLS bool `mapstructure:"ingress_tls" yaml:"ingress_tls"`
}

// StorageClass defines a Kubernetes StorageClass for CSI.
type StorageClass struct {
	Name                string            `mapstructure:"name" yaml:"name"`
	Encrypted           bool              `mapstructure:"encrypted" yaml:"encrypted"`
	ReclaimPolicy       string            `mapstructure:"reclaim_policy" yaml:"reclaim_policy"`
	DefaultStorageClass bool              `mapstructure:"default_storage_class" yaml:"default_storage_class"`
	ExtraParameters     map[string]string `mapstructure:"extra_parameters" yaml:"extra_parameters"`
}

// CiliumConfig defines the Cilium CNI configuration.
type CiliumConfig struct {
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Helm allows customizing the Helm chart repository, version, and values.
	Helm HelmChartConfig `mapstructure:"helm" yaml:"helm"`

	// Encryption
	EncryptionEnabled bool   `mapstructure:"encryption_enabled" yaml:"encryption_enabled"`
	EncryptionType    string `mapstructure:"encryption_type" yaml:"encryption_type"` // wireguard, ipsec

	// IPSec specific
	IPSecAlgorithm string `mapstructure:"ipsec_algorithm" yaml:"ipsec_algorithm"`
	IPSecKeySize   int    `mapstructure:"ipsec_key_size" yaml:"ipsec_key_size"`
	IPSecKeyID     int    `mapstructure:"ipsec_key_id" yaml:"ipsec_key_id"`

	// Routing
	RoutingMode string `mapstructure:"routing_mode" yaml:"routing_mode"` // native, tunnel

	// BPF Configuration
	// BPFDatapathMode sets the mode for Pod devices. Valid values: veth, netkit, netkit-l2.
	// Warning: Netkit is still in beta and should not be used with IPsec encryption.
	// Default: "veth"
	BPFDatapathMode string `mapstructure:"bpf_datapath_mode" yaml:"bpf_datapath_mode"`

	// PolicyCIDRMatchMode allows cluster nodes to be selected by CIDR network policies.
	// Set to "nodes" to enable targeting the kube-api server with k8s NetworkPolicy.
	// Default: "" (disabled)
	PolicyCIDRMatchMode string `mapstructure:"policy_cidr_match_mode" yaml:"policy_cidr_match_mode"`

	// SocketLBHostNamespaceOnly limits Cilium's socket-level load-balancing to the host namespace only.
	// Default: false
	SocketLBHostNamespaceOnly bool `mapstructure:"socket_lb_host_namespace_only" yaml:"socket_lb_host_namespace_only"`

	// KubeProxy Replacement
	KubeProxyReplacementEnabled bool `mapstructure:"kube_proxy_replacement_enabled" yaml:"kube_proxy_replacement_enabled"`

	// Gateway API
	GatewayAPIEnabled bool `mapstructure:"gateway_api_enabled" yaml:"gateway_api_enabled"`

	// GatewayAPIProxyProtocolEnabled enables PROXY Protocol on Cilium Gateway API for external LB traffic.
	// Default: true
	GatewayAPIProxyProtocolEnabled *bool `mapstructure:"gateway_api_proxy_protocol_enabled" yaml:"gateway_api_proxy_protocol_enabled"`

	// GatewayAPIExternalTrafficPolicy controls traffic routing for Gateway API services.
	// Valid values: "Cluster" (cluster-wide) or "Local" (node-local endpoints).
	// Default: "Cluster"
	GatewayAPIExternalTrafficPolicy string `mapstructure:"gateway_api_external_traffic_policy" yaml:"gateway_api_external_traffic_policy"`

	// Egress Gateway
	EgressGatewayEnabled bool `mapstructure:"egress_gateway_enabled" yaml:"egress_gateway_enabled"`

	// Hubble Observability
	HubbleEnabled      bool `mapstructure:"hubble_enabled" yaml:"hubble_enabled"`
	HubbleRelayEnabled bool `mapstructure:"hubble_relay_enabled" yaml:"hubble_relay_enabled"`
	HubbleUIEnabled    bool `mapstructure:"hubble_ui_enabled" yaml:"hubble_ui_enabled"`

	// Prometheus Integration
	// ServiceMonitorEnabled enables Prometheus ServiceMonitor resources.
	// Requires Prometheus Operator CRDs to be installed.
	// Default: false
	ServiceMonitorEnabled bool `mapstructure:"service_monitor_enabled" yaml:"service_monitor_enabled"`
}

// TalosBackupConfig defines the Talos etcd backup configuration.
type TalosBackupConfig struct {
	Enabled            bool   `mapstructure:"enabled" yaml:"enabled"`
	Version            string `mapstructure:"version" yaml:"version"`
	Schedule           string `mapstructure:"schedule" yaml:"schedule"`
	S3Bucket           string `mapstructure:"s3_bucket" yaml:"s3_bucket"`
	S3Region           string `mapstructure:"s3_region" yaml:"s3_region"`
	S3Endpoint         string `mapstructure:"s3_endpoint" yaml:"s3_endpoint"`
	S3Prefix           string `mapstructure:"s3_prefix" yaml:"s3_prefix"`
	S3AccessKey        string `mapstructure:"s3_access_key" yaml:"s3_access_key"`
	S3SecretKey        string `mapstructure:"s3_secret_key" yaml:"s3_secret_key"`
	S3PathStyle        bool   `mapstructure:"s3_path_style" yaml:"s3_path_style"`
	AGEX25519PublicKey string `mapstructure:"age_x25519_public_key" yaml:"age_x25519_public_key"`
	EnableCompression  bool   `mapstructure:"enable_compression" yaml:"enable_compression"`

	// EncryptionDisabled disables backup encryption when set to true.
	// Default: false (encryption enabled). When enabled, backups are encrypted using age encryption.
	// Warning: Disabling encryption stores etcd data unencrypted in the S3 bucket.
	EncryptionDisabled bool `mapstructure:"encryption_disabled" yaml:"encryption_disabled,omitempty"`

	// S3HcloudURL is a convenience field for Hetzner Object Storage.
	// Format: bucket.region.your-objectstorage.com or https://bucket.region.your-objectstorage.com
	// When set, automatically extracts S3Bucket, S3Region, and S3Endpoint.
	S3HcloudURL string `mapstructure:"s3_hcloud_url" yaml:"s3_hcloud_url"`
}

// GatewayAPICRDsConfig defines the Gateway API CRDs configuration.
type GatewayAPICRDsConfig struct {
	// Enabled enables the Gateway API CRDs deployment.
	// Default: true
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Version specifies the Gateway API CRDs version.
	// Default: "v1.4.1"
	Version string `mapstructure:"version" yaml:"version"`

	// ReleaseChannel specifies the release channel (standard or experimental).
	// Default: "standard"
	ReleaseChannel string `mapstructure:"release_channel" yaml:"release_channel"`
}

// PrometheusOperatorCRDsConfig defines the Prometheus Operator CRDs configuration.
type PrometheusOperatorCRDsConfig struct {
	// Enabled enables the Prometheus Operator CRDs deployment.
	// Default: true
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Version specifies the Prometheus Operator CRDs version.
	// Default: "v0.87.1"
	Version string `mapstructure:"version" yaml:"version"`
}

// KubePrometheusStackConfig defines the kube-prometheus-stack configuration.
// This deploys a full monitoring stack including Prometheus, Grafana, and Alertmanager.
type KubePrometheusStackConfig struct {
	// Enabled enables the kube-prometheus-stack deployment.
	// Default: false
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Helm allows customizing the Helm chart repository, version, and values.
	Helm HelmChartConfig `mapstructure:"helm" yaml:"helm"`

	// Grafana configuration
	Grafana KubePrometheusGrafanaConfig `mapstructure:"grafana" yaml:"grafana"`

	// Prometheus configuration
	Prometheus KubePrometheusPrometheusConfig `mapstructure:"prometheus" yaml:"prometheus"`

	// Alertmanager configuration
	Alertmanager KubePrometheusAlertmanagerConfig `mapstructure:"alertmanager" yaml:"alertmanager"`

	// DefaultRules enables the default PrometheusRule resources.
	// Default: true
	DefaultRules *bool `mapstructure:"default_rules" yaml:"default_rules"`

	// NodeExporter enables the node-exporter DaemonSet.
	// Default: true
	NodeExporter *bool `mapstructure:"node_exporter" yaml:"node_exporter"`

	// KubeStateMetrics enables the kube-state-metrics deployment.
	// Default: true
	KubeStateMetrics *bool `mapstructure:"kube_state_metrics" yaml:"kube_state_metrics"`
}

// KubePrometheusGrafanaConfig defines Grafana configuration.
type KubePrometheusGrafanaConfig struct {
	// Enabled enables Grafana deployment.
	// Default: true
	Enabled *bool `mapstructure:"enabled" yaml:"enabled"`

	// AdminPassword sets the initial Grafana admin password.
	// If empty, a random password is generated.
	AdminPassword string `mapstructure:"admin_password" yaml:"admin_password"`

	// IngressEnabled enables Ingress for Grafana.
	IngressEnabled bool `mapstructure:"ingress_enabled" yaml:"ingress_enabled"`

	// IngressHost is the hostname for the Grafana Ingress.
	// Required when IngressEnabled is true.
	IngressHost string `mapstructure:"ingress_host" yaml:"ingress_host"`

	// IngressClassName specifies the IngressClass to use.
	// Default: "traefik"
	IngressClassName string `mapstructure:"ingress_class_name" yaml:"ingress_class_name"`

	// IngressTLS enables TLS for the Ingress with cert-manager.
	// Requires cert-manager to be installed.
	IngressTLS bool `mapstructure:"ingress_tls" yaml:"ingress_tls"`

	// Persistence enables persistent storage for Grafana.
	Persistence KubePrometheusPersistenceConfig `mapstructure:"persistence" yaml:"persistence"`
}

// KubePrometheusPrometheusConfig defines Prometheus server configuration.
type KubePrometheusPrometheusConfig struct {
	// Enabled enables Prometheus deployment.
	// Default: true
	Enabled *bool `mapstructure:"enabled" yaml:"enabled"`

	// RetentionDays specifies how many days to retain Prometheus data.
	// Default: 15
	RetentionDays *int `mapstructure:"retention_days" yaml:"retention_days"`

	// IngressEnabled enables Ingress for Prometheus.
	IngressEnabled bool `mapstructure:"ingress_enabled" yaml:"ingress_enabled"`

	// IngressHost is the hostname for the Prometheus Ingress.
	IngressHost string `mapstructure:"ingress_host" yaml:"ingress_host"`

	// IngressClassName specifies the IngressClass to use.
	IngressClassName string `mapstructure:"ingress_class_name" yaml:"ingress_class_name"`

	// IngressTLS enables TLS for the Ingress.
	IngressTLS bool `mapstructure:"ingress_tls" yaml:"ingress_tls"`

	// Persistence enables persistent storage for Prometheus.
	Persistence KubePrometheusPersistenceConfig `mapstructure:"persistence" yaml:"persistence"`

	// Resources defines resource requests and limits.
	Resources KubePrometheusResourcesConfig `mapstructure:"resources" yaml:"resources"`
}

// KubePrometheusAlertmanagerConfig defines Alertmanager configuration.
type KubePrometheusAlertmanagerConfig struct {
	// Enabled enables Alertmanager deployment.
	// Default: true
	Enabled *bool `mapstructure:"enabled" yaml:"enabled"`
}

// KubePrometheusPersistenceConfig defines storage persistence settings.
type KubePrometheusPersistenceConfig struct {
	// Enabled enables persistent storage.
	// Default: false
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Size specifies the volume size (e.g., "10Gi", "50Gi").
	// Default: "10Gi"
	Size string `mapstructure:"size" yaml:"size"`

	// StorageClass specifies the StorageClass to use.
	// If empty, uses the default StorageClass.
	StorageClass string `mapstructure:"storage_class" yaml:"storage_class"`
}

// KubePrometheusResourcesConfig defines resource requests and limits.
type KubePrometheusResourcesConfig struct {
	// Requests defines resource requests.
	Requests KubePrometheusResourceSpec `mapstructure:"requests" yaml:"requests"`

	// Limits defines resource limits.
	Limits KubePrometheusResourceSpec `mapstructure:"limits" yaml:"limits"`
}

// KubePrometheusResourceSpec defines CPU and memory specifications.
type KubePrometheusResourceSpec struct {
	CPU    string `mapstructure:"cpu" yaml:"cpu"`
	Memory string `mapstructure:"memory" yaml:"memory"`
}

// TalosCCMConfig defines the Talos Cloud Controller Manager configuration.
// This is separate from the Hetzner CCM - it's the Siderolabs Talos CCM.
type TalosCCMConfig struct {
	// Enabled enables the Talos CCM deployment.
	// Default: true
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Version specifies the Talos CCM version.
	// Default: "v1.11.0"
	Version string `mapstructure:"version" yaml:"version"`
}

// CloudflareConfig defines Cloudflare DNS integration settings.
// This is shared by external-dns and cert-manager for DNS management.
type CloudflareConfig struct {
	// Enabled enables Cloudflare DNS integration.
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// APIToken is the Cloudflare API token.
	// Can also be set via CF_API_TOKEN env var (recommended for security).
	// Required permissions: Zone:Zone:Read, Zone:DNS:Edit
	APIToken string `mapstructure:"api_token" yaml:"api_token"`

	// Domain is the base domain for DNS records (e.g., k8zner.org).
	// Can also be set via CF_DOMAIN env var.
	Domain string `mapstructure:"domain" yaml:"domain"`

	// ZoneID is optional - if not set, external-dns will auto-detect from domain.
	ZoneID string `mapstructure:"zone_id" yaml:"zone_id"`

	// Proxied enables Cloudflare proxy (orange cloud) by default for DNS records.
	// Default: false (DNS only, no proxy)
	Proxied bool `mapstructure:"proxied" yaml:"proxied"`
}

// ExternalDNSConfig defines the external-dns addon configuration.
// External-dns automatically creates DNS records from Ingress annotations.
type ExternalDNSConfig struct {
	// Enabled enables the external-dns addon.
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Helm allows customizing the Helm chart repository, version, and values.
	Helm HelmChartConfig `mapstructure:"helm" yaml:"helm"`

	// TXTOwnerID identifies this cluster's DNS records for conflict prevention.
	// Default: cluster_name
	TXTOwnerID string `mapstructure:"txt_owner_id" yaml:"txt_owner_id"`

	// Policy controls DNS record deletion behavior.
	// "sync" - deletes records when resources are removed (default)
	// "upsert-only" - never deletes records, only creates/updates
	Policy string `mapstructure:"policy" yaml:"policy"`

	// Sources specifies which Kubernetes resources to watch for DNS records.
	// Default: ["ingress"]
	// Options: ingress, service, gateway-httproute, etc.
	Sources []string `mapstructure:"sources" yaml:"sources"`
}

// OperatorConfig defines the k8zner-operator addon configuration.
// The operator provides self-healing functionality for the cluster.
type OperatorConfig struct {
	// Enabled enables the k8zner-operator for self-healing.
	// When enabled, the operator monitors node health and automatically
	// replaces failed nodes.
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Version specifies the operator image version.
	// Default: "main" (latest from main branch)
	Version string `mapstructure:"version" yaml:"version"`

	// HostNetwork enables hostNetwork mode for the operator pod.
	// Auto-enabled during CLI bootstrap (before CNI is installed).
	// Not needed for normal operation — the operator runs after CNI is ready.
	HostNetwork bool `mapstructure:"host_network" yaml:"host_network"`
}
