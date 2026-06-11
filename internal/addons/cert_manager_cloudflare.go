package addons

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"text/template"
	"time"

	"github.com/milankappen/k8zner/internal/addons/k8sclient"
	"github.com/milankappen/k8zner/internal/config"
)

// defaultStagingEmail is used for staging ClusterIssuer when no email is provided.
// Staging certificates don't require account recovery, so a placeholder is acceptable.
const defaultStagingEmail = "staging@k8zner.local"

// applyCertManagerCloudflare creates ClusterIssuers for Let's Encrypt with Cloudflare DNS01 solver.
func applyCertManagerCloudflare(ctx context.Context, client k8sclient.Client, cfg *config.Config) error {
	cfCfg := cfg.Addons.CertManager.Cloudflare

	// Wait for cert-manager CRDs to be ready before creating ClusterIssuer
	log.Println("Waiting for cert-manager CRDs and webhook to be ready...")
	if err := waitForCertManagerCRDs(ctx, client); err != nil {
		return fmt.Errorf("failed waiting for cert-manager CRDs: %w", err)
	}
	log.Println("cert-manager CRDs and webhook are ready")

	// Determine email for staging - use placeholder if not provided
	stagingEmail := cfCfg.Email
	if stagingEmail == "" {
		stagingEmail = defaultStagingEmail
		log.Printf("No email provided, using placeholder '%s' for staging certificates", stagingEmail)
	}

	// Create staging ClusterIssuer with retry logic
	stagingManifest, err := buildClusterIssuerManifest(stagingEmail, false)
	if err != nil {
		return fmt.Errorf("failed to build staging ClusterIssuer manifest: %w", err)
	}
	if err := applyClusterIssuerWithRetry(ctx, client, "letsencrypt-cloudflare-staging", stagingManifest); err != nil {
		return fmt.Errorf("failed to apply staging ClusterIssuer: %w", err)
	}

	// Only create production ClusterIssuer if a real email is provided
	// Production Let's Encrypt requires a valid email for account recovery
	if cfCfg.Email != "" {
		productionManifest, err := buildClusterIssuerManifest(cfCfg.Email, true)
		if err != nil {
			return fmt.Errorf("failed to build production ClusterIssuer manifest: %w", err)
		}
		if err := applyClusterIssuerWithRetry(ctx, client, "letsencrypt-cloudflare-production", productionManifest); err != nil {
			return fmt.Errorf("failed to apply production ClusterIssuer: %w", err)
		}
	} else {
		log.Println("Skipping production ClusterIssuer (no email provided - staging certificates only)")
	}

	return nil
}

// cloudflareIssuerName returns the ClusterIssuer that applyCertManagerCloudflare
// created: production when an email is configured, otherwise staging only.
func cloudflareIssuerName(cfCfg config.CertManagerCloudflareConfig) string {
	if cfCfg.Email != "" {
		return "letsencrypt-cloudflare-production"
	}
	return "letsencrypt-cloudflare-staging"
}

// applyWildcardCertificate requests one DNS01 certificate for "*.{domain}" and
// makes it Traefik's default via a TLSStore, so ingresses without an explicit
// TLS secret are covered. It must run in the traefik step: the TLSStore CRD
// ships with the Traefik chart, which installs after cert-manager.
func applyWildcardCertificate(ctx context.Context, client k8sclient.Client, cfg *config.Config) error {
	cfCfg := cfg.Addons.CertManager.Cloudflare
	domain := cfg.Addons.Cloudflare.Domain
	if !cfCfg.WildcardCertificate || domain == "" {
		return nil
	}

	issuerName := cloudflareIssuerName(cfCfg)
	certManifest, err := buildWildcardCertManifest(domain, issuerName)
	if err != nil {
		return fmt.Errorf("failed to build wildcard certificate manifest: %w", err)
	}
	if err := applyManifests(ctx, client, "wildcard-certificate", certManifest); err != nil {
		return fmt.Errorf("failed to apply wildcard certificate: %w", err)
	}
	log.Printf("Wildcard certificate for *.%s requested via %s", domain, issuerName)

	// The TLSStore CRD was installed by the Traefik chart moments ago; give
	// discovery a moment to catch up. If it never appears (e.g. a custom
	// chart without CRDs), degrade to a warning instead of failing the step:
	// the certificate above still exists for explicit ingress TLS use.
	if err := waitForResource(ctx, func(ctx context.Context) (bool, error) {
		return client.HasCRD(ctx, "traefik.io/v1alpha1/TLSStore")
	}, DefaultResourceWaitTime, "Traefik TLSStore CRD"); err != nil {
		log.Printf("WARNING: %v - wildcard certificate issued but not set as Traefik default", err)
		return nil
	}
	if err := client.RefreshDiscovery(ctx); err != nil {
		log.Printf("Warning: failed to refresh discovery before applying TLSStore: %v", err)
	}

	if err := applyManifests(ctx, client, "wildcard-tlsstore", buildTLSStoreManifest()); err != nil {
		return fmt.Errorf("failed to apply default TLSStore: %w", err)
	}
	return nil
}

// applyClusterIssuerWithRetry applies a ClusterIssuer manifest with retry for transient webhook failures.
func applyClusterIssuerWithRetry(ctx context.Context, client k8sclient.Client, name string, manifest []byte) error {
	maxRetries := 6
	retryInterval := 10 * time.Second

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			log.Printf("Retrying ClusterIssuer %s creation (attempt %d/%d)...", name, i+1, maxRetries)
		}

		err := applyManifests(ctx, client, name, manifest)
		if err == nil {
			log.Printf("ClusterIssuer %s created successfully", name)
			return nil
		}

		lastErr = err
		log.Printf("Failed to create ClusterIssuer %s: %v", name, err)

		// Check if this is a webhook-related error that might be transient
		if i < maxRetries-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryInterval):
				// Refresh discovery before retry to pick up any API changes
				if refreshErr := client.RefreshDiscovery(ctx); refreshErr != nil {
					log.Printf("Warning: failed to refresh discovery: %v", refreshErr)
				}
			}
		}
	}

	return fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

// waitForCertManagerCRDs waits for cert-manager CRDs and webhook to be ready.
// Helm applies CRDs asynchronously and the webhook needs time to initialize.
func waitForCertManagerCRDs(ctx context.Context, client k8sclient.Client) error {
	timeout := 3 * time.Minute
	interval := 5 * time.Second
	// Grace period after webhook endpoint is detected to let the webhook server initialize
	webhookGracePeriod := 15 * time.Second

	deadline := time.Now().Add(timeout)

	crdReady := false
	webhookReady := false

	log.Println("[cert-manager] Starting readiness checks...")

	for time.Now().Before(deadline) {
		// Step 1: Check if the ClusterIssuer CRD is available
		if !crdReady {
			hasCRD, err := client.HasCRD(ctx, "cert-manager.io/v1/ClusterIssuer")
			switch {
			case err != nil:
				log.Printf("[cert-manager] Error checking for CRD: %v", err)
			case hasCRD:
				log.Println("[cert-manager] ClusterIssuer CRD is registered in API")
				crdReady = true
				// Refresh the client's REST mapper to pick up the new CRD
				if err := client.RefreshDiscovery(ctx); err != nil {
					log.Printf("[cert-manager] Warning: failed to refresh discovery after CRD found: %v", err)
				}
			default:
				log.Println("[cert-manager] Waiting for ClusterIssuer CRD to be registered...")
			}
		}

		// Step 2: Check if the webhook service has endpoints (pod is ready)
		if crdReady && !webhookReady {
			ready, err := client.HasReadyEndpoints(ctx, "cert-manager", "cert-manager-webhook")
			switch {
			case err != nil:
				log.Printf("[cert-manager] Error checking webhook endpoints: %v", err)
			case ready:
				log.Println("[cert-manager] Webhook endpoint detected, waiting for webhook server to initialize...")
				// Wait for the webhook server inside the pod to fully initialize
				// This is critical because the endpoint can exist before the HTTPS server is ready
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(webhookGracePeriod):
					log.Println("[cert-manager] Webhook grace period complete")
				}
				webhookReady = true
			default:
				log.Println("[cert-manager] Waiting for webhook endpoint to be ready...")
			}
		}

		// If both are ready, we're done
		if crdReady && webhookReady {
			log.Println("[cert-manager] CRDs and webhook are fully ready")
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
			// Continue waiting
		}
	}

	if !crdReady {
		return fmt.Errorf("timeout waiting for cert-manager CRDs to be available after %v", timeout)
	}
	return fmt.Errorf("timeout waiting for cert-manager webhook to be ready after %v", timeout)
}

// buildClusterIssuerManifest creates a ClusterIssuer manifest for Let's Encrypt with Cloudflare DNS01.
func buildClusterIssuerManifest(email string, production bool) ([]byte, error) {
	data := clusterIssuerData{
		Email:          email,
		SecretName:     cloudflareSecretName,
		SecretKey:      "api-token",
		Production:     production,
		Name:           "letsencrypt-cloudflare-staging",
		Server:         "https://acme-staging-v02.api.letsencrypt.org/directory",
		PrivateKeyName: "letsencrypt-cloudflare-staging-key",
	}

	if production {
		data.Name = "letsencrypt-cloudflare-production"
		data.Server = "https://acme-v02.api.letsencrypt.org/directory"
		data.PrivateKeyName = "letsencrypt-cloudflare-production-key"
	}

	tmpl, err := template.New("clusterissuer").Parse(clusterIssuerTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ClusterIssuer template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to execute ClusterIssuer template: %w", err)
	}

	return buf.Bytes(), nil
}

// clusterIssuerData holds the data for rendering the ClusterIssuer template.
type clusterIssuerData struct {
	Name           string
	Email          string
	Server         string
	PrivateKeyName string
	SecretName     string
	SecretKey      string
	Production     bool
}

// clusterIssuerTemplate is the YAML template for a ClusterIssuer with Cloudflare DNS01 solver.
const clusterIssuerTemplate = `apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: {{ .Name }}
spec:
  acme:
    # Email address for Let's Encrypt account
    email: {{ .Email }}
    # ACME server URL
    server: {{ .Server }}
    # Secret to store the ACME account private key
    privateKeySecretRef:
      name: {{ .PrivateKeyName }}
    # DNS01 solver using Cloudflare
    solvers:
    - dns01:
        cloudflare:
          apiTokenSecretRef:
            name: {{ .SecretName }}
            key: {{ .SecretKey }}
`

// buildWildcardCertManifest renders a cert-manager Certificate for
// "*.{domain}" in the traefik namespace.
func buildWildcardCertManifest(domain, issuerName string) ([]byte, error) {
	data := wildcardCertData{
		Name:       "wildcard-" + strings.ReplaceAll(domain, ".", "-"),
		Domain:     domain,
		IssuerName: issuerName,
		SecretName: wildcardTLSSecretName,
	}

	tmpl, err := template.New("wildcardcert").Parse(wildcardCertTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse wildcard certificate template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to execute wildcard certificate template: %w", err)
	}

	return buf.Bytes(), nil
}

// buildTLSStoreManifest renders the Traefik default TLSStore pointing at the
// wildcard certificate secret.
func buildTLSStoreManifest() []byte {
	return []byte(`apiVersion: traefik.io/v1alpha1
kind: TLSStore
metadata:
  name: default
  namespace: traefik
spec:
  defaultCertificate:
    secretName: ` + wildcardTLSSecretName + `
`)
}

// wildcardTLSSecretName is where cert-manager stores the wildcard certificate
// and where the default TLSStore looks for it.
const wildcardTLSSecretName = "wildcard-tls"

// wildcardCertData holds the data for rendering the wildcard certificate template.
type wildcardCertData struct {
	Name       string
	Domain     string
	IssuerName string
	SecretName string
}

// wildcardCertTemplate renders the wildcard Certificate.
const wildcardCertTemplate = `apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: {{ .Name }}
  namespace: traefik
spec:
  secretName: {{ .SecretName }}
  dnsNames:
    - "*.{{ .Domain }}"
    - "{{ .Domain }}"
  issuerRef:
    name: {{ .IssuerName }}
    kind: ClusterIssuer
`
