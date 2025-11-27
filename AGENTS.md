# AGENTS.md

This file provides instructions for autonomous agents working in the operator-controller repository.

## Repository Context

This is **operator-controller**, part of OLM v1. Two main Go binaries: `operator-controller` and `catalogd`. Kubernetes controllers using controller-runtime framework. Focus on simplicity, security, predictability.

## Search and Exploration Guidelines

### When searching for code:

**Controllers and reconciliation logic:**
- ClusterExtension controller: `internal/operator-controller/controllers/clusterextension_controller.go`
- ClusterCatalog controller: `internal/catalogd/controllers/core/`
- Look for `Reconcile()` methods and `SetupWithManager()` functions

**API definitions:**
- All CRDs defined in `api/v1/*_types.go`
- Generated DeepCopy code: `api/v1/zz_generated.deepcopy.go`
- CRD manifests (generated): `helm/olmv1/base/*/crd/standard/`

**Resolution and bundle handling:**
- Bundle resolution: `internal/operator-controller/resolve/`
- Bundle rendering: `internal/operator-controller/rukpak/render/`
- Catalog metadata: `internal/operator-controller/catalogmetadata/`

**Testing:**
- Unit tests: Co-located with source files (`*_test.go`)
- E2E tests: `test/e2e/`, `test/experimental-e2e/`, `test/extension-developer-e2e/`
- Test helpers: `test/helpers/`

### Search patterns to use:

**Find controller setup:**
```
Pattern: "SetupWithManager"
Files: internal/*/controllers/**/*.go
```

**Find API validations:**
```
Pattern: "+kubebuilder:validation"
Files: api/v1/*.go
```

**Find feature gates:**
```
Pattern: "features."
Directories: internal/*/features/
```

**Find HTTP/catalog endpoints:**
```
Pattern: "http.HandleFunc|HandleFunc"
Files: internal/catalogd/
```

## Code Modification Guidelines

### CRITICAL: Do not modify generated files

These files are AUTO-GENERATED - never edit directly:
- `api/v1/zz_generated.deepcopy.go`
- `helm/olmv1/base/*/crd/**/*.yaml`
- Any file in `manifests/` directory
- `config/` directory files

To update these, run: `make generate`, `make update-crds`, or `make manifests`

### When modifying API types:

1. Edit `api/v1/*_types.go` only
2. Add kubebuilder markers for validation/printing
3. Run `make generate` to regenerate DeepCopy methods
4. Run `make update-crds` to regenerate CRD manifests
5. Run `make manifests` to regenerate deployment manifests
6. Always run `make verify` before considering task complete

### When modifying controllers:

1. Controllers must implement `reconcile.Reconciler` interface
2. Use controller-runtime patterns (client, scheme, logging)
3. Update status conditions using metav1.Condition
4. Handle finalizers properly for cleanup
5. Add unit tests using ENVTEST framework
6. Run `make test-unit` to verify

### When adding dependencies:

1. Add to `go.mod` via `go get`
2. Run `make tidy`
3. For Kubernetes dependencies, may need `make k8s-pin` to align versions
4. Check if `.bingo/` managed tools need updating

## Testing Requirements

### Before completing any code change:

1. **Format and lint:**
   ```bash
   make fmt
   make lint
   ```

2. **Run unit tests:**
   ```bash
   make test-unit
   ```

3. **Verify generated code is current:**
   ```bash
   make verify
   ```

### For new features:

1. Add unit tests in `*_test.go` files
2. Use `envtest` for controller tests
3. Add e2e tests in appropriate directory
4. Document new features in `docs/`

### Test execution patterns:

```bash
# Single test function
go test -v ./path/to/package -run TestSpecificFunction

# With ENVTEST (for controller tests)
KUBEBUILDER_ASSETS="$(./bin/setup-envtest use -p path 1.31.x)" \
  go test -v ./internal/operator-controller/controllers

# E2E on kind cluster
make test-e2e  # Full standard e2e suite
make test-experimental-e2e  # Experimental features
```

## File Organization Patterns

### Controller structure:
```
internal/{component}/controllers/
├── {resource}_controller.go       # Main reconciliation logic
├── {resource}_controller_test.go  # ENVTEST-based tests
└── suite_test.go                  # Test suite setup
```

### Package structure:
```
internal/{component}/
├── controllers/      # Reconcilers
├── features/        # Feature gates
├── {package}/       # Business logic packages
└── scheme/          # Scheme registration
```

## Common Pitfalls to Avoid

1. **Don't edit manifests directly** - They're generated from Helm charts
2. **Don't skip `make verify`** - It catches generated code drift
3. **Don't use cluster-admin** - Design for least-privilege ServiceAccount
4. **Don't assume multi-tenancy** - This is explicitly NOT supported
5. **Don't hardcode namespaces** - Use `olmv1-system` constant or config
6. **Don't ignore RBAC** - All operations via ServiceAccount
7. **Don't skip preflight checks** - CRD upgrade safety is critical

## Architecture Context for Agents

### Data Flow:
1. User creates ClusterCatalog → catalogd unpacks → HTTP server serves content
2. User creates ClusterExtension → operator-controller queries catalogd
3. Resolver selects bundle → Renderer processes manifests → Applier deploys
4. Status conditions updated throughout lifecycle

### Key Interfaces:
- `reconcile.Reconciler`: All controllers implement this
- `client.Client`: Kubernetes API interactions
- `Resolver`: Bundle selection logic
- `Applier`: Manifest application via boxcutter

### Security Model:
- ServiceAccount specified in ClusterExtension.spec.serviceAccount
- All manifest application uses that ServiceAccount's permissions
- No privilege escalation - user's SA permissions = installation permissions

## Build and Deployment Context

### Make targets for common tasks:

**Development cycle:**
```bash
make build          # Compile binaries
make test-unit     # Fast feedback
make verify        # Ensure generated code current
```

**Full validation:**
```bash
make test          # All tests (unit, e2e, regression)
make lint         # Code quality
```

**Local deployment:**
```bash
make run           # Build, deploy to kind, includes cleanup
make run-experimental  # Same but with experimental features
```

### Environment variables:

- `OPCON_IMAGE_REPO`: operator-controller image repository
- `CATD_IMAGE_REPO`: catalogd image repository
- `IMAGE_TAG`: Image tag (default: devel)
- `KIND_CLUSTER_NAME`: Kind cluster name
- `INSTALL_DEFAULT_CATALOGS`: Install default catalogs (true/false)

## Reporting Back to User

### When completing search tasks:

Provide:
1. Exact file paths with line numbers
2. Relevant code snippets
3. Context about what the code does
4. Related files user might need to check

### When completing code modification tasks:

Report:
1. Files modified with brief description of changes
2. Any generated files that need regeneration
3. Make commands that need to be run
4. Test results if tests were run
5. Any remaining work or considerations

### When encountering issues:

Report:
1. Exact error messages
2. What was attempted
3. Relevant logs or output
4. Suggestions for resolution or what to investigate

## Quick Reference

**Find definition of**: `grep -r "type {Name} struct" api/`
**Find usages of**: `grep -r "{identifier}" internal/`
**Find tests for**: Look for `*_test.go` in same directory
**Check generated**: `make verify` (fails if drift detected)
**Run specific test**: `go test -v ./path -run TestName`
**Validate changes**: `make fmt && make lint && make test-unit && make verify`
