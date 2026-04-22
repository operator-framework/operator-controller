# Tech Stack

## Language & Runtime

- **Language:** Go (version specified in go.mod; intentionally lags upstream to give integrators adoption time)
- **Module:** `github.com/operator-framework/operator-controller`
- **Runtime platform:** Linux containers (multi-arch: amd64, arm64, ppc64le, s390x)
- **Developer platform:** macOS and Linux
- **Container base:** `gcr.io/distroless/static:nonroot` (user 65532:65532)
- **Build tags:** `containers_image_openpgp`

## Core Dependencies

| Dependency | Purpose |
|---|---|
| k8s.io/client-go, api, apimachinery | Kubernetes API interaction |
| sigs.k8s.io/controller-runtime | Controller framework |
| operator-framework/api | OLMv0 API types |
| operator-framework/operator-registry | File-based catalog (FBC) processing |
| helm-operator-plugins | Helm-based bundle rendering |
| cert-manager | TLS certificate management |
| package-operator.run (boxcutter) | Object set management |
| google/go-containerregistry | OCI image handling |
| spf13/cobra | CLI tooling |

## Dev Dependencies

| Tool | Purpose | Management |
|---|---|---|
| controller-gen | CRD/RBAC/deepcopy generation | bingo |
| golangci-lint | Linting | bingo |
| goreleaser | Release builds | bingo |
| helm | Chart rendering/linting | bingo |
| kind | Local Kubernetes clusters | bingo |
| kustomize | Manifest composition | bingo |
| setup-envtest | Test environment binaries | bingo |
| operator-sdk | Extension developer tooling | bingo |
| yamlfmt | YAML formatting | bingo |
| conftest | Helm policy testing | bingo |
| godog/cucumber | E2E BDD tests | go.mod |

## Project Structure

```
/
+-- api/v1/                          # API definitions (CRD types)
+-- applyconfigurations/             # Generated apply configurations
+-- cmd/
|   +-- operator-controller/         # Operator controller binary
|   +-- catalogd/                    # Catalogd binary
+-- internal/
|   +-- operator-controller/         # Operator controller internals
|   +-- catalogd/                    # Catalogd internals
|   +-- shared/                      # Shared utilities
+-- helm/olmv1/                      # Helm chart (base CRDs, templates, values)
+-- test/
|   +-- e2e/                         # End-to-end tests (godog/cucumber)
|   +-- extension-developer-e2e/     # Extension developer tests
|   +-- upgrade-e2e/                 # Upgrade tests
|   +-- regression/                  # Regression tests
+-- config/samples/                  # Example manifests
+-- docs/                            # Documentation (mkdocs)
+-- hack/                            # Scripts and dev tools
+-- manifests/                       # Generated release manifests
+-- .github/workflows/               # CI/CD workflows
+-- .bingo/                          # Pinned tool versions
```

## Build Commands

| Command | Purpose |
|---|---|
| `make build` | Build binaries for local platform |
| `make build-linux` | Build binaries for Linux (required for Docker) |
| `make docker-build` | Build container images |
| `make lint` | Run golangci-lint |
| `make fmt` | Format code (Go + YAML) |
| `make test-unit` | Run unit tests (envtest-based) |
| `make test-e2e` | Run e2e tests (requires kind cluster) |
| `make test-regression` | Run regression tests |
| `make verify` | Verify all generated code is up-to-date |
| `make manifests` | Generate CRDs and release manifests |
| `make generate` | Generate deepcopy and apply configurations |
| `make run` | Create kind cluster and deploy (standard) |
| `make run-experimental` | Create kind cluster and deploy (experimental) |
| `make kind-clean` | Tear down kind cluster |

### Primary Check Command

```bash
make lint && make test-unit
```

### Format Command

```bash
make fmt
```

### Container Verification

```bash
make docker-build
```

## CI/CD

GitHub Actions workflows:
- `unit-test.yaml` - Unit tests with coverage
- `e2e.yaml` - Multiple e2e test suites (7 variants)
- `sanity.yaml` - Verification, linting, helm linting
- `test-regression.yaml` - Regression tests
- `go-apidiff.yaml` - API compatibility checks
- `crd-diff.yaml` - CRD compatibility verification
- `release.yaml` - Automated releases on tags (goreleaser)

## Containerization

- Two Dockerfiles: `Dockerfile.operator-controller`, `Dockerfile.catalogd`
- Multi-arch builds: amd64, arm64, ppc64le, s390x
- Base image: `gcr.io/distroless/static:nonroot`
- Non-root user: `65532:65532`

## Feature Gates

Two manifest variants:
- **Standard:** Production-ready features
- **Experimental:** Features under development (includes `ClusterObjectSet` API)
