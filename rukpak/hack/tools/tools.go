//go:build tools
// +build tools

package tools

import (
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint" // Better linting
	_ "github.com/goreleaser/goreleaser"                    // For releasing rukpak
	_ "github.com/onsi/ginkgo/v2/ginkgo"                    // For running E2E tests
	_ "sigs.k8s.io/controller-runtime/tools/setup-envtest"  // Generate deepcopy, conversion, and CRDs
	_ "sigs.k8s.io/controller-tools/cmd/controller-gen"     // Generate deepcopy, conversion, and CRDs
	_ "sigs.k8s.io/kind"                                    // For running e2e tests
)
