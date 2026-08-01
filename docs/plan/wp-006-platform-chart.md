# WP-006 — `k8zner-platform` Helm chart + image + embedded sync

Lane A · Depends on: WP-002, WP-003 · Blocks: WP-007

## Goal

The deployable form of the platform: a container image running `k8zner serve` (same
binary as the CLI, SPA embedded) and a Helm chart the `platform` addon step
installs. Replaces WP-002's placeholder.

## Context & anchors

- Image template: `Dockerfile.operator` (2-stage, distroless static, nonroot,
  ldflags version inject). The platform image builds `./cmd/k8zner` **with
  `-tags webui`** after a Node build stage produces the SPA assets.
- Chart + embed sync pattern: canonical `deploy/helm/k8zner-operator/` vs embedded
  `internal/addons/operator-chart/` kept in sync by `make sync-operator-chart` /
  `check-operator-chart` (Makefile:116–132); embed + temp-dir extraction in
  `internal/addons/operator.go` (`extractOperatorChart`).
- Publish workflow template: `.github/workflows/operator-image.yaml`
  (path-filtered build/push to ghcr).
- Ingress/TLS helpers: `helm.IngressAnnotations` (`internal/addons/helm/builders.go`),
  host derivation `BuildIngressHost` (`internal/config/addon_defaults.go`).
- `.dockerignore` — ensure `web/node_modules` exclusion (WP-004 may have done it).

## Contract

- `Dockerfile.platform`: stage 1 Node builds `web/`, stage 2 Go builds
  `./cmd/k8zner` with `-tags webui`, final distroless-static nonroot image,
  `ENTRYPOINT ["/k8zner", "serve"]`.
- Canonical chart `deploy/helm/k8zner-platform/`: Deployment (probes on
  `/healthz`/`/readyz`, resources, securityContext), Service, ServiceAccount +
  ClusterRole/Binding (read `k8znerclusters` + workload-discovery: deployments,
  statefulsets, ingresses, httproutes, namespaces, nodes), bootstrap-token Secret
  (generated if absent), optional ingress: HTTPRoute/IngressRoute + cert-manager
  Certificate gated on a `host` value.
- Embedded copy `internal/addons/platform-chart/` + `sync-platform-chart` /
  `check-platform-chart` Makefile targets; `internal/addons/platform.go` from
  WP-002 now extracts and installs this chart (same mechanics as the operator
  addon), with `buildPlatformValues(cfg)` deriving host from
  `cfg`/`BuildIngressHost` (subdomain default: `k8zner`).
- Publish workflow `platform-image.yaml` → `ghcr.io/milankappen/k8zner-platform`,
  path-filtered on `web/**`, `internal/api/**`, `cmd/k8zner/**`,
  `Dockerfile.platform`.
- `check-platform-chart` wired into CI next to the existing chart check.

## Non-goals

- OIDC values/config (WP-108 extends the chart). Kind test (WP-007).
- Publishing the chart to OCI (follow-up once versioning is settled; the addon
  installs the embedded copy, so this isn't blocking).

## Acceptance criteria

1. `docker build -f Dockerfile.platform .` succeeds; the image runs as 65532 and
   serves SPA + API in-cluster with only its ServiceAccount.
2. `helm template deploy/helm/k8zner-platform` renders validly with defaults, with
   `host` set, and with ingress disabled.
3. On a cluster with `platform: true` and a domain: platform reachable at
   `https://k8zner.<domain>` with a valid certificate; without a domain: Service
   only, addon still healthy.
4. `make check-platform-chart` passes; drift between copies fails CI.
5. WP-002's health-table row now reflects the real Deployment name.

## Hints

- Bootstrap token: Helm `lookup`-based generate-if-absent (as many charts do) or an
  init path in `serve` that creates it — either way it must survive `helm upgrade`
  without rotating.
- Keep chart values minimal (image, host, resources, token secret name); every knob
  is future API surface.
