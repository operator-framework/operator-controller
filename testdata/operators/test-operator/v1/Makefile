SHELL := /usr/bin/env bash -o pipefail
.SHELLFLAGS := -ec

include ../../../../.bingo/Variables.mk

ifeq ($(origin BUNDLE_VERSION), undefined)
BUNDLE_VERSION := 1.0.0
endif
export BUNDLE_VERSION

ifeq ($(origin BUNDLE_CHANNELS), undefined)
BUNDLE_CHANNELS := alpha
endif
export BUNDLE_CHANNELS

ifeq ($(origin BUNDLE_MANIFEST_PATH), undefined)
BUNDLE_MANIFEST_PATH := bundle
endif
export BUNDLE_MANIFEST_PATH

ifeq ($(origin CONTROLLER_BINARY_PATH), undefined)
CONTROLLER_BINARY_PATH := bin/manager
endif
export CONTROLLER_BINARY_PATH

.PHONY: build
build:
	go build $(GO_BUILD_FLAGS) $(GO_BUILD_EXTRA_FLAGS) -tags '$(GO_BUILD_TAGS)' -ldflags '$(GO_BUILD_LDFLAGS)' -gcflags '$(GO_BUILD_GCFLAGS)' -asmflags '$(GO_BUILD_ASMFLAGS)' -o $(CONTROLLER_BINARY_PATH) cmd/main.go

.PHONY: test-unit
test-unit: $(SETUP_ENVTEST)
	KUBEBUILDER_ASSETS="$(shell $(SETUP_ENVTEST) use -p path $(ENVTEST_VERSION) $(SETUP_ENVTEST_BIN_DIR_OVERRIDE))" \
		CGO_ENABLED=1 go test -tags '$(GO_BUILD_TAGS)' -count=1 -race -short ./...

.PHONY: manifests
manifests: $(CONTROLLER_GEN)
	$(CONTROLLER_GEN) --load-build-tags=$(GO_BUILD_TAGS) crd paths="./api/..." output:dir=dist/chart/crds
    $(CONTROLLER_GEN) --load-build-tags=$(GO_BUILD_TAGS) rbac:roleName=test-operator-manager-role paths="./..." output:dir=dist/chart/templates

.PHONY: bundle
bundle: manifests $(HELM) $(OPERATOR_SDK)
	$(HELM) template $(HELM_OPTS) --include-crds dist/chart | $(OPERATOR_SDK) generate bundle --channels $(BUNDLE_CHANNELS) --version $(BUNDLE_VERSION) --package test --output-dir $(BUNDLE_MANIFEST_PATH) --overwrite --quiet
	sed -i '/\s*createdAt:/d' $(BUNDLE_MANIFEST_PATH)/manifests/*.yaml
