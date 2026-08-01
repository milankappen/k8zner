# Engineering Conventions for Plan Work Packages

Guardrails for anyone (agent or human) executing a work package. Derived from
`.claude/CODE_STRUCTURE.md`, the CI pipeline, and existing code — read those sources
when in doubt; this file is the distilled, plan-scoped version.

## Go

- Functions < 50 lines; one package, one responsibility; packages sized 3–8 files.
- Interfaces only when there are 2+ implementations (or a test seam is genuinely
  needed — see the hand-written mock pattern below).
- Cobra stays isolated in `cmd/k8zner/commands/` (thin command definitions); all
  logic lives in `cmd/k8zner/handlers/` or `internal/`. The new API server follows
  the same split: `commands/serve.go` is thin, everything real is in `internal/api/`.
- CRD types in `api/v1alpha1/` never import internal packages.
- Errors: wrap with `fmt.Errorf("doing X: %w", err)`; no naked returns of raw errors
  across package boundaries.
- Dual-path rule (ADR-002, `docs/design/adr-002-dual-path.md`): every platform
  feature must be reachable via the CRD/operator path AND visible via CLI/UI. The
  three config representations must round-trip consistently:
  `config.Spec` (user YAML) ↔ `config.Config` (runtime) ↔ CRD `K8znerClusterSpec`.

## Tests

- Table-driven (`tests := []struct{...}` + `t.Run`) with `t.Parallel()` at both
  levels; assertions via testify `assert`/`require`.
- Mocks are **hand-written func-field structs** — see
  `internal/platform/hcloud/mock_client.go` (`CreateServerFunc func(...)` fields +
  `var _ Iface = (*Mock)(nil)`). No mockery, no gomock.
- Build tags: `-tags=integration` for envtest suites
  (`internal/operator/controller/suite_test.go` is the pattern), `-tags=kind` for
  `tests/kind/`, `-tags=e2e` for real-Hetzner tests in `tests/e2e/`.
- Coverage gates (codecov.yml): project 60% / patch 50%. Don't ship untested logic
  and rely on the threshold slack.

## CI must stay green

- `make check` = fmt + golangci-lint (v2 config, `.golangci.yml`) + `go test -race`
  + build. Run it before every push.
- `gofmt -s` clean — CI fails on any diff.
- The `security` job runs **govulncheck + Trivy + gitleaks over every PR**. Every
  new Go or npm dependency passes through these gates:
  - Prefer stdlib and already-present dependencies.
  - Each genuinely new dependency needs a one-line justification in the PR body.
- New job or binary? Update `ci.yaml`'s `test-unit` package list (it is an explicit
  list, NOT `./...`), the `build` matrix if a new binary is released, the
  `ci-success` gate if a new job is added, and `codecov.yml` `ignore` for any new
  `main.go`.

## Frontend (`web/`)

- TypeScript `strict: true`; Vite + React; TanStack Query (server state) + TanStack
  Router; Tailwind + shadcn/ui components.
- API access ONLY through the typed client generated from `api/openapi.yaml` —
  no hand-written `fetch` calls in components. Regenerate the client when the spec
  changes; the spec is the frozen cross-lane contract.
- Component tests with Vitest; every view handles loading / empty / error states.
- The SPA is embedded into the Go binary behind a build tag (see WP-004); never make
  the plain Go build require Node.

## Addons

Adding a Helm-based addon (full pattern, follow an existing example like
`internal/addons/argocd.go`):

1. Chart pin: one entry in `internal/addons/helm/registry.go` `DefaultChartSpecs`.
2. `internal/addons/<name>.go`: `applyX(ctx, client, cfg)` +
   `buildXValues(cfg) helm.Values` ending in
   `helm.MergeCustomValues(values, cfg.Addons.X.Helm.Values)`; install via
   `installHelmAddon(...)` (`internal/addons/apply.go:202`).
3. `internal/addons/steps.go`: 3 edits — `StepX` const, `EnabledSteps()` entry with
   the next free `Order`, `InstallStep()` switch case.
4. Config chain: `internal/config/addon_types.go` (`XConfig` in `AddonsConfig`),
   `addon_defaults.go` (`DefaultX()`), `spec_expand.go` (`expandX`), and — if
   user-facing — a `Spec` bool in `spec.go` (the `Monitoring` flag is the precedent).
5. CRD side: `AddonSpec` bool + `AddonNameX` const in `api/v1alpha1/types.go`;
   mapping in `cmd/k8zner/handlers/cluster_crd.go` (`buildAddonSpec`) and
   `internal/operator/provisioning/spec_converter.go` (`buildAddonsConfig`).
6. Health row in `internal/operator/controller/reconcile_addon_health.go`
   (`checkDeployment` / `checkDaemonSet` / `checkCronJob` helpers).

## CRDs

Adding a CRD kind:

1. Types in `api/v1alpha1/` with kubebuilder markers; register in
   `groupversion_info.go` `init()` — one `SchemeBuilder.Register(...)` line.
2. `make generate && make manifests` (introduced by WP-001; controller-gen pinned).
3. CRD YAML lands in `config/crd/bases/` (canonical) and is synced to
   `deploy/crds/` and `internal/addons/operator-chart/crds/` via `make sync-crds`.
4. Controller in `internal/operator/controller/<kind>_controller.go` with its own
   `SetupWithManager`, registered in `cmd/operator/main.go`.
5. RBAC rules in **both** chart copies (`deploy/helm/k8zner-operator/templates/rbac.yaml`
   and the embedded `internal/addons/operator-chart/` copy — `make sync-operator-chart`).
6. envtest coverage under `-tags=integration`.

## Commits, PRs, docs

- Conventional commits (`feat:`, `fix:`, `docs:`, `chore:` — matches history).
- `CHANGELOG.md` entry for user-visible changes (the release workflow extracts
  release notes from it and fails on an empty section).
- Update your WP's row in `docs/plan/README.md` and, on deviation from the WP spec,
  the WP doc itself (add a `## Deviations` section) — in the same PR.

## Security defaults

- Secrets never in CRD specs — Secret refs only; tokens hashed at rest.
- New HTTP surface: constant-time token compares, HMAC-verified webhooks, CSRF for
  cookie-authenticated SPA routes, no wildcard CORS.
- Containers: distroless, non-root (`USER 65532:65532`), pinned base images —
  `Dockerfile.operator` is the template.
