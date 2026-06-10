package addons

import (
	"context"
	"log"

	"github.com/milankappen/k8zner/internal/addons/helm"
	"github.com/milankappen/k8zner/internal/addons/k8sclient"
	"github.com/milankappen/k8zner/internal/config"
)

// applyArgoCD installs ArgoCD for declarative GitOps continuous delivery.
func applyArgoCD(ctx context.Context, client k8sclient.Client, cfg *config.Config) error {
	argoCDCfg := cfg.Addons.ArgoCD

	// Wait for IngressClass if ingress is enabled
	// This ensures Traefik (or another ingress controller) is ready before creating Ingress resources
	if argoCDCfg.IngressEnabled && argoCDCfg.IngressHost != "" {
		ingressClass := argoCDCfg.IngressClassName
		if ingressClass == "" {
			ingressClass = "traefik" // Default to Traefik
		}

		if err := waitForIngressClass(ctx, client, ingressClass, DefaultResourceWaitTime); err != nil {
			log.Printf("[ArgoCD] WARNING: IngressClass %q not ready, ingress may not work: %v", ingressClass, err)
			log.Printf("[ArgoCD] Continuing installation - Ingress will become functional when IngressClass is available")
			// Continue anyway - Ingress resource will be created and become functional once the controller is ready
		}
	}

	if err := ensureNamespace(ctx, client, "argocd", withBaselinePodSecurity(map[string]string{"name": "argocd"})); err != nil {
		return err
	}

	// Build values based on configuration
	values := buildArgoCDValues(cfg)

	return installHelmAddon(ctx, client, "argo-cd", "argocd", cfg.Addons.ArgoCD.Helm, values)
}

// buildArgoCDValues creates helm values for ArgoCD configuration.
func buildArgoCDValues(cfg *config.Config) helm.Values {
	argoCDCfg := cfg.Addons.ArgoCD

	values := helm.Values{
		// Global settings
		"global": helm.Values{
			"domain": argoCDCfg.IngressHost,
		},
		// Install CRDs
		"crds": helm.Values{
			"install": true,
			"keep":    true,
		},
		// Configure ArgoCD to run in insecure mode (no TLS on backend)
		// This allows the ingress controller (Traefik) to handle TLS termination
		// See: https://argo-cd.readthedocs.io/en/stable/operator-manual/ingress/
		"configs": helm.Values{
			"params": helm.Values{
				"server.insecure": true,
			},
		},
		// Enable the redis secret init job to create the argocd-redis secret
		// This is a TOP-LEVEL key, not nested under redis
		// See: https://github.com/argoproj/argo-helm/issues/3057
		"redisSecretInit": helm.Values{
			"enabled": true,
		},
		// Controller configuration
		"controller": buildArgoCDController(argoCDCfg),
		// Server configuration
		"server": buildArgoCDServer(cfg),
		// Repo server configuration
		"repoServer": buildArgoCDRepoServer(argoCDCfg),
		// Redis configuration
		"redis": buildArgoCDRedis(argoCDCfg),
		// Dex (OIDC provider) - disabled by default, users can enable via custom values
		"dex": helm.Values{
			"enabled": false,
		},
		// ApplicationSet controller
		"applicationSet": helm.Values{
			"enabled": true,
		},
		// Notifications controller
		"notifications": helm.Values{
			"enabled": true,
		},
	}

	// Configure HA mode
	if argoCDCfg.HA {
		values["redis-ha"] = helm.Values{
			"enabled": true,
			// Disable redis-ha auth to avoid secret dependency issues
			// See: https://github.com/argoproj/argo-helm/issues/3057
			"auth": false,
		}
		// Disable standalone redis when using HA
		values["redis"] = helm.Values{
			"enabled": false,
		}
	} else {
		// Explicitly disable redis-ha for non-HA mode
		values["redis-ha"] = helm.Values{
			"enabled": false,
		}
	}

	// Merge custom Helm values from config
	return helm.MergeCustomValues(values, argoCDCfg.Helm.Values)
}

// buildArgoCDController creates the application controller configuration.
func buildArgoCDController(cfg config.ArgoCDConfig) helm.Values {
	replicas := 1
	if cfg.HA && cfg.ControllerReplicas != nil {
		replicas = *cfg.ControllerReplicas
	}

	return helm.Values{
		"replicas":    replicas,
		"tolerations": []helm.Values{helm.CCMUninitializedToleration()},
		"resources": helm.Values{
			"requests": helm.Values{
				"cpu":    "100m",
				"memory": "256Mi",
			},
			"limits": helm.Values{
				"memory": "512Mi",
			},
		},
	}
}

// buildArgoCDServer creates the ArgoCD server configuration.
func buildArgoCDServer(cfg *config.Config) helm.Values {
	argoCDCfg := cfg.Addons.ArgoCD
	replicas := 1
	if argoCDCfg.HA {
		replicas = 2
		if argoCDCfg.ServerReplicas != nil {
			replicas = *argoCDCfg.ServerReplicas
		}
	}

	server := helm.Values{
		"replicas":    replicas,
		"tolerations": []helm.Values{helm.CCMUninitializedToleration()},
		"resources": helm.Values{
			"requests": helm.Values{
				"cpu":    "50m",
				"memory": "128Mi",
			},
			"limits": helm.Values{
				"memory": "256Mi",
			},
		},
	}

	// Configure ingress if enabled
	if argoCDCfg.IngressEnabled && argoCDCfg.IngressHost != "" {
		server["ingress"] = buildArgoCDIngress(cfg)
	}

	return server
}

// buildArgoCDRepoServer creates the repo server configuration.
func buildArgoCDRepoServer(cfg config.ArgoCDConfig) helm.Values {
	replicas := 1
	if cfg.HA {
		replicas = 2
		if cfg.RepoServerReplicas != nil {
			replicas = *cfg.RepoServerReplicas
		}
	}

	return helm.Values{
		"replicas":    replicas,
		"tolerations": []helm.Values{helm.CCMUninitializedToleration()},
		"resources": helm.Values{
			"requests": helm.Values{
				"cpu":    "50m",
				"memory": "128Mi",
			},
			"limits": helm.Values{
				"memory": "512Mi",
			},
		},
	}
}

// buildArgoCDRedis creates the Redis configuration.
func buildArgoCDRedis(cfg config.ArgoCDConfig) helm.Values {
	// Disable standalone redis if HA is enabled (uses redis-ha instead)
	if cfg.HA {
		return helm.Values{
			"enabled": false,
		}
	}

	return helm.Values{
		"enabled":     true,
		"tolerations": []helm.Values{helm.CCMUninitializedToleration()},
		"resources": helm.Values{
			"requests": helm.Values{
				"cpu":    "50m",
				"memory": "64Mi",
			},
			"limits": helm.Values{
				"memory": "128Mi",
			},
		},
	}
}

// buildArgoCDIngress creates the ingress configuration for ArgoCD server.
func buildArgoCDIngress(cfg *config.Config) helm.Values {
	argoCDCfg := cfg.Addons.ArgoCD

	ingress := helm.Values{
		"enabled":  true,
		"hostname": argoCDCfg.IngressHost,
	}

	// Set ingress class if specified
	if argoCDCfg.IngressClassName != "" {
		ingress["ingressClassName"] = argoCDCfg.IngressClassName
	}

	// Configure TLS if enabled
	// The v9.x chart uses tls: true (boolean) and derives the secret name
	// from the hostname automatically (argocd-server-tls)
	if argoCDCfg.IngressTLS {
		ingress["tls"] = true
		ingress["annotations"] = helm.IngressAnnotations(cfg, argoCDCfg.IngressHost)
	}

	return ingress
}
