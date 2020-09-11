.DEFAULT_GOAL:=help

BIN_DIR       ?= bin
TOOLS_DIR     := hack/tools
TOOLS_BIN_DIR := $(TOOLS_DIR)/bin
GOLANGCI_LINT := $(TOOLS_BIN_DIR)/golangci-lint

GIT_BRANCH = $(shell git rev-parse --abbrev-ref HEAD | sed 's/\///g')
GIT_COMMIT = $(shell git rev-parse HEAD)
GIT_SHA    = $(shell git rev-parse --short HEAD)
GIT_TAG    = $(shell git describe --tags --abbrev=0 --exact-match 2>/dev/null)

VERSION = $(GIT_BRANCH).$(GIT_SHA)
ifneq ($(GIT_TAG),)
	VERSION = $(GIT_TAG)
endif

LDFLAGS := -s -w
LDFLAGS += -X "github.com/criticalstack/e2d/internal/buildinfo.Date=$(shell date -u +'%Y-%m-%dT%TZ')"
LDFLAGS += -X "github.com/criticalstack/e2d/internal/buildinfo.GitSHA=$(GIT_SHA)"
LDFLAGS += -X "github.com/criticalstack/e2d/internal/buildinfo.Version=$(VERSION)"
GOFLAGS = -gcflags "all=-trimpath=$(PWD)" -asmflags "all=-trimpath=$(PWD)"

GO_BUILD_ENV_VARS := GO111MODULE=on CGO_ENABLED=0

##@ Building

.PHONY: e2d

e2d: ## Build the e2d golang binary
	$(GO_BUILD_ENV_VARS) go build -o bin/e2d $(GOFLAGS) -ldflags '$(LDFLAGS)' ./cmd/e2d

.PHONY: update-codegen
update-codegen: ## Update generated code (slow)
	@echo "Updating generated code files ..."
	@echo "  *** This can be slow and does not need to run every build ***"
	@hack/tools/update-codegen.sh

##@ Testing

.PHONY: test test-e2e lint lint-full

test: ## Run all tests
	@go test $(shell go list ./... | grep -v e2e)

test-e2e: ## Run e2e tests
	@go test ./e2e -parallel=16 -count=1

lint: $(GOLANGCI_LINT) ## Lint codebase
	$(GOLANGCI_LINT) run -v

lint-full: $(GOLANGCI_LINT) ## Run slower linters to detect possible issues
	$(GOLANGCI_LINT) run -v --fast=false

##@ Helpers

.PHONY: help clean

$(GOLANGCI_LINT): $(TOOLS_DIR)/go.mod # Build golangci-lint from tools folder.
	cd $(TOOLS_DIR); go build -tags=tools -o $(BIN_DIR)/golangci-lint github.com/golangci/golangci-lint/cmd/golangci-lint

clean: ## Cleanup the project folders
	@rm -rf ./bin/*
	@rm -rf hack/tools/bin

help:  ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)


# TODO: move to hack/tools
generate:
	protoc -I pkg/manager/e2dpb \
		-I vendor/ \
		--gogo_out=plugins=grpc,paths=source_relative,\
Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,\
Mgoogle/protobuf/duration.proto=github.com/gogo/protobuf/types,\
Mgoogle/protobuf/empty.proto=github.com/gogo/protobuf/types,\
Mgoogle/api/annotations.proto=github.com/gogo/googleapis/google/api,\
Mgoogle/protobuf/field_mask.proto=github.com/gogo/protobuf/types:\
./pkg/manager/e2dpb/ \
		pkg/manager/e2dpb/e2dpb.proto

install:
	go get github.com/gogo/protobuf/protoc-gen-gogo@v1.2.1
