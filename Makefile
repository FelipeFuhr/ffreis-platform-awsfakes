SHELL := /bin/bash

GO ?= go

# Coverage floor per the workspace-wide 75% invariant (this repo already
# measures ~93%; the gate exists so a future regression is caught in CI,
# not the threshold this repo is expected to hover at).
COVERAGE_THRESHOLD ?= 75
MUTATION_THRESHOLD ?= 60
# scan-fix(gremlins:path-syntax): a plain directory, NOT a Go "/..." wildcard —
# gremlins silently reports "No results to report" (0 mutants, gate passes
# vacuously) for "./..." or "./awsfakes/...". Verified locally: "./awsfakes"
# finds 9 runnable mutants; "./..." and "./awsfakes/..." find none.
MUTATION_PACKAGES  ?= ./awsfakes
FUZZ_PACKAGES      ?= ./...
FUZZ_TIME          ?= 30s

.PHONY: help build-all test lint tidy fmt fmt-check coverage coverage-gate integration-coverage-gate mutation fuzz secrets-scan-staged

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build-all: ## Build all packages required by the lefthook release tier
	$(GO) build ./...

fuzz: ## Run all Fuzz* targets for FUZZ_TIME each (no-op when none exist)
	@for pkg in $$($(GO) list $(FUZZ_PACKAGES)); do targets=$$($(GO) test -list 'Fuzz.*' "$$pkg" 2>/dev/null | grep '^Fuzz' || true); for target in $$targets; do $(GO) test -run='^$$' -fuzz="^$${target}$$" -fuzztime="$(FUZZ_TIME)" "$$pkg"; done; done

secrets-scan-staged: ## Scan staged changes for credentials
	gitleaks protect --staged --redact

test: ## Run tests with race + shuffle
	$(GO) test -race -shuffle=on ./...

lint: ## Run golangci-lint (if installed)
	@command -v golangci-lint >/dev/null && golangci-lint run ./... || echo "golangci-lint not installed; skipping"

fmt: ## Format all Go files in place
	gofmt -w -s .

fmt-check: ## Fail if any file would be reformatted
	@out=$$(gofmt -l -s .); \
	if [ -n "$$out" ]; then \
		echo "Unformatted files:"; echo "$$out"; \
		echo "Run: make fmt"; \
		exit 1; \
	fi

tidy: ## go mod tidy
	$(GO) mod tidy

coverage: ## Generate an HTML coverage report
	$(GO) test -race -shuffle=on -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

coverage-gate: ## Fail if total coverage is below $(COVERAGE_THRESHOLD)%
	$(GO) test -race -shuffle=on -coverprofile=coverage.out ./...
	@$(GO) tool cover -func=coverage.out | tee /dev/stderr | \
		awk '/^total:/ { gsub(/%/, "", $$3); cov = $$3 + 0; if (cov < $(COVERAGE_THRESHOLD)) \
		{ print "Coverage " cov "% is below threshold $(COVERAGE_THRESHOLD)%"; exit 1 } }'
	# scan-fix(coverage-gate:numeric-compare): "cov = $$3 + 0" forces a numeric
	# coercion before the comparison. Without it, gsub() strips gawk's STRNUM
	# flag from $3, so "$3 < threshold" becomes a STRING compare and
	# "93.6" < "100" is true lexically only for the leading character
	# ('9' > '1'), i.e. the gate always reports NOT-below-threshold and never
	# fails — verified locally by running this target with
	# COVERAGE_THRESHOLD=100 before adding the "+ 0" fix.

integration-coverage-gate: ## Run //go:build integration tests; fail if below $(COVERAGE_THRESHOLD)% (no-op if none exist)
	@if ! grep -rl '^//go:build integration' --include='*.go' . >/dev/null 2>&1; then \
		echo "No '//go:build integration' files found — skipping integration-coverage-gate."; \
		exit 0; \
	fi; \
	$(GO) test -tags=integration -race -shuffle=on -coverprofile=coverage-integration.out ./...; \
	$(GO) tool cover -func=coverage-integration.out | tee /dev/stderr | \
		awk '/^total:/ { gsub(/%/, "", $$3); cov = $$3 + 0; if (cov < $(COVERAGE_THRESHOLD)) \
		{ print "Integration coverage " cov "% is below threshold $(COVERAGE_THRESHOLD)%"; exit 1 } }'

mutation: ## Run mutation testing with gremlins (slow — CI only)
	@which gremlins >/dev/null 2>&1 || go install github.com/go-gremlins/gremlins/cmd/gremlins@latest
	gremlins unleash --threshold-efficacy $(MUTATION_THRESHOLD) $(MUTATION_PACKAGES)
