package crds

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedCRD(t *testing.T) {
	t.Parallel()

	crd, err := clusterCRD()
	require.NoError(t, err)

	assert.Equal(t, "k8znerclusters.k8zner.io", crd.Name)
	assert.Equal(t, "CustomResourceDefinition", crd.Kind)
	assert.Equal(t, "apiextensions.k8s.io/v1", crd.APIVersion)
	require.NotEmpty(t, crd.Spec.Versions)
	assert.Equal(t, "v1alpha1", crd.Spec.Versions[0].Name)

	// Server-side apply rejects objects carrying state from another cluster.
	assert.Empty(t, crd.ResourceVersion)
	assert.Empty(t, crd.UID)
}
