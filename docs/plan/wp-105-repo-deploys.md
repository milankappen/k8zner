# WP-105 — Repo-based deploys (manifests / Helm / Kustomize)

Lane A · Depends on: WP-101, WP-102 · Blocks: WP-106

## Goal

Applications deployed from a Git repository, not just an image:
`source.git: {gitSourceRef, path, revision, type}` mapped onto ArgoCD source
types, with webhook-triggered refresh so pushes deploy without waiting for
ArgoCD's poll interval.

## Context & anchors

- `Application` CRD source union prepared in WP-102 (this WP fills the `git`
  branch); the WP-102 Deviations section records how manifests are delivered —
  read it first, it determines how much of this WP is "switch to ArgoCD" vs
  "extend ArgoCD usage".
- `GitSource` credentials (WP-101): ArgoCD needs repo credentials — it has its own
  repo-credential Secret format (`argocd.argoproj.io/secret-type: repository`).
- Webhook dispatch seam from WP-104 (`push` events carry repo + branch).

## Contract

- `source.git`: `gitSourceRef` (same-namespace GitSource), `path`, `revision`
  (branch/tag/SHA; default: GitSource's `defaultBranch`), `type`
  (`manifests|helm|kustomize`; `helm` gets optional `valuesFiles`/inline
  `values`). Exactly one of `source.image` / `source.git` — validated.
- Controller renders the corresponding ArgoCD Application source (directory, helm,
  kustomize) and materializes repo credentials from the GitSource secret into the
  ArgoCD repo-credential format (owned, synced on change).
- Push webhook for a matching repo+branch → targeted refresh of the affected
  Applications (annotation-based ArgoCD refresh), not a blanket sync of everything.
- Status: `revision` deployed (SHA), `lastSyncedAt`, sync errors surfaced in
  conditions with ArgoCD's message.
- Routes/env/resources from WP-102/103 apply identically to git-sourced apps
  (routes attach to whatever Service the manifests expose — see Hints).

## Non-goals

- Building images from source (out of Phase 1 entirely). Previews (WP-106).
  Multi-source ArgoCD apps.

## Acceptance criteria

1. envtest: git-sourced Application (fake GitSource) produces an ArgoCD
   Application with correct source block per type (3 golden cases) and repo
   credentials Secret.
2. Webhook push event → exactly the matching Applications get the refresh
   annotation (test via the dispatch seam with 2 apps on different branches).
3. Invalid combos rejected: both sources set, gitSourceRef missing, unknown type.
4. `make check` green; CRD regenerated + synced.

## Hints

- For `type: manifests|kustomize`, k8zner doesn't know which Service the repo
  creates — `routes[].serviceName` (optional, defaulting to the app name) solves
  it; add the field here since this WP creates the ambiguity, and note it in the
  WP-102 schema docs.
- Keep the ArgoCD-facing translation in one file (`application_argocd.go`-style)
  — WP-106 reuses it under an ApplicationSet.
