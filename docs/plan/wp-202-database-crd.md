# WP-202 — `Database` CRD + controller

Lane A · Depends on: WP-001, WP-201 · Blocks: WP-204, WP-205

## Goal

A platform-level `Database` object: request a PostgreSQL database with a size and
HA choice, get a running CNPG cluster with a stable connection Secret — without
knowing CNPG exists.

## Context & anchors

- CRD checklist: `conventions.md` → "CRDs"; controller precedents WP-101/102.
- CNPG API: `postgresql.cnpg.io/v1` `Cluster` — manipulate as unstructured (avoid
  the dep, mirroring the ArgoCD decision in WP-102).
- S3/backup fields intentionally absent here — WP-204 attaches backups via
  `BackupPolicy`.

## Contract

- `Database` spec: `engine: postgres` (enum, versioned: `major: 16|17|...`
  validated against a supported list), `instances: 1|3` (dev vs HA),
  `storage: {size, storageClass?}`, `resources?`,
  `deletionProtection: bool` (default true).
- Reconciles to a CNPG `Cluster` (owned, name-derived); status maps CNPG phase +
  conditions into `Ready` condition, `primaryEndpoint`, `instances` ready count;
  printcolumns: engine, instances, ready, age.
- Connection Secret with a **stable, documented name** (`<db-name>-connection`):
  host (rw service), port, dbname, user, password, and an assembled `uri` key —
  synthesized/owned by the controller from CNPG's generated secrets so consumers
  are insulated from CNPG's naming.
- `deletionProtection: true` → finalizer refuses deletion until the flag is
  flipped (condition + Event explain it); protection off → deletion cascades to
  the CNPG Cluster (PVCs per CNPG semantics — document what survives).
- Databases addon disabled → `Ready=False`, reason `DatabasesAddonDisabled`,
  actionable message.
- Spec updates: storage growth allowed (CNPG supports volume expansion where the
  storage class does); shrink rejected by validation; instance count changes
  reconciled.

## Non-goals

- Backups (WP-204). Non-Postgres engines. In-place major version upgrades
  (reject the spec change with a message; document the manual path). UI (WP-205).

## Acceptance criteria

1. envtest: Database → CNPG Cluster unstructured object with expected fields
   (golden); connection Secret synthesized once CNPG secrets exist (faked);
   deletion-protection behavior both ways.
2. Validation: unsupported major, storage shrink, bad instances count all
   rejected at admission (CEL markers) — covered by tests.
3. kind: end-to-end `Database` → running 1-instance postgres; `psql` from a test
   pod using only the connection Secret succeeds.
4. RBAC (both chart copies) includes `postgresql.cnpg.io` clusters + secrets;
   `make check` + `check-crds` green.

## Hints

- CNPG phase→condition mapping deserves its own small file + table test.
- Keep the supported-majors list in one place (config or const) — the UI (WP-205)
  and validation both need it; expose it via the API in WP-205's backend slice.
