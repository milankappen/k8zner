# WP-004 — `web/` SPA scaffold + embedding

Lane C · Depends on: WP-003 · Blocks: WP-005

## Goal

A production-grade frontend scaffold: Vite + React + TypeScript (strict), with a
typed API client generated from `api/openapi.yaml`, embedded into the Go binary
behind a build tag so plain Go builds never require Node. After this WP,
`k8zner serve` (built with the tag) serves the SPA shell at `/` and the dashboard
work (WP-005) is pure feature development.

## Context & anchors

- `api/openapi.yaml` — the contract from WP-003. Client generation must be
  scripted, not hand-written.
- Embed pattern: `internal/addons/operator.go:20` (`//go:embed` + `embed.FS`).
- `.dockerignore` currently does NOT exclude `node_modules`; `.gitignore` has no
  web entries — both need updating.
- CI structure: `.github/workflows/ci.yaml` — add a `web-ci` job; note the
  `ci-success` aggregate gate lists required jobs explicitly.
- Design guardrails: `docs/plan/conventions.md` Frontend section.

## Contract

- `web/` contains the Vite app: React 18+, TS strict, TanStack Query + Router,
  Tailwind + shadcn/ui, Vitest configured. Routing shell with a sidebar layout and
  placeholder routes for Overview / Nodes / Addons / Workloads.
- `npm run generate-client` (or equivalent script) regenerates the typed client
  from `../api/openapi.yaml` into `web/src/api/` (generated code committed, marked
  as generated); a CI step fails if regeneration produces a diff.
- Embedding: `internal/api` serves the SPA from an `embed.FS` **behind build tag
  `webui`**; without the tag, the existing "UI not built in" page remains. SPA
  fallback routing (serve `index.html` for unknown non-`/api` paths).
- Auth handling in the shell: bearer token entry (stored in memory/sessionStorage),
  401 → token prompt. (Real login UX comes with WP-108.)
- Makefile: `web-build` (npm ci + build into the embed location), `web-dev` (Vite
  dev server proxying `/api` to a running `k8zner serve`), and a `build-full`
  target chaining `web-build` + tagged Go build.
- CI: `web-ci` job (npm ci, lint, typecheck, vitest, build, client-freshness check)
  added to `ci.yaml` and to the `ci-success` gate; `.gitignore`/`.dockerignore`
  updated (`web/node_modules`, build output).

## Non-goals

- Real dashboard views/data (WP-005). OIDC login flow (WP-108).
- Component library bikeshedding beyond shadcn/ui defaults; theming later.

## Acceptance criteria

1. `make web-build && go build -tags webui ./cmd/k8zner && ./k8zner serve` →
   browser at the serve address shows the SPA shell; deep link to `/nodes`
   works (SPA fallback); `/api/v1/cluster` still answers JSON.
2. `go build ./...` (no tag, no Node installed) succeeds.
3. `npm run generate-client` twice → no diff; deleting the generated client and
   regenerating restores it identically.
4. Vitest suite passes; `web-ci` green in CI.

## Hints

- Generator choice is yours (openapi-typescript + openapi-fetch is lightweight;
  @hey-api/openapi-ts is fuller) — pick one, pin it, justify in the PR.
- Put the built assets under e.g. `internal/api/webdist/` (gitignored) and embed
  from there, so `web/` stays a pure Node workspace and the Go module never pulls
  `web/` paths without the tag.
- Keep the Vite dev proxy config in-repo so `web-dev` works for every agent.
