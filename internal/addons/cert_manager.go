package addons

import (
	"context"

	"github.com/milankappen/k8zner/internal/addons/helm"
	"github.com/milankappen/k8zner/internal/addons/k8sclient"
	"github.com/milankappen/k8zner/internal/config"
)

// applyCertManager installs cert-manager for TLS certificate management.
func applyCertManager(ctx context.Context, client k8sclient.Client, cfg *config.Config) error {
	if err := ensureNamespace(ctx, client, "cert-manager", withBaselinePodSecurity(nil)); err != nil {
		return err
	}

	// Build values for the addon
	values := buildCertManagerValues(cfg)

	return installHelmAddon(ctx, client, "cert-manager", "cert-manager", cfg.Addons.CertManager.Helm, values)
}

// buildCertManagerValues creates helm values for the addon.
func buildCertManagerValues(cfg *config.Config) helm.Values {
	controlPlaneCount := getControlPlaneCount(cfg)
	replicas := 1
	if controlPlaneCount > 1 {
		replicas = 2
	}

	baseConfig := buildCertManagerBaseConfig(replicas)

	values := helm.Values{
		"crds":                      helm.Values{"enabled": true},
		"startupapicheck":           helm.Values{"enabled": false},
		"config":                    buildCertManagerConfig(cfg),
		"replicaCount":              baseConfig["replicaCount"],
		"podDisruptionBudget":       baseConfig["podDisruptionBudget"],
		"topologySpreadConstraints": buildCertManagerTopologySpread("controller"),
		"nodeSelector":              baseConfig["nodeSelector"],
		"tolerations":               baseConfig["tolerations"],
		// Enable ingress-shim to auto-create Certificate resources from Ingress annotations
		// This watches for annotations like cert-manager.io/cluster-issuer on Ingress resources
		// and automatically creates the corresponding Certificate resources.
		// Note: ingressShim is a top-level Helm value, NOT part of the config section.
		"ingressShim": helm.Values{
			"defaultIssuerKind": "ClusterIssuer",
			// defaultIssuerName left empty - users specify via annotation on each Ingress
		},
		"webhook": helm.Values{
			"replicaCount":              baseConfig["replicaCount"],
			"podDisruptionBudget":       baseConfig["podDisruptionBudget"],
			"topologySpreadConstraints": buildCertManagerTopologySpread("webhook"),
			"nodeSelector":              baseConfig["nodeSelector"],
			"tolerations":               baseConfig["tolerations"],
		},
		"cainjector": helm.Values{
			"replicaCount":              baseConfig["replicaCount"],
			"podDisruptionBudget":       baseConfig["podDisruptionBudget"],
			"topologySpreadConstraints": buildCertManagerTopologySpread("cainjector"),
			"nodeSelector":              baseConfig["nodeSelector"],
			"tolerations":               baseConfig["tolerations"],
		},
	}

	// Merge custom Helm values from config
	return helm.MergeCustomValues(values, cfg.Addons.CertManager.Helm.Values)
}

// buildCertManagerBaseConfig creates the base configuration shared by all components.
func buildCertManagerBaseConfig(replicas int) helm.Values {
	return helm.Values{
		"replicaCount": replicas,
		"podDisruptionBudget": helm.Values{
			"enabled":        true,
			"minAvailable":   nil,
			"maxUnavailable": 1,
		},
		"nodeSelector": helm.ControlPlaneNodeSelector(),
		"tolerations": []helm.Values{
			{
				"key":      "node-role.kubernetes.io/control-plane",
				"effect":   "NoSchedule",
				"operator": "Exists",
			},
			helm.CCMUninitializedToleration(),
		},
	}
}

// buildCertManagerTopologySpread creates topology spread constraints for a component.
func buildCertManagerTopologySpread(component string) []helm.Values {
	return []helm.Values{
		{
			"topologyKey":       "kubernetes.io/hostname",
			"maxSkew":           1,
			"whenUnsatisfiable": "DoNotSchedule",
			"labelSelector": helm.Values{
				"matchLabels": helm.Values{
					"app.kubernetes.io/instance":  "cert-manager",
					"app.kubernetes.io/component": component,
				},
			},
			"matchLabelKeys": []string{"pod-template-hash"},
		},
	}
}

func buildCertManagerConfig(cfg *config.Config) helm.Values {
	return helm.Values{
		"enableGatewayAPI": false,
		"featureGates": helm.Values{
			// Workaround for Traefik ingress path handling
			"ACMEHTTP01IngressPathTypeExact": !cfg.Addons.Traefik.Enabled,
		},
	}
}
