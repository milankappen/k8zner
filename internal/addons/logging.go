package addons

import (
	"context"
	"fmt"
	"log"

	"github.com/milankappen/k8zner/internal/addons/helm"
	"github.com/milankappen/k8zner/internal/addons/k8sclient"
	"github.com/milankappen/k8zner/internal/config"
)

// defaultLogRetention bounds how long Loki keeps logs when the user does not
// configure a retention period.
const defaultLogRetention = "168h"

// lokiEndpoint is the in-cluster push/query URL for the Loki single binary.
const lokiEndpoint = "http://loki.logging.svc.cluster.local:3100"

// applyLogging installs the logging stack: Loki (single binary, persisted via
// the Hetzner CSI default storage class) and Grafana Alloy as the collector.
// Alloy tails pod logs through the Kubernetes API rather than hostPath
// mounts, so the namespace stays on baseline pod security.
func applyLogging(ctx context.Context, client k8sclient.Client, cfg *config.Config) error {
	if !cfg.Addons.Logging.Enabled {
		return nil
	}

	if err := ensureNamespace(ctx, client, "logging", withBaselinePodSecurity(map[string]string{"name": "logging"})); err != nil {
		return err
	}

	log.Printf("[addons] Installing Loki (retention=%s)...", logRetention(cfg))
	if err := installHelmAddon(ctx, client, "loki", "logging", cfg.Addons.Logging.LokiHelm, buildLokiValues(cfg)); err != nil {
		return fmt.Errorf("failed to install Loki: %w", err)
	}

	log.Printf("[addons] Installing Alloy log collector...")
	if err := installHelmAddon(ctx, client, "alloy", "logging", cfg.Addons.Logging.AlloyHelm, buildAlloyValues()); err != nil {
		return fmt.Errorf("failed to install Alloy: %w", err)
	}

	// Register Loki as a Grafana datasource when monitoring is installed:
	// the kube-prometheus-stack Grafana sidecar watches for this label.
	if cfg.Addons.KubePrometheusStack.Enabled {
		log.Printf("[addons] Registering Loki datasource in Grafana...")
		if err := applyManifests(ctx, client, "loki-datasource", buildLokiDatasourceManifest()); err != nil {
			return fmt.Errorf("failed to apply Loki datasource: %w", err)
		}
	}

	log.Printf("[addons] Logging stack installed successfully")
	return nil
}

// logRetention returns the configured retention with the default applied.
func logRetention(cfg *config.Config) string {
	if r := cfg.Addons.Logging.Retention; r != "" {
		return r
	}
	return defaultLogRetention
}

// buildLokiValues renders helm values for a single-binary Loki suited to the
// cluster sizes k8zner targets: filesystem storage on a persistent volume,
// no caches, no canary, no gateway.
func buildLokiValues(cfg *config.Config) helm.Values {
	return helm.Values{
		"deploymentMode": "SingleBinary",
		"loki": helm.Values{
			"auth_enabled": false,
			"commonConfig": helm.Values{
				"replication_factor": 1,
			},
			"storage": helm.Values{
				"type": "filesystem",
			},
			"schemaConfig": helm.Values{
				"configs": []helm.Values{
					{
						"from":         "2024-01-01",
						"store":        "tsdb",
						"object_store": "filesystem",
						"schema":       "v13",
						"index": helm.Values{
							"prefix": "index_",
							"period": "24h",
						},
					},
				},
			},
			"limits_config": helm.Values{
				"retention_period": logRetention(cfg),
			},
			"compactor": helm.Values{
				"retention_enabled":    true,
				"delete_request_store": "filesystem",
			},
		},
		"singleBinary": helm.Values{
			"replicas": 1,
			"persistence": helm.Values{
				"enabled": true,
				"size":    "10Gi",
			},
		},
		"backend":      helm.Values{"replicas": 0},
		"read":         helm.Values{"replicas": 0},
		"write":        helm.Values{"replicas": 0},
		"chunksCache":  helm.Values{"enabled": false},
		"resultsCache": helm.Values{"enabled": false},
		"lokiCanary":   helm.Values{"enabled": false},
		"gateway":      helm.Values{"enabled": false},
		"test":         helm.Values{"enabled": false},
	}
}

// buildAlloyValues renders helm values for Grafana Alloy configured to tail
// all pod logs via the Kubernetes API and push them to Loki. API-based
// tailing needs no hostPath mounts, so a single replica suffices and the pod
// passes baseline pod security.
func buildAlloyValues() helm.Values {
	alloyConfig := `discovery.kubernetes "pods" {
  role = "pod"
}

loki.source.kubernetes "pods" {
  targets    = discovery.kubernetes.pods.targets
  forward_to = [loki.write.default.receiver]
}

loki.write "default" {
  endpoint {
    url = "` + lokiEndpoint + `/loki/api/v1/push"
  }
}
`

	return helm.Values{
		"alloy": helm.Values{
			"configMap": helm.Values{
				"content": alloyConfig,
			},
		},
		"controller": helm.Values{
			"type":     "statefulset",
			"replicas": 1,
		},
	}
}

// buildLokiDatasourceManifest renders the ConfigMap the kube-prometheus-stack
// Grafana sidecar picks up to register Loki as a datasource.
func buildLokiDatasourceManifest() []byte {
	return []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: loki-datasource
  namespace: monitoring
  labels:
    grafana_datasource: "1"
data:
  loki-datasource.yaml: |-
    apiVersion: 1
    datasources:
      - name: Loki
        type: loki
        access: proxy
        url: ` + lokiEndpoint + `
        isDefault: false
`)
}
