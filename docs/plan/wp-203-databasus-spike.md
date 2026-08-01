# WP-203 — Databasus validation spike (time-boxed, go/no-go)

Lane A · Depends on: WP-201 · Blocks: WP-204

## Goal

Decide, with evidence, whether Databasus is the backup engine behind
`BackupPolicy` (WP-204) or whether we fall back to CNPG-native Barman/S3 backups
with Databasus as optional UI only. The vision (§3.3) names this exact fallback;
this spike converts the open question into a written verdict. **The deliverable
is a decision document, not production code.**

## Context & anchors

- Vision: `docs/design/platform-vision.md` §3.3 (`BackupPolicy` reconciled against
  the Databasus API) and §7 risk "Databasus maturity".
- WP-204 defines what the winner must support — read its Contract first; the
  spike evaluates against those needs.
- Existing S3 conventions: etcd backup config (`BackupSpec` in
  `api/v1alpha1/types.go`, `configureBackup` in
  `internal/operator/provisioning/spec_converter.go`, `internal/platform/s3`).

## Contract

Time-box: one focused session. Produce `## Verdict` in THIS file (living doc)
answering, with evidence (commands run, API calls, versions):

1. **Deployability**: Does Databasus deploy cleanly in-cluster (Helm/manifests?
   version pinning? resource footprint? license compatible with Apache-2.0
   distribution as an addon?).
2. **Headless API**: Can ALL of the following be driven purely via API, no UI
   click-ops: register a Postgres target (from a connection Secret), define a
   scheduled backup to S3-compatible storage, trigger an ad-hoc backup, list
   backups + sizes + status, trigger a restore, delete/retention?
3. **Auth & multi-tenancy**: API auth mechanism automatable (static token /
   service account)? Can k8zner own the instance fully (no manual bootstrap)?
4. **Observability**: Status/webhook/metrics surface adequate for `BackupPolicy`
   conditions (lastBackupTime, failures) without scraping HTML?
5. **Restore semantics**: Restore into the same CNPG cluster / a new one —
   compatible with how CNPG manages PGDATA? (This is the likeliest dealbreaker:
   an agent-based tool that assumes it owns the Postgres process may fight the
   CNPG operator.)

Verdict: **GO** (WP-204 targets Databasus; note API endpoints + deploy method) or
**NO-GO** (WP-204 targets CNPG Barman `barmanObjectStore` + `ScheduledBackup`;
Databasus optionally revisited later as UI-only). Partial results count: a
qualified GO must list the gaps and how WP-204 works around them. Also update
vision §3.3 to state the outcome.

## Non-goals

- Any `BackupPolicy` implementation. Polishing the spike deployment. Benchmarks.

## Acceptance criteria

1. `## Verdict` section in this file with the 5 questions answered + evidence.
2. Vision doc §3.3 updated to reflect the decision.
3. WP-204's Context section updated to point at the chosen engine (one-line edit).
4. Spike artifacts (manifests/scripts) in `docs/plan/spike-databasus/` if useful
   for WP-204, clearly marked non-production.

## Hints

- Test against a CNPG cluster from WP-201's kind fixture — that IS the target
  environment; a spike against a standalone Postgres proves too little.
- If Databasus turns out to be dump-based (pg_dump-style) rather than
  WAL/PITR-based, weigh that honestly: dump-based is simpler and fine for the
  target audience's typical DB sizes, but say so in the verdict so the product
  claim ("automated backups") is described accurately in docs.
