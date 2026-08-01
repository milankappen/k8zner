# WP-003 — `k8zner serve` subcommand + `internal/api` package

Lane B · Depends on: — · Blocks: WP-004, WP-006, WP-104, WP-108

## Goal

The first HTTP server in the repo: a read-only platform API, shipped **inside the
existing CLI binary** as `k8zner serve`. Decision (do not relitigate): no second
binary — the API server shares `internal/config`, `api/v1alpha1`, and the k8s
client layers with the CLI by construction, and reuses all existing
goreleaser/CI/brew wiring. `commands/serve.go` stays thin; everything real lives in
`internal/api/`, which keeps a later binary split cheap if image size ever demands
one.

## Context & anchors

- CLI split rule: `cmd/k8zner/commands/*.go` are thin cobra defs delegating to
  handlers — see `commands/apply.go` + `handlers/apply.go`. `serve` follows the same
  shape but delegates to `internal/api` (it's a long-running service, not a
  one-shot handler).
- `api/v1alpha1/groupversion_info.go` exposes a package-level `Scheme` (client-go +
  k8zner types) — reuse it for the controller-runtime client.
- There is NO existing HTTP server or web framework dep; only clients. Keep it that
  way: stdlib `net/http` with Go 1.22+ method/wildcard routing (`GET /api/v1/...`).
- Kubeconfig loading precedents: the CLI writes `secrets/<cluster>/kubeconfig`; the
  TUI polls the CRD via a controller-runtime client (`internal/ui/tui/apply.go`).
- CI: `.github/workflows/ci.yaml` `test-unit` uses an explicit package list — new
  packages must be added there. `codecov.yml` flag paths already cover `internal/`.

## Contract

- `k8zner serve [--addr :8420] [--kubeconfig path]` runs the server in two modes
  from one code path:
  - **local**: kubeconfig from flag / `$KUBECONFIG` / default discovery — instant
    dashboard against any cluster, nothing installed;
  - **in-cluster**: `rest.InClusterConfig()` when no kubeconfig is resolvable.
- `api/openapi.yaml` (OpenAPI v3) is authored **design-first** in this WP and is the
  frozen cross-lane contract for WP-004/005/107. Endpoints:
  - `GET /healthz`, `GET /readyz` (no auth)
  - `GET /api/v1/cluster` — name, phase, conditions, versions, health summary from
    `K8znerClusterStatus`
  - `GET /api/v1/nodes` — node inventory (role, status, addresses, versions)
  - `GET /api/v1/addons` — per-addon status/health from `Status.Addons`
  - `GET /api/v1/workloads` — read-only discovery: Deployments, StatefulSets,
    Ingresses, HTTPRoutes across namespaces (system namespaces filterable)
- Every response type carries the cluster identity (fleet guardrail in `epics.md`) —
  handlers may default to the single `K8znerCluster`, types must not assume it.
- Auth middleware: `Authorization: Bearer <token>` checked with
  `crypto/subtle.ConstantTimeCompare` against a bootstrap token (in-cluster: from a
  Secret; local: from a flag/env or generated-and-printed on startup). `/healthz`,
  `/readyz` exempt. Real OIDC lands in WP-108 behind the same middleware seam.
- Graceful shutdown on SIGINT/SIGTERM; structured logging consistent with the repo.
- Static file serving seam for WP-004: a `http.Handler` slot that this WP fills with
  a minimal "UI not built in" page.

## Non-goals

- The SPA and embedding (WP-004). OIDC/sessions/API tokens (WP-108).
- Any write/mutating endpoint. Metrics/log streaming (WP-107 adds SSE).
- No Dockerfile/chart (WP-006).

## Acceptance criteria

1. `k8zner serve` against a kubeconfig for a cluster with a `K8znerCluster` CR
   answers all four `/api/v1/*` endpoints with data matching the CR; against a
   cluster without one, `/api/v1/cluster` returns 404 with a typed error body.
2. Requests without/with-wrong bearer token → 401; correct token → 200. Verified in
   handler tests using a fake controller-runtime client
   (`sigs.k8s.io/controller-runtime/pkg/client/fake`).
3. `api/openapi.yaml` validates (any standard OpenAPI validator) and matches the
   implemented routes — add a test that walks the mux and asserts every route
   exists in the spec.
4. `make check` green; `internal/api/...` added to ci.yaml `test-unit` list.

## Hints

- Package layout suggestion (not a contract): `internal/api/server.go` (mux, mw,
  lifecycle), `internal/api/handlers_*.go`, `internal/api/types.go` (response DTOs —
  do NOT expose CRD structs raw; map them), `internal/api/auth.go`.
- The workloads discovery client needs list permissions cluster-wide; in local mode
  that's the user's kubeconfig, in-cluster it's WP-006's ClusterRole — return a
  clear 403-derived error body when RBAC denies, don't 500.
- Version the DTOs from day one (`/api/v1/`); breaking DTO changes require an
  OpenAPI diff in the PR description.
