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

ifneq (, $(shell command -v docker 2>/dev/null))
CONTAINER_RUNTIME := docker
else ifneq (, $(shell command -v podman 2>/dev/null))
CONTAINER_RUNTIME := podman
else
$(warning Could not find docker or podman in path! This may result in targets requiring a container runtime failing!)
endif

# bingo manages consistent tooling versions for things like kind, kustomize, etc.
include ./../.bingo/Variables.mk

# Dependencies
export CERT_MGR_VERSION := v1.15.3
ENVTEST_SERVER_VERSION := $(shell go list -m k8s.io/client-go | cut -d" " -f2 | sed 's/^v0\.\([[:digit:]]\{1,\}\)\.[[:digit:]]\{1,\}$$/1.\1.x/')

# Cluster configuration
ifeq ($(origin KIND_CLUSTER_NAME), undefined)
KIND_CLUSTER_NAME := catalogd
endif

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
	$(CONTROLLER_GEN) object:headerFile="../hack/boilerplate.go.txt" paths="./..."
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/base/crd/bases output:rbac:artifacts:config=config/base/rbac output:webhook:artifacts:config=config/base/manager/webhook/

##@ Build

BINARIES=catalogd
LINUX_BINARIES=$(join $(addprefix linux/,$(BINARIES)), )

# Build info
ifeq ($(origin VERSION), undefined)
VERSION := $(shell git describe --tags --always --dirty)
endif
export VERSION

export VERSION_PKG     := $(shell go list -m)/internal/shared/version

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
export GO_BUILD_TAGS     := containers_image_openpgp

BUILDCMD = go build -tags '$(GO_BUILD_TAGS)' -ldflags '$(GO_BUILD_LDFLAGS)' -gcflags '$(GO_BUILD_GCFLAGS)' -asmflags '$(GO_BUILD_ASMFLAGS)' -o $(BUILDBIN)/$(notdir $@) ./cmd/$(notdir $@)

.PHONY: build-deps
build-deps: generate

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

.PHONY: build-container
build-container: build-linux ## Build docker image for catalogd.
	$(CONTAINER_RUNTIME) build -f Dockerfile -t $(IMAGE) ./bin/linux

.PHONY: kind-load
kind-load: $(KIND) ## Load the built images onto the local cluster
	$(CONTAINER_RUNTIME) save $(IMAGE) | $(KIND) load image-archive /dev/stdin --name $(KIND_CLUSTER_NAME)

