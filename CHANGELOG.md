# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.10.0] - 2026-07-31

### 🔄 Changed

- **Module path renamed** `github.com/imamik/k8zner` → `github.com/milankappen/k8zner` following the GitHub username change. Update imports and reinstall via `go install github.com/milankappen/k8zner/cmd/k8zner@latest`. Homebrew users: `brew untap imamik/tap && brew tap milankappen/tap`.
- **All references repointed** to `milankappen` — badges, install commands, Helm chart metadata, `ghcr.io/milankappen/k8zner-operator` image path, GoReleaser config, release scripts, and CI image names.
- **Dependencies bumped** — containerd 1.7.30 → 1.7.33, helm 3.20.1 → 3.20.2, k8s.io/* 0.35.3 → 0.35.4, golang.org/x/net → 0.55.0, golang.org/x/crypto → 0.52.0, golang.org/x/text → 0.39.0, google.golang.org/grpc → 1.82.1, oras.land/oras-go/v2 → 2.6.2, aws-sdk-go-v2/s3 → 1.101.0, mattn/go-isatty → 0.0.22, plus transitive updates.
- **Go toolchain bumped** 1.26.3 → 1.26.5 for stdlib CVE fixes.
- **GitHub Actions bumped** — codecov-action v5 → v6, setup-qemu-action v3 → v4, setup-kubectl v4 → v5, login-action v3 → v4, metadata-action v5 → v6; `trivy-action` pinned to `0.35.0` (was tracking `@master`).

### 🔒 Security

- **containerd runAsNonRoot bypass (high)** resolved via containerd 1.7.32.
- **Helm chart extraction path collapse (medium)** resolved via helm 3.20.2.
- **crypto/tls ECH privacy leak (GO-2026-5856)** resolved via Go 1.26.5.
- **grpc HTTP/2 transport + xDS RBAC (GO-2026-6061)** resolved via grpc 1.82.1.
- **x/text infinite loop on invalid input (GO-2026-5970)** resolved via x/text 0.39.0.
- **oras-go tarball/redirect credential issues (GO-2026-5885, 5884; CVE-2026-50163)** resolved via oras-go 2.6.2.
- **containerd CRI image-config command execution (GO-2026-5758)** resolved via containerd 1.7.33.
- **cel-go JSON private field exposure (GHSA-gcjh-h69q-9w9g)** resolved via cel-go 0.26.1 → 0.29.0 (transitive, via siderolabs/talos machinery).
- `govulncheck`'s CI allowlist now covers four advisories with no released fix, all reached only via package `init()` of transitive deps we never invoke (x/crypto/openpgp's unmaintained-package advisory, and three containerd CRI checkpoint/restore advisories pulled in transitively through Helm's OCI registry client) — not reachable through any code path this project actually calls.

### 🐛 Fixed

- **Flaky handler tests** — `TestPersistAccessData`, `TestWriteTalosFiles`, `TestWriteKubeconfig`, `TestInitializeTalosGenerator`, `TestFactoryVariables`, and `TestDestroy` swapped package-global factory vars while running in parallel, racing each other; these tests now run serially.
- **`TestFindConfigFile` on macOS** — resolve symlinks on the temp dir so the `/var` → `/private/var` resolution from `os.Getwd` no longer fails the assertion.
- **`TestClearCache` race** — ran in parallel against the real, shared `~/.cache/k8zner/charts` directory that `TestDownloadChartIntegration` also writes to; intermittently wiped the chart the other test had just downloaded. Now uses its own isolated cache dir.

## [0.9.3] - 2026-04-04

### 🔄 Changed

- **Go 1.25.7 → 1.26.1** — fixes 3 stdlib CVEs (GO-2026-4601, GO-2026-4602, GO-2026-4603)
- **All Go dependencies bumped to latest** — grpc 1.80.0, aws-sdk-go-v2 1.41.5, k8s 0.35.3, controller-runtime 0.23.3, helm 3.20.1, hcloud-go 2.37.0, charmbracelet/huh 1.0.0, and 40+ transitive deps
- **GitHub Actions bumped** — upload-artifact v7, download-artifact v8, setup-buildx v4, build-push v7, goreleaser v7, gh-release v2
- **golangci-lint 2.8.0 → 2.11.4** — required for Go 1.26 compatibility
- **Dockerfile.operator base image** bumped to Go 1.26

### 🐛 Fixed

- **`doctor --json` output** — cost hint was appended after JSON, breaking machine-parseable output
- **Data race in controller scaling test** — `nodeReadyWaiterCalls` slice now protected by mutex
- **Shared scheme race in parallel controller tests** — each subtest gets its own `runtime.Scheme` to avoid concurrent map access with controller-runtime v0.23.3
- **Deprecated API usage** — removed `result.Requeue` checks (use `RequeueAfter` only), suppressed false-positive gosec G101 findings

## [0.9.2] - 2026-02-17

### 🐛 Fixed

- **Prometheus/Grafana PVC binding** — omit `storageClassName` field when empty to allow Kubernetes default StorageClass selection (#159)
  - Previously set empty string explicitly, which prevented dynamic provisioning with Hetzner CSI (WaitForFirstConsumer binding mode)
  - PVCs now bind successfully on first pod creation without requiring manual intervention
- **CHANGELOG.md sourcing** in release workflow — removed `changelog.disable: true` that silently blocked release notes from appearing on GitHub releases page

### 🔄 Changed

- **Shared addon defaults** — extracted common Helm values (tolerations, resources, ingress) to reusable helpers, reducing duplication across addon builders (#158)
- **Provisioner ceremony removal** — simplified orchestration controller: removed observer abstraction, phases-as-struct pattern, and unused provisioner options (#158, #157)
- **Documentation cleanup** — removed 3 phantom ADR files (missing from Git but referenced in docs), fixed broken internal links (#158)
- **Dead code removal** — ~1,200 lines trimmed: unused rDNS module, duplicate normalize helpers, redundant config marshaling, orphaned test utilities (#157, #151)

## [0.9.1] - 2026-02-16

### 🔄 Changed

- **Observer interface simplified** to Printf-only — removed Event struct, EventType, and Log* helpers (#151)
- **Pipeline struct replaced** with `RunPhases` function — less abstraction for a single call site (#151)
- **DeepMerge unexported** to `deepMerge` — only used within the `helm` package (#151)
- **Namespace creation deduplicated** — extracted `ensureNamespace` helper and `baselinePodSecurityLabels` constant, deleted 5 per-addon `createXNamespace` functions (#151)
- **213 tests redistributed** from catch-all coverage files to per-source test files (#151)
- **~3,500 net lines removed** — dead code, unused abstractions, no-op rDNS modules, and duplicate normalize helpers (#151)

### 🐛 Fixed

- **LB health check timeout** increased from 5 to 10 minutes — prevents premature timeouts on slower Hetzner LB provisioning (#151)
- **Multi-arch Dockerfile** now uses `TARGETARCH` instead of hardcoded `amd64` (#151)
- **GitHub release notes** now sourced from `CHANGELOG.md` instead of GoReleaser's auto-generated commit list

## [0.9.0] - 2026-02-15

### ✨ Added

- **`secrets` command** — retrieve cluster credentials (ArgoCD, Grafana passwords, kubeconfig, talosconfig) from the running cluster (#149)
- **`cost` command** — calculate current and planned monthly cluster costs with Hetzner pricing (#149)
- **TUI granular stages** — `doctor`/`apply` shows per-stage progress with adaptive ETA (#149)
- **Access data persistence** — ArgoCD and Grafana credentials saved to `access-data.yaml` on cluster creation (#149)
- **Metrics-server health** wired into monitoring stack probes and Prometheus scraping (#149)
- **Init-to-apply pipeline tests** for end-to-end config validation (#149)

### 🔄 Changed

- **Random 5-char server IDs** — CLI names now match operator convention (`cp-n19op` not `cp-1`), label-based discovery for idempotency (#149)
- **Dead code removal** — 805 lines stripped (pre-provisioned LB, unused addon types, dead Helm specs) (#149)
- **Magic numbers extracted** to named constants across Talos upgrade, bootstrap, cleanup, and RDNS retry logic (#149)
- **Init defaults** — simplified Spec format output, improved Talos image build fallback (#149)

### 🐛 Fixed

- **Data race** in parallel node provisioning (added sync.Mutex for concurrent status updates) (#149)
- **Cost calculation** now counts ingress LB and always includes S3 storage (#149)
- **LB health check wait** before operator installation prevents EOF errors (#149)
- **Doctor hang** when cluster doesn't exist (clean exit with NotFound handling) (#149)
- **Worker scaling** now applies Talos configs in parallel (#149)
- **Conflict retry** for `persistClusterStatus` during CP replacement (#149)
- **hcloud-go v2** compatibility — use `AllWithOpts` for filtered API calls (#149)

## [0.8.0] - 2026-02-12

### ✨ Added

- **TUI dashboard** for real-time provisioning observability (#138)
  - Progress bar with ETA, node status table, addon grid with timing and retry info
  - Phase history timeline with durations, error ring buffer (last 10)
  - `apply` shows TUI during bootstrap; `doctor --watch` uses TUI; `--ci` flag for plain log output
- **Operator addon health probes** — core addon and connectivity checks (DNS, TLS, HTTPS) reported in CRD status (#139)
- **Cloudflare DNS cleanup** on `destroy` — removes DNS records created by external-dns (#139)

### 🔄 Changed

- **E2E test refactor** — shared addon verification functions across FullStack and HA test suites (#139)
- **Operator hardening** — phase transition recording, addon retry with exponential backoff (10s/30s/60s), timeout detection with K8s Warning events (#138)
- `doctor` health check timeout increased to 10m (#139)

## [0.7.0] - 2026-02-11

### ✨ Added

- **Operator-first architecture** — `apply` is now the single entry point for cluster lifecycle
  - New clusters: bootstraps infrastructure, deploys operator, creates CRD
  - Existing clusters: updates CRD spec, operator reconciles changes
  - `--wait` flag to wait for operator to complete provisioning
- **`K8znerCluster` CRD** for declarative cluster management via Kubernetes-native API
- **`doctor` command** — Diagnose cluster configuration and live status
  - Pre-cluster mode: validates config, shows what would be created
  - Cluster mode: ASCII table with emoji indicators for infrastructure, nodes, and addons
  - `--watch` flag for continuous updates, `--json` for machine-readable output
- **Control plane self-healing** — Detect failure, remove etcd member, replace node automatically
- **Worker and CP scaling** via CRD updates (`k8zner apply` updates CRD, operator reconciles)
- **kube-prometheus-stack** addon (Prometheus, Grafana, Alertmanager) via `monitoring: true`
- **Operator Helm chart** for in-cluster deployment with leader election and metrics endpoint
- **KIND-based integration tests** for operator reconciliation logic
- **3 Architecture Decision Records**: networking, dual-path architecture, bootstrap tolerations
- **Comprehensive test coverage improvements** (~30k lines of tests across 142 test files)

### 🔄 Changed

- **Traefik**: always LoadBalancer service (was DaemonSet with hostNetwork) — **breaking**
- **cert-manager**: DNS-01 via Cloudflare (was HTTP-01) for wildcard certificate support
- **Cilium**: kube-proxy replacement enabled by default, tunnel mode, Hetzner-specific device config (`enp+`)
- `config/v2` package consolidated into `config` (internal refactor, no user-facing config change)
- E2E tests rewritten for operator-based lifecycle (24/24 passing)
- `destroy` command config flag is now optional (auto-detects k8zner.yaml)

### 🗑️ Removed

- **7 CLI commands** removed in favor of operator-first approach:
  - `create` — absorbed into `apply`
  - `bootstrap` — replaced by operator
  - `upgrade` — handled by operator via CRD update
  - `health` — replaced by `doctor`
  - `image` — image management is automatic
  - `cost` — cost estimation removed
  - `migrate` — migration path no longer needed
- **`internal/orchestration`** — CLI-only reconciler replaced by operator
- **`internal/pricing`** — CLI-only cost estimation
- **`internal/util/prerequisites`** — not needed with operator approach
- **`PrerequisitesCheckEnabled`** config field
- **Floating IP support** — LoadBalancer-only approach
- **ingress-nginx support** — Traefik is the sole ingress controller

### 🐛 Fixed

- CSI CrashLoopBackOff during bootstrap (DNS resolution timing)
- Cilium device detection on Talos (`enp+` pattern, not `eth0`)
- ArgoCD Redis secret initialization (`redisSecretInit.enabled` must be true)
- Bootstrap toleration gaps (3 tolerations needed for all bootstrap-critical pods)
- CIDR drift between CLI and operator config paths

## [0.6.0] - 2026-01-31

### ✨ Added

- **Simplified Configuration (v2)** - Opinionated, production-ready defaults in ~12 lines
  - New `k8zner.yaml` format with only 5 fields: name, region, mode, workers, domain
  - Two cluster modes: `dev` (1 CP, 1 shared LB) and `ha` (3 CPs, 2 separate LBs)
  - IPv6-only nodes by default (saves IPv4 costs, better security)
  - Hardcoded addon stack: Cilium, Traefik, cert-manager, ArgoCD, metrics-server
  - Pinned, tested version matrix for Talos and Kubernetes
  - Auto-detection of k8zner.yaml in current directory
- **Cost Estimator** (`k8zner cost`) - Calculate monthly cluster costs
  - Fetches live pricing from Hetzner API
  - Shows line-item breakdown with VAT calculation
  - JSON and compact output formats
- **Simplified Wizard** - Only 5-6 questions to create a cluster
  - Cluster name, region, mode, worker count, worker size, domain (optional)
  - Shows cost estimate after configuration
- **Automatic etcd Backups** (`backup: true`) - S3-based etcd snapshots
  - Backs up etcd to Hetzner Object Storage every hour
  - Bucket auto-created during provisioning: `{cluster-name}-etcd-backups`
  - Requires `HETZNER_S3_ACCESS_KEY` and `HETZNER_S3_SECRET_KEY` environment variables
  - Uses talos-backup with compression enabled

### 🔄 Changed

- `k8zner init` now creates `k8zner.yaml` (was `cluster.yaml`)
- `k8zner apply` auto-detects config file in current directory
- Removed `--advanced` and `--full` flags (config is now always minimal)
- Traefik now uses DaemonSet with hostNetwork for direct port binding
- All clusters use Traefik instead of ingress-nginx by default

### 🗑️ Removed

- Legacy complex wizard with 20+ questions
- Server architecture/category selection (now uses standard cx22 for control planes)
- Manual addon configuration (all addons are now automatic)

## [0.5.0] - 2025-01-27

### ✨ Added

- **Cloudflare DNS Integration** - Automatic DNS record management and TLS certificates
  - **external-dns addon** - Automatically creates DNS records from Ingress annotations
    - Cloudflare provider with API token authentication
    - Configurable sync policy (sync or upsert-only)
    - TXT record ownership for safe multi-cluster deployments
    - Support for Ingress and Service sources
  - **cert-manager Cloudflare DNS01 solver** - Wildcard and DNS-validated certificates
    - Let's Encrypt staging and production support
    - ClusterIssuer automatically created: `letsencrypt-cloudflare-staging` / `letsencrypt-cloudflare-production`
    - Works with Cloudflare proxied and non-proxied records
  - Environment variable support: `CF_API_TOKEN` and `CF_DOMAIN`
  - Full E2E test coverage with real DNS validation

### 🔄 Changed

- **Ingress controllers now use LoadBalancer by default** for proper external IP allocation
  - ingress-nginx: Changed from NodePort to LoadBalancer service type
  - Traefik: Enabled LoadBalancer service for web entrypoint
- cert-manager ingress-shim enabled for automatic Certificate creation from Ingress annotations
- Improved E2E cleanup to wait for server deletion before removing firewalls

### 🐛 Fixed

- cert-manager Gateway API disabled by default (requires Gateway API CRDs to be installed)
- Firewall deletion now retries when resources are still in use

## [0.4.0] - 2025-01-26

### ✨ Added

- **ArgoCD GitOps Addon** - Continuous delivery for Kubernetes (CNCF Graduated project)
  - Full ArgoCD server, application controller, and repo server deployment
  - Configurable via `addons.argocd` in cluster config
  - HA mode support with multiple replicas
  - Optional Dex SSO integration
  - ApplicationSet controller for multi-cluster deployments
  - Notifications controller for alerts and webhooks
  - Custom Helm values override support
  - E2E tested with full lifecycle validation

## [0.3.0] - 2025-01-26

### ✨ Added

- **Traefik Ingress Controller** - Alternative ingress controller option
  - Full Traefik v3 support with Helm chart integration
  - Configurable via `addons.traefik` in cluster config
  - Proxy protocol support for preserving client IPs
  - Prometheus metrics integration
  - Interactive wizard updated to offer Traefik as an ingress option

- **Runtime Helm Chart Downloading** - Charts downloaded at runtime instead of embedded
  - Removes ~4.9MB of embedded chart templates (464 files)
  - Charts cached in `~/.cache/k8zner/charts/` for faster subsequent runs
  - Users can override chart versions via `helm.version` in addon config
  - Automatic chart updates without rebuilding the binary

### 🔄 Changed

- Updated addon chart versions to latest stable releases:
  - Cilium: 1.18.5
  - cert-manager: v1.19.2
  - cluster-autoscaler: 9.50.1
  - hcloud-ccm: 1.29.0
  - hcloud-csi: 2.18.3
  - ingress-nginx: 4.11.3
  - longhorn: 1.10.1
  - metrics-server: 3.12.2
  - traefik: 39.0.0

### 🐛 Fixed

- JSON schema validation for Cilium chart with slice type values
- YAML rendering now correctly skips whitespace-only template output

## [0.2.1] - 2025-01-25

### ✨ Added

- Comprehensive wizard documentation (`docs/wizard.md`)
- Full configuration reference (`docs/configuration.md`)
- System architecture documentation (`docs/architecture.md`)

### 🔄 Changed

- Streamlined README with focus on interactive wizard as primary setup method
- Updated server types documentation with all Hetzner families

## [0.2.0] - 2025-01-25

### ✨ Added

- **Interactive Config Builder** (`k8zner init`) - New wizard for creating cluster configurations
  - Hierarchical server selection: choose architecture (x86/ARM) first, then category (shared/dedicated/cost-optimized)
  - Updated Hetzner instance types with current pricing data (CX, CPX, CCX, CAX families)
  - CNI selection as dedicated choice: Cilium, Talos default (Flannel), or none
  - Optional SSH keys - auto-generated if not provided
  - Minimal YAML output by default (only essential values)
  - `--full` flag for complete YAML with all configuration options
  - `--advanced` flag for network, security, and Cilium customization
- Configurable timeouts for cluster operations
- CI pipeline optimization with parallel test jobs
- Codecov integration for test coverage reporting

### 🔄 Changed

- Cluster bootstrap now uses configurable timeouts instead of hardcoded values
- Network configuration uses improved retry logic

## [0.1.1] - 2025-01-20

### 🐛 Fixed

- Minor bug fixes and stability improvements

## [0.1.0] - 2025-01-15

### ✨ Added

- Initial public release
- Declarative YAML configuration for cluster definition
- Talos Linux support for immutable, secure Kubernetes nodes
- High availability control plane with automatic failover
- Worker pool management with labels and taints
- Auto-scaling support via Cluster Autoscaler integration
- Snapshot-based provisioning for fast node creation
- Full addon suite:
  - Cilium CNI with network policies
  - Hetzner Cloud Controller Manager (CCM)
  - Hetzner CSI Driver for persistent volumes
  - cert-manager for TLS certificate management
  - ingress-nginx for ingress routing
  - metrics-server for Kubernetes metrics
- Self-contained binary (no kubectl or talosctl required at runtime)
- Shell completions for bash, zsh, fish, and PowerShell
- Commands:
  - `apply` - Create or update cluster infrastructure
  - `destroy` - Tear down all cluster resources
  - `upgrade` - Upgrade Talos and/or Kubernetes versions
  - `image build` - Build Talos image snapshots
  - `image delete` - Delete Talos image snapshots
  - `completion` - Generate shell completion scripts
  - `version` - Print version information

### 🔐 Security

- Private network isolation for inter-node communication
- Firewall rules for control plane and worker nodes
- Secrets stored locally in `./secrets/` directory
- No credentials stored in cluster state

[0.10.0]: https://github.com/milankappen/k8zner/compare/v0.9.3...v0.10.0
[0.9.3]: https://github.com/milankappen/k8zner/compare/v0.9.2...v0.9.3
[0.9.2]: https://github.com/milankappen/k8zner/compare/v0.9.1...v0.9.2
[0.9.1]: https://github.com/milankappen/k8zner/compare/v0.9.0...v0.9.1
[0.9.0]: https://github.com/milankappen/k8zner/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/milankappen/k8zner/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/milankappen/k8zner/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/milankappen/k8zner/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/milankappen/k8zner/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/milankappen/k8zner/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/milankappen/k8zner/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/milankappen/k8zner/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/milankappen/k8zner/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/milankappen/k8zner/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/milankappen/k8zner/releases/tag/v0.1.0
