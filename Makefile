# margin — common dev tasks. Run `make` (or `make help`) for the list.

BINARY  := margin
PKG     := ./cmd/margin
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOFLAGS := -trimpath
export CGO_ENABLED := 0

.DEFAULT_GOAL := help

.PHONY: help
help: ## List available targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the static binary into ./margin
	go build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o $(BINARY) $(PKG)

.PHONY: install
install: ## Install margin onto your PATH ($GOBIN or $GOPATH/bin)
	go install $(GOFLAGS) -ldflags='$(LDFLAGS)' $(PKG)

.PHONY: run
run: ## Run the server (make run ARGS="--port 8848")
	go run $(PKG) serve $(ARGS)

.PHONY: test
test: ## Run tests with the race detector
	go test -race ./...

.PHONY: cover
cover: ## Run tests with a coverage summary
	go test -race -cover ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: fmt
fmt: ## Format the tree (gofumpt, falling back to gofmt)
	@gofumpt -w . 2>/dev/null || gofmt -w .

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run

.PHONY: e2e
e2e: build ## Run the Playwright end-to-end tests
	cd e2e && npm install && npx playwright install chromium && npx playwright test

.PHONY: check
check: vet lint test ## Pre-push gate: vet + lint + race tests

.PHONY: clean
clean: ## Remove build/test artifacts
	rm -f $(BINARY)
	rm -rf dist e2e/test-results e2e/playwright-report e2e/shots e2e/e2e-data
