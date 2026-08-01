# WP-007 — Phase 0 integration test, wizard toggle, docs

Lane A · Depends on: WP-005, WP-006 · Blocks: Phase 1 rollout confidence

## Goal

Close Phase 0: prove the platform addon end-to-end in the kind test suite, expose
the flag in the interactive wizard, and document the feature. Phase 1 WPs should
not start user-facing work until this is done (the plan's stop-anywhere property).

## Context & anchors

- Kind test framework: `tests/kind/` (`framework.go`, `assert.go`, `wait.go`,
  `kubectl.go`), layered subtests `TestKindAddons/01_CRDs` … `06_Integration`,
  build tag `kind`, Makefile `test-kind`, CI jobs `kind-smoke` / `kind-tests`.
- Wizard: `internal/config/spec_wizard.go` (charmbracelet/huh) — see how the
  monitoring/backup toggles are asked and mapped in `(*WizardResult).ToSpec()`.
- Docs layout: `docs/README.md` index, `docs/configuration.md` (flag reference),
  `examples/*.yaml`.
- CHANGELOG convention: release workflow extracts notes per version section.

## Contract

- Kind layer test (new numbered layer or extension of an existing one) asserting:
  platform Deployment Ready; `GET /api/v1/cluster` through the Service (port-forward
  or in-cluster curl pod) returns 200 with the bootstrap token and 401 without;
  response cluster name matches the CR.
- Wizard: a "Platform dashboard" confirm toggle wired to `Spec.Platform`.
- Docs: new `docs/platform.md` (what it is, `platform: true`, local
  `k8zner serve` mode, bootstrap token retrieval, ingress/host behavior, security
  notes); `docs/configuration.md` flag row; `examples/` gains/updates a config
  showing `platform: true`; `README.md` optional-features table row; CHANGELOG
  entry.

## Non-goals

- e2e (real Hetzner) coverage — kind is sufficient for Phase 0; e2e picks it up
  when Phase 1 lands.
- Screenshots/marketing.

## Acceptance criteria

1. `make test-kind` (or the targeted layer) passes locally and in the `kind-tests`
   CI job with the platform layer included; `kind-smoke` stays within its current
   runtime budget (add the layer only to the full suite if smoke would slow).
2. `k8zner init` wizard run shows the toggle; resulting YAML round-trips through
   `LoadSpec` + `Validate`.
3. All new docs pages linked from `docs/README.md`; flags documented match
   implementation exactly.

## Hints

- The kind suite runs without Hetzner credentials — the platform addon must not
  require any (it only reads the CRD and cluster objects; it does not touch
  hcloud). If WP-003/006 accidentally introduced an HCLOUD_TOKEN dependency in
  serve, that's a bug to fix here.
- Follow `tests/kind/diagnostics.go` to dump platform pod logs on failure.
