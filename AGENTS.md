# AGENTS.md - AI Coding Assistant Briefing

This document serves as a comprehensive briefing for AI coding assistants working with the operator-controller
repository. It covers the WHAT, WHY, and HOW of contributing to this codebase.

---

## Architecture Overview

operator-controller is the central component of Operator Lifecycle Manager (OLM) v1, extending Kubernetes with APIs to
install and manage cluster extensions. The project follows a microservices architecture with two main binaries:

**operator-controller** 
 - manages `ClusterExtension` and `ClusterExtensionRevision` CRDs
 - resolves bundles from configured source
 - unpacks bundles and renders manifests from them
 - applies manifests with phase-based rollouts
 - monitors extension lifecycle

**catalogd**
 - manages user-defined `ClusterCatalog` resources to make references catalog metadata available to the cluster.
 - unpacks and serves operator catalog content via HTTP.
 - serves catalog metadata to clients in the cluster that need to use or present information about the contents of the
   catalog. For example, operator-controller queries catalogd for available bundles.

---

## Tech Stack

**Languages:**
- **Go:** The Go version used by the project often lags the latest upstream available version in order to give
  integrators the ability to consume the latest versions of OLMv1 without being required to also consume the latest
  versions of Go.
- **Runtime Platform:** Linux containers (multi-arch: amd64, arm64, ppc64le, s390x)
- **Developer Platform:** Generally macOS and Linux. It is important that all shell commands used in Makefiles and
  other helper scripts work on both Linux and macOS.

**Core Frameworks:**
- **Kubernetes:** client-go, api, apimachinery
- **controller-runtime**
- **operator-framework/api:** For OLMv0 API types that are relevant to OLMv1
- **operator-registry** For file-based catalog (FBC) processing
- **Helm:** helm-operator-plugins (which depends on helm itself)

**Key Dependencies:**
- **cert-manager**
- **boxcutter (package-operator.run)**

**Container Base:**
- Base image: `gcr.io/distroless/static:nonroot`
- User: `65532:65532` (non-root)

**Build Tags:**
- `containers_image_openpgp` - required for image handling

**Tools (managed via .bingo/):**
- controller-gen, golangci-lint, goreleaser, helm, kind, kustomize, setup-envtest, operator-sdk

---

## Build & Test Commands

### Build

```bash
# Build for local platform
make build

# Build for Linux (required for docker)
make build-linux

# Build docker images
make docker-build

# Full release build
make release
```

### Test

```bash
# Unit tests (uses ENVTEST)
make test-unit

# E2E tests
make test-e2e                      # Standard features
make test-experimental-e2e         # Experimental features
make test-extension-developer-e2e  # Extension developer workflow

# Regression tests
make test-regression

# All (non-upgrade, non-experimental) tests
make test
```

### Linting & Verification

```bash
# Run golangci-lint
make lint

# Run helm lint
make lint-helm

# Verify all generated code is up-to-date
make verify

# Format code
make fmt

# Fix lint issues automatically
make fix-lint
```

### Local Development

```bash
# Create kind cluster and deploy
make run                    # Standard manifest
make run-experimental       # Experimental manifest

# OR step by step:
make kind-cluster          # Create cluster
make docker-build          # Build images
make kind-load             # Load into kind
make kind-deploy           # Deploy manifests
make wait                  # Wait for ready

# Clean up
make kind-clean
```

### Manifest Generation

```bash
# Generate CRDs and manifests
make manifests

# Update CRDs and reference docs (when Go-based API definitions change)
make update-crds crd-ref-docs

# Generate code (DeepCopy methods)
make generate
```

---

## Conventions & Patterns

### Folder Structure

```
/
├── api/v1/                          # API definitions (CRD types)
├── cmd/                             # Main entry points
│   ├── operator-controller/         # Operator controller binary
│   └── catalogd/                    # Catalogd binary
├── internal/                        # Private implementation
│   ├── operator-controller/         # Operator controller internals
│   ├── catalogd/                    # Catalogd internals
│   └── shared/                      # Shared utilities
├── helm/                            # Helm charts
│   ├── olmv1/                       # Main OLM v1 chart
│   │   ├── base/                    # Base manifests & CRDs
│   │   ├── templates/               # Helm templates
│   │   └── values.yaml              # Default values
│   └── prometheus/                  # Prometheus monitoring
├── test/                            # Test suites
│   ├── e2e/                         # End-to-end tests
│   ├── extension-developer-e2e/     # Extension developer tests
│   ├── upgrade-e2e/                 # Upgrade tests
│   └── regression/                  # Regression tests
├── docs/                            # Documentation (mkdocs)
├── hack/                            # Scripts and tools
├── config/samples                   # Example manifests for ClusterCatalog and ClusterExtension
├── manifests/                       # Generated manifests
├── .github/workflows/               # CI/CD workflows
├── OWNERS                           # Defines approver and reviewer groups
└── OWNERS_ALIASES                   # Defined group membership

```

### Naming Conventions

- **Controllers:** `{resource}_controller.go`
- **Tests:** `{name}_test.go`
- **Internal packages:** lowercase, no underscores
- **Generated files:** `zz_generated.*.go`
- **CRDs:** `{group}.{domain}_{resources}.yaml`

### Core APIs

- **Primary CRDs:**
  - `ClusterExtension` - declares desired extension installations
  - `ClusterExtensionRevision` - revision management (experimental)
  - `ClusterCatalog` - catalog source definitions
- **API domain:** `olm.operatorframework.io`
  - This is the API group of our user-facing CRDs
  - This is also the domain that should be used in ALL label and annotation prefixes that are generated by OLMv1)
- **API version:** `v1`

### Feature Gates

Two manifest variants exist:
- **Standard:** Production-ready features
- **Experimental:** Features under development/testing (includes `ClusterExtensionRevision` API)

---

## Git Workflows

### Branching Strategy

- **Main branch:** `main` (default, protected)
- **Release branches:** `release-v{MAJOR}.{MINOR}` (e.g., `release-v1.2`)
- **Feature branches:** Created from `main`, usually in forks, merged via PR

### Commit Message Format

```
<High-level description>

<Detailed description>
```

### PR Requirements

- Must pass all CI checks (unit-test, e2e, sanity, lint)
- Must have both `approved` and `lgtm` labels (from repository approvers and reviewers)
- DCO sign-off required (Developer Certificate of Origin)
- Reasonable title and description
- Draft PRs: prefix with "WIP:" or use GitHub draft feature
- PR title must use specific prefix based on the type of the PR:
   -  ⚠ (:warning:, major/breaking change)
   -  ✨ (:sparkles:, minor/compatible change)
   -  🐛 (:bug:, patch/bug fix)
   -  📖 (:book:, docs)
   -  🌱 (:seedling:, other)

### CI Workflows

- `unit-test.yaml` - Unit tests with coverage
- `e2e.yaml` - Multiple e2e test suites (7 variants)
- `sanity.yaml` - Verification, linting, helm linting
- `test-regression.yaml` - Regression tests
- `go-apidiff.yaml` - API compatibility checks
- `crd-diff.yaml` - CRD compatibility verification
- `release.yaml` - Automated releases on tags

### Release Process

- **Semantic versioning:** `vMAJOR.MINOR.PATCH`
- Tags trigger automated release via goreleaser
- Patch releases from `release-v*` branches
- Major/minor releases from `main` branch
- Creates multi-arch container images
- Generates release manifests and install scripts

---

## Boundaries (What Not to Touch)

### Generated Files (require special process)

**Schema Files:**
- `/api/v1/zz_generated.deepcopy.go` - Generated by controller-gen
- `/helm/olmv1/base/*/crd/standard/*.yaml` - Standard CRDs (generated)
- `/helm/olmv1/base/*/crd/experimental/*.yaml` - Experimental CRDs (generated)
- **Process:** Modify types in `/api/v1/*_types.go`, then run `make manifests`

**Generated Manifests:**
- `/manifests/standard.yaml` - Generated by `make manifests`
- `/manifests/experimental.yaml` - Generated by `make manifests`
- `/manifests/standard-e2e.yaml` - Generated by `make manifests`
- `/manifests/experimental-e2e.yaml` - Generated by `make manifests`
- **Process:** Modify Helm charts in `/helm/olmv1/`, then run `make manifests

**Generated Docs:**
- `/docs/api-reference/olmv1-api-reference.md` - Generated by `make crd-ref-docs`
- **Process:** Requires regeneration whenever API definitions change (`api/*/*_types.go`)

### CI/CD & Project Metadata

**Never modify without explicit permission:**
- `/.github/workflows/*.yaml` - CI pipelines (consult team)
- `/.goreleaser.yml` - Release configuration
- `/Makefile` - Core build logic (discuss changes first)
- `/go.mod` - Dependencies (use `make tidy`, avoid Go version bumps without discussion)
- `/PROJECT` - Kubebuilder project config
- `/OWNERS` & `/OWNERS_ALIASES` - Maintainer lists
- `/CODEOWNERS` - Code ownership
- `/mkdocs.yml` - Documentation site config
- `/.bingo/*.mod` - Tool dependencies (managed by bingo)
- `/.golangci.yaml` - Linter configuration
- `/kind-config.yaml` - Kind cluster config

### Helm Charts (requires careful review)

- `/helm/olmv1/Chart.yaml` - Chart metadata
- `/helm/olmv1/values.yaml` - Default values
- `/helm/olmv1/templates/*.yaml` - Chart templates

### Security & Compliance

- `/DCO` - Developer Certificate of Origin
- `/LICENSE` - Apache 2.0 license
- `/codecov.yml` - Code coverage config

---

## Important Notes for AI Agents

1. **Never commit to `main` directly** - always use PRs
2. **CRD changes** require running `make update-crds` and may break compatibility
3. **API changes** in `/api/v1/` trigger CRD and CRD reference docs regeneration
4. **Generated files** must be committed after running generators
5. **Helm changes** require `make manifests` to update manifests
6. **Go version changes** need community discussion (see CONTRIBUTING.md)
7. **Dependencies:** 2-week cooldown policy for non-critical updates

### Development Workflow

1. Make changes to source code
2. Run `make verify` to ensure generated code is updated
3. Run `make lint` and `make test-unit`
4. Commit both source and generated files
5. CI will verify everything is in sync

### Key Components to Understand

**operator-controller:**
- `ClusterExtension` controller - manages extension installations
- `ClusterExtensionRevision` controller - manages revision lifecycle
- Resolver - bundle version selection
- Applier - applies manifests to cluster
- Content Manager - manages extension content

**catalogd:**
- Catalog controllers - manage catalog unpacking
- Storage - catalog storage backend
- Server utilities - HTTP server for catalog content

---

**Last Updated:** 2025-12-10
