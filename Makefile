# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL := /usr/bin/env bash -o pipefail
.SHELLFLAGS := -ec

ifeq ($(origin IMAGE_REPO), undefined)
IMAGE_REPO := quay.io/operator-framework/catalogd
endif
export IMAGE_REPO

ifeq ($(origin IMAGE_TAG), undefined)
IMAGE_TAG := devel
endif
export IMAGE_TAG

IMAGE := $(IMAGE_REPO):$(IMAGE_TAG)

# By default setup-envtest will write to $XDG_DATA_HOME, or $HOME/.local/share if that is not defined.
# If $HOME is not set, we need to specify a binary directory to prevent an error in setup-envtest.
# Useful for some CI/CD environments that set neither $XDG_DATA_HOME nor $HOME.
SETUP_ENVTEST_BIN_DIR_OVERRIDE=
ifeq ($(shell [[ $$HOME == "" || $$HOME == "/" ]] && [[ $$XDG_DATA_HOME == "" ]] && echo true ), true)
	SETUP_ENVTEST_BIN_DIR_OVERRIDE += --bin-dir /tmp/envtest-binaries
endif

# bingo manages consistent tooling versions for things like kind, kustomize, etc.
include .bingo/Variables.mk

# Dependencies
export CERT_MGR_VERSION := v1.15.3
ENVTEST_SERVER_VERSION := $(shell go list -m k8s.io/client-go | cut -d" " -f2 | sed 's/^v0\.\([[:digit:]]\{1,\}\)\.[[:digit:]]\{1,\}$$/1.\1.x/')

# Cluster configuration
ifeq ($(origin KIND_CLUSTER_NAME), undefined)
KIND_CLUSTER_NAME := catalogd
endif

# E2E configuration
TESTDATA_DIR := testdata

CATALOGD_NAMESPACE := olmv1-system
KIND_CLUSTER_IMAGE := kindest/node:v1.30.0@sha256:047357ac0cfea04663786a612ba1eaba9702bef25227a794b52890dd8bcd692e

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
	awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
.DEFAULT_GOAL := help

##@ Development

clean: ## Remove binaries and test artifacts
	rm -rf bin

.PHONY: generate
generate: $(CONTROLLER_GEN) ## Generate code and manifests.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/base/crd/bases output:rbac:artifacts:config=config/base/rbac output:webhook:artifacts:config=config/base/manager/webhook/

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: bingo-upgrade
bingo-upgrade: $(BINGO) #EXHELP Upgrade tools
	@for pkg in $$($(BINGO) list | awk '{ print $$1 }' | tail -n +3); do \
		echo "Upgrading $$pkg to latest..."; \
		$(BINGO) get "$$pkg@latest"; \
	done

.PHONY: test-unit
test-unit: generate fmt vet $(SETUP_ENVTEST) ## Run tests.
	eval $$($(SETUP_ENVTEST) use -p env $(ENVTEST_SERVER_VERSION) $(SETUP_ENVTEST_BIN_DIR_OVERRIDE)) && go test $(shell go list ./... | grep -v /test/e2e | grep -v /test/upgrade) -coverprofile cover.out

FOCUS := $(if $(TEST),-v -focus "$(TEST)")
ifeq ($(origin E2E_FLAGS), undefined)
E2E_FLAGS :=
endif
test-e2e: $(GINKGO) ## Run the e2e tests
	$(GINKGO) $(E2E_FLAGS) -trace -vv $(FOCUS) test/e2e

e2e: KIND_CLUSTER_NAME=catalogd-e2e
e2e: run image-registry test-e2e kind-cluster-cleanup ## Run e2e test suite on local kind cluster

image-registry: ## Setup in-cluster image registry
	./test/tools/imageregistry/registry.sh

.PHONY: tidy
tidy: ## Update dependencies
	go mod tidy

.PHONY: verify
verify: tidy fmt vet generate ## Verify the current code generation and lint
	git diff --exit-code

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Run golangci linter.
	$(GOLANGCI_LINT) run $(GOLANGCI_LINT_ARGS)

## image-registry target has to come after run-latest-release,
## because the image-registry depends on the olm-ca issuer.
.PHONY: test-upgrade-e2e
test-upgrade-e2e: export TEST_CLUSTER_CATALOG_NAME := test-catalog
test-upgrade-e2e: export TEST_CLUSTER_CATALOG_IMAGE := docker-registry.catalogd-e2e.svc:5000/test-catalog:e2e
test-upgrade-e2e: kind-cluster cert-manager build-container kind-load run-latest-release image-registry pre-upgrade-setup only-deploy-manifest wait post-upgrade-checks kind-cluster-cleanup ## Run upgrade e2e tests on a local kind cluster

pre-upgrade-setup:
	./test/tools/imageregistry/pre-upgrade-setup.sh ${TEST_CLUSTER_CATALOG_IMAGE} ${TEST_CLUSTER_CATALOG_NAME}

.PHONY: run-latest-release
run-latest-release:
	curl -L -s https://github.com/operator-framework/catalogd/releases/latest/download/install.sh | bash -s

.PHONY: post-upgrade-checks
post-upgrade-checks: $(GINKGO)
	$(GINKGO) $(E2E_FLAGS) -trace -vv $(FOCUS) test/upgrade

##@ Build

BINARIES=manager
LINUX_BINARIES=$(join $(addprefix linux/,$(BINARIES)), )

# Build info
ifeq ($(origin VERSION), undefined)
VERSION := $(shell git describe --tags --always --dirty)
endif
export VERSION

export VERSION_PKG     := $(shell go list -m)/internal/version

export GIT_COMMIT      := $(shell git rev-parse HEAD)
export GIT_VERSION     := $(shell git describe --tags --always --dirty)
export GIT_TREE_STATE  := $(shell [ -z "$(shell git status --porcelain)" ] && echo "clean" || echo "dirty")
export GIT_COMMIT_DATE := $(shell TZ=UTC0 git show --quiet --date=format:'%Y-%m-%dT%H:%M:%SZ' --format="%cd")

export CGO_ENABLED       := 0
export GO_BUILD_ASMFLAGS := all=-trimpath=${PWD}
export GO_BUILD_LDFLAGS  := -s -w \
    -X "$(VERSION_PKG).gitVersion=$(GIT_VERSION)" \
    -X "$(VERSION_PKG).gitCommit=$(GIT_COMMIT)" \
    -X "$(VERSION_PKG).gitTreeState=$(GIT_TREE_STATE)" \
    -X "$(VERSION_PKG).commitDate=$(GIT_COMMIT_DATE)"
export GO_BUILD_GCFLAGS  := all=-trimpath=${PWD}

BUILDCMD = go build -ldflags '$(GO_BUILD_LDFLAGS)' -gcflags '$(GO_BUILD_GCFLAGS)' -asmflags '$(GO_BUILD_ASMFLAGS)' -o $(BUILDBIN)/$(notdir $@) ./cmd/$(notdir $@)

.PHONY: build-deps
build-deps: generate fmt vet

.PHONY: build go-build-local $(BINARIES)
build: build-deps go-build-local ## Build binaries for current GOOS and GOARCH.
go-build-local: $(BINARIES)
$(BINARIES): BUILDBIN = bin
$(BINARIES):
	$(BUILDCMD)

.PHONY: build-linux go-build-linux $(LINUX_BINARIES)
build-linux: build-deps go-build-linux ## Build binaries for GOOS=linux and local GOARCH.
go-build-linux: $(LINUX_BINARIES)
$(LINUX_BINARIES): BUILDBIN = bin/linux
$(LINUX_BINARIES):
	GOOS=linux $(BUILDCMD)


.PHONY: run
run: generate kind-cluster install ## Create a kind cluster and install a local build of catalogd

.PHONY: build-container
build-container: build-linux ## Build docker image for catalogd.
	docker build -f Dockerfile -t $(IMAGE) bin/linux

##@ Deploy

.PHONY: kind-cluster
kind-cluster: $(KIND) kind-cluster-cleanup ## Standup a kind cluster
	$(KIND) create cluster --name $(KIND_CLUSTER_NAME) --image $(KIND_CLUSTER_IMAGE)
	$(KIND) export kubeconfig --name $(KIND_CLUSTER_NAME)

.PHONY: kind-cluster-cleanup
kind-cluster-cleanup: $(KIND) ## Delete the kind cluster
	$(KIND) delete cluster --name $(KIND_CLUSTER_NAME)

.PHONY: kind-load
kind-load: $(KIND) ## Load the built images onto the local cluster
	$(KIND) export kubeconfig --name $(KIND_CLUSTER_NAME)
	$(KIND) load docker-image $(IMAGE) --name $(KIND_CLUSTER_NAME)

.PHONY: install
install: build-container kind-load deploy wait ## Install local catalogd

.PHONY: deploy
deploy: export MANIFEST="./catalogd.yaml"
deploy: export DEFAULT_CATALOGS="./config/base/default/clustercatalogs/default-catalogs.yaml"
deploy: $(KUSTOMIZE) ## Deploy Catalogd to the K8s cluster specified in ~/.kube/config with cert-manager and default clustercatalogs
	cd config/base/manager && $(KUSTOMIZE) edit set image controller=$(IMAGE) && cd ../../..
	$(KUSTOMIZE) build config/overlays/cert-manager | sed "s/cert-git-version/cert-$(GIT_VERSION)/g" > catalogd.yaml
	envsubst '$$CERT_MGR_VERSION,$$MANIFEST,$$DEFAULT_CATALOGS' < scripts/install.tpl.sh | bash -s

.PHONY: only-deploy-manifest
only-deploy-manifest: $(KUSTOMIZE) ## Deploy just the Catalogd manifest--used in e2e testing where cert-manager is installed in a separate step
	cd config/base/manager && $(KUSTOMIZE) edit set image controller=$(IMAGE)
	$(KUSTOMIZE) build config/overlays/cert-manager | kubectl apply -f -

wait:
	kubectl wait --for=condition=Available --namespace=$(CATALOGD_NAMESPACE) deployment/catalogd-controller-manager --timeout=60s
	kubectl wait --for=condition=Ready --namespace=$(CATALOGD_NAMESPACE) certificate/catalogd-catalogserver-cert # Avoid upgrade test flakes when reissuing cert


.PHONY: cert-manager
cert-manager:
	kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/${CERT_MGR_VERSION}/cert-manager.yaml
	kubectl wait --for=condition=Available --namespace=cert-manager deployment/cert-manager-webhook --timeout=60s

##@ Release

ifeq ($(origin ENABLE_RELEASE_PIPELINE), undefined)
ENABLE_RELEASE_PIPELINE := false
endif
export ENABLE_RELEASE_PIPELINE

ifeq ($(origin GORELEASER_ARGS), undefined)
GORELEASER_ARGS := --snapshot --clean
endif

release: $(GORELEASER) ## Runs goreleaser for catalogd. By default, this will run only as a snapshot and will not publish any artifacts unless it is run with different arguments. To override the arguments, run with "GORELEASER_ARGS=...". When run as a github action from a tag, this target will publish a full release.
	$(GORELEASER) $(GORELEASER_ARGS)

quickstart: export MANIFEST := https://github.com/operator-framework/catalogd/releases/download/$(VERSION)/catalogd.yaml
quickstart: export DEFAULT_CATALOGS := https://github.com/operator-framework/catalogd/releases/download/$(VERSION)/default-catalogs.yaml
quickstart: $(KUSTOMIZE) generate ## Generate the installation release manifests and scripts
	$(KUSTOMIZE) build config/overlays/cert-manager | sed "s/:devel/:$(GIT_VERSION)/g" | sed "s/cert-git-version/cert-$(GIT_VERSION)/g" > catalogd.yaml
	envsubst '$$CERT_MGR_VERSION,$$MANIFEST,$$DEFAULT_CATALOGS' < scripts/install.tpl.sh > install.sh

.PHONY: demo-update
demo-update:
	hack/scripts/generate-asciidemo.sh
