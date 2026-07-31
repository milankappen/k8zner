package addons

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/milankappen/k8zner/internal/addons/helm"
	"github.com/milankappen/k8zner/internal/config"
)

func TestBuildCertManagerValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                      string
		controlPlaneCount         int
		traefikEnabled            bool
		expectedReplicas          int
		expectedACMEPathTypeExact bool
	}{
		{
			name:                      "single control plane, no ingress",
			controlPlaneCount:         1,
			traefikEnabled:            false,
			expectedReplicas:          1,
			expectedACMEPathTypeExact: true,
		},
		{
			name:                      "HA control plane, no ingress",
			controlPlaneCount:         3,
			traefikEnabled:            false,
			expectedReplicas:          2,
			expectedACMEPathTypeExact: true,
		},
		{
			name:                      "single control plane, with traefik",
			controlPlaneCount:         1,
			traefikEnabled:            true,
			expectedReplicas:          1,
			expectedACMEPathTypeExact: false,
		},
		{
			name:                      "HA control plane, with traefik",
			controlPlaneCount:         3,
			traefikEnabled:            true,
			expectedReplicas:          2,
			expectedACMEPathTypeExact: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &config.Config{
				ControlPlane: config.ControlPlaneConfig{
					NodePools: []config.ControlPlaneNodePool{
						{Count: tt.controlPlaneCount},
					},
				},
				Addons: config.AddonsConfig{
					Traefik: config.TraefikConfig{Enabled: tt.traefikEnabled},
				},
			}

			values := buildCertManagerValues(cfg)

			// Check replica count
			assert.Equal(t, tt.expectedReplicas, values["replicaCount"])

			// Check CRDs enabled
			crds, ok := values["crds"].(helm.Values)
			require.True(t, ok)
			assert.Equal(t, true, crds["enabled"])

			// Check startupapicheck disabled
			startupapicheck, ok := values["startupapicheck"].(helm.Values)
			require.True(t, ok)
			assert.Equal(t, false, startupapicheck["enabled"])

			// Check Gateway API disabled (requires Gateway API CRDs which may not be installed)
			configSection, ok := values["config"].(helm.Values)
			require.True(t, ok)
			assert.Equal(t, false, configSection["enableGatewayAPI"])

			// Check ACME HTTP01 feature gate
			featureGates, ok := configSection["featureGates"].(helm.Values)
			require.True(t, ok)
			assert.Equal(t, tt.expectedACMEPathTypeExact, featureGates["ACMEHTTP01IngressPathTypeExact"])

			// Check PDB
			pdb, ok := values["podDisruptionBudget"].(helm.Values)
			require.True(t, ok)
			assert.Equal(t, true, pdb["enabled"])
			assert.Equal(t, 1, pdb["maxUnavailable"])

			// Check node selector
			nodeSelector, ok := values["nodeSelector"].(helm.Values)
			require.True(t, ok)
			assert.Contains(t, nodeSelector, "node-role.kubernetes.io/control-plane")

			// Check tolerations (should have control-plane and CCM uninitialized tolerations)
			tolerations, ok := values["tolerations"].([]helm.Values)
			require.True(t, ok)
			assert.Len(t, tolerations, 2)
			assert.Equal(t, "node-role.kubernetes.io/control-plane", tolerations[0]["key"])
			assert.Equal(t, "NoSchedule", tolerations[0]["effect"])
			assert.Equal(t, "Exists", tolerations[0]["operator"])
			assert.Equal(t, "node.cloudprovider.kubernetes.io/uninitialized", tolerations[1]["key"])

			// Check topology spread constraints for controller (main component)
			tsc, ok := values["topologySpreadConstraints"].([]helm.Values)
			require.True(t, ok)
			assert.Len(t, tsc, 1)
			assert.Equal(t, "kubernetes.io/hostname", tsc[0]["topologyKey"])
			assert.Equal(t, 1, tsc[0]["maxSkew"])
			assert.Equal(t, "DoNotSchedule", tsc[0]["whenUnsatisfiable"])

			// Verify controller component label selector
			labelSelector, ok := tsc[0]["labelSelector"].(helm.Values)
			require.True(t, ok)
			matchLabels, ok := labelSelector["matchLabels"].(helm.Values)
			require.True(t, ok)
			assert.Equal(t, "cert-manager", matchLabels["app.kubernetes.io/instance"])
			assert.Equal(t, "controller", matchLabels["app.kubernetes.io/component"])

			// Check webhook configuration
			webhook, ok := values["webhook"].(helm.Values)
			require.True(t, ok)
			assert.Equal(t, tt.expectedReplicas, webhook["replicaCount"])

			// Verify webhook has its own topology spread constraints
			webhookTSC, ok := webhook["topologySpreadConstraints"].([]helm.Values)
			require.True(t, ok)
			assert.Len(t, webhookTSC, 1)
			webhookLabels, ok := webhookTSC[0]["labelSelector"].(helm.Values)
			require.True(t, ok)
			webhookMatchLabels, ok := webhookLabels["matchLabels"].(helm.Values)
			require.True(t, ok)
			assert.Equal(t, "webhook", webhookMatchLabels["app.kubernetes.io/component"])

			// Check cainjector configuration
			cainjector, ok := values["cainjector"].(helm.Values)
			require.True(t, ok)
			assert.Equal(t, tt.expectedReplicas, cainjector["replicaCount"])

			// Verify cainjector has its own topology spread constraints
			cainjectorTSC, ok := cainjector["topologySpreadConstraints"].([]helm.Values)
			require.True(t, ok)
			assert.Len(t, cainjectorTSC, 1)
			cainjectorLabels, ok := cainjectorTSC[0]["labelSelector"].(helm.Values)
			require.True(t, ok)
			cainjectorMatchLabels, ok := cainjectorLabels["matchLabels"].(helm.Values)
			require.True(t, ok)
			assert.Equal(t, "cainjector", cainjectorMatchLabels["app.kubernetes.io/component"])
		})
	}
}

func TestBuildCertManagerValues_Tolerations(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		ControlPlane: config.ControlPlaneConfig{
			NodePools: []config.ControlPlaneNodePool{
				{Count: 1},
			},
		},
	}
	values := buildCertManagerValues(cfg)

	// Controller tolerations
	tolerations, ok := values["tolerations"].([]helm.Values)
	require.True(t, ok)
	assert.Len(t, tolerations, 2, "cert-manager needs control-plane + CCM uninitialized tolerations")
	assert.Equal(t, "node-role.kubernetes.io/control-plane", tolerations[0]["key"])
	assert.Equal(t, "node.cloudprovider.kubernetes.io/uninitialized", tolerations[1]["key"])

	// Webhook gets same tolerations
	webhook, ok := values["webhook"].(helm.Values)
	require.True(t, ok)
	webhookTolerations, ok := webhook["tolerations"].([]helm.Values)
	require.True(t, ok)
	assert.Len(t, webhookTolerations, 2)

	// Cainjector gets same tolerations
	cainjector, ok := values["cainjector"].(helm.Values)
	require.True(t, ok)
	cainjectorTolerations, ok := cainjector["tolerations"].([]helm.Values)
	require.True(t, ok)
	assert.Len(t, cainjectorTolerations, 2)
}

func TestBuildCertManagerValues_GatewayAPI(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		ControlPlane: config.ControlPlaneConfig{
			NodePools: []config.ControlPlaneNodePool{
				{Count: 1},
			},
		},
	}
	values := buildCertManagerValues(cfg)

	configSection, ok := values["config"].(helm.Values)
	require.True(t, ok)
	assert.Equal(t, false, configSection["enableGatewayAPI"],
		"Gateway API should be disabled by default (requires separate CRD install)")
}

func TestCertManagerNamespace(t *testing.T) {
	t.Parallel()
	ns := helm.NamespaceManifest("cert-manager", withBaselinePodSecurity(nil))

	assert.Contains(t, ns, "apiVersion: v1")
	assert.Contains(t, ns, "kind: Namespace")
	assert.Contains(t, ns, "name: cert-manager")
	assert.Contains(t, ns, "pod-security.kubernetes.io/enforce: baseline")
	assert.Contains(t, ns, "pod-security.kubernetes.io/audit: baseline")
	assert.Contains(t, ns, "pod-security.kubernetes.io/warn: baseline")
}
