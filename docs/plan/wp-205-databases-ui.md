# WP-205 — Databases & backups UI

Lane C · Depends on: WP-107, WP-202, WP-204 · Blocks: — (closes Phase 2)

## Goal

Databases and their backups in the browser: create a database, see its health and
connection info, configure backup policies, browse backup history, and run a
deliberately-guarded restore. Closes Phase 2.

## Context & anchors

- UI conventions + patterns established in WP-005/107 (list/detail/wizard/status
  shapes — reuse, don't reinvent).
- CRDs: `Database` (WP-202 — supported-majors list, connection Secret contract),
  `BackupPolicy` (WP-204 — status fields, backup-now annotation, restore
  mediation).
- `api/openapi.yaml` extended here with database endpoints (backend slice in
  `internal/api`, same auth/audit rules as WP-107's mutating endpoints).

## Contract

- **Backend additions** (spec-first):
  - `GET/POST /api/v1/databases`, `GET/PUT/DELETE /api/v1/databases/{ns}/{name}`
  - `GET /api/v1/databases/{ns}/{name}/connection` — connection details;
    password/uri fields returned **only** by this dedicated endpoint (audited),
    never embedded in list/detail responses
  - `GET/POST/PUT/DELETE /api/v1/backuppolicies...`;
    `POST .../backup-now` (sets the WP-204 annotation)
  - `GET .../backups` — backup history from engine status
  - `POST .../restore` — restore a named backup; requires a confirmation field
    echoing the database name; audited; surfaces engine progress/status
  - `GET /api/v1/databases/options` — supported engines/majors/instance presets
    (from the WP-202 single source of truth)
- **UI**:
  - Databases list (engine, version, instances ready, backup health rollup) +
    detail (conditions, storage, resources, backup policy summary).
  - Connection info panel: hidden by default, reveal-on-click per field, copy
    buttons, auto-hide; reveal calls the dedicated endpoint.
  - Create wizard: engine/major/size/HA presets (dev: 1 instance, prod: 3) from
    the options endpoint; deletion-protection toggle default on.
  - Backup policy editor (schedule with human-readable cron helper, retention,
    S3 target with credential secret picker); backup history table (time, size,
    status); "Back up now" button.
  - Restore flow: pick backup → typed confirmation of the database name →
    progress → outcome. Destructive framing throughout (this overwrites data).
  - Delete flow honors deletion protection: UI explains the flag and requires
    flipping it first — two deliberate steps, never one.
- Vitest coverage per established conventions (states + wizard/restore guards).

## Non-goals

- Query console / table browser. Metrics dashboards (Grafana links suffice).
  Per-preview databases. User management of engine internals.

## Acceptance criteria

1. Full loop on a dev cluster: create DB in UI → Ready → reveal connection →
   create policy against MinIO → backup-now → history shows it → restore with
   confirmation succeeds. Documented in the PR.
2. Secrets hygiene verified in handler tests: list/detail responses contain no
   credential material; the connection endpoint requires auth and is audit-logged.
3. Restore/delete guards covered by component tests (button disabled until typed
   name matches; protected delete blocked with explanation).
4. OpenAPI + client freshness checks green; `make check` green.

## Hints

- The backup health rollup on the list view is the feature users actually watch —
  make `Healthy=False` loud (it means backups are silently failing somewhere).
- Reuse WP-107's SSE/log patterns only if restore progress needs streaming;
  polling the policy/restore status is likely sufficient — don't add machinery.
