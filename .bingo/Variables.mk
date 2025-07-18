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

CONTROLLER_GEN := $(GOBIN)/controller-gen-v0.18.0
$(CONTROLLER_GEN): $(BINGO_DIR)/controller-gen.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/controller-gen-v0.18.0"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=controller-gen.mod -o=$(GOBIN)/controller-gen-v0.18.0 "sigs.k8s.io/controller-tools/cmd/controller-gen"

CRD_DIFF := $(GOBIN)/crd-diff-v0.2.0
$(CRD_DIFF): $(BINGO_DIR)/crd-diff.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/crd-diff-v0.2.0"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=crd-diff.mod -o=$(GOBIN)/crd-diff-v0.2.0 "github.com/everettraven/crd-diff"

CRD_REF_DOCS := $(GOBIN)/crd-ref-docs-v0.1.0
$(CRD_REF_DOCS): $(BINGO_DIR)/crd-ref-docs.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/crd-ref-docs-v0.1.0"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=crd-ref-docs.mod -o=$(GOBIN)/crd-ref-docs-v0.1.0 "github.com/elastic/crd-ref-docs"

GOLANGCI_LINT := $(GOBIN)/golangci-lint-v2.1.6
$(GOLANGCI_LINT): $(BINGO_DIR)/golangci-lint.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/golangci-lint-v2.1.6"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=golangci-lint.mod -o=$(GOBIN)/golangci-lint-v2.1.6 "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"

GORELEASER := $(GOBIN)/goreleaser-v1.26.2
$(GORELEASER): $(BINGO_DIR)/goreleaser.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/goreleaser-v1.26.2"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=goreleaser.mod -o=$(GOBIN)/goreleaser-v1.26.2 "github.com/goreleaser/goreleaser"

HELM := $(GOBIN)/helm-v3.18.4
$(HELM): $(BINGO_DIR)/helm.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/helm-v3.18.4"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=helm.mod -o=$(GOBIN)/helm-v3.18.4 "helm.sh/helm/v3/cmd/helm"

KIND := $(GOBIN)/kind-v0.29.0
$(KIND): $(BINGO_DIR)/kind.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/kind-v0.29.0"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=kind.mod -o=$(GOBIN)/kind-v0.29.0 "sigs.k8s.io/kind"

KUSTOMIZE := $(GOBIN)/kustomize-v5.6.0
$(KUSTOMIZE): $(BINGO_DIR)/kustomize.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/kustomize-v5.6.0"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=kustomize.mod -o=$(GOBIN)/kustomize-v5.6.0 "sigs.k8s.io/kustomize/kustomize/v5"

OPERATOR_SDK := $(GOBIN)/operator-sdk-v1.39.1
$(OPERATOR_SDK): $(BINGO_DIR)/operator-sdk.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/operator-sdk-v1.39.1"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -ldflags=-X=github.com/operator-framework/operator-sdk/internal/version.Version=v1.34.1 -mod=mod -modfile=operator-sdk.mod -o=$(GOBIN)/operator-sdk-v1.39.1 "github.com/operator-framework/operator-sdk/cmd/operator-sdk"

OPM := $(GOBIN)/opm-v1.51.0
$(OPM): $(BINGO_DIR)/opm.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/opm-v1.51.0"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=opm.mod -o=$(GOBIN)/opm-v1.51.0 "github.com/operator-framework/operator-registry/cmd/opm"

SETUP_ENVTEST := $(GOBIN)/setup-envtest-v0.0.0-20250620151452-b9a9ca01fd37
$(SETUP_ENVTEST): $(BINGO_DIR)/setup-envtest.mod
	@# Install binary/ries using Go 1.14+ build command. This is using bwplotka/bingo-controlled, separate go module with pinned dependencies.
	@echo "(re)installing $(GOBIN)/setup-envtest-v0.0.0-20250620151452-b9a9ca01fd37"
	@cd $(BINGO_DIR) && GOWORK=off $(GO) build -mod=mod -modfile=setup-envtest.mod -o=$(GOBIN)/setup-envtest-v0.0.0-20250620151452-b9a9ca01fd37 "sigs.k8s.io/controller-runtime/tools/setup-envtest"

