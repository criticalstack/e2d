# If you update this file, please follow
# https://suva.sh/posts/well-documented-makefiles

.DEFAULT_GOAL:=help

# Use GOPROXY environment variable if set
GOPROXY := $(shell go env GOPROXY)
ifeq ($(GOPROXY),)
GOPROXY := https://proxy.golang.org
endif
export GOPROXY

# Directories.
TOOLS_DIR := hack
TOOLS_BIN_DIR := $(TOOLS_DIR)/bin

# Binaries.
GOLANGCI_LINT := $(TOOLS_BIN_DIR)/golangci-lint

# Golang build env
GCFLAGS = -gcflags "all=-trimpath=$(PWD)" -asmflags "all=-trimpath=$(PWD)"
GO_BUILD_ENV_VARS := GO111MODULE=on CGO_ENABLED=0

.PHONY: build test test-manager clean

build: clean ## Build the e2d golang binary
	$(GO_BUILD_ENV_VARS) go build -o bin/e2d $(GCFLAGS) ./cmd/e2d

test: ## Run all tests
	go test ./...

test-manager: ## Test the manager package
	go test ./pkg/manager -test.long

clean: ## Cleanup the project folders
	@rm -rf ./bin/*

##@ Helpers

.PHONY: help

help:  ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

