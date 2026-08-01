# Epics — Phases 3–5 (guardrails only)

These phases are intentionally NOT broken into work packages yet. They will be
planned in detail when Phases 0–2 have shipped and taught us what the specs should
say. Until then, this file records the direction and the guardrails that earlier
work must not violate. Vision references: `../design/platform-vision.md`.

## Epic: Teams & projects (vision §3.4, Phase B)

Projects map to namespaces + RBAC + ResourceQuota + NetworkPolicy. Platform roles
(owner / developer / viewer) are translated by `internal/api` into generated
Kubernetes Roles/RoleBindings.

**Guardrails:**
- Kubernetes RBAC stays the single source of truth for authorization. No separate
  authz database, ever. The API server holds sessions and identity, not permissions.
- Every Phase 0–2 API endpoint should be written namespace-aware even while
  authorization is still all-or-nothing, so scoping later is a filter, not a rewrite.

## Epic: Provider seam & second provider (vision §3.5)

The bar: a provider is a **full-comfort conformance module** (image pipeline,
machine lifecycle, networking, firewall, LB or blessed substitute, CCM, CSI,
validated catalog) — not an adapter over a servers API.

**Sequencing:** (a) mechanically de-leak hcloud-go types from
`internal/platform/hcloud.InfrastructureManager`, config, and CRD enums;
(b) move Hetzner-specific reconciliation phases and addons behind the provider
seam; (c) build the second provider (evaluate Scaleway / OVHcloud / Exoscale for
API + image + CCM/CSI maturity) to force the abstraction right.

**Guardrails:**
- No half-comfort providers ship. The conformance suite defines "supported".
- BYO-server is a **reduced-comfort tier** ("managed cluster on unmanaged
  machines"), shares its machinery with hybrid workers (KubeSpan/WireGuard join,
  kube-vip / Cilium LB-IPAM, local-path/Longhorn presets), and is labeled honestly
  in UI and docs.
- Earlier phases must not deepen the coupling: new code paths avoid importing
  hcloud-go types outside `internal/platform/hcloud`.

## Epic: Fleet / hosted platform (vision §3.4 Phase C + §3.5 multi-cloud)

Multi-cloud is a **fleet of single-provider clusters** managed by one platform
control plane — never a stretched cluster (etcd latency, CCM conflicts, LB
reachability; see vision §3.5). Application placement across clusters, DNS-based
failover via external-dns, optional Cilium Cluster Mesh later. Hosted multi-tenant
operation is a deployment mode of the same code, plus billing and onboarding.

**Guardrails:**
- `Application.spec.clusterRef` (optional-with-default, introduced in WP-102) is the
  pre-planted hook — keep it working even while there is only ever one cluster.
- A stretched control plane across providers is never a roadmap item or config
  option.
- Nothing in `internal/api` may assume "exactly one cluster" in its data model;
  handlers may default to the single cluster, but types should carry the cluster
  identity.
