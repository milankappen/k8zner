// Package crds lets the operator self-manage its CustomResourceDefinitions.
//
// Helm never upgrades charts' crds/ directory, so operator deployments
// installed or upgraded via Helm would otherwise keep serving the CRD schema
// from their first install. The operator instead embeds its CRD (synced from
// config/crd/bases via `make sync-crds`) and server-side-applies it at
// startup, making the running operator the source of truth for the schema it
// reconciles.
package crds

import (
	"context"
	_ "embed"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

//go:embed manifests/k8zner.io_k8znerclusters.yaml
var clusterCRDManifest []byte

// fieldOwner identifies the operator in managedFields for server-side apply.
const fieldOwner = "k8zner-operator"

// clusterCRD decodes the embedded K8znerCluster CRD manifest.
func clusterCRD() (*apiextensionsv1.CustomResourceDefinition, error) {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := yaml.Unmarshal(clusterCRDManifest, crd); err != nil {
		return nil, fmt.Errorf("failed to decode embedded CRD manifest: %w", err)
	}
	return crd, nil
}

// Ensure server-side-applies the operator's CRDs so the served schema always
// matches the operator binary. ForceOwnership takes over fields previously
// applied by kubectl or the CLI install path.
func Ensure(ctx context.Context, c client.Client) error {
	crd, err := clusterCRD()
	if err != nil {
		return err
	}

	if err := c.Patch(ctx, crd, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return fmt.Errorf("failed to apply CRD %s: %w", crd.Name, err)
	}
	return nil
}
