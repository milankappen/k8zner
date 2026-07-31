package addons

import (
	"testing"

	"github.com/milankappen/k8zner/internal/addons/helm"
	"github.com/milankappen/k8zner/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildKubePrometheusStackValues(t *testing.T) {
	t.Parallel()
	t.Run("basic configuration", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				KubePrometheusStack: config.KubePrometheusStackConfig{
					Enabled: true,
				},
			},
		}

		values := buildKubePrometheusStackValues(cfg)

		// Check default rules are enabled
		defaultRules := values["defaultRules"].(helm.Values)
		assert.True(t, defaultRules["create"].(bool))

		// Check node exporter is enabled by default
		nodeExporter := values["nodeExporter"].(helm.Values)
		assert.True(t, nodeExporter["enabled"].(bool))

		// Check kube-state-metrics is enabled by default
		ksm := values["kubeStateMetrics"].(helm.Values)
		assert.True(t, ksm["enabled"].(bool))
	})

	t.Run("with grafana ingress", func(t *testing.T) {
		t.Parallel()
		enabled := true
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				KubePrometheusStack: config.KubePrometheusStackConfig{
					Enabled: true,
					Grafana: config.KubePrometheusGrafanaConfig{
						Enabled:          &enabled,
						IngressEnabled:   true,
						IngressHost:      "grafana.example.com",
						IngressClassName: "traefik",
						IngressTLS:       true,
					},
				},
				CertManager: config.CertManagerConfig{
					Cloudflare: config.CertManagerCloudflareConfig{
						Enabled:    true,
						Production: true,
					},
				},
				Cloudflare: config.CloudflareConfig{
					Enabled: true,
				},
				ExternalDNS: config.ExternalDNSConfig{
					Enabled: true,
				},
			},
		}

		values := buildKubePrometheusStackValues(cfg)

		// Check Grafana configuration
		grafana := values["grafana"].(helm.Values)
		assert.True(t, grafana["enabled"].(bool))

		ingress := grafana["ingress"].(helm.Values)
		assert.True(t, ingress["enabled"].(bool))
		assert.Equal(t, "traefik", ingress["ingressClassName"])

		hosts := ingress["hosts"].([]string)
		require.Len(t, hosts, 1)
		assert.Equal(t, "grafana.example.com", hosts[0])

		// Check TLS configuration
		tls := ingress["tls"].([]helm.Values)
		require.Len(t, tls, 1)
		assert.Equal(t, "grafana-tls", tls[0]["secretName"])

		// Check annotations
		annotations := ingress["annotations"].(helm.Values)
		assert.Equal(t, "letsencrypt-cloudflare-production", annotations["cert-manager.io/cluster-issuer"])
		assert.Equal(t, "grafana.example.com", annotations["external-dns.alpha.kubernetes.io/hostname"])
	})

	t.Run("with prometheus persistence", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				KubePrometheusStack: config.KubePrometheusStackConfig{
					Enabled: true,
					Prometheus: config.KubePrometheusPrometheusConfig{
						Persistence: config.KubePrometheusPersistenceConfig{
							Enabled:      true,
							Size:         "100Gi",
							StorageClass: "hcloud-volumes",
						},
					},
				},
			},
		}

		values := buildKubePrometheusStackValues(cfg)

		prometheus := values["prometheus"].(helm.Values)
		promSpec := prometheus["prometheusSpec"].(helm.Values)

		// Check storage spec
		storageSpec := promSpec["storageSpec"].(helm.Values)
		vct := storageSpec["volumeClaimTemplate"].(helm.Values)
		spec := vct["spec"].(helm.Values)
		resources := spec["resources"].(helm.Values)
		requests := resources["requests"].(helm.Values)
		assert.Equal(t, "100Gi", requests["storage"])
		assert.Equal(t, "hcloud-volumes", spec["storageClassName"])
	})

	t.Run("disabled components", func(t *testing.T) {
		t.Parallel()
		disabled := false
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				KubePrometheusStack: config.KubePrometheusStackConfig{
					Enabled:          true,
					NodeExporter:     &disabled,
					KubeStateMetrics: &disabled,
					DefaultRules:     &disabled,
				},
			},
		}

		values := buildKubePrometheusStackValues(cfg)

		defaultRules := values["defaultRules"].(helm.Values)
		assert.False(t, defaultRules["create"].(bool))

		nodeExporter := values["nodeExporter"].(helm.Values)
		assert.False(t, nodeExporter["enabled"].(bool))

		ksm := values["kubeStateMetrics"].(helm.Values)
		assert.False(t, ksm["enabled"].(bool))
	})
}

func TestBuildIngressAnnotations(t *testing.T) {
	t.Parallel()
	t.Run("with cloudflare staging", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				CertManager: config.CertManagerConfig{
					Cloudflare: config.CertManagerCloudflareConfig{
						Enabled:    true,
						Production: false,
					},
				},
				Cloudflare: config.CloudflareConfig{
					Enabled: true,
				},
				ExternalDNS: config.ExternalDNSConfig{
					Enabled: true,
				},
			},
		}

		annotations := helm.IngressAnnotations(cfg, "test.example.com")

		assert.Equal(t, "letsencrypt-cloudflare-staging", annotations["cert-manager.io/cluster-issuer"])
		assert.Equal(t, "test.example.com", annotations["external-dns.alpha.kubernetes.io/hostname"])
	})

	t.Run("without cloudflare", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				CertManager: config.CertManagerConfig{
					Cloudflare: config.CertManagerCloudflareConfig{
						Enabled: false,
					},
				},
			},
		}

		annotations := helm.IngressAnnotations(cfg, "test.example.com")

		// Should use default fallback issuer
		assert.Equal(t, "letsencrypt-prod", annotations["cert-manager.io/cluster-issuer"])
		// Should not have external-dns annotations
		_, hasHostname := annotations["external-dns.alpha.kubernetes.io/hostname"]
		assert.False(t, hasHostname)
	})
}

func TestBuildPrometheusIngress(t *testing.T) {
	t.Parallel()
	t.Run("with TLS enabled", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				KubePrometheusStack: config.KubePrometheusStackConfig{
					Prometheus: config.KubePrometheusPrometheusConfig{
						IngressEnabled:   true,
						IngressHost:      "prometheus.example.com",
						IngressClassName: "traefik",
						IngressTLS:       true,
					},
				},
				CertManager: config.CertManagerConfig{
					Cloudflare: config.CertManagerCloudflareConfig{
						Enabled:    true,
						Production: true,
					},
				},
				Cloudflare: config.CloudflareConfig{
					Enabled: true,
				},
				ExternalDNS: config.ExternalDNSConfig{
					Enabled: true,
				},
			},
		}

		ingress := buildPrometheusIngress(cfg)

		assert.True(t, ingress["enabled"].(bool))
		assert.Equal(t, "traefik", ingress["ingressClassName"])

		hosts := ingress["hosts"].([]string)
		require.Len(t, hosts, 1)
		assert.Equal(t, "prometheus.example.com", hosts[0])

		tls := ingress["tls"].([]helm.Values)
		require.Len(t, tls, 1)
		assert.Equal(t, "prometheus-tls", tls[0]["secretName"])

		annotations := ingress["annotations"].(helm.Values)
		assert.Equal(t, "letsencrypt-cloudflare-production", annotations["cert-manager.io/cluster-issuer"])
	})

	t.Run("with default ingress class", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				KubePrometheusStack: config.KubePrometheusStackConfig{
					Prometheus: config.KubePrometheusPrometheusConfig{
						IngressEnabled: true,
						IngressHost:    "prometheus.example.com",
						IngressTLS:     false,
					},
				},
			},
		}

		ingress := buildPrometheusIngress(cfg)

		assert.Equal(t, "traefik", ingress["ingressClassName"]) // Default
		_, hasTLS := ingress["tls"]
		assert.False(t, hasTLS)
	})
}

func TestBuildAlertmanagerValues(t *testing.T) {
	t.Parallel()
	t.Run("disabled by default pointer", func(t *testing.T) {
		t.Parallel()
		disabled := false
		cfg := &config.Config{
			Addons: config.AddonsConfig{
				KubePrometheusStack: config.KubePrometheusStackConfig{
					Alertmanager: config.KubePrometheusAlertmanagerConfig{
						Enabled: &disabled,
					},
				},
			},
		}

		values := buildAlertmanagerValues(cfg)
		assert.False(t, values["enabled"].(bool))
	})
}

func TestBuildPrometheusOperatorValues(t *testing.T) {
	t.Parallel()
	values := buildPrometheusOperatorValues()

	// Check tolerations are set
	tolerations := values["tolerations"].([]helm.Values)
	require.Len(t, tolerations, 1)
	assert.Equal(t, "node.cloudprovider.kubernetes.io/uninitialized", tolerations[0]["key"])
	assert.Equal(t, "Exists", tolerations[0]["operator"])

	// Check resources are set
	resources := values["resources"].(helm.Values)
	requests := resources["requests"].(helm.Values)
	assert.Equal(t, "100m", requests["cpu"])
	assert.Equal(t, "128Mi", requests["memory"])
}

func TestBuildResourceValues(t *testing.T) {
	t.Parallel()
	t.Run("with custom values", func(t *testing.T) {
		t.Parallel()
		resources := config.KubePrometheusResourcesConfig{
			Requests: config.KubePrometheusResourceSpec{
				CPU:    "1",
				Memory: "2Gi",
			},
			Limits: config.KubePrometheusResourceSpec{
				CPU:    "4",
				Memory: "8Gi",
			},
		}

		values := buildResourceValues(resources, "100m", "128Mi", "500m", "256Mi")

		requests := values["requests"].(helm.Values)
		assert.Equal(t, "1", requests["cpu"])
		assert.Equal(t, "2Gi", requests["memory"])

		limits := values["limits"].(helm.Values)
		assert.Equal(t, "4", limits["cpu"])
		assert.Equal(t, "8Gi", limits["memory"])
	})

	t.Run("with defaults", func(t *testing.T) {
		t.Parallel()
		resources := config.KubePrometheusResourcesConfig{}

		values := buildResourceValues(resources, "100m", "128Mi", "500m", "256Mi")

		requests := values["requests"].(helm.Values)
		assert.Equal(t, "100m", requests["cpu"])
		assert.Equal(t, "128Mi", requests["memory"])

		limits := values["limits"].(helm.Values)
		assert.Equal(t, "500m", limits["cpu"])
		assert.Equal(t, "256Mi", limits["memory"])
	})
}

func TestMonitoringNamespace(t *testing.T) {
	t.Parallel()
	ns := helm.NamespaceManifest("monitoring", withHostAccessPodSecurity(map[string]string{"name": "monitoring"}))

	assert.Contains(t, ns, "apiVersion: v1")
	assert.Contains(t, ns, "kind: Namespace")
	assert.Contains(t, ns, "name: monitoring")
	// node-exporter needs hostPath/hostNetwork/hostPID, which baseline
	// forbids: enforcing baseline here would reject its pods.
	assert.Contains(t, ns, "pod-security.kubernetes.io/enforce: privileged")
	assert.Contains(t, ns, "pod-security.kubernetes.io/audit: baseline")
	assert.Contains(t, ns, "pod-security.kubernetes.io/warn: baseline")
}

func TestBuildGrafanaValues_AdminPassword(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Addons: config.AddonsConfig{
			KubePrometheusStack: config.KubePrometheusStackConfig{
				Enabled: true,
				Grafana: config.KubePrometheusGrafanaConfig{
					AdminPassword: "secret-password",
				},
			},
		},
	}

	values := buildGrafanaValues(cfg)
	assert.Equal(t, "secret-password", values["adminPassword"])
}

func TestBuildGrafanaValues_Persistence(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Addons: config.AddonsConfig{
			KubePrometheusStack: config.KubePrometheusStackConfig{
				Enabled: true,
				Grafana: config.KubePrometheusGrafanaConfig{
					Persistence: config.KubePrometheusPersistenceConfig{
						Enabled:      true,
						Size:         "20Gi",
						StorageClass: "hcloud-volumes",
					},
				},
			},
		},
	}

	values := buildGrafanaValues(cfg)
	persistence := values["persistence"].(helm.Values)
	assert.True(t, persistence["enabled"].(bool))
	assert.Equal(t, "20Gi", persistence["size"])
	assert.Equal(t, "hcloud-volumes", persistence["storageClassName"])
}

func TestBuildGrafanaValues_PersistenceDefaults(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Addons: config.AddonsConfig{
			KubePrometheusStack: config.KubePrometheusStackConfig{
				Enabled: true,
				Grafana: config.KubePrometheusGrafanaConfig{
					Persistence: config.KubePrometheusPersistenceConfig{
						Enabled: true,
					},
				},
			},
		},
	}

	values := buildGrafanaValues(cfg)
	persistence := values["persistence"].(helm.Values)
	assert.Equal(t, "10Gi", persistence["size"])
}

func TestBuildGrafanaIngress_DefaultClassName(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Addons: config.AddonsConfig{
			KubePrometheusStack: config.KubePrometheusStackConfig{
				Enabled: true,
				Grafana: config.KubePrometheusGrafanaConfig{
					IngressEnabled:   true,
					IngressHost:      "grafana.example.com",
					IngressClassName: "", // empty should default to "traefik"
				},
			},
		},
	}

	ingress := buildGrafanaIngress(cfg)
	assert.Equal(t, "traefik", ingress["ingressClassName"])
}

func TestBuildPrometheusValues_Ingress(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Addons: config.AddonsConfig{
			KubePrometheusStack: config.KubePrometheusStackConfig{
				Enabled: true,
				Prometheus: config.KubePrometheusPrometheusConfig{
					IngressEnabled:   true,
					IngressHost:      "prometheus.example.com",
					IngressClassName: "traefik",
					IngressTLS:       true,
				},
			},
			CertManager: config.CertManagerConfig{
				Cloudflare: config.CertManagerCloudflareConfig{
					Enabled:    true,
					Production: true,
				},
			},
			Cloudflare: config.CloudflareConfig{
				Enabled: true,
			},
		},
	}

	values := buildPrometheusValues(cfg)
	ingress := values["ingress"].(helm.Values)
	assert.True(t, ingress["enabled"].(bool))
	assert.Equal(t, "traefik", ingress["ingressClassName"])

	hosts := ingress["hosts"].([]string)
	require.Len(t, hosts, 1)
	assert.Equal(t, "prometheus.example.com", hosts[0])
}

func TestHelperFunctions(t *testing.T) {
	t.Parallel()
	t.Run("getBoolWithDefault", func(t *testing.T) {
		t.Parallel()
		trueVal := true
		falseVal := false

		assert.True(t, getBoolWithDefault(&trueVal, false))
		assert.False(t, getBoolWithDefault(&falseVal, true))
		assert.True(t, getBoolWithDefault(nil, true))
		assert.False(t, getBoolWithDefault(nil, false))
	})

	t.Run("getIntWithDefault", func(t *testing.T) {
		t.Parallel()
		val := 42

		assert.Equal(t, 42, getIntWithDefault(&val, 10))
		assert.Equal(t, 10, getIntWithDefault(nil, 10))
	})

	t.Run("getStringWithDefault", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "custom", getStringWithDefault("custom", "default"))
		assert.Equal(t, "default", getStringWithDefault("", "default"))
	})
}
