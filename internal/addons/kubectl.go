package addons

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/milankappen/k8zner/internal/addons/helm"
	"github.com/milankappen/k8zner/internal/addons/k8sclient"
)

// baselinePodSecurityLabels are the standard pod security labels for namespaces
// that need baseline pod security standards.
var baselinePodSecurityLabels = map[string]string{
	"pod-security.kubernetes.io/enforce": "baseline",
	"pod-security.kubernetes.io/audit":   "baseline",
	"pod-security.kubernetes.io/warn":    "baseline",
}

// withBaselinePodSecurity merges the baseline pod security labels into the given
// label map. Caller-provided keys win on conflict, and the input map is left
// untouched so shared label maps stay safe to reuse.
func withBaselinePodSecurity(labels map[string]string) map[string]string {
	merged := make(map[string]string, len(labels)+len(baselinePodSecurityLabels))
	for k, v := range baselinePodSecurityLabels {
		merged[k] = v
	}
	for k, v := range labels {
		merged[k] = v
	}
	return merged
}

// ensureNamespace creates a Kubernetes namespace with the given labels using server-side apply.
func ensureNamespace(ctx context.Context, client k8sclient.Client, name string, labels map[string]string) error {
	yaml := helm.NamespaceManifest(name, labels)
	if err := applyManifests(ctx, client, name+"-namespace", []byte(yaml)); err != nil {
		return fmt.Errorf("failed to create %s namespace: %w", name, err)
	}
	return nil
}

// applyManifests applies Kubernetes manifests using the k8sclient.
// It uses Server-Side Apply with the given field manager name.
func applyManifests(ctx context.Context, client k8sclient.Client, addonName string, manifestBytes []byte) error {
	if err := client.ApplyManifests(ctx, manifestBytes, addonName); err != nil {
		return fmt.Errorf("failed to apply manifests for addon %s: %w", addonName, err)
	}
	return nil
}

// applyFromURL downloads a manifest from a URL and applies it using the k8sclient.
// This is useful for applying CRDs or other manifests hosted remotely.
func applyFromURL(ctx context.Context, client k8sclient.Client, addonName, manifestURL string) error {
	manifestBytes, err := fetchManifestURL(ctx, manifestURL)
	if err != nil {
		return err
	}
	return applyManifests(ctx, client, addonName, manifestBytes)
}

// fetchManifestURL downloads a manifest from a URL and returns the bytes.
func fetchManifestURL(ctx context.Context, manifestURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for %s: %w", manifestURL, err)
	}

	httpClient := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download manifest from %s: %w", manifestURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download manifest from %s: HTTP %d", manifestURL, resp.StatusCode)
	}

	manifestBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest from %s: %w", manifestURL, err)
	}

	return manifestBytes, nil
}
