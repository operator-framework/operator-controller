###########################
# Configuration Variables #
###########################
# Image URL to use all building/pushing image targets
export IMAGE_REPO ?= quay.io/operator-framework/operator-controller
export IMAGE_TAG ?= devel
export GO_BUILD_TAGS ?= upstream

# If any of these versions are modified, remember to run `make ensure-external-manifests` to update 
# the manifests in the testdata/manifests directory.
export CERT_MGR_VERSION ?= v1.9.0
export CATALOGD_VERSION ?= v0.1.3
export RUKPAK_VERSION=$(shell go list -mod=mod -m -f "{{.Version}}" github.com/operator-framework/rukpak)

export GORELEASER_VERSION ?= v1.16.2
export WAIT_TIMEOUT ?= 60s
IMG?=$(IMAGE_REPO):$(IMAGE_TAG)

OPERATOR_CONTROLLER_NAMESPACE ?= operator-controller-system
KIND_CLUSTER_NAME ?= operator-controller

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

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: verify
verify: fmt vet manifests generate ensure-external-manifests ## Run all verification steps.
	git diff --exit-code

.PHONY: test test-e2e e2e kind-load kind-cluster kind-cluster-cleanup
test: manifests generate fmt vet test-unit e2e ## Run all tests.

FOCUS := $(if $(TEST),-v -focus "$(TEST)")
E2E_FLAGS ?= ""
test-e2e: ginkgo ## Run the e2e tests
	$(GINKGO) --tags $(GO_BUILD_TAGS) $(E2E_FLAGS) -trace -progress $(FOCUS) test/e2e

ENVTEST_VERSION = $(shell go list -m k8s.io/client-go | cut -d" " -f2 | sed 's/^v0\.\([[:digit:]]\{1,\}\)\.[[:digit:]]\{1,\}$$/1.\1.x/')
UNIT_TEST_DIRS=$(shell go list ./... | grep -v /test/)
test-unit: envtest ## Run the unit tests
	eval $$($(ENVTEST) use -p env $(ENVTEST_VERSION)) && go test -tags $(GO_BUILD_TAGS) -count=1 -short $(UNIT_TEST_DIRS) -coverprofile cover.out

e2e: KIND_CLUSTER_NAME=operator-controller-e2e
e2e: run test-e2e kind-cluster-cleanup ## Run e2e test suite on local kind cluster

kind-load: kind ## Loads the currently constructed image onto the cluster
	$(KIND) load docker-image $(IMG) --name $(KIND_CLUSTER_NAME)

kind-cluster: kind kind-cluster-cleanup ## Standup a kind cluster
	$(KIND) create cluster --name ${KIND_CLUSTER_NAME}
	$(KIND) export kubeconfig --name ${KIND_CLUSTER_NAME}

kind-cluster-cleanup: kind ## Delete the kind cluster
	$(KIND) delete cluster --name ${KIND_CLUSTER_NAME}

##@ Build

BUILDCMD = sh -c 'mkdir -p $(BUILDBIN) && ${GORELEASER} build ${GORELEASER_ARGS} --single-target -o $(BUILDBIN)/manager'
BUILDDEPS = manifests generate fmt vet goreleaser

.PHONY: build
build: BUILDBIN = bin
build: $(BUILDDEPS) ## Build manager binary using goreleaser for current GOOS and GOARCH.
	$(BUILDCMD)

.PHONY: build-linux
build-linux: BUILDBIN = bin/linux
build-linux: $(BUILDDEPS) ## Build manager binary using goreleaser for GOOS=linux and local GOARCH.
	GOOS=linux $(BUILDCMD)

.PHONY: run
run: docker-build kind-cluster kind-load ensure-external-manifests ensure-external-images-kindload install ## Build the operator-controller then deploy it into a new kind cluster.

.PHONY: ensure-external-manifests ## Ensure that the external manifests are present in the manifests directory.
ensure-external-manifests:
	./scripts/pull-manifests.sh

.PHONY: ensure-external-images-kindload
ensure-external-images-kindload: ensure-external-manifests ## Ensure that the external images are loaded into the kind cluster.
	./scripts/kind-load-images.sh

.PHONY: wait
wait:
	kubectl wait --for=condition=Available --namespace=$(OPERATOR_CONTROLLER_NAMESPACE) deployment/operator-controller-controller-manager --timeout=$(WAIT_TIMEOUT)

.PHONY: docker-build
docker-build: build-linux ## Build docker image for operator-controller with GOOS=linux and local GOARCH.
	docker build -t ${IMG} -f Dockerfile ./bin/linux

###########
# Release #
###########

##@ Release:
export ENABLE_RELEASE_PIPELINE ?= false
export GORELEASER_ARGS ?= --snapshot --clean
export VERSION ?= $(shell git describe --abbrev=0 --tags)

release: goreleaser ## Runs goreleaser for the operator-controller. By default, this will run only as a snapshot and will not publish any artifacts unless it is run with different arguments. To override the arguments, run with "GORELEASER_ARGS=...". When run as a github action from a tag, this target will publish a full release.
	$(GORELEASER) $(GORELEASER_ARGS)

quickstart: export MANIFEST="https://github.com/operator-framework/operator-controller/releases/download/$(VERSION)/operator-controller.yaml"
quickstart: kustomize generate ensure-external-manifests ## Generate the installation release manifests and scripts
	kubectl kustomize config/default | sed "s/:devel/:$(VERSION)/g" > operator-controller.yaml
	envsubst '$$CATALOGD_VERSION,$$CERT_MGR_VERSION,$$RUKPAK_VERSION,$$MANIFEST' < scripts/install.tpl.sh > install.sh

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: export MANIFEST="./operator-controller.yaml"
install: manifests kustomize generate ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	kubectl kustomize config/default > operator-controller.yaml
	envsubst '$$CATALOGD_VERSION,$$CERT_MGR_VERSION,$$RUKPAK_VERSION,$$MANIFEST' < scripts/install.tpl.sh | bash -s

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

################
# Hack / Tools #
################

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/hack/tools/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
KIND ?= $(LOCALBIN)/kind
GINKGO ?= $(LOCALBIN)/ginkgo
GORELEASER := $(LOCALBIN)/goreleaser
ENVTEST ?= $(LOCALBIN)/setup-envtest

## Tool Versions
KUSTOMIZE_VERSION ?= v4.5.7
CONTROLLER_TOOLS_VERSION ?= v0.10.0

.PHONY: kind
kind: $(KIND) ## Download kind locally if necessary.
$(KIND): $(LOCALBIN)
	test -s $(LOCALBIN)/kind || GOBIN=$(LOCALBIN) go install sigs.k8s.io/kind@v0.15.0

.PHONY: ginkgo
ginkgo: $(GINKGO) ## Download ginkgo locally if necessary.
$(GINKGO): $(LOCALBIN)
	test -s $(LOCALBIN)/ginkgo || GOBIN=$(LOCALBIN) go install github.com/onsi/ginkgo/v2/ginkgo@v2.1.4

.PHONY: goreleaser
goreleaser: $(GORELEASER) ## Builds a local copy of goreleaser
$(GORELEASER): $(LOCALBIN)
	test -s $(LOCALBIN)/goreleaser || GOBIN=$(LOCALBIN) go install github.com/goreleaser/goreleaser@${GORELEASER_VERSION}

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary. If wrong version is installed, it will be removed before downloading.
$(KUSTOMIZE): $(LOCALBIN)
	@if test -x $(LOCALBIN)/kustomize && ! $(LOCALBIN)/kustomize version | grep -q $(KUSTOMIZE_VERSION); then \
		echo "$(LOCALBIN)/kustomize version is not expected $(KUSTOMIZE_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/kustomize; \
	fi
	test -s $(LOCALBIN)/kustomize || { curl -Ss $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); }

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
