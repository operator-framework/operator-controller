# Tech Stack

## Language and Runtime

- **Go** (version pinned in `go.mod`, currently 1.26.x)
- **Kubernetes controller-runtime** for controller infrastructure
- Module path: `github.com/operator-framework/operator-controller`

## Core Dependencies

| Dependency | Purpose |
|---|---|
| `sigs.k8s.io/controller-runtime` | Controller framework, reconciliation, webhooks |
| `github.com/operator-framework/operator-registry` | Catalog/registry content formats and APIs |
| `github.com/operator-framework/helm-operator-plugins` | Helm-based extension installation |
| `github.com/operator-framework/api` | Shared OLM API types |
| `github.com/google/go-containerregistry` | OCI image/registry interaction |
| `github.com/cert-manager/cert-manager` | TLS certificate management |
| `github.com/graphql-go/graphql` | GraphQL API for catalog queries |
| `helm.sh/helm/v3` | Helm chart rendering and installation |

## Dev Tooling

| Tool | Purpose | Managed By |
|---|---|---|
| `golangci-lint` | Linting (includes custom linter) | bingo |
| `controller-gen` | CRD/RBAC/DeepCopy code generation | bingo |
| `setup-envtest` | Kubernetes API server for unit tests | bingo |
| `mockgen` | Mock generation for testing | bingo |
| `yamlfmt` | YAML formatting | bingo |
| `helm` | Chart linting and templating | bingo |
| `kind` | Local Kubernetes clusters | bingo |
| `conftest` | Policy-based Helm chart testing | bingo |
| `crd-diff` | CRD compatibility checking | bingo |
| `bingo` | Dev tool version management | go install |

## Project Structure

```
operator-controller/
  api/v1/                    # Kubernetes API types (ClusterExtension, ClusterCatalog, etc.)
  applyconfigurations/       # Generated apply configuration types
  cmd/
    operator-controller/     # operator-controller binary entry point
    catalogd/                # catalogd binary entry point
  internal/
    operator-controller/     # operator-controller controllers and logic
    catalogd/                # catalogd controllers and logic
    shared/                  # Shared internal packages
    testing/                 # Test helpers
    testutil/                # Test utilities
  config/                    # Kustomize/deployment manifests
  helm/                      # Helm chart
  test/
    e2e/                     # End-to-end tests
    extension-developer-e2e/ # Extension developer workflow tests
    upgrade-e2e/             # Upgrade scenario tests
    regression/              # Regression tests
  docs/                      # Documentation (published to GitHub Pages)
  hack/                      # Build and CI scripts
  scripts/                   # Utility scripts
  manifests/                 # Generated manifests
  testdata/                  # Test fixtures
```

## Build Commands

| Command | Purpose |
|---|---|
| `make build` | Build binaries for local GOOS/GOARCH |
| `make build-linux` | Build binaries for linux |
| `make test-unit` | Run unit tests (uses envtest) |
| `make test-e2e` | Run end-to-end tests |
| `make test-regression` | Run regression tests |
| `make lint` | Run golangci-lint (includes custom linter) |
| `make lint-helm` | Lint Helm chart |
| `make fmt` | Format code (Go + YAML) |
| `make generate` | Generate DeepCopy, mocks, apply configurations |
| `make manifests` | Generate CRDs and deployment manifests |
| `make verify` | Verify all generated code is up-to-date |
| `make verify-crd-compatibility` | Check CRD backward compatibility |
| `make tidy` | Run go mod tidy |

## Containers

- `Dockerfile.operator-controller` - operator-controller image
- `Dockerfile.catalogd` - catalogd image
- Registry: `quay.io/operator-framework/operator-controller` and `quay.io/operator-framework/catalogd`
- Tag convention: `devel` for local, semver tags for releases

## Local Development

- **kind** clusters for local Kubernetes (`make kind-cluster`, `make kind-deploy`)
- **Tilt** for live-reload development
- `make kind-load` to push images into kind
- `make wait` to wait for deployments to be ready

## CI/CD

GitHub Actions workflows:
- `unit-test.yaml` - unit tests
- `e2e.yaml` - end-to-end tests
- `sanity.yaml` - lint, verify, format checks
- `crd-diff.yaml` - CRD compatibility
- `api-diff-lint.yaml` / `go-apidiff.yaml` - API compatibility
- `pr-title.yaml` - PR title format validation
- `release.yaml` - release automation
- `pages.yaml` - documentation publishing

## Version Control

- **jj (Jujutsu)** co-located with git (`.jj/` directory present)
- Use jj commands for all VCS operations; git is the transport layer
