BINARY  := ch-compass
PKG     := ./cmd/ch-compass
BIN_DIR := bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/josemimbre/ch-compass/internal/cli.version=$(VERSION)

.DEFAULT_GOAL := build

.PHONY: build
build: ## Build the binary into bin/
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(PKG)

.PHONY: install
install: ## Install the binary into $GOPATH/bin
	go install -ldflags "$(LDFLAGS)" $(PKG)

.PHONY: test
test: ## Run tests
	go test ./...

.PHONY: cover
cover: ## Run tests with coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

.PHONY: lint
lint: ## Run go vet
	go vet ./...

.PHONY: fmt
fmt: ## Format the code
	gofmt -s -w .

.PHONY: tidy
tidy: ## Tidy go.mod / go.sum
	go mod tidy

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) coverage.out

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
