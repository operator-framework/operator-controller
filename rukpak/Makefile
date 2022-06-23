###########################
# Configuration Variables #
###########################
ORG := github.com/operator-framework
PKG := $(ORG)/rukpak
export IMAGE_REPO ?= quay.io/operator-framework/rukpak
export IMAGE_TAG ?= latest
IMAGE?=$(IMAGE_REPO):$(IMAGE_TAG)
KIND_CLUSTER_NAME ?= rukpak
BIN_DIR := bin
TESTDATA_DIR := testdata
VERSION_PATH := $(PKG)/internal/version
GIT_COMMIT ?= $(shell git rev-parse HEAD)
PKGS = $(shell go list ./...)
export CERT_MGR_VERSION ?= v1.7.1
RUKPAK_NAMESPACE ?= rukpak-system

CONTAINER_RUNTIME ?= docker

# kernel-style V=1 build verbosity
ifeq ("$(origin V)", "command line")
  BUILD_VERBOSE = $(V)
endif

ifeq ($(BUILD_VERBOSE),1)
  Q =
else
  Q = @
endif

###############
# Help Target #
###############
.PHONY: help
help: ## Show this help screen
	@echo 'Usage: make <OPTIONS> ... <TARGETS>'
	@echo ''
	@echo 'Available targets are:'
	@echo ''
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

###################
# Code management #
###################
.PHONY: lint tidy fmt clean generate verify

##@ code management:

lint: golangci-lint ## Run golangci linter
	$(Q)$(GOLANGCI_LINT) run

tidy: ## Update dependencies
	$(Q)go mod tidy
	$(Q)(cd $(TOOLS_DIR) && go mod tidy)

fmt: ## Format Go code
	$(Q)go fmt ./...
	$(Q)(cd $(TOOLS_DIR) && go fmt $$(go list -tags=tools ./...))

clean: ## Remove binaries and test artifacts
	@rm -rf bin

generate: controller-gen ## Generate code and manifests
	$(Q)$(CONTROLLER_GEN) crd:crdVersions=v1,generateEmbeddedObjectMeta=true output:crd:dir=./manifests/apis/crds paths=./api/...
	$(Q)$(CONTROLLER_GEN) webhook paths=./api/... output:stdout > ./manifests/apis/webhooks/resources/webhook.yaml
	$(Q)$(CONTROLLER_GEN) object:headerFile=./hack/boilerplate.go.txt paths=./api/...
	$(Q)$(CONTROLLER_GEN) rbac:roleName=plain-provisioner-admin paths=./internal/provisioner/plain/... output:stdout > ./manifests/provisioners/plain/resources/cluster_role.yaml
	$(Q)$(CONTROLLER_GEN) rbac:roleName=registry-provisioner-admin paths=./internal/provisioner/registry/... output:stdout > ./manifests/provisioners/registry/resources/cluster_role.yaml

verify: tidy fmt generate ## Verify the current code generation and lint
	git diff --exit-code

###########
# Testing #
###########
.PHONY: test test-unit test-e2e image-registry

##@ testing:

test: test-unit test-e2e ## Run the tests

ENVTEST_VERSION = $(shell go list -m k8s.io/client-go | cut -d" " -f2 | sed 's/^v0\.\([[:digit:]]\{1,\}\)\.[[:digit:]]\{1,\}$$/1.\1.x/')
UNIT_TEST_DIRS=$(shell go list ./... | grep -v /test/)
test-unit: setup-envtest ## Run the unit tests
	eval $$($(SETUP_ENVTEST) use -p env $(ENVTEST_VERSION)) && go test -count=1 -short $(UNIT_TEST_DIRS)

FOCUS := $(if $(TEST),-v -focus "$(TEST)")
test-e2e: ginkgo ## Run the e2e tests
	$(GINKGO) -trace -progress $(FOCUS) test/e2e

e2e: KIND_CLUSTER_NAME=rukpak-e2e
e2e: run image-registry kind-load-bundles test-e2e kind-cluster-cleanup ## Run e2e tests against an ephemeral kind cluster

kind-cluster: kind kind-cluster-cleanup ## Standup a kind cluster
	$(KIND) create cluster --name ${KIND_CLUSTER_NAME}
	$(KIND) export kubeconfig --name ${KIND_CLUSTER_NAME}

kind-cluster-cleanup: kind ## Delete the kind cluster
	$(KIND) delete cluster --name ${KIND_CLUSTER_NAME}

image-registry: ## Setup in-cluster image registry 
	./tools/imageregistry/setup_imageregistry.sh ${KIND_CLUSTER_NAME}

###################
# Install and Run #
###################
.PHONY: install install-manifests wait run cert-mgr uninstall

##@ install/run:

install: generate cert-mgr install-manifests wait ## Install rukpak

install-manifests:
	kubectl apply -k manifests

wait:
	kubectl wait --for=condition=Available --namespace=$(RUKPAK_NAMESPACE) deployment/plain-provisioner --timeout=60s
	kubectl wait --for=condition=Available --namespace=$(RUKPAK_NAMESPACE) deployment/registry-provisioner --timeout=60s
	kubectl wait --for=condition=Available --namespace=$(RUKPAK_NAMESPACE) deployment/rukpak-core-webhook --timeout=60s
	kubectl wait --for=condition=Available --namespace=crdvalidator-system deployment/crd-validation-webhook --timeout=60s

run: build-container kind-cluster kind-load install ## Build image, stop/start a local kind cluster, and run operator in that cluster

cert-mgr: ## Install the certification manager
	kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/$(CERT_MGR_VERSION)/cert-manager.yaml
	kubectl wait --for=condition=Available --namespace=cert-manager deployment/cert-manager-webhook --timeout=60s

uninstall: ## Remove all rukpak resources from the cluster
	kubectl delete -k manifests

##################
# Build and Load #
##################
.PHONY: build plain unpack core rukpakctl build-container kind-load kind-load-bundles kind-cluster registry-load-bundles

##@ build/load:

# Binary builds
VERSION_FLAGS=-ldflags "-X $(VERSION_PATH).GitCommit=$(GIT_COMMIT)"
build: plain registry unpack core crdvalidator rukpakctl

plain:
	CGO_ENABLED=0 go build $(VERSION_FLAGS) -o $(BIN_DIR)/$@ ./internal/provisioner/$@

registry:
	CGO_ENABLED=0 go build $(VERSION_FLAGS) -o $(BIN_DIR)/$@ ./internal/provisioner/$@

unpack:
	CGO_ENABLED=0 go build $(VERSION_FLAGS) -o $(BIN_DIR)/$@ ./cmd/unpack/...

core:
	CGO_ENABLED=0 go build $(VERSION_FLAGS) -o $(BIN_DIR)/$@ ./cmd/core/...

crdvalidator:
	CGO_ENABLED=0 go build $(VERSION_FLAGS) -o $(BIN_DIR)/$@ ./cmd/crdvalidator

rukpakctl:
	CGO_ENABLED=0 go build $(VERSION_FLAGS) -o $(BIN_DIR)/$@ ./cmd/rukpakctl

build-container: export GOOS=linux
build-container: BIN_DIR:=$(BIN_DIR)/$(GOOS)
build-container: build ## Builds provisioner container image locally
	$(CONTAINER_RUNTIME) build -f Dockerfile -t $(IMAGE) $(BIN_DIR)

kind-load-bundles: kind ## Load the e2e testdata container images into a kind cluster
	$(CONTAINER_RUNTIME) build $(TESTDATA_DIR)/bundles/plain-v0/valid -t testdata/bundles/plain-v0:valid
	$(CONTAINER_RUNTIME) build $(TESTDATA_DIR)/bundles/plain-v0/dependent -t testdata/bundles/plain-v0:dependent
	$(CONTAINER_RUNTIME) build $(TESTDATA_DIR)/bundles/plain-v0/provides -t testdata/bundles/plain-v0:provides
	$(CONTAINER_RUNTIME) build $(TESTDATA_DIR)/bundles/plain-v0/empty -t testdata/bundles/plain-v0:empty
	$(CONTAINER_RUNTIME) build $(TESTDATA_DIR)/bundles/plain-v0/no-manifests -t testdata/bundles/plain-v0:no-manifests
	$(CONTAINER_RUNTIME) build $(TESTDATA_DIR)/bundles/plain-v0/invalid-missing-crds -t testdata/bundles/plain-v0:invalid-missing-crds
	$(CONTAINER_RUNTIME) build $(TESTDATA_DIR)/bundles/plain-v0/invalid-crds-and-crs -t testdata/bundles/plain-v0:invalid-crds-and-crs
	$(CONTAINER_RUNTIME) build $(TESTDATA_DIR)/bundles/plain-v0/subdir -t testdata/bundles/plain-v0:subdir
	$(KIND) load docker-image testdata/bundles/plain-v0:valid --name $(KIND_CLUSTER_NAME)
	$(KIND) load docker-image testdata/bundles/plain-v0:dependent --name $(KIND_CLUSTER_NAME)
	$(KIND) load docker-image testdata/bundles/plain-v0:provides --name $(KIND_CLUSTER_NAME)
	$(KIND) load docker-image testdata/bundles/plain-v0:empty --name $(KIND_CLUSTER_NAME)
	$(KIND) load docker-image testdata/bundles/plain-v0:no-manifests --name $(KIND_CLUSTER_NAME)
	$(KIND) load docker-image testdata/bundles/plain-v0:invalid-missing-crds --name $(KIND_CLUSTER_NAME)
	$(KIND) load docker-image testdata/bundles/plain-v0:invalid-crds-and-crs --name $(KIND_CLUSTER_NAME)
	$(KIND) load docker-image testdata/bundles/plain-v0:subdir --name $(KIND_CLUSTER_NAME)

kind-load: kind ## Loads the currently constructed image onto the cluster
	$(KIND) load docker-image $(IMAGE) --name $(KIND_CLUSTER_NAME)

registry-load-bundles: kind-load-bundles ## Load the e2e testdata container images into registry
	./tools/imageregistry/load_test_image.sh

###########
# Release #
###########

##@ release:

export DISABLE_RELEASE_PIPELINE ?= true
substitute:
	envsubst < .goreleaser.template.yml > .goreleaser.yml

release: GORELEASER_ARGS ?= --snapshot --rm-dist
release: goreleaser substitute ## Run goreleaser
	$(GORELEASER) $(GORELEASER_ARGS)

quickstart: VERSION ?= $(shell git describe --abbrev=0 --tags)
quickstart: generate ## Generate the installation release manifests
	kubectl kustomize manifests | sed "s/:latest/:$(VERSION)/g" > rukpak.yaml

################
# Hack / Tools #
################
TOOLS_DIR := hack/tools
TOOLS_BIN_DIR := $(TOOLS_DIR)/bin

##@ hack/tools:

.PHONY: golangci-lint ginkgo controller-gen goreleaser kind

GOLANGCI_LINT := $(abspath $(TOOLS_BIN_DIR)/golangci-lint)
GINKGO := $(abspath $(TOOLS_BIN_DIR)/ginkgo)
CONTROLLER_GEN := $(abspath $(TOOLS_BIN_DIR)/controller-gen)
SETUP_ENVTEST := $(abspath $(TOOLS_BIN_DIR)/setup-envtest)
GORELEASER := $(abspath $(TOOLS_BIN_DIR)/goreleaser)
KIND := $(abspath $(TOOLS_BIN_DIR)/kind)

controller-gen: $(CONTROLLER_GEN) ## Build a local copy of controller-gen
ginkgo: $(GINKGO) ## Build a local copy of ginkgo
golangci-lint: $(GOLANGCI_LINT) ## Build a local copy of golangci-lint
setup-envtest: $(SETUP_ENVTEST) ## Build a local copy of envtest
goreleaser: $(GORELEASER) ## Builds a local copy of goreleaser
kind: $(KIND) ## Builds a local copy of kind

$(CONTROLLER_GEN): $(TOOLS_DIR)/go.mod # Build controller-gen from tools folder.
	cd $(TOOLS_DIR); go build -tags=tools -o $(BIN_DIR)/controller-gen sigs.k8s.io/controller-tools/cmd/controller-gen
$(GINKGO): $(TOOLS_DIR)/go.mod # Build ginkgo from tools folder.
	cd $(TOOLS_DIR); go build -tags=tools -o $(BIN_DIR)/ginkgo github.com/onsi/ginkgo/v2/ginkgo
$(GOLANGCI_LINT): $(TOOLS_DIR)/go.mod # Build golangci-lint from tools folder.
	cd $(TOOLS_DIR); go build -tags=tools -o $(BIN_DIR)/golangci-lint github.com/golangci/golangci-lint/cmd/golangci-lint
$(SETUP_ENVTEST): $(TOOLS_DIR)/go.mod # Build setup-envtest from tools folder.
	cd $(TOOLS_DIR); go build -tags=tools -o $(BIN_DIR)/setup-envtest sigs.k8s.io/controller-runtime/tools/setup-envtest
$(GORELEASER): $(TOOLS_DIR)/go.mod # Build goreleaser from tools folder.
	cd $(TOOLS_DIR); go build -tags=tools -o $(BIN_DIR)/goreleaser github.com/goreleaser/goreleaser
$(KIND): $(TOOLS_DIR)/go.mod
	cd $(TOOLS_DIR); go build -tags=tools -o $(BIN_DIR)/kind sigs.k8s.io/kind

