# WP-102 — `Application` CRD + controller (image deploys)

Lane A · Depends on: WP-001 · Blocks: WP-103, WP-105, WP-107

## Goal

The central PaaS object: a user describes *what to run* and the platform runs it.
This WP covers prebuilt-image deploys reconciled through ArgoCD; repo sources come
in WP-105, routes in WP-103. This is the "Coolify moment" foundation — get the
spec shape right, it is the product's main API.

## Context & anchors

- CRD checklist: `conventions.md` → "CRDs".
- ArgoCD is installed by default (`internal/addons/argocd.go`, namespace `argocd`);
  the controller manipulates `argoproj.io/v1alpha1` Application objects as
  unstructured or via the ArgoCD types package (dep-weight decision — see Hints).
- Fleet guardrail (`epics.md`): `clusterRef` optional-with-default from day one.
- Status aggregation pattern: `updateStatusWithRetry`, conditions + Events as in
  the cluster controller.

## Contract

- `Application` spec (v1alpha1, expect iteration — favor small + extensible):
  - `source`: union, this WP implements `image: {repository, tag}` only; the
    union is structured so `git: {gitSourceRef, path, revision, type}` (WP-105)
    slots in without breaking changes.
  - `env`: list of `{name, value | secretRef}`; secrets by reference only.
  - `resources` (k8s ResourceRequirements), `replicas` or
    `autoscale: {min,max,targetCPU}` (mutually exclusive, validated).
  - `port` (container port the Service targets).
  - `routes`: `[]{host, path}` — **schema defined here, reconciled in WP-103**;
    until then routes get a `RoutesPending` condition, not silent ignore.
  - `previews: bool` — schema only, wired in WP-106.
  - `clusterRef`: optional, defaults to the single K8znerCluster.
- Controller reconciles to: target namespace (default: Application's namespace),
  generated Deployment+Service manifests delivered **via an ArgoCD Application**
  (ArgoCD is the delivery engine but an implementation detail — users never touch
  the ArgoCD CR; label it `app.kubernetes.io/managed-by: k8zner`).
- Status: conditions (`Ready`, `Synced`, `Progressing`) aggregated from ArgoCD
  sync/health + Deployment status; `observedGeneration`; printcolumns
  (image, ready, synced, age).
- ArgoCD disabled/absent → `Ready=False` with reason `ArgoCDUnavailable`, clear
  message, no crash-looping.
- Deletion: finalizer removes the ArgoCD Application (cascade) so workloads are
  cleaned up.

## Non-goals

- Routes/TLS/DNS reconciliation (WP-103). Git sources (WP-105). Previews (WP-106).
  Build-from-source (explicitly out of Phase 1 entirely; vision §3.6).

## Acceptance criteria

1. envtest: Application with an image → ArgoCD Application CR exists with expected
   generated manifests; spec change (tag bump) → ArgoCD app updated;
   delete → ArgoCD app gone.
2. Status conditions verified for: happy path, ArgoCD missing, invalid spec
   (replicas+autoscale both set rejected by CEL/webhook-free validation markers).
3. Env secretRef renders as `valueFrom.secretKeyRef` — never resolved/inlined.
4. `make check` + `check-crds` green; RBAC for `applications` +
   `argoproj.io/applications` added in both chart copies.

## Hints

- Manifest delivery: simplest robust option is rendering manifests into the ArgoCD
  Application's inline source is not possible — use a ConfigMap-backed or
  CMP-free approach: generate manifests and store them where ArgoCD can sync from.
  Two workable designs: (a) ArgoCD Application pointing at a k8zner-owned in-cluster
  manifest store, or (b) skip ArgoCD for image-only deploys (operator applies
  Deployment/Service directly via server-side apply, `k8sclient.ApplyManifests`)
  and introduce ArgoCD when sources are git (WP-105). Option (b) is less moving
  parts now and the vision only demands ArgoCD stay hidden — pick one, write a
  short rationale in this doc's Deviations section.
- Use unstructured for ArgoCD objects to avoid importing the argoproj module (it's
  heavy); a small typed helper wrapper keeps call sites clean.
