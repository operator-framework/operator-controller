###########################
# Configuration Variables #
###########################
# Image URL to use all building/pushing image targets
export IMAGE_REPO ?= quay.io/operator-framework/operator-controller
export IMAGE_TAG ?= devel
export CERT_MGR_VERSION ?= v1.9.0
export CATALOGD_VERSION ?= $(shell go list -mod=mod -m -f "{{.Version}}" github.com/operator-framework/catalogd)
export RUKPAK_VERSION=$(shell go list -mod=mod -m -f "{{.Version}}" github.com/operator-framework/rukpak)
export WAIT_TIMEOUT ?= 60s
IMG?=$(IMAGE_REPO):$(IMAGE_TAG)
TESTDATA_DIR := testdata

# setup-envtest on *nix uses XDG_DATA_HOME, falling back to HOME, as the default storage directory. Some CI setups
# don't have XDG_DATA_HOME set; in those cases, we set it here so setup-envtest functions correctly. This shouldn't
# affect developers.
export XDG_DATA_HOME ?= /tmp/.local/share

# bingo manages consistent tooling versions for things like kind, kustomize, etc.
include .bingo/Variables.mk

OPERATOR_CONTROLLER_NAMESPACE ?= operator-controller-system
KIND_CLUSTER_NAME ?= operator-controller

CONTAINER_RUNTIME ?= docker

KUSTOMIZE_BUILD_DIR ?= config/default

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# Disable -j flag for make
.NOTPARALLEL:

.DEFAULT_GOAL := build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Run golangci linter.
	$(GOLANGCI_LINT) run --build-tags $(GO_BUILD_TAGS) $(GOLANGCI_LINT_ARGS)

.PHONY: tidy
tidy: ## Update dependencies.
	$(Q)go mod tidy

.PHONY: manifests
manifests: $(CONTROLLER_GEN) ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: $(CONTROLLER_GEN) ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: verify
verify: tidy fmt generate ## Verify the current code generation.
	git diff --exit-code

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet test-unit e2e ## Run all tests.

.PHONY: test-e2e
FOCUS := $(if $(TEST),-v -focus "$(TEST)")
E2E_FLAGS ?= ""
test-e2e: $(GINKGO) ## Run the e2e tests
	$(GINKGO) --tags $(GO_BUILD_TAGS) $(E2E_FLAGS) -trace -progress $(FOCUS) test/e2e

.PHONY: test-unit
ENVTEST_VERSION = $(shell go list -m k8s.io/client-go | cut -d" " -f2 | sed 's/^v0\.\([[:digit:]]\{1,\}\)\.[[:digit:]]\{1,\}$$/1.\1.x/')
UNIT_TEST_DIRS=$(shell go list ./... | grep -v /test/)
test-unit: $(SETUP_ENVTEST) ## Run the unit tests
	eval $$($(SETUP_ENVTEST) use -p env $(ENVTEST_VERSION)) && go test -tags $(GO_BUILD_TAGS) -count=1 -short $(UNIT_TEST_DIRS) -coverprofile cover.out

.PHONY: e2e
e2e: KIND_CLUSTER_NAME=operator-controller-e2e
e2e: KUSTOMIZE_BUILD_DIR=config/e2e
e2e: GO_BUILD_FLAGS=-cover
e2e: run kind-load-test-artifacts test-e2e e2e-coverage kind-cluster-cleanup ## Run e2e test suite on local kind cluster

.PHONY: e2e-coverage
e2e-coverage:
	COVERAGE_OUTPUT=./e2e-cover.out ./hack/e2e-coverage.sh

.PHONY: kind-load
kind-load: $(KIND) ## Loads the currently constructed image onto the cluster
	$(KIND) load docker-image $(IMG) --name $(KIND_CLUSTER_NAME)

.PHONY: kind-cluster
kind-cluster: $(KIND) ## Standup a kind cluster
	$(KIND) create cluster --name ${KIND_CLUSTER_NAME}
	$(KIND) export kubeconfig --name ${KIND_CLUSTER_NAME}

.PHONY: kind-cluster-cleanup
kind-cluster-cleanup: $(KIND) ## Delete the kind cluster
	$(KIND) delete cluster --name ${KIND_CLUSTER_NAME}

.PHONY: kind-load-test-artifacts
kind-load-test-artifacts: $(KIND) ## Load the e2e testdata container images into a kind cluster
	$(CONTAINER_RUNTIME) build $(TESTDATA_DIR)/bundles/registry-v1/prometheus-operator.v0.37.0 -t localhost/testdata/bundles/registry-v1/prometheus-operator:v0.37.0
	$(CONTAINER_RUNTIME) build $(TESTDATA_DIR)/bundles/registry-v1/prometheus-operator.v0.47.0 -t localhost/testdata/bundles/registry-v1/prometheus-operator:v0.47.0
	$(CONTAINER_RUNTIME) build $(TESTDATA_DIR)/bundles/registry-v1/prometheus-operator.v0.65.1 -t localhost/testdata/bundles/registry-v1/prometheus-operator:v0.65.1
	$(CONTAINER_RUNTIME) build $(TESTDATA_DIR)/bundles/plain-v0/plain.v0.1.0 -t localhost/testdata/bundles/plain-v0/plain:v0.1.0
	$(CONTAINER_RUNTIME) build $(TESTDATA_DIR)/catalogs -f $(TESTDATA_DIR)/catalogs/test-catalog.Dockerfile -t localhost/testdata/catalogs/test-catalog:e2e
	$(KIND) load docker-image localhost/testdata/bundles/registry-v1/prometheus-operator:v0.37.0 --name $(KIND_CLUSTER_NAME)
	$(KIND) load docker-image localhost/testdata/bundles/registry-v1/prometheus-operator:v0.47.0 --name $(KIND_CLUSTER_NAME)
	$(KIND) load docker-image localhost/testdata/bundles/registry-v1/prometheus-operator:v0.65.1 --name $(KIND_CLUSTER_NAME)
	$(KIND) load docker-image localhost/testdata/bundles/plain-v0/plain:v0.1.0 --name $(KIND_CLUSTER_NAME)
	$(KIND) load docker-image localhost/testdata/catalogs/test-catalog:e2e --name $(KIND_CLUSTER_NAME)

##@ Build

export VERSION           ?= $(shell git describe --tags --always --dirty)
export CGO_ENABLED       ?= 0
export GO_BUILD_ASMFLAGS ?= all=-trimpath=${PWD}
export GO_BUILD_LDFLAGS  ?= -s -w -X $(shell go list -m)/version.Version=$(VERSION)
export GO_BUILD_GCFLAGS  ?= all=-trimpath=${PWD}
export GO_BUILD_TAGS     ?= upstream
export GO_BUILD_FLAGS    ?=

BUILDCMD = go build $(GO_BUILD_FLAGS) -tags '$(GO_BUILD_TAGS)' -ldflags '$(GO_BUILD_LDFLAGS)' -gcflags '$(GO_BUILD_GCFLAGS)' -asmflags '$(GO_BUILD_ASMFLAGS)' -o $(BUILDBIN)/manager ./cmd/manager

.PHONY: build-deps
build-deps: manifests generate fmt vet

.PHONY: build go-build-local
build: build-deps go-build-local ## Build manager binary for current GOOS and GOARCH.
go-build-local: BUILDBIN = bin
go-build-local:
	$(BUILDCMD)

.PHONY: build-linux go-build-linux
build-linux: build-deps go-build-linux ## Build manager binary for GOOS=linux and local GOARCH.
go-build-linux: BUILDBIN = bin/linux
go-build-linux:
	GOOS=linux $(BUILDCMD)

.PHONY: run
run: docker-build kind-cluster kind-load install ## Build the operator-controller then deploy it into a new kind cluster.

.PHONY: docker-build
docker-build: build-linux ## Build docker image for operator-controller with GOOS=linux and local GOARCH.
	docker build -t ${IMG} -f Dockerfile ./bin/linux

###########
# Release #
###########

##@ Release:
export ENABLE_RELEASE_PIPELINE ?= false
export GORELEASER_ARGS ?= --snapshot --clean

.PHONY: release
release: $(GORELEASER) ## Runs goreleaser for the operator-controller. By default, this will run only as a snapshot and will not publish any artifacts unless it is run with different arguments. To override the arguments, run with "GORELEASER_ARGS=...". When run as a github action from a tag, this target will publish a full release.
	$(GORELEASER) $(GORELEASER_ARGS)

.PHONY: quickstart
quickstart: export MANIFEST="https://github.com/operator-framework/operator-controller/releases/download/$(VERSION)/operator-controller.yaml"
quickstart: $(KUSTOMIZE) generate ## Generate the installation release manifests and scripts
	$(KUSTOMIZE) build $(KUSTOMIZE_BUILD_DIR) | sed "s/:devel/:$(VERSION)/g" > operator-controller.yaml
	envsubst '$$CATALOGD_VERSION,$$CERT_MGR_VERSION,$$RUKPAK_VERSION,$$MANIFEST' < scripts/install.tpl.sh > install.sh

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: export MANIFEST="./operator-controller.yaml"
install: manifests $(KUSTOMIZE) generate ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build $(KUSTOMIZE_BUILD_DIR) > operator-controller.yaml
	envsubst '$$CATALOGD_VERSION,$$CERT_MGR_VERSION,$$RUKPAK_VERSION,$$MANIFEST' < scripts/install.tpl.sh | bash -s

.PHONY: uninstall
uninstall: manifests $(KUSTOMIZE) ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests $(KUSTOMIZE) ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build $(KUSTOMIZE_BUILD_DIR) | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build $(KUSTOMIZE_BUILD_DIR) | kubectl delete --ignore-not-found=$(ignore-not-found) -f -
