# WP-107 — Applications UI

Lane C · Depends on: WP-005, WP-102 (WP-104/105/106 features appear as their
backends land) · Blocks: WP-205

## Goal

The application lifecycle in the browser: list, inspect, create, and edit
Applications; watch deploys; read logs. Built incrementally against the Phase 1
backend WPs — ship list/detail/create for image-sourced apps as soon as WP-102 is
done, then layer git sources, forge connect, and previews as those land.

## Context & anchors

- Typed client + view conventions from WP-004/005; `api/openapi.yaml` is extended
  by this WP for Application/GitSource/forge endpoints (coordinate: WP-104 defines
  forge endpoints; this WP defines Application CRUD endpoints in `internal/api`).
- CRD schemas: `api/v1alpha1` Application/GitSource types (WP-101/102) — DTOs map
  from them, never expose raw CRDs.
- SSE precedent: none yet — this WP introduces the first streaming endpoint.

## Contract

- **Backend additions** (`internal/api`, spec-first):
  - Application CRUD: `GET/POST /api/v1/applications`,
    `GET/PUT/DELETE /api/v1/applications/{ns}/{name}` — server validates and
    writes the CRD (this is the first mutating API surface: gate it behind the
    auth middleware and Events/audit-log each mutation).
  - `GET /api/v1/applications/{ns}/{name}/logs?container=&follow=` — SSE stream
    proxying pod logs (k8s `GetLogs`), multi-pod merged with pod-name prefixes,
    follow + last-N.
- **UI**:
  - List: status-at-a-glance (Ready/Synced badges, source, routes), filter by
    namespace/status.
  - Detail: conditions timeline, revision info, per-route links (from
    `status.routes`), preview environments panel (from `status.previews`,
    appears with WP-106), log viewer (SSE, container picker, pause/clear).
  - Create/edit wizard: image or git source (git tab enabled when GitSources
    exist; links to forge connect flow from WP-104), env editor (secret refs
    selectable, values write-only — never display secret values), resources,
    routes, previews toggle.
  - GitSources page: list + create (repo picker via forge endpoints), status.
- Optimistic UX: mutations show pending state and reconcile-driven status is
  polled; deletes require typed confirmation of the app name.

## Non-goals

- RBAC-scoped visibility (Phase 3 epic). Metrics charts. Exec/shell into pods
  (explicitly out — security review needed first).

## Acceptance criteria

1. Full loop in a dev cluster: create image app in UI → Ready → route link works →
   edit tag → rollout visible → delete → gone. (Manual verification documented in
   PR with the steps.)
2. Vitest: wizard validation (image/git exclusivity, route host rules mirrored
   from CRD validation), list/detail states, log viewer against a mocked SSE
   source.
3. Backend handler tests: CRUD happy/invalid paths, log SSE endpoint streams and
   terminates cleanly, mutations rejected without auth.
4. OpenAPI spec + regenerated client in sync (CI freshness check passes).

## Hints

- Mirror CRD validation rules client-side for instant feedback but treat the
  server as authoritative — surface its structured errors in the form.
- SSE over websockets: SSE is sufficient for logs, works through the existing
  auth middleware (token via `Authorization` header using fetch-based EventSource
  polyfill or query-token — prefer header; document the choice).
