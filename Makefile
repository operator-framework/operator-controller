
# Image URL to use all building/pushing image targets
IMG ?= quay.io/operator-framework/operator:latest
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

.PHONY: tidy
tidy:  ## Update Go module dependencies.
	go mod tidy

.PHONY: verify
verify: generate tidy  ## Run verification checks.
	git diff --exit-code

.PHONY: lint
lint: golangci-lint  ## Run golangci-lint linter checks.
	$(GOLANGCI_LINT) run

UNIT_TEST_DIRS=$(shell go list ./... | grep -v /test/)
.PHONY: unit
unit: generate envtest ## Run unit tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test -count=1 -short $(UNIT_TEST_DIRS)

.PHONY: e2e
e2e: KIND_CLUSTER_NAME=e2e
e2e: deploy test-e2e

.PHONY: test-e2e
FOCUS := $(if $(TEST),-v -focus "$(TEST)")
JUNIT_REPORT := $(if $(ARTIFACT_DIR), -output-dir $(abspath $(ARTIFACT_DIR)) -junit-report junit_e2e.xml)
test-e2e: ginkgo ## Run e2e tests.
	$(GINKGO) -trace -progress $(JUNIT_REPORT) $(FOCUS) test/e2e

##@ Build

.PHONY: build
build: ## Build manager binary.
	CGO_ENABLED=0 go build -o bin/manager ./cmd/...

.PHONY: build-container
build-container: build ## Builds provisioner container image locally.
	$(CONTAINER_RUNTIME) build -f Dockerfile -t $(IMG) $(BIN_DIR)

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: kind-load
kind-load: kind ## Loads the currently constructed image onto the cluster.
	$(KIND) load docker-image $(IMG) --name $(KIND_CLUSTER_NAME)

.PHONY: kind-cluster
kind-cluster: kind ## Standup a kind cluster.
	$(KIND) get clusters | grep $(KIND_CLUSTER_NAME) || $(KIND) create cluster --name $(KIND_CLUSTER_NAME)
	$(KIND) export kubeconfig --name ${KIND_CLUSTER_NAME}

.PHONY: install
install: generate kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: generate kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: run
run: build-container kind-cluster kind-load install  ## Deploy the Operator controller in a K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: deploy
deploy: run rukpak olm  ## Deploy the OLM 1.x stack in a K8s cluster specified in ~/.kube/config.

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: install-samples
install-samples:  ## Install the sample manifests found in config/samples/manifests.
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
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk

## Tool Versions
KUSTOMIZE_VERSION ?= v3.8.7
CONTROLLER_TOOLS_VERSION ?= v0.9.0
ENVTEST_VERSION ?= latest
KIND_VERSION ?= v0.14.0
GINKGO_VERSION ?= v2.1.4
GOLANGCI_LINT_VERSION ?= v1.49.0
KIND_VERSION ?= v0.14.0
OPERATOR_SDK_VERSION ?= v1.25.0
RUKPAK_RELEASE ?= v0.11.0

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
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@$(ENVTEST_VERSION)

.PHONY: ginkgo
ginkgo: $(GINKGO)
$(GINKGO): $(LOCALBIN) ## Download ginkgo locally if necessary.
	GOBIN=$(LOCALBIN) go install github.com/onsi/ginkgo/v2/ginkgo@$(GINKGO_VERSION)

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT)
$(GOLANGCI_LINT): $(LOCALBIN) ## Download golangci-lint locally if necessary.
	GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: kind
kind: $(KIND) ## Download kind locally if necessary.
$(KIND): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/kind@$(KIND_VERSION)

.PHONY: operator-sdk
operator-sdk: $(OPERATOR_SDK)
$(OPERATOR_SDK): $(LOCALBIN) ## Download operator-sdk locally if necessary.
	curl -sL https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk_linux_amd64 -o $(OPERATOR_SDK) && chmod +x $(OPERATOR_SDK)

##@ Install Dependencies

.PHONY: rukpak
rukpak:  ## Installs the rukpak project.
	kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.9.0/cert-manager.yaml
	kubectl wait --for=condition=Available --namespace=cert-manager deployment/cert-manager-webhook --timeout=60s
	kubectl apply -f https://github.com/operator-framework/rukpak/releases/download/$(RUKPAK_RELEASE)/rukpak.yaml
	kubectl wait --for=condition=Available --namespace=rukpak-system deployment/core --timeout=60s
	kubectl wait --for=condition=Available --namespace=rukpak-system deployment/helm-provisioner --timeout=60s
	kubectl wait --for=condition=Available --namespace=rukpak-system deployment/rukpak-webhooks --timeout=60s
	kubectl wait --for=condition=Available --namespace=crdvalidator-system deployment/crd-validation-webhook --timeout=60s

.PHONY: olm
olm: operator-sdk  ## Installs the OLM project.
	$(OPERATOR_SDK) olm install
