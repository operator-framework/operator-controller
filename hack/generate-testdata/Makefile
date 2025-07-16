KUBEBUILDER_VERSION := 4.7.0
BIN_DIR := ./bin
OS := $(shell uname | tr A-Z a-z)
ARCH := $(shell uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
KUBEBUILDER_BIN := $(BIN_DIR)/kubebuilder

.PHONY: install-kubebuilder
install-kubebuilder:
	mkdir -p $(BIN_DIR)
	curl -fsSL -o $(KUBEBUILDER_BIN) https://github.com/kubernetes-sigs/kubebuilder/releases/download/v$(KUBEBUILDER_VERSION)/kubebuilder_$(OS)_$(ARCH)
	chmod +x $(KUBEBUILDER_BIN)

.PHONY: generate-testdata
generate-testdata: install-kubebuilder
	go run ./internal
