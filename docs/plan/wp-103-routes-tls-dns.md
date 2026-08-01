# WP-103 — Automatic routes: HTTPRoute + TLS + DNS

Lane A · Depends on: WP-102 · Blocks: WP-106

## Goal

`Application.spec.routes` becomes real: each `{host, path}` yields a Gateway API
HTTPRoute through the existing Traefik, a cert-manager Certificate, and external-dns
records — the "push code, get a URL with a padlock" experience.

## Context & anchors

- Traefik ships with Gateway API support (`internal/addons/`, README "Batteries
  Included"); find the Gateway/GatewayClass the cluster exposes and how existing
  addon ingresses attach (ArgoCD/Grafana hosts) — `helm.IngressAnnotations`
  (`internal/addons/helm/builders.go`) and `BuildIngressHost`
  (`internal/config/addon_defaults.go`).
- cert-manager + Cloudflare issuer wiring: `internal/addons/cert_manager*.go`.
- external-dns is enabled when a domain is configured (`expandExternalDNS` in
  `internal/config/spec_expand.go`).
- The Application controller from WP-102 owns reconciliation; this WP extends it
  (or adds a focused sub-reconciler file, matching the cluster controller's
  file-per-concern layout).

## Contract

- For each route: HTTPRoute (host+path match → Application's Service), TLS via a
  cert-manager Certificate using the cluster's existing issuer, DNS via
  external-dns (annotation or DNSEndpoint — whichever the deployed external-dns
  config consumes; verify, don't assume).
- Host defaulting: `routes: [{}]` (empty host) defaults to
  `<app-name>.<cluster-domain>`; explicit hosts outside the cluster domain are
  allowed but get a condition warning that DNS/TLS may be external.
- Cluster has no domain configured → routes get `RoutesPending=False` with reason
  `NoDomainConfigured` and an actionable message; nothing half-created.
- Route removal from spec cleans up its HTTPRoute/Certificate (owner refs).
- Status: per-route readiness (route accepted, cert ready, URL) surfaced in a
  `status.routes` list the UI (WP-107) can render as links.

## Non-goals

- Preview-environment hosts (WP-106 builds on the same helpers).
- Custom/BYO certificates, non-HTTP protocols, TCP routes.

## Acceptance criteria

1. envtest: routes produce HTTPRoute + Certificate with owner references; spec
   edits add/remove them symmetrically.
2. kind: an Application with a route reaches Ready with the route accepted by
   Traefik (cert stays pending without real DNS — assert accepted route +
   Certificate created, not issuance).
3. No-domain cluster → documented condition, no orphan objects.
4. `status.routes` populated with URL + per-route state.

## Hints

- Keep host/annotation derivation in shared helpers next to the existing ones so
  WP-106 can reuse (`pr-N.` prefixing must compose with them).
- Wildcard certs: if the cluster's Cloudflare issuer supports DNS-01, a per-app
  cert is still simpler to reason about than wildcard management — start per-app.
