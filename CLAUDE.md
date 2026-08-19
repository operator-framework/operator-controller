# operator-controller

The central component of Operator Lifecycle Manager (OLM) v1, providing APIs and controllers for packaging, distributing, and managing the lifecycle of Kubernetes cluster extensions - bundles of arbitrary Kubernetes objects that extend cluster functionality. The project contains two components: operator-controller (core lifecycle management) and catalogd (catalog serving).

## Architecture

- `api/v1/` - Kubernetes API types (ClusterExtension, ClusterCatalog, etc.)
- `cmd/operator-controller/` and `cmd/catalogd/` - binary entry points
- `internal/operator-controller/` and `internal/catalogd/` - controller logic
- `internal/shared/` - shared internal packages
- `config/` - deployment manifests (kustomize)
- `helm/` - Helm chart for installation
- `test/e2e/`, `test/upgrade-e2e/`, `test/regression/` - test suites

## Design Principles

1. Do not fight Kubernetes - work with global API registration, single-owner semantics
2. Secure by default - no cluster-admin; user-supplied service accounts
3. Simple and predictable - declarative, eventually-consistent, GitOps-friendly
4. Opinionated guardrails with escape hatches
5. Extension-agnostic packaging - bundles can contain any Kubernetes objects
6. Constraint checking, not dependency management

## Build and Test

```
make build              # Build binaries
make test-unit          # Unit tests (envtest)
make test-e2e           # End-to-end tests
make lint               # golangci-lint (includes custom linter)
make fmt                # Format code (Go + YAML)
make verify             # Verify all generated code is up-to-date
make generate           # Generate DeepCopy, mocks, apply configurations
make manifests          # Generate CRDs and deployment manifests
make verify-crd-compatibility  # Check CRD backward compatibility
make tidy               # go mod tidy
```

Local development uses kind clusters (`make kind-cluster`, `make kind-deploy`) and Tilt for live reload.

## Conventions

- Commit messages use emoji prefixes: `:sparkles:` (feature), `:bug:` (fix), `:seedling:` (chore), `:book:` (docs), `:warning:` (breaking)
- PR titles use the same emoji prefix format
- See `specs/conventions.md` for full details

## SDD Workflow

Work items are tracked as spec directories under `specs/YYYY-MM-DD-<slug>/` with status frontmatter (`idea`, `ready`, `in-progress`, `pr-submitted`, `done`).

| Command | Purpose |
|---|---|
| `/sdd-ideate` | Brainstorm and add new work items to the backlog |
| `/sdd-plan-next-phase` | Plan and spec out the next work item |
| `/sdd-implement` | Implement a work item from its spec |
| `/sdd-review` | Review changes for correctness and consistency |
| `/sdd-ship` | Verify, commit, push, and create PR |
| `/sdd-cleanup` | Archive completed specs, flag stale items |
| `/sdd-quick-item` | Quickly capture an idea to the backlog |

Governing docs live in `specs/`: `mission.md`, `tech-stack.md`, `conventions.md`.
