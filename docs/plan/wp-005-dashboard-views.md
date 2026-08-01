# WP-005 — Dashboard views (read-only)

Lane C · Depends on: WP-004 · Blocks: WP-007, WP-107

## Goal

Fill the SPA shell with the actual read-only dashboard: cluster overview, nodes,
addons, and discovered workloads. After this WP, `platform: true` (or a local
`k8zner serve`) delivers a genuinely useful "what is my cluster doing" view — the
Phase 0 user-facing payoff.

## Context & anchors

- Data source: the typed client for `/api/v1/cluster|nodes|addons|workloads`
  (WP-003 contract).
- Field inventory for the overview: `internal/ui/tui/model.go` and `view.go` — the
  TUI already renders phases, node status, addon status from the CRD; the dashboard
  should show at least what the TUI shows.
- Phase/state vocabulary: `api/v1alpha1/types.go` (`ProvisioningPhase`,
  `ClusterPhase`, `NodePhase`, `AddonPhase`) — render these as user-comprehensible
  states, not raw strings.
- Design guardrails: `conventions.md` Frontend section (loading/empty/error states
  are mandatory per view).

## Contract

- **Overview**: cluster name/region, provisioning + cluster phase with a visual
  state (progress for provisioning phases), health summary, k8s/Talos versions,
  counts (nodes ready/total, addons healthy/total), condition list with timestamps.
- **Nodes**: table of nodes (role, phase, addresses, versions, age); degraded
  states visually distinct.
- **Addons**: table from `Status.Addons` (phase, health, message, retries);
  deep-links to Grafana/Hubble/ArgoCD when the corresponding addon is enabled and
  a domain is configured (hosts derivable from the cluster response — extend the
  WP-003 DTO if a field is missing, updating `api/openapi.yaml` + client in the
  same PR per the contract-change rule).
- **Workloads**: grouped by namespace; kind, name, ready/desired, images, linked
  routes (Ingress/HTTPRoute hosts as clickable links); system namespaces
  collapsed by default.
- Data fetching via TanStack Query with polling (default ~5s, paused on hidden
  tab); every view implements loading / empty / error (including 401 → token
  prompt from WP-004).
- Vitest component tests per view against mocked client responses, covering the
  three states plus a populated render.

## Non-goals

- Any mutation (scale, restart, edit). Log viewing (WP-107). Metrics charts —
  Grafana links suffice in Phase 0.

## Acceptance criteria

1. Against a live cluster (or `k8zner serve` + fake/fixture backend), all four
   views render real data; a mid-provisioning cluster shows sensible phase
   progress rather than errors.
2. Kill the API mid-session → views degrade to error state and recover when it
   returns (Query retry), no white screens.
3. Vitest suite covers each view's states; `web-ci` green.
4. Lighthouse-level sanity: initial JS bundle < 500 KB gzipped (guard against
   accidental heavyweight deps).

## Hints

- Reuse the phase→(label, tone) mapping in one module shared by all views; the
  TUI's `styles.go` shows which states exist and how they're grouped.
- shadcn/ui `Table`, `Card`, `Badge` cover almost everything here; resist custom
  CSS beyond Tailwind utilities.
