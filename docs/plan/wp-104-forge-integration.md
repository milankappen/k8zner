# WP-104 — Forge integration (GitHub App / GitLab OAuth + webhooks)

Lane B · Depends on: WP-003, WP-101 · Blocks: WP-106

## Goal

Connect the platform to GitHub and GitLab: an operator of the platform links a
forge, picks repositories, and k8zner creates the corresponding `GitSource` CRs,
stores tokens as Secrets, and receives HMAC-verified webhooks. This is the bridge
between "UI/API" and "Git-driven deploys".

## Context & anchors

- `internal/api` server structure + auth middleware (WP-003); OpenAPI contract in
  `api/openapi.yaml` — this WP extends it.
- `GitSource` CRD + forge client seam from WP-101
  (`internal/platform/forge/` if WP-101 followed the hint).
- Secret conventions: `conventions.md` Security defaults; existing Secret helpers
  `internal/addons/k8sclient/secret.go`.

## Contract

- **GitHub**: GitHub App flow (installation-based, fine-grained repo access,
  webhook delivery built in) — App credentials (app ID, private key) supplied via
  Secret/config, per-installation tokens minted on demand, never persisted beyond
  their TTL. **GitLab**: OAuth app flow (gitlab.com and self-managed base URL
  support) with token refresh.
- New endpoints (all in `api/openapi.yaml`):
  - `GET /api/v1/forges` — configured forges + connection status
  - `POST /api/v1/forges/{forge}/connect` → begin flow; callback endpoint
    completes it
  - `GET /api/v1/forges/{forge}/repos` — repos visible to the connection
  - `POST /api/v1/gitsources` / `GET /api/v1/gitsources` — create from a selected
    repo (server creates the CR + webhook at the forge with a generated secret,
    stored via `webhookSecretRef`), list with status
  - `POST /webhooks/{forge}` — receiver: HMAC verification (`X-Hub-Signature-256`
    / `X-Gitlab-Token`) against the GitSource's webhook secret, constant-time;
    unverified → 401, unknown source → 404; verified events are dispatched
    internally (push, PR opened/closed) — consumers arrive in WP-105/106, so this
    WP ends at a typed event dispatch seam with logging.
- Tokens/credentials only in Secrets; API responses never echo them. Webhook
  receiver is auth-middleware-exempt (its auth IS the HMAC) but rate-limited.
- CSRF-safe flows (state parameter on OAuth, one-time nonces).

## Non-goals

- Acting on webhook events (WP-105 refresh, WP-106 previews). UI (WP-107).
  Bitbucket/Gitea (structure for them, don't build).

## Acceptance criteria

1. Handler tests with mocked forge clients: connect flow state validation, repo
   listing, GitSource creation incl. webhook registration call, callback error
   paths.
2. Webhook receiver: golden-payload tests for GitHub + GitLab (valid signature →
   202 + dispatched event; tampered body → 401; replayed delivery id → deduped).
3. A GitSource created via API reaches `Validated=True` end-to-end in envtest with
   the fake forge.
4. OpenAPI spec extended + regenerated client compiles (Lane C consumes in
   WP-107); `make check` green; new deps justified.

## Hints

- Keep forge specifics behind the WP-101 interface; `internal/api` should not
  import go-github/gitlab clients directly.
- Store the GitHub App vs OAuth distinction in the forge config model now — it
  determines token semantics everywhere later.
- Webhook dedup: forges resend; a small TTL cache of delivery IDs is enough.
