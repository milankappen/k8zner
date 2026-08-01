# WP-106 — Branch preview environments

Lane A · Depends on: WP-103, WP-104, WP-105 · Blocks: —

## Goal

`previews: true` on a git-sourced Application gives every open pull request its
own isolated environment at `pr-<N>.<app-host>`, created on PR open and destroyed
on PR close — the flagship branch-based-deployment feature.

## Context & anchors

- ArgoCD ApplicationSet with the Pull Request generator is the engine (ArgoCD
  addon already installed; ApplicationSet controller ships with the chart — verify
  the deployed values enable it, fix the addon values if not:
  `internal/addons/argocd.go`).
- Host/cert/DNS helpers from WP-103 (`pr-N.` prefixing must compose).
- Webhook events (PR opened/closed) from WP-104's dispatch seam; forge API
  credentials via GitSource (WP-101).
- ArgoCD translation layer from WP-105 (`application_argocd.go`-style file).

## Contract

- `previews: true` (git-sourced apps only — validated) → the controller manages an
  ApplicationSet: PR generator against the GitSource repo (forge-appropriate
  generator config + credentials), template deploying the PR's head revision into
  namespace `<app>-pr-<number>`, route at `pr-<number>.<primary-route-host>` with
  TLS/DNS via the WP-103 helpers.
- Resource guardrails: preview namespaces get a default ResourceQuota (values from
  the Application spec: `previews` may be upgraded from bool to
  `{enabled, quota?}` — keep bool compatibility via CRD defaulting).
- Teardown: PR close/merge removes the generated Application and its namespace
  (generator handles removal; controller adds a periodic GC sweep for orphaned
  `*-pr-*` namespaces as belt-and-braces, logged and Evented).
- Status: `status.previews` list (PR number, revision, URL, health) for WP-107 UI.
- PR webhook events trigger ApplicationSet refresh so previews appear within
  seconds, not on poll.

## Non-goals

- Preview comments back on the PR (nice follow-up, needs forge write scope —
  note as future work). Previews for image-sourced apps. Per-preview databases.

## Acceptance criteria

1. envtest: `previews: true` produces the expected ApplicationSet (golden per
   forge); toggling off removes it and existing preview namespaces.
2. GC: an orphaned preview namespace older than the TTL with no matching open-PR
   Application is removed (fake clock / short TTL in test).
3. Quota object present in generated preview namespaces.
4. kind (extends WP-007 layer or new layer): ApplicationSet materializes an
   Application for a simulated PR (generator against a fixture repo or mocked
   forge API — document the approach taken).

## Hints

- The PR generator needs forge API access with different token semantics for
  GitHub App vs OAuth (WP-104 stored the distinction) — mint/rotate the Secret
  ArgoCD consumes rather than handing it long-lived credentials.
- Namespace names: sanitize app names for the `<app>-pr-<n>` pattern (length,
  charset) with the existing naming helpers in `internal/util/naming`.
