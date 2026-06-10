.PHONY: fmt lint test test-coverage test-unit test-integration test-kind build install check e2e e2e-fast e2e-snapshot-only clean help \
       setup-hooks scan-secrets setup-envtest setup-kind sync-crds check-crds sync-operator-chart check-operator-chart \
       generate manifests check-manifests

# Default target
.DEFAULT_GOAL := help

# Envtest configuration
ENVTEST_K8S_VERSION ?= 1.35.0
ENVTEST_ASSETS_DIR ?= $(shell pwd)/bin/envtest
ENVTEST := $(shell pwd)/bin/setup-envtest

# Code generation
CONTROLLER_GEN_VERSION ?= v0.20.0
CONTROLLER_GEN := $(shell pwd)/bin/controller-gen

fmt:
	go fmt ./...

lint:
	golangci-lint run ./...

test: test-unit
	@echo "All unit tests passed!"

test-unit:
	go test -v -race ./...

test-integration: setup-envtest
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(ENVTEST_ASSETS_DIR) -p path)" \
		go test -v -race -tags=integration ./internal/operator/controller/...

test-all: test-unit test-integration
	@echo "All tests (unit + integration) passed!"

# Kind-based integration tests (tests addons against a local k8s cluster)
# Faster than full E2E but tests real k8s interactions
test-kind: setup-kind
	go test -v -tags=kind -timeout=30m ./tests/kind/...

# Run specific test layer (e.g., make test-kind-layer LAYER=01_CRDs)
# Available layers: 01_CRDs, 02_Core, 03_Ingress, 04_GitOps, 05_Monitoring, 06_Integration
test-kind-layer: setup-kind
	go test -v -tags=kind -timeout=15m -run "TestKindAddons/$(LAYER)" ./tests/kind/...

# Quick smoke test - just CRDs (fast, ~2 min)
test-kind-smoke: setup-kind
	go test -v -tags=kind -timeout=5m -run "TestKindAddons/01_CRDs" ./tests/kind/...

# Core infrastructure test (cert-manager, metrics-server, ~5 min)
test-kind-core: setup-kind
	go test -v -tags=kind -timeout=10m -run "TestKindAddons/0[12]_" ./tests/kind/...

# Keep kind cluster after test (for debugging)
test-kind-keep: setup-kind
	KEEP_KIND_CLUSTER=1 go test -v -tags=kind -timeout=30m ./tests/kind/...

# Delete kind test cluster manually
test-kind-cleanup:
	kind delete cluster --name k8zner-test 2>/dev/null || true

# Setup kind if not already installed
setup-kind:
	@if ! command -v kind >/dev/null 2>&1; then \
		echo "Installing kind..."; \
		go install sigs.k8s.io/kind@latest; \
	fi
	@if ! command -v kubectl >/dev/null 2>&1; then \
		echo "kubectl not found. Please install kubectl."; \
		exit 1; \
	fi

test-coverage:
	go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

build:
	go build -o bin/k8zner ./cmd/k8zner

install: build
	cp bin/k8zner $(GOPATH)/bin/ 2>/dev/null || cp bin/k8zner /usr/local/bin/

# Run all checks: format, lint, test, and build
check: fmt lint test build
	@echo "All checks passed!"

# Full e2e test suite (use in CI)
# Builds snapshots, runs all tests, cleans up
e2e:
	go test -v -timeout=1h -tags=e2e ./tests/e2e/...

# Fast e2e for local development
# Keeps snapshots between runs, skips snapshot build test
e2e-fast:
	@echo "Running fast e2e tests (keeping snapshots, skipping build test)"
	@echo "WARNING: This skips TestSnapshotCreation - use 'make e2e' for full validation"
	E2E_KEEP_SNAPSHOTS=true E2E_SKIP_SNAPSHOT_BUILD_TEST=true go test -v -timeout=1h -tags=e2e ./tests/e2e/...

# Test snapshot creation only
# Useful for verifying image builder changes
e2e-snapshot-only:
	go test -v -timeout=30m -tags=e2e -run TestSnapshotCreation ./tests/e2e/...

# Regenerate deepcopy functions from api/ types
generate: $(CONTROLLER_GEN)
	$(CONTROLLER_GEN) object paths=./api/...

# Regenerate CRD manifests from api/ types and propagate to all copies
manifests: $(CONTROLLER_GEN) generate
	$(CONTROLLER_GEN) crd paths=./api/... output:crd:artifacts:config=config/crd/bases
	$(MAKE) sync-crds

# Check that generated manifests and deepcopy code are up to date (for CI)
check-manifests: manifests
	@git diff --exit-code config/crd deploy/crds internal/addons/operator-chart/crds api/v1alpha1/zz_generated.deepcopy.go || \
		(echo "ERROR: generated manifests out of date. Run 'make manifests' and commit the result." && exit 1)
	@echo "Generated manifests up to date."

$(CONTROLLER_GEN):
	@mkdir -p $(shell pwd)/bin
	GOBIN=$(shell pwd)/bin go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)

# Sync CRD from canonical source to deploy/ and operator-chart/
sync-crds:
	@echo "Syncing CRDs from config/crd/bases/ ..."
	cp config/crd/bases/k8zner.io_k8znerclusters.yaml deploy/crds/k8zner.io_k8znerclusters.yaml
	cp config/crd/bases/k8zner.io_k8znerclusters.yaml internal/addons/operator-chart/crds/k8zner.io_k8znerclusters.yaml
	@echo "CRDs synced."

# Check that CRD copies are in sync (for CI)
check-crds:
	@diff -q config/crd/bases/k8zner.io_k8znerclusters.yaml deploy/crds/k8zner.io_k8znerclusters.yaml || \
		(echo "ERROR: deploy/crds/ CRD out of sync. Run 'make sync-crds'" && exit 1)
	@diff -q config/crd/bases/k8zner.io_k8znerclusters.yaml internal/addons/operator-chart/crds/k8zner.io_k8znerclusters.yaml || \
		(echo "ERROR: operator-chart/crds/ CRD out of sync. Run 'make sync-crds'" && exit 1)
	@echo "CRDs in sync."

# Sync operator chart from deploy/helm/ (source of truth) to internal/addons/operator-chart/
sync-operator-chart:
	@echo "Syncing operator chart from deploy/helm/k8zner-operator/ ..."
	@for f in Chart.yaml values.yaml; do \
		cp deploy/helm/k8zner-operator/$$f internal/addons/operator-chart/$$f; \
	done
	@cp deploy/helm/k8zner-operator/templates/* internal/addons/operator-chart/templates/
	@echo "Operator chart synced."

# Check that operator chart copies are in sync (for CI)
check-operator-chart:
	@diff -rq deploy/helm/k8zner-operator/Chart.yaml internal/addons/operator-chart/Chart.yaml || \
		(echo "ERROR: operator-chart/Chart.yaml out of sync. Run 'make sync-operator-chart'" && exit 1)
	@diff -rq deploy/helm/k8zner-operator/values.yaml internal/addons/operator-chart/values.yaml || \
		(echo "ERROR: operator-chart/values.yaml out of sync. Run 'make sync-operator-chart'" && exit 1)
	@diff -rq deploy/helm/k8zner-operator/templates/ internal/addons/operator-chart/templates/ || \
		(echo "ERROR: operator-chart/templates/ out of sync. Run 'make sync-operator-chart'" && exit 1)
	@echo "Operator chart in sync."

clean:
	rm -rf bin/ coverage.out coverage.html

# Install git hooks for secret detection
setup-hooks:
	@echo "Installing git hooks..."
	@if command -v gitleaks >/dev/null 2>&1; then \
		echo "gitleaks found at: $$(which gitleaks)"; \
	else \
		echo "Installing gitleaks..."; \
		go install github.com/zricethezav/gitleaks/v8@latest; \
	fi
	@cp scripts/pre-commit .git/hooks/pre-commit 2>/dev/null || \
		echo '#!/bin/sh\ngitleaks protect --staged --redact -v' > .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "Pre-commit hook installed successfully!"

# Setup envtest - download controller-runtime envtest binaries
setup-envtest: $(ENVTEST)
	$(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(ENVTEST_ASSETS_DIR)

$(ENVTEST):
	@mkdir -p $(shell pwd)/bin
	GOBIN=$(shell pwd)/bin go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

# Scan for secrets in git history
scan-secrets:
	@echo "Scanning git history for secrets..."
	@if command -v gitleaks >/dev/null 2>&1; then \
		gitleaks detect --source . --verbose --redact; \
	else \
		echo "gitleaks not found. Install with: go install github.com/zricethezav/gitleaks/v8@latest"; \
		exit 1; \
	fi

help:
	@echo "k8zner development commands:"
	@echo ""
	@echo "Build & Install:"
	@echo "  make build          Build the binary"
	@echo "  make install        Build and install to GOPATH/bin or /usr/local/bin"
	@echo "  make clean          Remove build artifacts"
	@echo ""
	@echo "Testing (fastest to slowest):"
	@echo "  make test           Run unit tests (~1s)"
	@echo "  make test-integration  Controller tests with envtest (~15s)"
	@echo "  make test-kind      Full kind addon tests (~20m)"
	@echo "  make e2e            Full E2E on Hetzner Cloud (~30m+)"
	@echo ""
	@echo "Kind Test Options:"
	@echo "  make test-kind-smoke   Quick CRD-only test (~2m)"
	@echo "  make test-kind-core    CRDs + cert-manager + metrics (~5m)"
	@echo "  make test-kind-layer LAYER=03_Ingress  Test specific layer"
	@echo "  make test-kind-keep    Keep cluster after tests (for debugging)"
	@echo "  make test-kind-cleanup Delete test cluster"
	@echo ""
	@echo "  Available layers: 01_CRDs, 02_Core, 03_Ingress, 04_GitOps, 05_Monitoring, 06_Integration"
	@echo ""
	@echo "E2E Test Options:"
	@echo "  make e2e            Full suite (requires HCLOUD_TOKEN)"
	@echo "  make e2e-fast       Skip snapshot build"
	@echo "  make e2e-snapshot-only  Test snapshot creation only"
	@echo ""
	@echo "Code Quality:"
	@echo "  make lint           Run golangci-lint"
	@echo "  make fmt            Format code with go fmt"
	@echo "  make test-coverage  Generate coverage report"
	@echo "  make check          Run all checks (fmt, lint, test, build)"
	@echo ""
	@echo "Setup:"
	@echo "  make setup-envtest  Download envtest binaries"
	@echo "  make setup-kind     Install kind"
	@echo "  make setup-hooks    Install pre-commit secret detection"
	@echo ""
	@echo "Sync:"
	@echo "  make sync-crds             Sync CRDs from config/crd/bases/"
	@echo "  make check-crds            Verify CRD copies are in sync"
	@echo "  make sync-operator-chart   Sync operator chart from deploy/helm/"
	@echo "  make check-operator-chart  Verify operator chart copies are in sync"
	@echo ""
	@echo "Security:"
	@echo "  make scan-secrets   Scan git history for leaked secrets"
