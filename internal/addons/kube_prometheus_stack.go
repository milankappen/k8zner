package addons

import (
	"context"
	"fmt"
	"log"

	"github.com/milankappen/k8zner/internal/addons/helm"
	"github.com/milankappen/k8zner/internal/addons/k8sclient"
	"github.com/milankappen/k8zner/internal/config"
)

// applyKubePrometheusStack installs the kube-prometheus-stack (Prometheus, Grafana, Alertmanager).
func applyKubePrometheusStack(ctx context.Context, client k8sclient.Client, cfg *config.Config) error {
	if err := ensureNamespace(ctx, client, "monitoring", withHostAccessPodSecurity(map[string]string{"name": "monitoring"})); err != nil {
		return err
	}

	// Build values based on configuration
	values := buildKubePrometheusStackValues(cfg)

	if err := installHelmAddon(ctx, client, "kube-prometheus-stack", "monitoring", cfg.Addons.KubePrometheusStack.Helm, values); err != nil {
		return err
	}

	log.Printf("[KubePrometheusStack] Monitoring stack installed successfully")
	return nil
}

// buildKubePrometheusStackValues creates helm values for kube-prometheus-stack configuration.
func buildKubePrometheusStackValues(cfg *config.Config) helm.Values {
	promCfg := cfg.Addons.KubePrometheusStack

	values := helm.Values{
		// Default alerting rules
		"defaultRules": helm.Values{
			"create": getBoolWithDefault(promCfg.DefaultRules, true),
		},
		// Prometheus configuration
		"prometheus": buildPrometheusValues(cfg),
		// Grafana configuration
		"grafana": buildGrafanaValues(cfg),
		// Alertmanager configuration
		"alertmanager": buildAlertmanagerValues(cfg),
		// Node Exporter
		"nodeExporter": helm.Values{
			"enabled": getBoolWithDefault(promCfg.NodeExporter, true),
		},
		// Kube State Metrics
		"kubeStateMetrics": helm.Values{
			"enabled": getBoolWithDefault(promCfg.KubeStateMetrics, true),
		},
		// Prometheus Operator configuration
		"prometheusOperator": buildPrometheusOperatorValues(),
	}

	// Merge custom Helm values from config
	return helm.MergeCustomValues(values, promCfg.Helm.Values)
}

// buildPrometheusValues creates the Prometheus server configuration.
func buildPrometheusValues(cfg *config.Config) helm.Values {
	promCfg := cfg.Addons.KubePrometheusStack.Prometheus

	values := helm.Values{
		"enabled": getBoolWithDefault(promCfg.Enabled, true),
		"prometheusSpec": helm.Values{
			// Retention period
			"retention":   fmt.Sprintf("%dd", getIntWithDefault(promCfg.RetentionDays, 15)),
			"tolerations": []helm.Values{helm.CCMUninitializedToleration()},
			"resources":   buildResourceValues(promCfg.Resources, "500m", "512Mi", "2", "2Gi"),
			// Service monitor selector - scrape all ServiceMonitors
			"serviceMonitorSelectorNilUsesHelmValues": false,
			"podMonitorSelectorNilUsesHelmValues":     false,
			"ruleSelectorNilUsesHelmValues":           false,
		},
	}

	// Storage configuration
	if promCfg.Persistence.Enabled {
		pvcSpec := helm.Values{
			"accessModes": []string{"ReadWriteOnce"},
			"resources": helm.Values{
				"requests": helm.Values{
					"storage": getStringWithDefault(promCfg.Persistence.Size, "50Gi"),
				},
			},
		}
		// Only set storageClassName if explicitly configured.
		// Omitting the field lets Kubernetes use the default StorageClass.
		// Setting it to "" explicitly requests NO StorageClass (manual PV binding).
		if promCfg.Persistence.StorageClass != "" {
			pvcSpec["storageClassName"] = promCfg.Persistence.StorageClass
		}
		values["prometheusSpec"].(helm.Values)["storageSpec"] = helm.Values{
			"volumeClaimTemplate": helm.Values{
				"spec": pvcSpec,
			},
		}
	}

	// Ingress configuration
	if promCfg.IngressEnabled && promCfg.IngressHost != "" {
		values["ingress"] = buildPrometheusIngress(cfg)
	}

	return values
}

// buildPrometheusIngress creates the ingress configuration for Prometheus.
func buildPrometheusIngress(cfg *config.Config) helm.Values {
	promCfg := cfg.Addons.KubePrometheusStack.Prometheus

	ingress := helm.Values{
		"enabled": true,
		"hosts": []string{
			promCfg.IngressHost,
		},
		"paths": []string{"/"},
	}

	// Set ingress class
	ingressClass := promCfg.IngressClassName
	if ingressClass == "" {
		ingressClass = "traefik"
	}
	ingress["ingressClassName"] = ingressClass

	// Configure TLS if enabled
	if promCfg.IngressTLS {
		ingress["tls"] = []helm.Values{
			{
				"hosts":      []string{promCfg.IngressHost},
				"secretName": "prometheus-tls",
			},
		}

		// Build annotations for TLS and DNS
		annotations := helm.IngressAnnotations(cfg, promCfg.IngressHost)
		ingress["annotations"] = annotations
	}

	return ingress
}

// buildGrafanaValues creates the Grafana configuration.
func buildGrafanaValues(cfg *config.Config) helm.Values {
	grafanaCfg := cfg.Addons.KubePrometheusStack.Grafana

	values := helm.Values{
		"enabled": getBoolWithDefault(grafanaCfg.Enabled, true),
		// Run Grafana in insecure mode - Traefik handles TLS termination
		"grafana.ini": helm.Values{
			"server": helm.Values{
				"root_url":            fmt.Sprintf("https://%s", grafanaCfg.IngressHost),
				"serve_from_sub_path": false,
			},
		},
		// Disable test framework to avoid service account race conditions
		"testFramework": helm.Values{
			"enabled": false,
		},
		"tolerations": []helm.Values{helm.CCMUninitializedToleration()},
		"resources": helm.Values{
			"requests": helm.Values{
				"cpu":    "100m",
				"memory": "128Mi",
			},
			"limits": helm.Values{
				"memory": "256Mi",
			},
		},
		// Default data sources
		"sidecar": helm.Values{
			"dashboards": helm.Values{
				"enabled": true,
			},
			"datasources": helm.Values{
				"enabled": true,
			},
		},
	}

	// Admin password
	if grafanaCfg.AdminPassword != "" {
		values["adminPassword"] = grafanaCfg.AdminPassword
	}

	// Persistence configuration
	if grafanaCfg.Persistence.Enabled {
		persistence := helm.Values{
			"enabled": true,
			"size":    getStringWithDefault(grafanaCfg.Persistence.Size, "10Gi"),
		}
		// Only set storageClassName if explicitly configured.
		// Omitting the field lets Kubernetes use the default StorageClass.
		// Setting it to "" explicitly requests NO StorageClass (manual PV binding).
		if grafanaCfg.Persistence.StorageClass != "" {
			persistence["storageClassName"] = grafanaCfg.Persistence.StorageClass
		}
		values["persistence"] = persistence
	} else {
		// Explicitly disable persistence
		values["persistence"] = helm.Values{
			"enabled": false,
		}
	}

	// Ingress configuration
	if grafanaCfg.IngressEnabled && grafanaCfg.IngressHost != "" {
		values["ingress"] = buildGrafanaIngress(cfg)
	}

	return values
}

// buildGrafanaIngress creates the ingress configuration for Grafana.
func buildGrafanaIngress(cfg *config.Config) helm.Values {
	grafanaCfg := cfg.Addons.KubePrometheusStack.Grafana

	ingress := helm.Values{
		"enabled": true,
		"hosts": []string{
			grafanaCfg.IngressHost,
		},
		"path": "/",
	}

	// Set ingress class
	ingressClass := grafanaCfg.IngressClassName
	if ingressClass == "" {
		ingressClass = "traefik"
	}
	ingress["ingressClassName"] = ingressClass

	// Configure TLS if enabled
	if grafanaCfg.IngressTLS {
		ingress["tls"] = []helm.Values{
			{ //nolint:gosec // G101: K8s TLS secret name, not credentials
				"hosts":      []string{grafanaCfg.IngressHost},
				"secretName": "grafana-tls",
			},
		}

		// Build annotations for TLS and DNS
		annotations := helm.IngressAnnotations(cfg, grafanaCfg.IngressHost)
		ingress["annotations"] = annotations
	}

	return ingress
}

// buildAlertmanagerValues creates the Alertmanager configuration.
func buildAlertmanagerValues(cfg *config.Config) helm.Values {
	alertCfg := cfg.Addons.KubePrometheusStack.Alertmanager

	values := helm.Values{
		"enabled": getBoolWithDefault(alertCfg.Enabled, true),
		"alertmanagerSpec": helm.Values{
			"tolerations": []helm.Values{helm.CCMUninitializedToleration()},
			// Resource defaults
			"resources": helm.Values{
				"requests": helm.Values{
					"cpu":    "50m",
					"memory": "64Mi",
				},
				"limits": helm.Values{
					"memory": "128Mi",
				},
			},
		},
	}

	return values
}

// buildPrometheusOperatorValues creates the Prometheus Operator configuration.
func buildPrometheusOperatorValues() helm.Values {
	return helm.Values{
		"tolerations": []helm.Values{helm.CCMUninitializedToleration()},
		"resources": helm.Values{
			"requests": helm.Values{
				"cpu":    "100m",
				"memory": "128Mi",
			},
			"limits": helm.Values{
				"memory": "256Mi",
			},
		},
	}
}

// buildResourceValues creates resource specifications with defaults.
func buildResourceValues(resources config.KubePrometheusResourcesConfig, defaultReqCPU, defaultReqMem, defaultLimitCPU, defaultLimitMem string) helm.Values {
	reqCPU := resources.Requests.CPU
	if reqCPU == "" {
		reqCPU = defaultReqCPU
	}
	reqMem := resources.Requests.Memory
	if reqMem == "" {
		reqMem = defaultReqMem
	}
	limitCPU := resources.Limits.CPU
	if limitCPU == "" {
		limitCPU = defaultLimitCPU
	}
	limitMem := resources.Limits.Memory
	if limitMem == "" {
		limitMem = defaultLimitMem
	}

	return helm.Values{
		"requests": helm.Values{
			"cpu":    reqCPU,
			"memory": reqMem,
		},
		"limits": helm.Values{
			"cpu":    limitCPU,
			"memory": limitMem,
		},
	}
}

// Helper functions

func getBoolWithDefault(ptr *bool, defaultVal bool) bool {
	if ptr == nil {
		return defaultVal
	}
	return *ptr
}

func getIntWithDefault(ptr *int, defaultVal int) int {
	if ptr == nil {
		return defaultVal
	}
	return *ptr
}

func getStringWithDefault(s string, defaultVal string) string {
	if s == "" {
		return defaultVal
	}
	return s
}
