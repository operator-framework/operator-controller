---
status: in-progress
---
# orb-operator Dependency

## Summary

Add the `github.com/joelanford/orb-operator` Go module dependency, register its API types (`orbv1alpha1`) with the controller-runtime scheme, and add orb-operator installation to the experimental deploy/install scripts. This is the foundational prerequisite for all other orb-operator integration work.

## Design

### Go module dependency

Add `github.com/joelanford/orb-operator` as a direct dependency in `go.mod`. The version should be the latest release (currently v0.0.2). This brings in:
- `api/v1alpha1` - ClusterObjectDeployment, ClusterObjectSet, ClusterObjectSlice types
- `applyconfigurations/api/v1alpha1` - SSA apply configuration builders

### Scheme registration

Register `orbv1alpha1` in `internal/operator-controller/scheme/scheme.go` alongside the existing OLM and Kubernetes types. This ensures the Go import keeps the dependency in `go.mod` and makes orb-operator types available to all controller-runtime clients.

### Experimental deployment

The install script (`scripts/install.tpl.sh`) needs an orb-operator install step, gated on whether `ORB_OPERATOR_VERSION` is set. Only the experimental variant passes this variable; the standard variant leaves it empty, so the install block is skipped.

The Makefile needs:
- `ORB_OPERATOR_VERSION` variable (derived from `go list -m`)
- The experimental `install-sh` call passes `ORB_OPERATOR_VERSION` as an extra envsubst variable
- Test/deploy targets that use the experimental manifest export `ORB_OPERATOR_VERSION`

orb-operator publishes an `install.json` at each release, so the install step is a single `kubectl apply -f` of the release URL, followed by a deployment wait.

### What this does NOT include

- No feature gate, controller wiring, or applier code - those belong in later specs
- No changes to the standard deployment variant - orb-operator is only installed in experimental mode
