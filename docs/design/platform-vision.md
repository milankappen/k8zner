# Design: From Cluster Provisioner to Infrastructure Platform

Status: **Draft / Proposal** — for discussion, not yet committed to.

## 1. Vision

Evolve k8zner from "operator-driven Kubernetes on Hetzner" into a **Kubernetes-native
infrastructure platform**: a self-hostable (and later hostable) product that lets a
single engineer or small team run professional-grade infrastructure on cheap European
hardware — with a UI, an API, and best practices baked in.

Think **Coolify / Zeabur class UX, but Kubernetes-native and professional-grade**:

- User authentication, teams/organizations
- Connect GitHub / GitLab (and other forges) for source-based deployments
- Configure applications visually or via API — while plain `kubectl`/GitOps deployments
  are recognized and visualized too (no lock-in, no parallel universe)
- Branch-based preview deployments, automatic TLS, automatic DNS
- Native database provisioning with automated backups (Databasus integration)
- Node management, metrics/monitoring for services and servers
- Auto-recovery and self-healing (already the operator's core job)
- Hetzner first; pluggable European providers (and bring-your-own-server) later
- EU-sovereign by default: EU regions, EU-friendly component choices, self-hostable

The differentiator vs. Coolify: **everything is Kubernetes**. The platform never invents
its own runtime; it is a control plane + UI over CRDs, so users can always eject to raw
Kubernetes, and everything the UI does is inspectable and GitOps-able.

## 2. Why k8zner is already halfway there

The existing architecture is unusually well-positioned for this. Nothing about the
vision requires a rewrite — it requires *extension*:

| Vision requirement | What exists today |
|---|---|
| Declarative platform state | `K8znerCluster` CRD + operator reconciliation phases (Infrastructure → Compute → CNI → Addons → Running) |
| Self-healing / auto-recovery | Operator reconcile loop + `HealthCheckSpec` |
| GitOps app delivery | ArgoCD installed by default |
| Automatic TLS | cert-manager + Let's Encrypt, Cloudflare DNS-01 |
| DNS automation | external-dns (enabled via `domain:`) |
| Ingress / routing | Traefik + Gateway API |
| Monitoring | kube-prometheus-stack addon (Prometheus, Grafana, Alertmanager) |
| Backups (etcd) | `BackupSpec` + S3 |
| Node management | Compute reconciliation (scale CPs/workers via CRD) |
| Addon framework | `internal/addons` — Helm-based, tested, easy to add new components |
| Provider layer | `internal/platform/hcloud` behind an interface — but hcloud-typed and Hetzner-shaped throughout; a real seam requires the refactor in §3.5 |

What does **not** exist yet: a long-running API server, a web UI, user/org identity,
Git-forge integration, an application-level CRD, and a database-service CRD. That is
the platform layer this document proposes.

## 3. Proposed architecture

### 3.1 Guiding principles

1. **CRDs are the API.** Every platform concept (application, database, backup policy,
   git source) is a CRD reconciled by the operator. The REST API and UI are thin,
   stateless-ish layers over the Kubernetes API. This keeps CLI, API, UI, and GitOps
   perfectly consistent — same rule as today's dual-path (CLI ↔ operator) design
   (ADR-002).
2. **Integrate, don't rebuild.** cert-manager, external-dns, ArgoCD, CNPG, Databasus,
   kube-prometheus-stack do the heavy lifting. k8zner adds opinionated glue, defaults,
   and one coherent UI on top.
3. **Visualize everything, own nothing exclusively.** Workloads deployed via plain
   `kubectl` or external GitOps show up in the dashboard (read-only discovery of
   Deployments/StatefulSets/Ingresses/Gateways). Only k8zner-managed resources get the
   full lifecycle UX. No forced migration.
4. **Self-hosted first, hosted later.** The platform ships as an in-cluster addon
   (`platform: true`). A multi-tenant hosted offering is a deployment mode of the same
   code, not a fork.

### 3.2 Components

```
┌────────────────────────────────────────────────────────────────┐
│  Web UI (SPA)          k8zner CLI              GitOps / kubectl │
└──────────┬──────────────────┬──────────────────────┬───────────┘
           ▼                  ▼                      │
┌─────────────────────────────────────┐              │
│  k8zner-api (new, in-cluster)       │              │
│  - AuthN: OIDC (built-in Dex or     │              │
│    external IdP), API tokens        │              │
│  - AuthZ: mapped to K8s RBAC        │              │
│  - REST/streaming façade over CRDs  │              │
│  - Git-forge webhooks (GH/GL)       │              │
└──────────────────┬──────────────────┘              │
                   ▼                                 ▼
┌────────────────────────────────────────────────────────────────┐
│                      Kubernetes API (CRDs)                     │
│  K8znerCluster · Application · Database · BackupPolicy ·       │
│  GitSource · (existing core resources)                         │
└──────────────────┬─────────────────────────────────────────────┘
                   ▼
┌────────────────────────────────────────────────────────────────┐
│  k8zner-operator (extended)                                    │
│  - existing cluster/addon reconciliation                        │
│  - Application → ArgoCD Application/ApplicationSet             │
│  - Database → CNPG Cluster (+ others later)                    │
│  - BackupPolicy → Databasus API                                │
└────────────────────────────────────────────────────────────────┘
```

**k8zner-api** (new binary, `cmd/api`): the only genuinely new service. Stateless where
possible; state lives in CRDs/Secrets. Handles OIDC login, session/API tokens,
Git-forge OAuth + webhooks, and translates REST calls into CRD mutations. Log/metric
streaming proxied from the cluster (pod logs, Prometheus queries).

**Web UI**: SPA served by k8zner-api. Views: cluster overview (nodes, health, phases —
data the operator already has in `K8znerClusterStatus`), applications, databases &
backups, monitoring (embed/link Grafana; render key series natively via the Prometheus
API), node management (scale via CRD edit), audit log.

**Operator extensions**: new controllers in `internal/operator/controller`, reusing the
existing addon/Helm machinery.

### 3.3 New CRDs (platform layer)

- **`GitSource`** — a connected repository (forge, repo, credentials ref, webhook
  secret). Created via forge OAuth flow in the UI or manually.
- **`Application`** — the central PaaS object: source (GitSource ref + path, or OCI
  image), build strategy (prebuilt image first; Buildpacks/Nixpacks builder later),
  env/secrets refs, routes (host/path → automatic Gateway + Certificate + DNS),
  resources/autoscaling, `previews: true` for branch deployments. Reconciled into an
  ArgoCD Application (or ApplicationSet with a pull-request generator for previews) —
  ArgoCD stays the delivery engine; k8zner owns the ergonomics.
- **`Database`** — engine (PostgreSQL first via CloudNativePG), version, size, HA,
  credentials as Secrets, optional `backupPolicyRef`.
- **`BackupPolicy`** — schedule, retention, S3 target, scope (database ref). Reconciled
  against the **Databasus** API (deployed as an addon) so backups are automated but
  also fully manageable in Databasus' own terms. k8zner UI shows status, last backup,
  restore actions — driving Databasus' API rather than reimplementing backup logic.

### 3.4 Identity & multi-tenancy

- **Phase A (single team, self-hosted):** OIDC via a bundled lightweight IdP (Dex) or
  bring-your-own IdP; all authenticated users are cluster operators. API tokens for
  automation.
- **Phase B (orgs/projects):** Projects map to namespaces + RBAC + ResourceQuota +
  NetworkPolicy. k8zner-api maps platform roles (owner/developer/viewer) to generated
  K8s Roles. Still no separate authorization database — Kubernetes RBAC remains the
  source of truth.
- **Phase C (hosted multi-tenant):** one management cluster running k8zner-api +
  fleet state; customer workload clusters registered as `K8znerCluster` objects
  (the CRD becomes fleet-capable: one operator instance managing N clusters, or one
  operator per cluster phoning home). This is the largest architectural step and is
  deliberately last.

### 3.5 Multi-provider & bring-your-own-server

**Honesty first: k8zner's provisioning and reconciliation are explicitly Hetzner
today, and the comfort that creates is the product.** Any provider we add must offer
the *same* comfort — fully managed machine lifecycle, image builds, load balancers,
networking, storage, cleanup — or it isn't a supported provider. A thin "servers
API" adapter would produce second-class providers and dilute the promise.

Where the coupling actually lives (inventory, not exhaustive):

- **Interface boundary leaks hcloud types.** `internal/platform/hcloud.InfrastructureManager`
  exists, but its signatures use hcloud-go types (`*hcloud.Server`,
  `hcloud.FirewallRule`, `hcloud.LoadBalancerAlgorithmType`, …), so the operator
  controllers (~15 files) and provisioning layers are written against Hetzner's data
  model, not a neutral one.
- **Image pipeline is Hetzner-mechanism-specific.** Talos images are built by booting
  a server into Hetzner **rescue mode**, writing the image, and snapshotting. Other
  providers need entirely different pipelines (custom-image upload, provider image
  imports, ISO boot), not a re-parameterized version of ours.
- **Reconciliation phases assume Hetzner objects.** Infrastructure phase = hcloud
  network + firewall + placement group + LB; cleanup is hcloud label-selector based.
- **The comfort layer is Hetzner-shaped.** Hetzner CCM (node lifecycle, LB services)
  and CSI (volumes) addons; LB semantics; private network zones.
- **Config & CRD validation encode the catalog.** Server types, regions
  (`fsn1;nbg1;hel1` is a kubebuilder enum on the CRD), architectures, network zones.

**Design response — providers as full-comfort modules, not adapters:**

1. **Define comfort as a conformance contract.** A provider is a package that
   delivers the full column: image build pipeline, machine lifecycle
   (create/delete/reset/rescue-equivalent), private networking, firewall, LB (or a
   blessed substitute), CCM, CSI, placement/anti-affinity, labeled cleanup, and a
   validated catalog (regions, machine types) that feeds config validation, the CRD
   schema, and the UI wizard. A shared conformance/e2e suite defines "supported".
2. **Providers own their reconciliation steps and addons.** Rather than forcing one
   generic infrastructure phase, the operator asks the provider for its phase
   implementations and its addon set (its CCM/CSI/LB glue). Neutral domain types
   (k8zner's own `Server`, `LoadBalancerSpec`, `FirewallRule`, …) exist at the
   boundary; provider-specific richness stays inside the provider package. Where a
   provider lacks a managed primitive, the provider module ships the substitute as
   part of its comfort obligation (e.g. kube-vip / Cilium LB-IPAM instead of a
   managed LB) — the user experience stays "it just works", the implementation
   differs per provider.
3. **Don't abstract against one implementation.** With only Hetzner in-tree, a
   speculative interface will be wrong. Sequence the work as: (a) mechanical de-leak —
   remove hcloud-go types from `InfrastructureManager` signatures and the CRD/config
   layer without changing behavior; (b) move Hetzner-specific reconciliation and
   addons behind the provider seam; (c) build the **second provider to force the
   abstraction right**, choosing it for API + image + CCM/CSI maturity (evaluate
   Scaleway, OVHcloud, Exoscale; netcup-class providers lack the API surface for
   full comfort and would enter as BYO-server targets instead).
4. **BYO-server is a tier, not a provider.** Machines the user brings (bare metal,
   arbitrary VPS) can never get machine-lifecycle comfort — no API to create,
   rescue, or replace them. Offer it as an explicitly labeled tier: *managed
   cluster on unmanaged machines* — Talos config, joining, addons, upgrades,
   self-healing at the Kubernetes layer, with kube-vip/LB-IPAM and
   local-path/Longhorn presets — while the UI is clear about what k8zner cannot
   heal (the machine itself). It complements, not substitutes, real provider
   integrations.

The realistic cost estimate: a new full-comfort provider is a large effort
(image pipeline + CCM/CSI validation + LB semantics + conformance), comparable to a
significant fraction of the original Hetzner work. That's the price of keeping the
promise, and why providers arrive one at a time, late in the roadmap, and only after
the seam is proven by the refactor.

**Multi-cloud: fleet of clusters, not one stretched cluster.**

"Multi-provider" must not be read as "one cluster spanning providers". Today the
entire data path rides the Hetzner private network: deterministic private IPs per
node pool, Cilium in **native routing over the private CIDR**, control-plane LB
targeting private IPs, etcd and kube-apiserver traffic on the private network. A
node in another provider has no path into that network at all.

The public-network part is, perhaps surprisingly, the *solvable* piece: Talos ships
**KubeSpan** (a WireGuard mesh over public internet, built for exactly this), and
Cilium supports WireGuard encryption with tunnel routing (our config already has
`encryption_type: wireguard`). What actually breaks a stretched cluster:

- **etcd quorum latency.** etcd wants low-single-digit-ms RTT; cross-provider links
  are 20–50 ms+. Stretched etcd means slow writes, spurious leader elections, and
  instability — a known anti-pattern. This alone rules out cross-provider control
  planes.
- **CCM semantics.** Hetzner CCM owns node lifecycle; nodes it can't find in the
  hcloud API get marked for deletion unless carefully excluded. Two CCMs in one
  cluster fight over the same responsibilities.
- **LB reachability.** A Hetzner LB targets Hetzner private IPs only — no provider
  LB can front nodes living elsewhere.
- **Secondary frictions**: provider-local CSI volumes (topology-manageable but
  sharp), WireGuard MTU overhead, cross-provider egress cost.

Supported multi-cloud shapes, in order of recommendation:

1. **Fleet (the multi-cloud story).** Each cluster lives entirely in one provider
   with full comfort; the platform layer (Phase 5 fleet mode) places applications
   across clusters, with DNS-based failover via external-dns. Sovereignty and
   provider redundancy are achieved at the platform level, where they're cheap,
   instead of at the etcd level, where they're ruinous. Optional later: Cilium
   Cluster Mesh for cross-cluster service connectivity between consenting clusters.
2. **Hybrid workers (a tier, reduced comfort).** Control plane and etcd stay in one
   provider; remote workers (another provider or bare metal) join over
   KubeSpan/WireGuard. Tractable because etcd stays local — but those nodes must be
   excluded from the resident CCM, can't sit behind the provider LB, and get no CSI
   unless their own provider's driver is added. This shares nearly all machinery
   with the BYO-server tier, so the two should be built as one capability.
3. **Stretched control plane across providers: never.** Not a roadmap item, not a
   config option.

### 3.6 CI/CD stance

"Professional CI/CD" ≠ building a CI system. Position:

- **CD is ours** (via ArgoCD under the hood): image promotion, branch previews,
  rollbacks, health-gated syncs — surfaced properly in the UI.
- **CI stays in the forge** (GitHub Actions / GitLab CI): k8zner provides ready-made
  workflow templates and a registry story (start: forge registries; later: optional
  in-cluster registry addon). An in-cluster build option (Buildpacks via kpack or a
  simple BuildKit job) is a later convenience, not a foundation.

## 4. Build vs. integrate

| Capability | Decision | Component |
|---|---|---|
| TLS | integrate (done) | cert-manager |
| DNS | integrate (done) | external-dns |
| GitOps/CD engine | integrate (done) | ArgoCD (+ ApplicationSet PR generator) |
| Metrics | integrate (done) | kube-prometheus-stack |
| PostgreSQL | integrate | CloudNativePG |
| DB backups | integrate | Databasus (addon + API-driven automation) |
| App abstraction | **build** | `Application` CRD + controller |
| Platform API + auth | **build** | k8zner-api (+ Dex bundled) |
| Web UI | **build** | SPA |
| Builds (later) | integrate | Buildpacks/kpack or BuildKit |
| Logs (later) | integrate | Loki (addon), UI tails via K8s API meanwhile |

## 5. Phased roadmap

Each phase is independently shippable and valuable; stop-anywhere is a feature.

1. **Phase 0 — Read-only dashboard (`platform: true` addon).**
   New `cmd/api` + minimal SPA: cluster status (from `K8znerClusterStatus`), nodes,
   discovered workloads, addon health, embedded Grafana links. Single admin auth
   (OIDC or bootstrap token). *Proves the addon-delivered UI pipeline end-to-end with
   minimal new surface.*
2. **Phase 1 — Applications.**
   `GitSource` + `Application` CRDs, forge OAuth + webhooks, deploy-from-image and
   deploy-from-repo (manifests/Helm/Kustomize via ArgoCD), automatic route + TLS +
   DNS, branch preview environments. This is the "Coolify moment" — the phase where
   k8zner becomes a platform rather than a provisioner.
3. **Phase 2 — Databases & backups.**
   CNPG addon, `Database` CRD, Databasus addon, `BackupPolicy` automation, backup/
   restore UX.
4. **Phase 3 — Teams & projects.**
   Orgs/projects → namespaces + RBAC + quotas; audit log; API tokens per project.
5. **Phase 4 — Providers.**
   In order: (a) de-leak hcloud types from interfaces/config/CRD, (b) move
   Hetzner reconciliation + addons behind the provider seam, (c) BYO-server as an
   explicitly reduced-comfort tier, (d) a second **full-comfort** EU provider
   (image pipeline + CCM/CSI + LB + conformance suite — see §3.5; this is a major
   effort, not an adapter).
6. **Phase 5 — Hosted platform.**
   Management-cluster fleet mode, billing, onboarding. Separate business decision;
   architecture above intentionally keeps the door open.

## 6. Repository & licensing strategy

- **Stay monorepo for now**: `cmd/api`, `web/` (SPA), new controllers alongside
  existing code. Shared CRD types and config are the whole point; splitting early
  creates versioning drag. Revisit at Phase 5.
- Core (CLI, operator, CRDs) stays **Apache-2.0**. Decide before Phase 3 whether the
  platform layer stays fully open (recommended for adoption; monetize hosting/support)
  or adopts an open-core line. Don't drift into this decision implicitly.

## 7. Risks & open questions

- **Scope**: this is 3–4 products. The phase gates exist to force sequencing; Phase 1
  should not start until Phase 0 is shipped and used.
- **ArgoCD coupling**: building app UX on ArgoCD is fast but couples us to its CRDs.
  Mitigation: the `Application` CRD is ours; ArgoCD is an implementation detail behind
  the controller and can be swapped (e.g. Flux) without breaking users.
- **UI stack**: needs its own mini-ADR (framework, typed API client generation,
  embedding strategy in the Go binary vs. separate image).
- **Databasus maturity**: validate its API surface for headless automation before
  committing `BackupPolicy` to it; fallback is CNPG's native Barman backups with
  Databasus as optional UX.
- **Single-cluster vs. fleet semantics** for `Application` (Phase 5 impact on Phase 1
  API design): keep `clusterRef` optional-with-default in the CRD from day one so the
  fleet case is additive.
- **Provider-parity cost**: the full-comfort bar (§3.5) means each provider is a
  major investment. The risk is shipping a half-comfortable provider under
  pressure; the conformance suite is the guardrail — a provider that doesn't pass
  it doesn't ship.
- **Name/branding**: "k8zner" is Hetzner-specific; a platform spanning providers may
  want an umbrella name with k8zner as the Hetzner provider. Defer, but note it.
