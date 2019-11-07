# If you update this file, please follow
# https://suva.sh/posts/well-documented-makefiles

.DEFAULT_GOAL:=help

ifeq ($(GOPROXY),)
export GOPROXY = direct
endif

# Directories.
TOOLS_DIR := hack/tools
TOOLS_BIN_DIR := $(TOOLS_DIR)/bin
BIN_DIR := bin

# Binaries.
GOLANGCI_LINT := $(TOOLS_BIN_DIR)/golangci-lint

# Golang build env
LDFLAGS := -s -w

GIT_BRANCH = $(shell git rev-parse --abbrev-ref HEAD | sed 's/\///g')
GIT_COMMIT = $(shell git rev-parse HEAD)
GIT_SHA    = $(shell git rev-parse --short HEAD)
GIT_TAG    = $(shell git describe --tags --abbrev=0 --exact-match 2>/dev/null)

VERSION = $(GIT_BRANCH).$(GIT_SHA)
ifneq ($(GIT_TAG),)
	VERSION = $(GIT_TAG)
endif

LDFLAGS += -X github.com/criticalstack/e2d/pkg/buildinfo.GitSHA=$(GIT_SHA)
LDFLAGS += -X github.com/criticalstack/e2d/pkg/buildinfo.Version=$(VERSION)
GOFLAGS = -gcflags "all=-trimpath=$(PWD)" -asmflags "all=-trimpath=$(PWD)"

GO_BUILD_ENV_VARS := GO111MODULE=on CGO_ENABLED=0

.PHONY: build test test-manager clean

build: clean ## Build the e2d golang binary
	@$(GO_BUILD_ENV_VARS) go build -o bin/e2d $(GOFLAGS) -ldflags '$(LDFLAGS)' ./cmd/e2d

test: ## Run all tests
	go test ./...

test-manager: ## Test the manager package
	go test ./pkg/manager -test.long

clean: ## Cleanup the project folders
	@rm -rf ./bin/*
	@rm -rf hack/tools/bin

.PHONY: lint

lint: $(GOLANGCI_LINT) ## Lint codebase
	$(GOLANGCI_LINT) run -v

lint-full: $(GOLANGCI_LINT) ## Run slower linters to detect possible issues
	$(GOLANGCI_LINT) run -v --fast=false

##@ Helpers

.PHONY: help

$(GOLANGCI_LINT): $(TOOLS_DIR)/go.mod # Build golangci-lint from tools folder.
	cd $(TOOLS_DIR); go build -tags=tools -o $(BIN_DIR)/golangci-lint github.com/golangci/golangci-lint/cmd/golangci-lint

help:  ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
