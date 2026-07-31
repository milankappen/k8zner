package handlers

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	k8znerv1alpha1 "github.com/milankappen/k8zner/api/v1alpha1"
	"github.com/milankappen/k8zner/internal/config"
	hcloudInternal "github.com/milankappen/k8zner/internal/platform/hcloud"
	"github.com/milankappen/k8zner/internal/provisioning"
	"github.com/milankappen/k8zner/internal/util/naming"
)

// createClusterCRD creates the K8znerCluster CRD and credentials Secret.
func createClusterCRD(ctx context.Context, cfg *config.Config, pCtx *provisioning.Context, infraInfo *InfrastructureInfo, kubeconfig []byte, hcloudToken string) error {
	kubecfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	scheme := k8znerv1alpha1.Scheme
	k8sClient, err := client.New(kubecfg, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	if err := ensureNamespace(ctx, k8sClient); err != nil {
		return fmt.Errorf("failed to ensure namespace: %w", err)
	}

	if err := createCredentialsSecret(ctx, k8sClient, cfg, hcloudToken); err != nil {
		return fmt.Errorf("failed to create credentials secret: %w", err)
	}

	if backupSecret := createBackupS3Secret(cfg, cfg.ClusterName); backupSecret != nil {
		if err := k8sClient.Create(ctx, backupSecret); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create backup S3 secret: %w", err)
		}
		log.Printf("Created backup S3 secret: %s", backupSecret.Name)
	}

	bootstrapName, bootstrapID, bootstrapIP := getBootstrapNode(pCtx)
	k8znerCluster := buildK8znerCluster(cfg, infraInfo, bootstrapName, bootstrapID, bootstrapIP)
	if err := k8sClient.Create(ctx, k8znerCluster); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create K8znerCluster: %w", err)
	}

	return updateClusterStatus(ctx, k8sClient, k8znerCluster)
}

// ensureNamespace creates the k8zner-system namespace if it doesn't exist.
func ensureNamespace(ctx context.Context, k8sClient client.Client) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: k8znerNamespace,
		},
	}
	if err := k8sClient.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create namespace: %w", err)
	}
	return nil
}

// createCredentialsSecret creates the Secret containing Hetzner and Talos credentials.
func createCredentialsSecret(ctx context.Context, k8sClient client.Client, cfg *config.Config, hcloudToken string) error {
	secretsData, err := os.ReadFile(secretsFile)
	if err != nil {
		return fmt.Errorf("failed to read secrets.yaml: %w", err)
	}
	talosConfigData, err := os.ReadFile(talosConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read talosconfig: %w", err)
	}

	credSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      credentialsSecretName,
			Namespace: k8znerNamespace,
			Labels: map[string]string{
				"cluster": cfg.ClusterName,
			},
		},
		Data: map[string][]byte{
			k8znerv1alpha1.CredentialsKeyHCloudToken:  []byte(hcloudToken),
			k8znerv1alpha1.CredentialsKeyTalosSecrets: secretsData,
			k8znerv1alpha1.CredentialsKeyTalosConfig:  talosConfigData,
		},
	}
	if cfg.Addons.Cloudflare.APIToken != "" {
		credSecret.Data[k8znerv1alpha1.CredentialsKeyCloudflareAPIToken] = []byte(cfg.Addons.Cloudflare.APIToken)
	}
	if err := k8sClient.Create(ctx, credSecret); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create credentials secret: %w", err)
	}
	return nil
}

// updateClusterStatus updates the K8znerCluster status subresource after creation.
func updateClusterStatus(ctx context.Context, k8sClient client.Client, k8znerCluster *k8znerv1alpha1.K8znerCluster) error {
	createdCluster := &k8znerv1alpha1.K8znerCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: k8znerCluster.Name, Namespace: k8znerCluster.Namespace}, createdCluster); err != nil {
		return fmt.Errorf("failed to get created K8znerCluster: %w", err)
	}

	createdCluster.Status = k8znerCluster.Status
	if err := k8sClient.Status().Update(ctx, createdCluster); err != nil {
		return fmt.Errorf("failed to update K8znerCluster status: %w", err)
	}

	return nil
}

// buildK8znerCluster builds the K8znerCluster CRD from config.
func buildK8znerCluster(cfg *config.Config, infraInfo *InfrastructureInfo, bootstrapName string, bootstrapID int64, bootstrapIP string) *k8znerv1alpha1.K8znerCluster {
	now := metav1.Now()

	k8znerCluster := &k8znerv1alpha1.K8znerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.ClusterName,
			Namespace: k8znerNamespace,
			Labels: map[string]string{
				"cluster": cfg.ClusterName,
			},
		},
		Spec:   buildClusterSpec(cfg, infraInfo, bootstrapName, bootstrapID, bootstrapIP, &now),
		Status: buildClusterStatus(cfg, infraInfo, bootstrapName, bootstrapID, bootstrapIP),
	}

	k8znerCluster.Spec.PlacementGroup = &k8znerv1alpha1.PlacementGroupSpec{
		Enabled: true,
		Type:    "spread",
	}

	return k8znerCluster
}

// buildClusterSpec creates the K8znerClusterSpec from config and infrastructure info.
func buildClusterSpec(cfg *config.Config, infraInfo *InfrastructureInfo, bootstrapName string, bootstrapID int64, bootstrapIP string, now *metav1.Time) k8znerv1alpha1.K8znerClusterSpec {
	return k8znerv1alpha1.K8znerClusterSpec{
		Region: cfg.Location,
		Domain: cfg.Addons.Cloudflare.Domain,
		ControlPlanes: k8znerv1alpha1.ControlPlaneSpec{
			Count: cfg.ControlPlane.NodePools[0].Count,
			Size:  cfg.ControlPlane.NodePools[0].ServerType,
		},
		Workers: k8znerv1alpha1.WorkerSpec{
			Count: cfg.WorkerCount(),
			Size:  getWorkerSize(cfg),
		},
		Network: k8znerv1alpha1.NetworkSpec{
			IPv4CIDR:     cfg.Network.IPv4CIDR,
			NodeIPv4CIDR: cfg.Network.NodeIPv4CIDR,
			PodCIDR:      cfg.Network.PodIPv4CIDR,
			ServiceCIDR:  cfg.Network.ServiceIPv4CIDR,
		},
		Firewall: k8znerv1alpha1.FirewallSpec{
			Enabled: true,
		},
		Kubernetes: k8znerv1alpha1.KubernetesSpec{
			Version: cfg.Kubernetes.Version,
		},
		Talos: k8znerv1alpha1.TalosSpec{
			Version:     cfg.Talos.Version,
			SchematicID: cfg.Talos.SchematicID,
			Extensions:  cfg.Talos.Extensions,
		},
		CredentialsRef: corev1.LocalObjectReference{
			Name: credentialsSecretName,
		},
		Bootstrap: &k8znerv1alpha1.BootstrapState{
			Completed:       true,
			CompletedAt:     now,
			BootstrapNode:   bootstrapName,
			BootstrapNodeID: bootstrapID,
			PublicIP:        bootstrapIP,
		},
		Addons: buildAddonSpec(cfg),
		Backup: buildBackupSpec(cfg, cfg.ClusterName),
	}
}

// buildClusterStatus creates the initial K8znerClusterStatus for a bootstrapped cluster.
func buildClusterStatus(cfg *config.Config, infraInfo *InfrastructureInfo, bootstrapName string, bootstrapID int64, bootstrapIP string) k8znerv1alpha1.K8znerClusterStatus {
	return k8znerv1alpha1.K8znerClusterStatus{
		Phase:             k8znerv1alpha1.ClusterPhaseProvisioning,
		ProvisioningPhase: k8znerv1alpha1.PhaseCNI,
		ControlPlanes: k8znerv1alpha1.NodeGroupStatus{
			Desired: cfg.ControlPlane.NodePools[0].Count,
			Ready:   1,
			Nodes: []k8znerv1alpha1.NodeStatus{
				{
					Name:     bootstrapName,
					ServerID: bootstrapID,
					PublicIP: bootstrapIP,
					Healthy:  true,
				},
			},
		},
		Workers: k8znerv1alpha1.NodeGroupStatus{
			Desired: cfg.WorkerCount(),
		},
		Infrastructure: k8znerv1alpha1.InfrastructureStatus{
			NetworkID:             infraInfo.NetworkID,
			FirewallID:            infraInfo.FirewallID,
			LoadBalancerID:        infraInfo.LoadBalancerID,
			LoadBalancerIP:        infraInfo.LoadBalancerIP,
			LoadBalancerPrivateIP: infraInfo.LoadBalancerPrivateIP,
			SSHKeyID:              infraInfo.SSHKeyID,
		},
		ControlPlaneEndpoint: infraInfo.LoadBalancerIP,
	}
}

// buildAddonSpec creates the addon spec from config.
func buildAddonSpec(cfg *config.Config) *k8znerv1alpha1.AddonSpec {
	spec := &k8znerv1alpha1.AddonSpec{
		Traefik:             cfg.Addons.Traefik.Enabled,
		CertManager:         cfg.Addons.CertManager.Enabled,
		ExternalDNS:         cfg.Addons.ExternalDNS.Enabled,
		ArgoCD:              cfg.Addons.ArgoCD.Enabled,
		MetricsServer:       cfg.Addons.MetricsServer.Enabled,
		Monitoring:          cfg.Addons.KubePrometheusStack.Enabled,
		Logging:             cfg.Addons.Logging.Enabled,
		WildcardCertificate: cfg.Addons.CertManager.Cloudflare.WildcardCertificate,
	}

	domain := cfg.Addons.Cloudflare.Domain
	if domain != "" {
		suffix := "." + domain
		if host := cfg.Addons.ArgoCD.IngressHost; host != "" && strings.HasSuffix(host, suffix) {
			sub := strings.TrimSuffix(host, suffix)
			if sub != "argo" {
				spec.ArgoSubdomain = sub
			}
		}
		if host := cfg.Addons.KubePrometheusStack.Grafana.IngressHost; host != "" && strings.HasSuffix(host, suffix) {
			sub := strings.TrimSuffix(host, suffix)
			if sub != "grafana" {
				spec.GrafanaSubdomain = sub
			}
		}
	}

	return spec
}

// buildBackupSpec creates the backup spec from config.
func buildBackupSpec(cfg *config.Config, clusterName string) *k8znerv1alpha1.BackupSpec {
	if !cfg.Addons.TalosBackup.Enabled {
		return nil
	}
	if cfg.Addons.TalosBackup.S3AccessKey == "" || cfg.Addons.TalosBackup.S3SecretKey == "" {
		return nil
	}

	return &k8znerv1alpha1.BackupSpec{
		Enabled:   true,
		Schedule:  cfg.Addons.TalosBackup.Schedule,
		Retention: "168h",
		S3SecretRef: &k8znerv1alpha1.SecretReference{
			Name: backupS3SecretName(clusterName),
		},
	}
}

func backupS3SecretName(clusterName string) string {
	return clusterName + "-backup-s3"
}

// createBackupS3Secret creates the Secret containing S3 credentials for backup.
func createBackupS3Secret(cfg *config.Config, clusterName string) *corev1.Secret {
	if !cfg.Addons.TalosBackup.Enabled {
		return nil
	}
	if cfg.Addons.TalosBackup.S3AccessKey == "" || cfg.Addons.TalosBackup.S3SecretKey == "" {
		return nil
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      backupS3SecretName(clusterName),
			Namespace: k8znerNamespace,
			Labels: map[string]string{
				"cluster": clusterName,
				"purpose": "backup",
			},
		},
		StringData: map[string]string{
			"access-key": cfg.Addons.TalosBackup.S3AccessKey,
			"secret-key": cfg.Addons.TalosBackup.S3SecretKey,
			"endpoint":   cfg.Addons.TalosBackup.S3Endpoint,
			"bucket":     cfg.Addons.TalosBackup.S3Bucket,
			"region":     cfg.Addons.TalosBackup.S3Region,
		},
	}
}

// buildInfraInfo gathers infrastructure details from provisioning state and API.
func buildInfraInfo(ctx context.Context, pCtx *provisioning.Context, infraClient hcloudInternal.InfrastructureManager, cfg *config.Config) *InfrastructureInfo {
	lb := pCtx.State.LoadBalancer
	if lb == nil {
		lbName := naming.KubeAPILoadBalancer(cfg.ClusterName)
		var err error
		lb, err = infraClient.GetLoadBalancer(ctx, lbName)
		if err != nil {
			log.Printf("Warning: failed to get load balancer info: %v", err)
		}
	}

	info := &InfrastructureInfo{
		NetworkID:   pCtx.State.Network.ID,
		NetworkName: pCtx.State.Network.Name,
		SSHKeyID:    pCtx.State.SSHKeyID,
	}
	if pCtx.State.Firewall != nil {
		info.FirewallID = pCtx.State.Firewall.ID
		info.FirewallName = pCtx.State.Firewall.Name
	}
	if lb != nil {
		info.LoadBalancerID = lb.ID
		info.LoadBalancerName = lb.Name
		info.LoadBalancerIP = hcloudInternal.LoadBalancerIPv4(lb)
		info.LoadBalancerPrivateIP = hcloudInternal.LoadBalancerPrivateIP(lb)
	}

	return info
}

func getWorkerSize(cfg *config.Config) string {
	if len(cfg.Workers) == 0 {
		return config.DefaultWorkerServerType
	}
	return cfg.Workers[0].ServerType
}

// getBootstrapNode returns the bootstrap node info from the provisioning state.
func getBootstrapNode(pCtx *provisioning.Context) (name string, serverID int64, ip string) {
	if len(pCtx.State.ControlPlaneIPs) == 0 {
		return "", 0, ""
	}

	names := make([]string, 0, len(pCtx.State.ControlPlaneIPs))
	for n := range pCtx.State.ControlPlaneIPs {
		names = append(names, n)
	}
	sort.Strings(names)

	name = names[0]
	ip = pCtx.State.ControlPlaneIPs[name]
	serverID = pCtx.State.ControlPlaneServerIDs[name]
	return name, serverID, ip
}
