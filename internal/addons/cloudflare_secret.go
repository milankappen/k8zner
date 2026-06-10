package addons

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/milankappen/k8zner/internal/addons/k8sclient"
	"github.com/milankappen/k8zner/internal/config"
)

const cloudflareSecretName = "cloudflare-api-token" //nolint:gosec // This is a secret name, not a credential

// createCloudflareSecrets creates the Cloudflare API token secret in namespaces
// where it's needed by external-dns and cert-manager.
func createCloudflareSecrets(ctx context.Context, client k8sclient.Client, cfg *config.Config) error {
	apiToken := cfg.Addons.Cloudflare.APIToken
	if apiToken == "" {
		return fmt.Errorf("cloudflare API token is required")
	}

	// Create namespaces first if needed
	if cfg.Addons.ExternalDNS.Enabled {
		if err := ensureNamespace(ctx, client, "external-dns", withBaselinePodSecurity(nil)); err != nil {
			return err
		}

		// Create secret in external-dns namespace
		if err := createCloudflareSecret(ctx, client, "external-dns", apiToken); err != nil {
			return fmt.Errorf("failed to create cloudflare secret in external-dns namespace: %w", err)
		}
	}

	// Create secret in cert-manager namespace if Cloudflare DNS01 is enabled
	if cfg.Addons.CertManager.Enabled && cfg.Addons.CertManager.Cloudflare.Enabled {
		// cert-manager namespace should already exist from cert-manager installation
		if err := createCloudflareSecret(ctx, client, "cert-manager", apiToken); err != nil {
			return fmt.Errorf("failed to create cloudflare secret in cert-manager namespace: %w", err)
		}
	}

	return nil
}

// createCloudflareSecret creates a Cloudflare API token secret in the specified namespace.
func createCloudflareSecret(ctx context.Context, client k8sclient.Client, namespace, apiToken string) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cloudflareSecretName,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"api-token": apiToken,
		},
	}

	if err := client.CreateSecret(ctx, secret); err != nil {
		return fmt.Errorf("failed to create cloudflare secret: %w", err)
	}

	return nil
}
