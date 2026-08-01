# WP-201 — CloudNativePG addon

Lane A · Depends on: — · Blocks: WP-202, WP-203

## Goal

CloudNativePG (CNPG) installable as a standard k8zner addon, giving the cluster a
production-grade PostgreSQL operator. Pure addon-pattern work — the `Database`
CRD that consumes it is WP-202.

## Context & anchors

Standard addon pattern, in full (see `conventions.md` → "Addons"; best example to
copy: `internal/addons/kube_prometheus_stack.go` — another operator-shipping
chart):

1. `internal/addons/helm/registry.go` — `DefaultChartSpecs` entry
   (`cloudnative-pg` chart from `https://cloudnative-pg.github.io/charts`, pinned
   version).
2. `internal/addons/cnpg.go` — `applyCNPG` / `buildCNPGValues` +
   `MergeCustomValues`.
3. `internal/addons/steps.go` — const, `EnabledSteps` (next free Order after
   platform's 11), `InstallStep` case.
4. Config chain (`Monitoring` precedent, as executed in WP-002): `Spec` flag —
   see Contract for naming — `AddonsConfig.CNPG`, `expandCNPG`, `DefaultCNPG`,
   CRD `AddonSpec` bool + `AddonNameCNPG`, both converters.
5. Health row: `checkDeployment("cnpg-system", "cnpg-controller-manager")` (verify
   actual namespace/name from the chart).

## Contract

- User-facing flag: `databases: true` in `k8zner.yaml` (product language — users
  enable "databases", not "cnpg"); internal/CRD naming may say CNPG.
- Addon installs CNPG operator into its own namespace with pinned chart version
  in the registry; ServiceMonitor enabled when monitoring addon is on.
- Round-trip and enable/disable behavior identical in shape to other addons;
  disable leaves existing database clusters untouched (document this — CNPG CRDs
  and instances survive operator removal; removal semantics stay conservative).
- Version matrix note added wherever addon versions are documented
  (`docs/configuration.md`).

## Non-goals

- `Database` CRD (WP-202), backups (WP-203/204), UI (WP-205).
- Non-Postgres engines.

## Acceptance criteria

1. Unit: values builder, steps registration, config round-trip incl. new flag.
2. envtest: `addons.databases: true` → addon step recorded in `Status.Addons`.
3. kind: CNPG operator Deployment Ready; a minimal CNPG `Cluster` fixture comes
   up (1 instance, small PVC via local-path) — proves the operator actually
   functions in the kind env.
4. `make check` + `check-crds` green.

## Hints

- CNPG needs cert-manager? No — it self-manages certs by default; keep defaults.
- Match kind-test storage expectations with whatever storage class `tests/kind`
  provides (check the kind config in the repo).
