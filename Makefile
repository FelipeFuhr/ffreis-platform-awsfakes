SHELL := /bin/bash

GO ?= go

.PHONY: help test lint tidy

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: ## Run tests with race + shuffle
	$(GO) test -race -shuffle=on ./...

lint: ## Run golangci-lint (if installed)
	@command -v golangci-lint >/dev/null && golangci-lint run ./... || echo "golangci-lint not installed; skipping"

tidy: ## go mod tidy
	$(GO) mod tidy
