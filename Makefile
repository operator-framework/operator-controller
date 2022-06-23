
# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.22
BIN_DIR := bin
CONTAINER_RUNTIME ?= docker
KIND_CLUSTER_NAME ?= kind

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the unit target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

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

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) crd:crdVersions=v1 output:crd:artifacts:config=config/crd/bases paths=./api/...
	$(CONTROLLER_GEN) object:headerFile=./hack/boilerplate.go.txt paths=./api/...
	$(CONTROLLER_GEN) rbac:roleName=manager-role paths=./... output:rbac:artifacts:config=config/rbac

.PHONY: lint
lint: ## Run golangci-lint linter checks.
lint: golangci-lint
	$(GOLANGCI_LINT) run

UNIT_TEST_DIRS=$(shell go list ./... | grep -v /test/)
.PHONY: unit
unit: generate envtest ## Run unit tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test -count=1 -short $(UNIT_TEST_DIRS)

.PHONY: e2e
e2e: deploy test-e2e

.PHONY: test-e2e
test-e2e: ginkgo ## Run e2e tests
	$(GINKGO) -trace -progress test/e2e

.PHONY: root-verify
root-verify: tidy generate
	git diff --exit-code

.PHONY: deppy-verify
deppy-verify:
	make -C deppy verify

.PHONY: rukpak-verify
rukpak-verify:
	make -C rukpak verify

.PHONY: verify
verify: root-verify deppy-verify rukpak-verify

##@ Build

.PHONY: build
build: ## Build manager binary.
	CGO_ENABLED=0 go build -o bin/manager ./cmd/...

.PHONY: build-container
build-container: build ## Builds provisioner container image locally
	$(CONTAINER_RUNTIME) build -f Dockerfile -t $(IMG) $(BIN_DIR)

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: kind-load
kind-load: build-container kind
	$(KIND) load docker-image $(IMG)

.PHONY: kind-cluster
kind-cluster: kind
	$(KIND) get clusters | grep $(KIND_CLUSTER_NAME) || $(KIND) create cluster --name $(KIND_CLUSTER_NAME)

.PHONY: install
install: generate kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: generate kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: run
run: build-container kind-cluster kind-load install
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: deploy
deploy: run rukpak deppy olm ## Deploy controller to the K8s cluster specified in ~/.kube/config.

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: install-samples
install-samples:  ## Install the sample manifests found in config/samples/manifests
	kubectl apply -f config/samples

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GINKGO ?= $(LOCALBIN)/ginkgo
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint
KIND ?= $(LOCALBIN)/kind

## Tool Versions
KUSTOMIZE_VERSION ?= v3.8.7
CONTROLLER_TOOLS_VERSION ?= v0.9.0

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	rm -f $(KUSTOMIZE)
	curl -s $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN)

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: ginkgo
ginkgo: $(GINKGO)
$(GINKGO): $(LOCALBIN) ## Download ginkgo locally if necessary.
	GOBIN=$(LOCALBIN) go install github.com/onsi/ginkgo/v2/ginkgo@v2.1.4

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT)
$(GOLANGCI_LINT): $(LOCALBIN) ## Download golangci-lint locally if necessary.
	GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.45.2

.PHONY: kind
kind: $(KIND) ## Download kind locally if necessary.
$(KIND): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/kind@v0.14.0

.PHONY: rukpak
rukpak: ## Install the rukpak stack locally
	make -C rukpak build-container kind-load install KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME)

.PHONY: deppy
deppy:  ## Install the deppy stack locally
	make -C deppy run KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME)

OLM_VERSION ?= v0.21.2
.PHONY: olm
olm:  ## Install OLM locally
	# TODO(tflannag): Deploy a BundleInstance using the OLM plain bundle image
	kubectl apply --server-side=true -f https://github.com/operator-framework/operator-lifecycle-manager/releases/download/$(OLM_VERSION)/crds.yaml
	# Wait for CRDs to be accepted before proceeding
	kubectl wait --for=condition=NamesAccepted crd/catalogsources.operators.coreos.com crd/clusterserviceversions.operators.coreos.com \
	crd/installplans.operators.coreos.com crd/olmconfigs.operators.coreos.com crd/operatorconditions.operators.coreos.com \
	crd/operatorgroups.operators.coreos.com crd/operators.operators.coreos.com crd/subscriptions.operators.coreos.com --timeout=60s
	kubectl apply --server-side=true -f https://github.com/operator-framework/operator-lifecycle-manager/releases/download/$(OLM_VERSION)/olm.yaml
