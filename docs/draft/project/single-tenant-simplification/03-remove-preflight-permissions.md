# Work Item: Remove PreflightPermissions feature gate and code

**Parent:** [Single-Tenant Simplification](../single-tenant-simplification.md)
**Status:** Not started
**Depends on:** Nothing (independent)

## Summary

Remove the `PreflightPermissions` feature gate (currently Alpha, default off) and all associated RBAC pre-authorization code. This also enables removing the `k8s.io/kubernetes` dependency from `go.mod`, along with 30 `replace` directives that exist solely to align `k8s.io/kubernetes` sub-module versions.

## Scope

### Remove feature gate and code

- Remove the `PreflightPermissions` feature gate definition from `internal/operator-controller/features/features.go:26-30`.
- Remove the `internal/operator-controller/authorization/` package entirely. This package contains the `PreAuthorizer` interface and the RBAC-based implementation that validates user permissions before applying manifests.
- Remove the `PreAuthorizer` field from the Boxcutter applier (`internal/operator-controller/applier/boxcutter.go:421`).
- Remove the conditional `PreAuthorizer` setup in `cmd/operator-controller/main.go:722-725`.
- Remove related tests (`internal/operator-controller/authorization/rbac_test.go`).

### Remove `k8s.io/kubernetes` dependency

The `authorization/rbac.go` imports four packages from `k8s.io/kubernetes` (lines 28-32):

- `k8s.io/kubernetes/pkg/apis/rbac`
- `k8s.io/kubernetes/pkg/apis/rbac/v1`
- `k8s.io/kubernetes/pkg/registry/rbac`
- `k8s.io/kubernetes/pkg/registry/rbac/validation`
- `k8s.io/kubernetes/plugin/pkg/auth/authorizer/rbac`

These are the **only** imports of `k8s.io/kubernetes` in the entire codebase. Removing the authorization package enables:

- Removing `k8s.io/kubernetes v1.35.0` from `go.mod` (line 46).
- Removing all 30 `replace` directives (lines 260-319) that exist to align `k8s.io/kubernetes` sub-module versions (`k8s.io/api`, `k8s.io/apiextensions-apiserver`, `k8s.io/apimachinery`, `k8s.io/apiserver`, `k8s.io/cli-runtime`, `k8s.io/client-go`, `k8s.io/cloud-provider`, `k8s.io/cluster-bootstrap`, etc.).
- Running `go mod tidy` to clean up any transitive dependencies that were only pulled in by `k8s.io/kubernetes`.

### Remove k8s-pin tooling

The `hack/tools/k8smaintainer/` program (invoked via `make k8s-pin`) exists to generate and maintain the `k8s.io/*` `replace` directives in `go.mod`. It reads the `k8s.io/kubernetes` version, enumerates all `k8s.io/*` staging modules in the dependency graph, and pins them to matching versions. The `make verify` target runs `k8s-pin` as a prerequisite.

With `k8s.io/kubernetes` removed from `go.mod`, this tool has nothing to do — the remaining `k8s.io/*` dependencies (`k8s.io/api`, `k8s.io/client-go`, etc.) are used directly and managed normally by `go mod tidy` without `replace` directives.

- Remove `hack/tools/k8smaintainer/` (including `main.go` and `README.md`).
- Remove the `k8s-pin` Makefile target and its invocation in the `verify` target (replace with just `tidy`).

### Remove RBAC list/watch permissions

With `PreflightPermissions` removed, operator-controller no longer needs the `clusterrolebindings`, `clusterroles`, `rolebindings`, `roles` list/watch permissions in the Helm ClusterRole. These can be removed as part of [01-cluster-admin](01-cluster-admin.md) or this work item.

## Key files

- `internal/operator-controller/authorization/rbac.go` — Only consumer of `k8s.io/kubernetes`
- `internal/operator-controller/authorization/rbac_test.go` — Tests
- `internal/operator-controller/features/features.go` — Feature gate definition
- `internal/operator-controller/applier/boxcutter.go` — `PreAuthorizer` field
- `cmd/operator-controller/main.go` — Conditional setup
- `go.mod` — `k8s.io/kubernetes` dependency and replace directives
- `hack/tools/k8smaintainer/` — `k8s-pin` tool that generates replace directives
- `Makefile` — `k8s-pin` target (line 157) and its use in `verify` (line 205)
