# WP-204 — `BackupPolicy` CRD + controller

Lane A · Depends on: WP-202, WP-203 · Blocks: WP-205

## Goal

Automated database backups as a first-class object: schedule, retention, S3
target, database reference — reconciled against the engine chosen by the WP-203
verdict (Databasus API or CNPG Barman). Users get "backups: on, here's the
evidence" without touching the engine.

## Context & anchors

- **Read the WP-203 verdict first** — it selects the reconciliation target and
  this doc's Hints assume you have.
- `Database` CRD + connection Secret contract (WP-202).
- S3 credential conventions: etcd backup path — `BackupSpec`
  (`api/v1alpha1/types.go`), `configureBackup`
  (`internal/operator/provisioning/spec_converter.go`), `internal/platform/s3`;
  reuse the credential Secret shape rather than inventing a new one.
- CRD checklist: `conventions.md` → "CRDs".

## Contract

- `BackupPolicy` spec: `databaseRef` (same-namespace `Database`),
  `schedule` (cron, validated), `retention` (count and/or age),
  `target: {s3: {endpoint, bucket, region?, credentialsSecretRef}}`,
  `suspend: bool`.
- Controller reconciles policy → engine configuration (Databasus API objects OR
  CNPG `barmanObjectStore` + `ScheduledBackup` — per verdict), owns cleanup on
  delete (backup *configuration* removed; existing backup *data* retained —
  documented explicitly).
- Status: `lastBackupTime`, `lastBackupSize`, `lastBackupID`, `consecutiveFailures`;
  conditions: `Configured`, `Healthy` (False after N consecutive failures, with
  Event); printcolumns: database, schedule, last backup, healthy.
- Ad-hoc backup trigger: annotation on the BackupPolicy
  (`k8zner.io/backup-now: <nonce>`) → one immediate backup; the API/UI (WP-205)
  uses this.
- Restore is engine-mediated and **deliberately manual-ish in this WP**: the
  controller exposes enough status (backup IDs) for WP-205's guarded restore
  flow; actual restore endpoint lands with WP-205's backend slice.
- Database deleted while policy exists → policy `Configured=False`, reason
  `DatabaseGone`, no crash-loop.

## Non-goals

- UI (WP-205). PITR beyond what the chosen engine gives by default. Cross-cluster
  restore. Non-S3 targets.

## Acceptance criteria

1. envtest: policy → engine objects/config (golden per the verdict's engine);
   schedule/retention changes propagate; delete removes config, retains data
   semantics verified where testable.
2. Status lifecycle: fake engine reports success → `lastBackupTime` etc. set;
   3 consecutive failures → `Healthy=False` + Event; recovery resets.
3. Cron + retention validation rejects garbage at admission.
4. kind: end-to-end backup of a WP-202 database to in-cluster MinIO (add MinIO as
   test fixture, not addon) — a backup object exists in the bucket.
5. RBAC updated (both chart copies); `make check` + `check-crds` green.

## Hints

- Engine access behind a small interface (`backupengine`) with the func-field
  mock pattern — regardless of verdict, tests shouldn't care which engine.
- If Databasus: its API client lives in `internal/platform/databasus` (mirrors
  cloudflare/s3 platform packages). If Barman: everything is CRD manipulation,
  unstructured like CNPG in WP-202.
