# WP-108 — Real auth: OIDC + API tokens

Lane B · Depends on: WP-003 · Blocks: Phase 3 epic (teams/projects)

## Goal

Replace bootstrap-token-only auth with real identity: OIDC login against a
bring-your-own IdP for humans, long-lived hashed API tokens for automation, and
the bootstrap token demoted to break-glass. This is the identity foundation the
teams/projects epic builds on — identity here, authorization stays Kubernetes
RBAC (see `epics.md` guardrail).

## Context & anchors

- Auth middleware seam from WP-003 (`internal/api/auth.go` or equivalent) — this
  WP turns one middleware into a chain: session cookie → bearer API token →
  bootstrap token.
- Chart values (WP-006) gain OIDC config; wizard/docs follow.
- Deps: `golang.org/x/oauth2` is already an indirect dep; `github.com/coreos/go-oidc/v3`
  is the standard verifier — both must pass the security gates with justification.
- Vision §3.4 Phase A: bundled Dex is OPTIONAL and deferred — BYO IdP only here;
  if scope stays small a Dex addon can be a fast-follow, decided then, not now.

## Contract

- OIDC authorization-code flow (with PKCE): issuer URL, client ID/secret, scopes
  from platform config (chart values + local-mode flags); ID token verified
  (issuer, audience, expiry, nonce); minimal claims recorded (sub, email, name).
- Session: HttpOnly Secure SameSite cookie, server-side session store in a Secret
  or in-memory with signed stateless fallback — choose and document; sessions
  expire and refresh sanely; logout endpoint.
- CSRF protection for all cookie-authenticated mutating routes (double-submit or
  Origin-check — document choice). Bearer-token routes are CSRF-exempt by nature.
- API tokens: `POST/GET/DELETE /api/v1/tokens` (create returns the token once;
  stored as salted hash in a Secret; list shows prefix + metadata only);
  middleware accepts them equivalently to sessions.
- Bootstrap token: still accepted, logged prominently on use, documented as
  break-glass; disabled entirely when config says so.
- SPA: login page (redirect to IdP), session-aware shell, token management UI
  under settings; the WP-004 token-prompt becomes the fallback.
- All auth events (login, failure, token create/delete, bootstrap use) logged in
  an auditable structured form.

## Non-goals

- Roles/permissions/multi-tenancy (Phase 3 epic) — every authenticated identity
  has full access for now, matching the current single-team posture.
- Dex/IdP bundling. SCIM/groups (record claims for later use, don't act on them).

## Acceptance criteria

1. Full OIDC flow against a test IdP in CI (dex or mock-oidc container in a
   docker-based integration test, or a well-mocked verifier — must cover: happy
   path, wrong audience, expired token, nonce replay).
2. Middleware chain tests: each credential type authenticates; precedence and
   401/403 behavior correct; CSRF blocks a forged mutation.
3. API token lifecycle: created token authenticates, deleted token stops working,
   hashes (not tokens) at rest — verified by reading the Secret in test.
4. Chart renders OIDC config; local `k8zner serve --oidc-issuer ...` works;
   `docs/platform.md` auth section updated.

## Hints

- Keep the identity record minimal and stable (`sub` as the key) — the teams
  epic will attach project bindings to it.
- Cookie + SPA on the same origin keeps CORS closed; don't open CORS for the
  API in this WP.
