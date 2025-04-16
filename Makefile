###########################
# Configuration Variables #
###########################
# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL := /usr/bin/env bash -o pipefail
.SHELLFLAGS := -ec
export ROOT_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))

GOLANG_VERSION := $(shell sed -En 's/^go (.*)$$/\1/p' "go.mod")
# Image URL to use all building/pushing image targets
ifeq ($(origin IMAGE_REGISTRY), undefined)
IMAGE_REGISTRY := quay.io/operator-framework
endif
export IMAGE_REGISTRY

ifeq ($(origin OPCON_IMAGE_REPO), undefined)
OPCON_IMAGE_REPO := $(IMAGE_REGISTRY)/operator-controller
endif
export OPCON_IMAGE_REPO

ifeq ($(origin CATD_IMAGE_REPO), undefined)
CATD_IMAGE_REPO := $(IMAGE_REGISTRY)/catalogd
endif
export CATD_IMAGE_REPO

ifeq ($(origin IMAGE_TAG), undefined)
IMAGE_TAG := devel
endif
export IMAGE_TAG

OPCON_IMG := $(OPCON_IMAGE_REPO):$(IMAGE_TAG)
CATD_IMG := $(CATD_IMAGE_REPO):$(IMAGE_TAG)

# Extract Kubernetes client-go version used to set the version to the PSA labels, for ENVTEST and KIND
ifeq ($(origin K8S_VERSION), undefined)
K8S_VERSION := $(shell go list -m k8s.io/client-go | cut -d" " -f2 | sed -E 's/^v0\.([0-9]+)\.[0-9]+$$/1.\1/')
endif

# Ensure ENVTEST_VERSION follows correct "X.Y.x" format
ENVTEST_VERSION := $(K8S_VERSION).x

# Not guaranteed to have patch releases available and node image tags are full versions (i.e v1.28.0 - no v1.28, v1.29, etc.)
# The K8S_VERSION is set by getting the version of the k8s.io/client-go dependency from the go.mod
# and sets major version to "1" and the patch version to "0". For example, a client-go version of v0.28.5
# will map to a K8S_VERSION of 1.28.0
KIND_CLUSTER_IMAGE := kindest/node:v$(K8S_VERSION).0

# Define dependency versions (use go.mod if we also use Go code from dependency)
export CERT_MGR_VERSION := v1.17.1
export WAIT_TIMEOUT := 60s

# Install default ClusterCatalogs
export INSTALL_DEFAULT_CATALOGS := true

# By default setup-envtest binary will write to $XDG_DATA_HOME, or $HOME/.local/share if that is not defined.
# If $HOME is not set, we need to specify a binary directory to prevent an error in setup-envtest.
# Useful for some CI/CD environments that set neither $XDG_DATA_HOME nor $HOME.
SETUP_ENVTEST_BIN_DIR_OVERRIDE += --bin-dir $(ROOT_DIR)/bin/envtest-binaries
ifeq ($(shell [[ $$HOME == "" || $$HOME == "/" ]] && [[ $$XDG_DATA_HOME == "" ]] && echo true ), true)
	SETUP_ENVTEST_BIN_DIR_OVERRIDE += --bin-dir /tmp/envtest-binaries
endif

# bingo manages consistent tooling versions for things like kind, kustomize, etc.
include .bingo/Variables.mk

ifeq ($(origin KIND_CLUSTER_NAME), undefined)
KIND_CLUSTER_NAME := operator-controller
endif


ifneq (, $(shell command -v docker 2>/dev/null))
CONTAINER_RUNTIME := docker
else ifneq (, $(shell command -v podman 2>/dev/null))
CONTAINER_RUNTIME := podman
else
$(warning Could not find docker or podman in path! This may result in targets requiring a container runtime failing!)
endif

KUSTOMIZE_BUILD_DIR := config/overlays/cert-manager

# Disable -j flag for make
.NOTPARALLEL:

.DEFAULT_GOAL := build

#SECTION General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '#SECTION' and the
# target descriptions by '#HELP' or '#EXHELP'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: #HELP something, and then pretty-format the target and help. Then,
# if there's a line with #SECTION something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php
# The extended-help target uses '#EXHELP' as the delineator.

.PHONY: help
help: #HELP Display essential help.
	@awk 'BEGIN {FS = ":[^#]*#HELP"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\n"} /^[a-zA-Z_0-9-]+:.*#HELP / { printf "  \033[36m%-17s\033[0m %s\n", $$1, $$2 } ' $(MAKEFILE_LIST)

.PHONY: help-extended
help-extended: #HELP Display extended help.
	@awk 'BEGIN {FS = ":.*#(EX)?HELP"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*#(EX)?HELP / { printf "  \033[36m%-25s\033[0m %s\n", $$1, $$2 } /^#SECTION / { printf "\n\033[1m%s\033[0m\n", substr($$0, 10) } ' $(MAKEFILE_LIST)

#SECTION Development

.PHONY: lint
lint: lint-custom $(GOLANGCI_LINT) #HELP Run golangci linter.
	$(GOLANGCI_LINT) run --build-tags $(GO_BUILD_TAGS) $(GOLANGCI_LINT_ARGS)

.PHONY: custom-linter-build
custom-linter-build: #EXHELP Build custom linter
	go build -tags $(GO_BUILD_TAGS) -o ./bin/custom-linter ./hack/ci/custom-linters/cmd

.PHONY: lint-custom
lint-custom: custom-linter-build #EXHELP Call custom linter for the project
	go vet -tags=$(GO_BUILD_TAGS) -vettool=./bin/custom-linter ./...

.PHONY: k8s-pin
k8s-pin: #EXHELP Pin k8s staging modules based on k8s.io/kubernetes version (in go.mod or from K8S_IO_K8S_VERSION env var) and run go mod tidy.
	K8S_IO_K8S_VERSION='$(K8S_IO_K8S_VERSION)' go run hack/tools/k8smaintainer/main.go

.PHONY: tidy #HELP Run go mod tidy.
tidy:
	go mod tidy

.PHONY: manifests
KUSTOMIZE_CATD_CRDS_DIR := config/base/catalogd/crd/bases
KUSTOMIZE_CATD_RBAC_DIR := config/base/catalogd/rbac
KUSTOMIZE_CATD_WEBHOOKS_DIR := config/base/catalogd/manager/webhook
KUSTOMIZE_OPCON_CRDS_DIR := config/base/operator-controller/crd/bases
KUSTOMIZE_OPCON_RBAC_DIR := config/base/operator-controller/rbac
CRD_WORKING_DIR := crd_work_dir
# Due to https://github.com/kubernetes-sigs/controller-tools/issues/837 we can't specify individual files
# So we have to generate them together and then move them into place
manifests: $(CONTROLLER_GEN) #EXHELP Generate WebhookConfiguration, ClusterRole, and CustomResourceDefinition objects.
	mkdir $(CRD_WORKING_DIR)
	$(CONTROLLER_GEN) --load-build-tags=$(GO_BUILD_TAGS) crd paths="./api/v1/..." output:crd:artifacts:config=$(CRD_WORKING_DIR)
	mv $(CRD_WORKING_DIR)/olm.operatorframework.io_clusterextensions.yaml $(KUSTOMIZE_OPCON_CRDS_DIR)
	mv $(CRD_WORKING_DIR)/olm.operatorframework.io_clustercatalogs.yaml $(KUSTOMIZE_CATD_CRDS_DIR)
	rmdir $(CRD_WORKING_DIR)
	# Generate the remaining operator-controller manifests
	$(CONTROLLER_GEN) --load-build-tags=$(GO_BUILD_TAGS) rbac:roleName=manager-role paths="./internal/operator-controller/..." output:rbac:artifacts:config=$(KUSTOMIZE_OPCON_RBAC_DIR)
	# Generate the remaining catalogd manifests
	$(CONTROLLER_GEN) --load-build-tags=$(GO_BUILD_TAGS) rbac:roleName=manager-role paths="./internal/catalogd/..." output:rbac:artifacts:config=$(KUSTOMIZE_CATD_RBAC_DIR)
	$(CONTROLLER_GEN) --load-build-tags=$(GO_BUILD_TAGS) webhook paths="./internal/catalogd/..." output:webhook:artifacts:config=$(KUSTOMIZE_CATD_WEBHOOKS_DIR)

.PHONY: generate
generate: $(CONTROLLER_GEN) #EXHELP Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) --load-build-tags=$(GO_BUILD_TAGS) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: verify
verify: k8s-pin fmt generate manifests crd-ref-docs generate-test-data #HELP Verify all generated code is up-to-date. Runs k8s-pin instead of just tidy.
	git diff --exit-code

# Renders registry+v1 bundles in test/convert
# Used by CI in verify to catch regressions in the registry+v1 -> plain conversion code
.PHONY: generate-test-data
generate-test-data:
	go run test/convert/generate-manifests.go

.PHONY: fix-lint
fix-lint: $(GOLANGCI_LINT) #EXHELP Fix lint issues
	$(GOLANGCI_LINT) run --fix --build-tags $(GO_BUILD_TAGS) $(GOLANGCI_LINT_ARGS)

.PHONY: fmt
fmt: #EXHELP Formats code
	go fmt ./...

.PHONY: bingo-upgrade
bingo-upgrade: $(BINGO) #EXHELP Upgrade tools
	@for pkg in $$($(BINGO) list | awk '{ print $$3 }' | tail -n +3 | sed 's/@.*//'); do \
		echo -e "Upgrading \033[35m$$pkg\033[0m to latest..."; \
		$(BINGO) get "$$pkg@latest"; \
	done

.PHONY: verify-crd-compatibility
CRD_DIFF_ORIGINAL_REF := git://main?path=
CRD_DIFF_UPDATED_REF := file://
CRD_DIFF_OPCON_SOURCE := config/base/operator-controller/crd/bases/olm.operatorframework.io_clusterextensions.yaml
CRD_DIFF_CATD_SOURCE := config/base/catalogd/crd/bases/olm.operatorframework.io_clustercatalogs.yaml
CRD_DIFF_CONFIG := crd-diff-config.yaml
verify-crd-compatibility: $(CRD_DIFF) manifests
	$(CRD_DIFF) --config="${CRD_DIFF_CONFIG}" "${CRD_DIFF_ORIGINAL_REF}${CRD_DIFF_OPCON_SOURCE}" ${CRD_DIFF_UPDATED_REF}${CRD_DIFF_OPCON_SOURCE}
	$(CRD_DIFF) --config="${CRD_DIFF_CONFIG}" "${CRD_DIFF_ORIGINAL_REF}${CRD_DIFF_CATD_SOURCE}" ${CRD_DIFF_UPDATED_REF}${CRD_DIFF_CATD_SOURCE}

#SECTION Test

.PHONY: test
test: manifests generate fmt lint test-unit test-e2e #HELP Run all tests.

.PHONY: e2e
e2e: #EXHELP Run the e2e tests.
	go test -count=1 -v ./test/e2e/...

E2E_REGISTRY_NAME := docker-registry
E2E_REGISTRY_NAMESPACE := operator-controller-e2e

export REG_PKG_NAME := registry-operator
export LOCAL_REGISTRY_HOST := $(E2E_REGISTRY_NAME).$(E2E_REGISTRY_NAMESPACE).svc:5000
export CLUSTER_REGISTRY_HOST := localhost:30000
export E2E_TEST_CATALOG_V1 := e2e/test-catalog:v1
export E2E_TEST_CATALOG_V2 := e2e/test-catalog:v2
export CATALOG_IMG := $(LOCAL_REGISTRY_HOST)/$(E2E_TEST_CATALOG_V1)
.PHONY: test-ext-dev-e2e
test-ext-dev-e2e: $(OPERATOR_SDK) $(KUSTOMIZE) $(KIND) #HELP Run extension create, upgrade and delete tests.
	test/extension-developer-e2e/setup.sh $(OPERATOR_SDK) $(CONTAINER_RUNTIME) $(KUSTOMIZE) $(KIND) $(KIND_CLUSTER_NAME) $(E2E_REGISTRY_NAMESPACE)
	go test -count=1 -v ./test/extension-developer-e2e/...

UNIT_TEST_DIRS := $(shell go list ./... | grep -v /test/)
COVERAGE_UNIT_DIR := $(ROOT_DIR)/coverage/unit

.PHONY: envtest-k8s-bins #HELP Uses setup-envtest to download and install the binaries required to run ENVTEST-test based locally at the project/bin directory.
envtest-k8s-bins: $(SETUP_ENVTEST)
	mkdir -p $(ROOT_DIR)/bin
	$(SETUP_ENVTEST) use -p env $(ENVTEST_VERSION) $(SETUP_ENVTEST_BIN_DIR_OVERRIDE)

.PHONY: test-unit
test-unit: $(SETUP_ENVTEST) envtest-k8s-bins #HELP Run the unit tests
	rm -rf $(COVERAGE_UNIT_DIR) && mkdir -p $(COVERAGE_UNIT_DIR)
	KUBEBUILDER_ASSETS="$(shell $(SETUP_ENVTEST) use -p path $(ENVTEST_VERSION) $(SETUP_ENVTEST_BIN_DIR_OVERRIDE))" \
            CGO_ENABLED=1 go test \
                -tags '$(GO_BUILD_TAGS)' \
                -cover -coverprofile ${ROOT_DIR}/coverage/unit.out \
                -count=1 -race -short \
                $(UNIT_TEST_DIRS) \
                -test.gocoverdir=$(COVERAGE_UNIT_DIR)

.PHONY: image-registry
E2E_REGISTRY_IMAGE=localhost/e2e-test-registry:devel
image-registry: export GOOS=linux
image-registry: export GOARCH=amd64
image-registry: ## Build the testdata catalog used for e2e tests and push it to the image registry
	go build $(GO_BUILD_FLAGS) $(GO_BUILD_EXTRA_FLAGS) -tags '$(GO_BUILD_TAGS)' -ldflags '$(GO_BUILD_LDFLAGS)' -gcflags '$(GO_BUILD_GCFLAGS)' -asmflags '$(GO_BUILD_ASMFLAGS)' -o ./testdata/registry/bin/registry ./testdata/registry/registry.go
	go build $(GO_BUILD_FLAGS) $(GO_BUILD_EXTRA_FLAGS) -tags '$(GO_BUILD_TAGS)' -ldflags '$(GO_BUILD_LDFLAGS)' -gcflags '$(GO_BUILD_GCFLAGS)' -asmflags '$(GO_BUILD_ASMFLAGS)' -o ./testdata/push/bin/push         ./testdata/push/push.go
	$(CONTAINER_RUNTIME) build -f ./testdata/Dockerfile -t $(E2E_REGISTRY_IMAGE) ./testdata
	$(CONTAINER_RUNTIME) save $(E2E_REGISTRY_IMAGE) | $(KIND) load image-archive /dev/stdin --name $(KIND_CLUSTER_NAME)
	./testdata/build-test-registry.sh $(E2E_REGISTRY_NAMESPACE) $(E2E_REGISTRY_NAME) $(E2E_REGISTRY_IMAGE)

# When running the e2e suite, you can set the ARTIFACT_PATH variable to the absolute path
# of the directory for the operator-controller e2e tests to store the artifacts, which
# may be helpful for debugging purposes after a test run.
#
# for example: ARTIFACT_PATH=/tmp/artifacts make test-e2e
.PHONY: test-e2e
test-e2e: KIND_CLUSTER_NAME := operator-controller-e2e
test-e2e: KUSTOMIZE_BUILD_DIR := config/overlays/e2e
test-e2e: GO_BUILD_EXTRA_FLAGS := -cover
test-e2e: run image-registry e2e e2e-coverage kind-clean #HELP Run e2e test suite on local kind cluster

.PHONY: extension-developer-e2e
extension-developer-e2e: KUSTOMIZE_BUILD_DIR := config/overlays/cert-manager
extension-developer-e2e: KIND_CLUSTER_NAME := operator-controller-ext-dev-e2e
extension-developer-e2e: export INSTALL_DEFAULT_CATALOGS := false
extension-developer-e2e: run image-registry test-ext-dev-e2e kind-clean #EXHELP Run extension-developer e2e on local kind cluster

.PHONY: run-latest-release
run-latest-release:
	curl -L -s https://github.com/operator-framework/operator-controller/releases/latest/download/install.sh | bash -s

.PHONY: pre-upgrade-setup
pre-upgrade-setup:
	./hack/test/pre-upgrade-setup.sh $(CATALOG_IMG) $(TEST_CLUSTER_CATALOG_NAME) $(TEST_CLUSTER_EXTENSION_NAME)

.PHONY: post-upgrade-checks
post-upgrade-checks:
	go test -count=1 -v ./test/upgrade-e2e/...

.PHONY: test-upgrade-e2e
test-upgrade-e2e: KIND_CLUSTER_NAME := operator-controller-upgrade-e2e
test-upgrade-e2e: export TEST_CLUSTER_CATALOG_NAME := test-catalog
test-upgrade-e2e: export TEST_CLUSTER_EXTENSION_NAME := test-package
test-upgrade-e2e: kind-cluster run-latest-release image-registry pre-upgrade-setup docker-build kind-load kind-deploy post-upgrade-checks kind-clean #HELP Run upgrade e2e tests on a local kind cluster

.PHONY: e2e-coverage
e2e-coverage:
	COVERAGE_OUTPUT=./coverage/e2e.out ./hack/test/e2e-coverage.sh

#SECTION KIND Cluster Operations

.PHONY: kind-load
kind-load: $(KIND) #EXHELP Loads the currently constructed images into the KIND cluster.
	$(CONTAINER_RUNTIME) save $(OPCON_IMG) | $(KIND) load image-archive /dev/stdin --name $(KIND_CLUSTER_NAME)
	$(CONTAINER_RUNTIME) save $(CATD_IMG) | $(KIND) load image-archive /dev/stdin --name $(KIND_CLUSTER_NAME)

.PHONY: kind-deploy
kind-deploy: export MANIFEST := ./operator-controller.yaml
kind-deploy: export DEFAULT_CATALOG := ./config/catalogs/clustercatalogs/default-catalogs.yaml
kind-deploy: manifests $(KUSTOMIZE)
	$(KUSTOMIZE) build $(KUSTOMIZE_BUILD_DIR) | sed "s/cert-git-version/cert-$(VERSION)/g" > $(MANIFEST)
	envsubst '$$DEFAULT_CATALOG,$$CERT_MGR_VERSION,$$INSTALL_DEFAULT_CATALOGS,$$MANIFEST' < scripts/install.tpl.sh | bash -s

.PHONY: kind-cluster
kind-cluster: $(KIND) #EXHELP Standup a kind cluster.
	-$(KIND) delete cluster --name $(KIND_CLUSTER_NAME)
	$(KIND) create cluster --name $(KIND_CLUSTER_NAME) --image $(KIND_CLUSTER_IMAGE) --config ./kind-config.yaml
	$(KIND) export kubeconfig --name $(KIND_CLUSTER_NAME)

.PHONY: kind-clean
kind-clean: $(KIND) #EXHELP Delete the kind cluster.
	$(KIND) delete cluster --name $(KIND_CLUSTER_NAME)

#SECTION Build

# attempt to generate the VERSION attribute for certificates
# fail if it is unset afterwards, since the side effects are indirect
ifeq ($(strip $(VERSION)),)
VERSION := $(shell git describe --tags --always --dirty)
endif
export VERSION
ifeq ($(strip $(VERSION)),)
	$(error undefined VERSION; resulting certs will be invalid)
endif

ifeq ($(origin CGO_ENABLED), undefined)
CGO_ENABLED := 0
endif
export CGO_ENABLED

export GIT_REPO := $(shell go list -m)
export VERSION_PATH := ${GIT_REPO}/internal/shared/version
export GO_BUILD_TAGS := containers_image_openpgp
export GO_BUILD_ASMFLAGS := all=-trimpath=$(PWD)
export GO_BUILD_GCFLAGS := all=-trimpath=$(PWD)
export GO_BUILD_EXTRA_FLAGS :=
export GO_BUILD_LDFLAGS := -s -w \
    -X '$(VERSION_PATH).version=$(VERSION)' \
    -X '$(VERSION_PATH).gitCommit=$(GIT_COMMIT)' \

BINARIES=operator-controller catalogd

.PHONY: $(BINARIES)
$(BINARIES):
	go build $(GO_BUILD_FLAGS) $(GO_BUILD_EXTRA_FLAGS) -tags '$(GO_BUILD_TAGS)' -ldflags '$(GO_BUILD_LDFLAGS)' -gcflags '$(GO_BUILD_GCFLAGS)' -asmflags '$(GO_BUILD_ASMFLAGS)' -o $(BUILDBIN)/$@ ./cmd/$@

.PHONY: build-deps
build-deps: manifests generate fmt

.PHONY: build go-build-local
build: build-deps go-build-local #HELP Build manager binary for current GOOS and GOARCH. Default target.
go-build-local: BUILDBIN := bin
go-build-local: $(BINARIES)

.PHONY: build-linux go-build-linux
build-linux: build-deps go-build-linux #EXHELP Build manager binary for GOOS=linux and local GOARCH.
go-build-linux: BUILDBIN := bin/linux
go-build-linux: export GOOS=linux
go-build-linux: export GOARCH=amd64
go-build-linux: $(BINARIES)

.PHONY: run
run: docker-build kind-cluster kind-load kind-deploy wait #HELP Build the operator-controller then deploy it into a new kind cluster.

CATD_NAMESPACE := olmv1-system
wait:
	kubectl wait --for=condition=Available --namespace=$(CATD_NAMESPACE) deployment/catalogd-controller-manager --timeout=60s
	kubectl wait --for=condition=Ready --namespace=$(CATD_NAMESPACE) certificate/catalogd-service-cert # Avoid upgrade test flakes when reissuing cert

.PHONY: docker-build
docker-build: build-linux #EXHELP Build docker image for operator-controller and catalog with GOOS=linux and local GOARCH.
	$(CONTAINER_RUNTIME) build -t $(OPCON_IMG) -f Dockerfile.operator-controller ./bin/linux
	$(CONTAINER_RUNTIME) build -t $(CATD_IMG) -f Dockerfile.catalogd ./bin/linux

#SECTION Release
ifeq ($(origin ENABLE_RELEASE_PIPELINE), undefined)
ENABLE_RELEASE_PIPELINE := false
endif
ifeq ($(origin GORELEASER_ARGS), undefined)
GORELEASER_ARGS := --snapshot --clean
endif

export ENABLE_RELEASE_PIPELINE
export GORELEASER_ARGS

.PHONY: release
release: $(GORELEASER) #EXHELP Runs goreleaser for the operator-controller. By default, this will run only as a snapshot and will not publish any artifacts unless it is run with different arguments. To override the arguments, run with "GORELEASER_ARGS=...". When run as a github action from a tag, this target will publish a full release.
	OPCON_IMAGE_REPO=$(OPCON_IMAGE_REPO) CATD_IMAGE_REPO=$(CATD_IMAGE_REPO) $(GORELEASER) $(GORELEASER_ARGS)

.PHONY: quickstart
quickstart: export MANIFEST := https://github.com/operator-framework/operator-controller/releases/download/$(VERSION)/operator-controller.yaml
quickstart: export DEFAULT_CATALOG := "https://github.com/operator-framework/operator-controller/releases/download/$(VERSION)/default-catalogs.yaml"
quickstart: $(KUSTOMIZE) manifests #EXHELP Generate the unified installation release manifests and scripts.
	$(KUSTOMIZE) build $(KUSTOMIZE_BUILD_DIR) | sed "s/cert-git-version/cert-$(VERSION)/g" | sed "s/:devel/:$(VERSION)/g" > operator-controller.yaml
	envsubst '$$DEFAULT_CATALOG,$$CERT_MGR_VERSION,$$INSTALL_DEFAULT_CATALOGS,$$MANIFEST' < scripts/install.tpl.sh > install.sh

##@ Docs

.PHONY: crd-ref-docs
API_REFERENCE_FILENAME := operator-controller-api-reference.md
API_REFERENCE_DIR := $(ROOT_DIR)/docs/api-reference
crd-ref-docs: $(CRD_REF_DOCS) #EXHELP Generate the API Reference Documents.
	rm -f $(API_REFERENCE_DIR)/$(API_REFERENCE_FILENAME)
	$(CRD_REF_DOCS) --source-path=$(ROOT_DIR)/api/ \
	--config=$(API_REFERENCE_DIR)/crd-ref-docs-gen-config.yaml \
	--renderer=markdown --output-path=$(API_REFERENCE_DIR)/$(API_REFERENCE_FILENAME);

VENVDIR := $(abspath docs/.venv)

.PHONY: build-docs
build-docs: venv
	. $(VENV)/activate; \
	mkdocs build

.PHONY: serve-docs
serve-docs: venv
	. $(VENV)/activate; \
	mkdocs serve

.PHONY: deploy-docs
deploy-docs: venv
	. $(VENV)/activate; \
	mkdocs gh-deploy --force

# The demo script requires to install asciinema with: brew install asciinema to run on mac os envs.
.PHONY: demo-update #EXHELP build demo
demo-update:
	./hack/demo/generate-asciidemo.sh -u -n catalogd-demo catalogd-demo-script.sh

include Makefile.venv
