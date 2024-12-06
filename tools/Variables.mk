TOOLS_DIR := $(dir $(lastword $(MAKEFILE_LIST)))
GO ?= $(shell which go)


CONTROLLER_GEN ?= $(GO) tool -modfile=$(TOOLS_DIR)/controller-gen.mod controller-gen

CRD_DIFF ?= $(GO) tool -modfile=$(TOOLS_DIR)/crd-diff.mod crd-diff

CRD_REF_DOCS ?= $(GO) tool -modfile=$(TOOLS_DIR)/crd-ref-docs.mod crd-ref-docs

GOLANGCI_LINT ?= $(GO) tool -modfile=$(TOOLS_DIR)/golangci-lint.mod golangci-lint

GORELEASER ?= $(GO) tool -modfile=$(TOOLS_DIR)/goreleaser.mod goreleaser

KIND ?= $(GO) tool -modfile=$(TOOLS_DIR)/kind.mod kind

KUSTOMIZE ?= $(GO) tool -modfile=kustomize.mod kustomize

# TODO: Check if we need to use go run instead in order to be able to pass build flags such as -ldflags as go tool doesn't currently support these
# -ldflags=-X=github.com/operator-framework/operator-sdk/internal/version.Version=v1.34.1
OPERATOR_SDK ?= $(GO) tool -modfile=operator-sdk.mod operator-sdk

SETUP_ENVTEST ?= $(GO) tool -modfile=setup-envtest.mod setup-envtest
