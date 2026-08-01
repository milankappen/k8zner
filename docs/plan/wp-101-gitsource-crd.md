# WP-101 — `GitSource` CRD + controller

Lane A · Depends on: WP-001 · Blocks: WP-104, WP-105

## Goal

A namespaced CRD representing a connected Git repository (GitHub or GitLab):
where the code lives, how to authenticate, and how webhooks are verified. The
controller validates connectivity and keeps status truthful. This is the anchor
object for repo-based deploys (WP-105) and forge webhooks (WP-104).

## Context & anchors

- CRD checklist: `conventions.md` → "CRDs" (register in
  `api/v1alpha1/groupversion_info.go` init; `make generate manifests sync-crds`;
  RBAC in both chart copies; controller registered in `cmd/operator/main.go`
  ~:82–93).
- Reconciler shape: `internal/operator/controller/cluster_controller.go` —
  functional options, `Recorder`, `updateStatusWithRetry` pattern; keep the new
  controller a **separate small type**, not more methods on `ClusterReconciler`.
- envtest: `internal/operator/controller/suite_test.go` (`-tags=integration`).
- Secret-handling precedent: credentials referenced, never inlined — see
  `CredentialsRef` on `K8znerClusterSpec`.

## Contract

- `GitSource` spec: `forge` (enum `github|gitlab`), `url` (repo HTTPS URL),
  `credentialsSecretRef` (token for API+clone; optional for public repos),
  `webhookSecretRef` (optional until WP-104 populates it). Status: conditions
  (`Validated`, `Connected`), `defaultBranch`, `lastValidated`; printcolumns for
  forge/url/validated.
- Controller: on reconcile, verifies the repo is reachable with the given
  credentials (forge API call or `git ls-remote`-equivalent via HTTP), sets
  conditions + `defaultBranch`, requeues on transient failure with backoff, emits
  Events on state change. Secret changes trigger re-validation (watch on
  referenced Secrets or periodic resync — document the choice).
- No secret material ever copied into status or logs.
- RBAC added for `gitsources` (+status/finalizers) and Secret reads in both chart
  copies.

## Non-goals

- Creating webhooks at the forge, OAuth flows (WP-104). Deployment semantics
  (WP-105). UI (WP-107).

## Acceptance criteria

1. envtest: create a GitSource with a fake validator seam → conditions transition
   Unknown → True; break the validator → False with reason; Events recorded.
2. Unit tests for the forge validation client with a func-field mock (no real
   network in tests).
3. Public-repo case works with no credentials secret.
4. `make check` + generated artifacts in sync (`check-crds` green).

## Hints

- Put forge API access behind a tiny interface in e.g.
  `internal/platform/forge/` (github/gitlab implementations) — WP-104 will reuse
  it from `internal/api`; two consumers justify the interface per conventions.
- New deps: `google/go-github` and `gitlab.com/gitlab-org/api/client-go` are the
  standard choices; alternatively raw REST for the two calls needed here and
  decide the dep question in WP-104 where usage grows. Either way, justify in PR.
