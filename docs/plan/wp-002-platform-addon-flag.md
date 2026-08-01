# WP-002 — `platform` addon flag end-to-end

Lane A · Depends on: WP-001 · Blocks: WP-006

## Goal

A user sets `platform: true` in `k8zner.yaml` (or `spec.addons.platform: true` on
the CRD) and the operator installs the k8zner platform (API + UI) into the cluster
as a regular addon. This WP wires the flag through all three config representations
and the addon step machinery; the actual chart comes in WP-006 (until then a
placeholder Deployment is acceptable).

## Context & anchors

The `Monitoring` flag is the exact precedent — trace it end-to-end before coding:

1. `internal/config/spec.go` — `Monitoring bool` on `Spec` + `HasMonitoring()`.
2. `internal/config/addon_types.go` — `AddonsConfig` struct (~line 20), per-addon
   `XConfig` with `Enabled bool` + `Helm HelmChartConfig`.
3. `internal/config/spec_expand.go` — `expandAddons()` (~:160) with per-addon
   helpers like `expandKubePrometheusStack`.
4. `internal/config/addon_defaults.go` — `DefaultX()` constructors.
5. `api/v1alpha1/types.go` — `AddonSpec` (~:228), addon-name consts (~:725),
   order consts; current max Order is 10 (talos-backup).
6. `cmd/k8zner/handlers/cluster_crd.go` (~:225) — `buildAddonSpec(cfg)` maps
   Config → CRD.
7. `internal/operator/provisioning/spec_converter.go` — reverse direction;
   `expandMonitoringFromSpec` at ~:194 is the pattern.
8. `internal/addons/steps.go` — const block (:14–24), `EnabledSteps()` (:34),
   `InstallStep()` switch (:72).
9. `internal/operator/controller/reconcile_addon_health.go` (:38–44) — health table.

## Contract

- `Spec.Platform bool` (`yaml:"platform,omitempty"`) + `HasPlatform()`; expansion
  produces `AddonsConfig.Platform PlatformConfig{Enabled, Helm}`; CRD `AddonSpec`
  gains `Platform bool` (`+optional`); both converters map it; round-trip
  Spec → Config → CRD → Config preserves the value.
- `steps.go`: `StepPlatform` with `Order: 11`, installed by the operator's Addons
  phase when enabled.
- `internal/addons/platform.go`: `applyPlatform(ctx, client, cfg)` — for this WP it
  may install a placeholder (namespace `k8zner-platform` + a minimal Deployment) and
  MUST be structured so WP-006 swaps in the embedded chart without touching steps.go
  again.
- Health table row using `checkDeployment("k8zner-platform", "<deployment-name>")`.
- `make generate manifests sync-crds` output committed (WP-001 tooling).

## Non-goals

- The real Helm chart, image, RBAC (WP-006). Wizard/docs/examples (WP-007).
- Any `internal/api` code (WP-003).

## Acceptance criteria

1. Unit tests: spec round-trip including `platform`, `EnabledSteps` includes/excludes
   the step by flag, `applyPlatform` covered with a func-field mock `k8sclient.Client`.
2. envtest (`-tags=integration`): a `K8znerCluster` with `addons.platform: true`
   reaches the platform addon step and records `Status.Addons["platform"]`.
3. `platform: false` / absent → zero platform resources, no status entry.
4. `make check` green; CRD YAML in all 3 locations updated via `make sync-crds`.

## Hints

- Copy the shape of `internal/addons/kube_prometheus_stack.go` +
  `kube_prometheus_stack_test.go` for file layout and test style.
- Validation: if `platform: true` requires nothing else today, keep `Validate()`
  untouched; when WP-006 adds ingress, it will want `HasDomain()` gating — leave a
  comment breadcrumb, not speculative code.
