# Auto generated binary variables helper managed by https://github.com/bwplotka/bingo v0.9. DO NOT EDIT.
# All tools are designed to be build inside $GOBIN.
BINGO_DIR := $(dir $(lastword $(MAKEFILE_LIST)))
GOPATH ?= $(shell go env GOPATH)
GOBIN  ?= $(firstword $(subst :, ,${GOPATH}))/bin
GO     ?= $(shell which go)

# Below generated variables ensure that every time a tool under each variable is invoked, the correct version
# will be used; reinstalling only if needed.
# For example for bingo variable:
#
# In your main Makefile (for non array binaries):
#
#include .bingo/Variables.mk # Assuming -dir was set to .bingo .
#
#command: $(BINGO)
#	@echo "Running bingo"
#	@$(BINGO) <flags/args..>
#
BINGO := $(GOBIN)/bingo-v0.9.0
$(BINGO): $(BINGO_DIR)/bingo.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/bingo-v0.9.0"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=bingo.mod -o=$(GOBIN)/bingo-v0.9.0 "github.com/bwplotka/bingo"

CONTROLLER_GEN := $(GOBIN)/controller-gen-v0.16.1
$(CONTROLLER_GEN): $(BINGO_DIR)/controller-gen.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/controller-gen-v0.16.1"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=controller-gen.mod -o=$(GOBIN)/controller-gen-v0.16.1 "sigs.k8s.io/controller-tools/cmd/controller-gen"

GINKGO := $(GOBIN)/ginkgo-v2.21.0
$(GINKGO): $(BINGO_DIR)/ginkgo.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/ginkgo-v2.21.0"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=ginkgo.mod -o=$(GOBIN)/ginkgo-v2.21.0 "github.com/onsi/ginkgo/v2/ginkgo"

GOLANGCI_LINT := $(GOBIN)/golangci-lint-v1.60.3
$(GOLANGCI_LINT): $(BINGO_DIR)/golangci-lint.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/golangci-lint-v1.60.3"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=golangci-lint.mod -o=$(GOBIN)/golangci-lint-v1.60.3 "github.com/golangci/golangci-lint/cmd/golangci-lint"

GORELEASER := $(GOBIN)/goreleaser-v1.26.2
$(GORELEASER): $(BINGO_DIR)/goreleaser.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/goreleaser-v1.26.2"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=goreleaser.mod -o=$(GOBIN)/goreleaser-v1.26.2 "github.com/goreleaser/goreleaser"

KIND := $(GOBIN)/kind-v0.24.0
$(KIND): $(BINGO_DIR)/kind.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/kind-v0.24.0"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=kind.mod -o=$(GOBIN)/kind-v0.24.0 "sigs.k8s.io/kind"

KUSTOMIZE := $(GOBIN)/kustomize-v5.4.3
$(KUSTOMIZE): $(BINGO_DIR)/kustomize.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/kustomize-v5.4.3"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=kustomize.mod -o=$(GOBIN)/kustomize-v5.4.3 "sigs.k8s.io/kustomize/kustomize/v5"

SETUP_ENVTEST := $(GOBIN)/setup-envtest-v0.0.0-20240820183333-e6c3d139d2b6
$(SETUP_ENVTEST): $(BINGO_DIR)/setup-envtest.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/setup-envtest-v0.0.0-20240820183333-e6c3d139d2b6"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=setup-envtest.mod -o=$(GOBIN)/setup-envtest-v0.0.0-20240820183333-e6c3d139d2b6 "sigs.k8s.io/controller-runtime/tools/setup-envtest"

