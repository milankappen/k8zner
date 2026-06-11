# ADR-004: K8znerCluster API maturity and the path to v1beta1

## Status

Accepted (plan) — implementation deferred until a second API consumer exists.

## Context

`k8zner.io/v1alpha1` is becoming a contract: the CLI writes it, the operator
reconciles it, and any future client (web UI, GitOps pipelines, additional
CLIs) will read and write it. Several decisions made during v1alpha1 need a
deliberate cleanup before more clients build on the API, because changing
them later multiplies the migration cost.

### What is wrong with v1alpha1

1. **State lives in spec.** `spec.bootstrap` (`BootstrapState`: `completed`,
   `completedAt`, `bootstrapNode`, `bootstrapNodeID`, ...) is observed state
   written by the CLI after bootstrap, not desired state declared by a user.
   This breaks GitOps round-tripping: a cluster manifest checked into git
   (without the bootstrap block) permanently diffs against the live object,
   and ArgoCD-style sync would strip the operator's view of bootstrap
   progress. Kubernetes API conventions are explicit that spec is desired
   state and status is observed state.

2. **`spec.workers.count` is also mutated by external actors** (scaling), but
   that one is legitimate desired state — users edit it. No change needed;
   noted here because it is the counterexample that makes `spec.bootstrap`
   clearly wrong.

### What is already in place (do not redo)

- Standard `metav1.Condition` usage with a top-level `Ready` condition, plus
  `ControlPlaneReady`, `WorkersReady`, `AddonsHealthy`,
  `InfrastructureReady`, `ImageReady`, `Bootstrapped`, and `UpToDate`
  (version-skew detection). Clients should key off conditions, not phases.
- CEL validation: `region` immutable, `credentialsRef` immutable once set,
  DNS-shaped validation for `domain` and subdomains.
- `status.observedGeneration`, phase history, addon version tracking.
- The operator self-applies its CRD at startup, so schema rollout is tied to
  the operator version, not the install method.

## Decision

### v1beta1 shape (the breaking changes worth making, all at once)

1. Move bootstrap state to status: `status.bootstrap` mirrors today's
   `BootstrapState`. The CLI records bootstrap completion via the status
   subresource instead of patching spec.
2. Drop `spec.bootstrap` from v1beta1. During conversion (see below) the
   value is carried over into status.
3. No other field renames: everything else in v1alpha1 has held up.

### Conversion and rollout strategy

We deliberately avoid a conversion webhook (operational cost: serving certs,
availability coupling, upgrade ordering). Instead:

1. v1beta1 is added as a new served version; v1alpha1 stays served and
   remains the **storage** version initially. Schemas are identical except
   for the bootstrap relocation, so round-trip without a webhook is possible
   only if we keep `spec.bootstrap` present-but-deprecated in v1beta1 for one
   release. That is the plan:
   - Release N: introduce v1beta1 (served, not storage) with
     `spec.bootstrap` marked deprecated and `status.bootstrap` authoritative.
     The operator reads both, writes status only. The CLI switches to
     creating v1beta1 objects and writing status.
   - Release N+1: flip storage to v1beta1; run the storage-version migration
     (touch every object once — the operator does this at startup, mirroring
     the CRD self-apply step).
   - Release N+2: drop `spec.bootstrap` from the v1beta1 schema and stop
     serving v1alpha1.
2. The operator's CRD self-apply makes each step a pure operator upgrade; no
   manual kubectl steps for users.

### Compatibility commitments starting now

- New spec fields must be optional with safe defaults; new validation must be
  CEL (no admission webhooks).
- Status is the only side the operator writes; the single existing violation
  (the operator currently flips nothing in spec — only the CLI writes
  `spec.bootstrap`) must not grow.
- Condition types are append-only; reasons are part of the contract for UIs.

## Consequences

- Until v1beta1 lands, GitOps users must omit-and-ignore `spec.bootstrap`
  (e.g. ArgoCD `ignoreDifferences`). Documented limitation.
- The three-release rollout is slower than a webhook conversion but keeps the
  operator dependency-free and single-binary, which matches the project's
  maintainability goals.
- A future multi-user control plane (web UI) builds on v1beta1 conditions
  and the status subresource without any further API rework.
