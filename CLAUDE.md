# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This repository contains the **operator-controller**, which is the central component of Operator Lifecycle Manager (OLM) v1. OLM v1 is a Kubernetes operator lifecycle management system that provides APIs, controllers, and tooling for packaging, distributing, and managing Kubernetes extensions/operators.

The project consists of two main components:
- **operator-controller**: Manages ClusterExtension lifecycle (install, upgrade, uninstall)
- **catalogd**: Serves operator catalogs and provides catalog content via HTTP API

## Essential Commands

### Build and Development
```bash
make build              # Build binaries for current GOOS/GOARCH
make build-linux        # Build binaries for GOOS=linux
make docker-build       # Build container images
make generate           # Generate code (DeepCopy methods, etc.)
make manifests          # Generate CRDs, RBAC, and other manifests
make fmt                # Format Go code
make lint               # Run golangci linter
make verify             # Verify all generated code is up-to-date
```

### Testing
```bash
make test               # Run all tests (unit, e2e, regression)
make test-unit          # Run unit tests only
make test-e2e           # Run e2e tests (requires kind cluster)
make test-experimental-e2e  # Run experimental e2e tests
make test-regression    # Run regression tests
make envtest-k8s-bins   # Download ENVTEST binaries for unit tests
```

### Local Development and Cluster Management
```bash
make kind-cluster       # Create local kind cluster
make kind-load          # Load built images into kind cluster
make kind-deploy        # Deploy to kind cluster
make kind-clean         # Delete kind cluster
make run                # Build, create cluster, and deploy (standard)
make run-experimental   # Build, create cluster, and deploy (experimental)
```

### Release and Documentation
```bash
make release            # Run goreleaser (snapshot by default)
make quickstart         # Generate release manifests and install scripts
make crd-ref-docs       # Generate API reference documentation
```

## Architecture and Patterns

### Core API Types
- **ClusterExtension**: Represents an operator/extension to be installed
- **ClusterCatalog**: References a catalog of available operators

### Controller Architecture
The system follows standard Kubernetes controller patterns:
- Controllers use **controller-runtime** framework
- **Reconciliation loops** manage desired vs actual state
- **Finalizers** ensure proper cleanup during deletion
- **Status conditions** track installation/upgrade progress

### Key Components
- **Resolver**: Determines which operator version to install based on constraints
- **Applier**: Handles Helm-based installations and upgrades
- **Authentication/Authorization**: Manages service account permissions and RBAC
- **Catalog Metadata Client**: Fetches operator metadata from catalogs
- **Bundle Utilities**: Processes operator bundle formats

### Testing Strategy
- **Unit tests**: Use ENVTEST with real Kubernetes APIs
- **E2E tests**: Full cluster testing with kind
- **Integration tests**: Test component interactions
- **Regression tests**: Verify conversion between bundle formats

## Development Environment

### Required Tools
- Go 1.24+
- Docker or Podman for container builds
- kubectl for cluster interaction
- kind for local development clusters
- make for build automation

### Tool Management
- Uses **bingo** for version-pinned tools (stored in `.bingo/`)
- Tools include: kind, kustomize, controller-gen, golangci-lint, etc.

### Code Generation
Most Kubernetes resources are generated:
- **CRDs** generated from Go struct definitions using controller-gen
- **RBAC** manifests generated based on controller annotations
- **DeepCopy methods** generated for API types
- **Kustomize overlays** for different deployment configurations

## Important Development Notes

### Multi-Component Coordination
- Changes often affect both operator-controller and catalogd
- Shared utilities exist in `internal/shared/`
- API definitions in `api/v1/` are used by both components

### Testing Requirements
- Always run `make verify` before submitting changes
- E2E tests are crucial for Kubernetes integration
- Unit tests use controller-runtime's envtest package
- Coverage tracking enabled for both unit and e2e tests

### Feature Gates
The project uses feature gates for experimental functionality:
- Standard vs Experimental builds via Go build tags
- Feature flags defined in `internal/*/features/`
- Different manifests for standard vs experimental deployments

### Configuration Management
- **Kustomize** used for manifest generation and customization
- Multiple overlays: standard, experimental, e2e variants
- Environment-specific configurations in `config/overlays/`

### Container Images and Registries
- Multi-arch container builds supported
- Default registry: `quay.io/operator-framework`
- Local development uses kind for loading images
- E2E tests include registry setup for testing

This is a complex, production-grade Kubernetes operator system requiring understanding of operator patterns, Helm integration, and catalog-based software distribution.