# Build info
GIT_COMMIT              ?= $(shell git rev-parse HEAD)
GIT_VERSION             ?= $(shell git describe --tags --always --dirty)
GIT_STATUS				?= $(shell git status --porcelain)
GIT_TREE_STATE          ?= $(shell [ -z "${GIT_STATUS}" ] && echo "clean" || echo "dirty")
COMMIT_DATE             ?= $(shell git show -s --date=format:'%Y-%m-%dT%H:%M:%SZ' --format=%cd)
ORG                     ?= github.com/operator-framework
REPO                    ?= $(ORG)/catalogd
VERSION_PKG             ?= $(REPO)/internal/version
CTRL_LDFLAGS            ?= -ldflags="-X '$(VERSION_PKG).gitVersion=$(GIT_VERSION)'"
SERVER_LDFLAGS          ?= -ldflags "-X '$(VERSION_PKG).gitVersion=$(GIT_VERSION)' -X '$(VERSION_PKG).gitCommit=$(GIT_COMMIT)' -X '$(VERSION_PKG).gitTreeState=$(GIT_TREE_STATE)' -X '$(VERSION_PKG).commitDate=$(COMMIT_DATE)'"
GO_BUILD_TAGS           ?= upstream
# Image URL to use all building/pushing controller image targets
CONTROLLER_IMG          ?= quay.io/operator-framework/catalogd-controller
# Image URL to use all building/pushing apiserver image targets
# TODO: When the apiserver is working properly, uncomment this line:
# SERVER_IMG              ?= quay.io/operator-framework/catalogd-server
# Tag to use when building/pushing images
IMG_TAG                 ?= devel
## Location to build controller/apiserver binaries in
LOCALBIN                ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)


# Dependencies
CERT_MGR_VERSION        ?= v1.11.0
ENVTEST_SERVER_VERSION = $(shell go list -m k8s.io/client-go | cut -d" " -f2 | sed 's/^v0\.\([[:digit:]]\{1,\}\)\.[[:digit:]]\{1,\}$$/1.\1.x/')

# Cluster configuration
KIND_CLUSTER_NAME       ?= catalogd
CATALOGD_NAMESPACE      ?= catalogd-system

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

##@ Development

clean: ## Remove binaries and test artifacts
	rm -rf bin

.PHONY: generate
generate: controller-gen ## Generate code and manifests.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test-unit: generate fmt vet setup-envtest ## Run tests.
	eval $$($(SETUP_ENVTEST) use -p env $(ENVTEST_SERVER_VERSION)) && go test ./... -coverprofile cover.out

.PHONY: tidy
tidy: ## Update dependencies
	go mod tidy

.PHONY: verify
verify: tidy fmt generate ## Verify the current code generation and lint
	git diff --exit-code

##@ Build

.PHONY: build-controller
build-controller: generate fmt vet ## Build manager binary.
	CGO_ENABLED=0 GOOS=linux go build -tags $(GO_BUILD_TAGS) $(CTRL_LDFLAGS) -o bin/manager cmd/manager/main.go

# TODO: When the apiserver is working properly, uncomment this target:
# .PHONY: build-server
# build-server: fmt vet ## Build api-server binary.
# 	CGO_ENABLED=0 GOOS=linux go build -tags $(GO_BUILD_TAGS) $(SERVER_LDFLAGS) -o bin/apiserver cmd/apiserver/main.go

.PHONY: run
run: generate fmt vet ## Run a controller from your host.
	go run ./main.go

.PHONY: docker-build-controller
docker-build-controller: build-controller test ## Build docker image with the controller manager.
	docker build -f controller.Dockerfile -t ${CONTROLLER_IMG}:${IMG_TAG} bin/

.PHONY: docker-push-controller
docker-push-controller: ## Push docker image with the controller manager.
	docker push ${CONTROLLER_IMG}

# TODO: When the apiserver is working properly, uncomment the 2 targets below:
# .PHONY: docker-build-server
# docker-build-server: build-server test ## Build docker image with the apiserver.
# 	docker build -f apiserver.Dockerfile -t ${SERVER_IMG}:${IMG_TAG} bin/

# .PHONY: docker-push-server
# docker-push-server: ## Push docker image with the apiserver.
# 	docker push ${SERVER_IMG}

##@ Deploy

.PHONY: kind-cluster
kind-cluster: kind kind-cluster-cleanup ## Standup a kind cluster
	$(KIND) create cluster --name ${KIND_CLUSTER_NAME} 
	$(KIND) export kubeconfig --name ${KIND_CLUSTER_NAME}

.PHONY: kind-cluster-cleanup
kind-cluster-cleanup: kind ## Delete the kind cluster
	$(KIND) delete cluster --name ${KIND_CLUSTER_NAME}

# TODO: When the apiserver is working properly, add this line back to the end of this target:
# $(KIND) load docker-image $(SERVER_IMG):${IMG_TAG} --name $(KIND_CLUSTER_NAME)
.PHONY: kind-load
kind-load: kind ## Load the built images onto the local cluster 
	$(KIND) export kubeconfig --name ${KIND_CLUSTER_NAME}
	$(KIND) load docker-image $(CONTROLLER_IMG):${IMG_TAG} --name $(KIND_CLUSTER_NAME)


# TODO: When the apiserver is working properly, add the `docker-build-server` and `cert-manager` targets back as a dependency to this target:
.PHONY: install 
install: docker-build-controller kind-load deploy wait ## Install local catalogd
	
# TODO: When the apiserver is working properly, add this line back after the manager edit:
# cd config/apiserver && $(KUSTOMIZE) edit set image apiserver=${SERVER_IMG}:${IMG_TAG}
.PHONY: deploy
deploy: kustomize ## Deploy CatalogSource controller and ApiServer to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${CONTROLLER_IMG}:${IMG_TAG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy CatalogSource controller and ApiServer from the K8s cluster specified in ~/.kube/config. 
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=true -f -	

.PHONY: uninstall 
uninstall: undeploy ## Uninstall local catalogd
	kubectl wait --for=delete namespace/$(CATALOGD_NAMESPACE) --timeout=60s

# TODO: cert-manager was only needed due to the apiserver. When the apiserver is working properly, uncomment this target
# .PHONY: cert-manager
# cert-manager: ## Deploy cert-manager on the cluster
# 	kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/$(CERT_MGR_VERSION)/cert-manager.yaml
# 	kubectl wait --for=condition=Available --namespace=cert-manager deployment/cert-manager-webhook --timeout=60s

# TODO: When the apiserver is working properly, add the following lines to this target:
# kubectl wait --for=condition=Available --namespace=$(CATALOGD_NAMESPACE) deployment/catalogd-apiserver --timeout=60s
# kubectl rollout status --watch --namespace=$(CATALOGD_NAMESPACE) statefulset/catalogd-etcd --timeout=60s

wait:
	kubectl wait --for=condition=Available --namespace=$(CATALOGD_NAMESPACE) deployment/catalogd-controller-manager --timeout=60s

##@ Release

export ENABLE_RELEASE_PIPELINE ?= false
export GORELEASER_ARGS         ?= --snapshot --clean
export CONTROLLER_IMAGE_REPO   ?= $(CONTROLLER_IMG)
# TODO: When the apiserver is working properly, uncomment this line:
# export APISERVER_IMAGE_REPO ?= $(SERVER_IMG)
export IMAGE_TAG               ?= $(IMG_TAG)
export VERSION_PKG             ?= $(VERSION_PKG)
export GIT_VERSION             ?= $(GIT_VERSION)
export GIT_COMMIT              ?= $(GIT_COMMIT)
export GIT_TREE_STATE          ?= $(GIT_TREE_STATE)
export COMMIT_DATE             ?= $(COMMIT_DATE)
release: goreleaser ## Runs goreleaser for catalogd. By default, this will run only as a snapshot and will not publish any artifacts unless it is run with different arguments. To override the arguments, run with "GORELEASER_ARGS=...". When run as a github action from a tag, this target will publish a full release.
	$(GORELEASER) $(GORELEASER_ARGS)

quickstart: kustomize generate ## Generate the installation release manifests and scripts
	$(KUSTOMIZE) build config/default | sed "s/:devel/:$(VERSION)/g" > catalogd.yaml
	
################
# Hack / Tools #
################
TOOLS_DIR := $(shell pwd)/hack/tools
TOOLS_BIN_DIR := $(TOOLS_DIR)/bin
$(TOOLS_BIN_DIR):
	mkdir -p $(TOOLS_BIN_DIR)


KUSTOMIZE_VERSION        ?= v5.0.1
KIND_VERSION             ?= v0.15.0
CONTROLLER_TOOLS_VERSION ?= v0.10.0
GORELEASER_VERSION       ?= v1.16.2
ENVTEST_VERSION          ?= latest

##@ hack/tools:

.PHONY: controller-gen goreleaser kind setup-envtest kustomize

CONTROLLER_GEN := $(abspath $(TOOLS_BIN_DIR)/controller-gen)
SETUP_ENVTEST := $(abspath $(TOOLS_BIN_DIR)/setup-envtest)
GORELEASER := $(abspath $(TOOLS_BIN_DIR)/goreleaser)
KIND := $(abspath $(TOOLS_BIN_DIR)/kind)
KUSTOMIZE := $(abspath $(TOOLS_BIN_DIR)/kustomize)

kind: $(TOOLS_BIN_DIR) ## Build a local copy of kind
	GOBIN=$(TOOLS_BIN_DIR) go install sigs.k8s.io/kind@$(KIND_VERSION)

controller-gen: $(TOOLS_BIN_DIR) ## Build a local copy of controller-gen
	GOBIN=$(TOOLS_BIN_DIR) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

goreleaser: $(TOOLS_BIN_DIR) ## Build a local copy of goreleaser
	GOBIN=$(TOOLS_BIN_DIR) go install github.com/goreleaser/goreleaser@$(GORELEASER_VERSION)

setup-envtest: $(TOOLS_BIN_DIR) ## Build a local copy of envtest
	GOBIN=$(TOOLS_BIN_DIR) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@$(ENVTEST_VERSION)

kustomize: $(TOOLS_BIN_DIR) ## Build a local copy of kustomize
	GOBIN=$(TOOLS_BIN_DIR) go install sigs.k8s.io/kustomize/kustomize/v5@$(KUSTOMIZE_VERSION)
