---
status: idea
---
# orb-operator Wiring

Wire up the orb-operator applier path in `main.go` behind a new feature gate, following the same pattern as the existing Helm/Boxcutter selection. This makes the orb-operator runtime selectable at startup.

## Deliverables

- `OrbOperatorRuntime` feature gate in `internal/operator-controller/features/features.go` (alpha, default off)
- `orbOperatorReconcilerConfigurator` in `main.go` implementing the existing configurator pattern:
  - Constructs `OrbOperator` applier with CODGenerator, preflights, and field owner
  - Sets reconcile steps: HandleFinalizers, ValidateClusterExtension, RetrieveRevisionStates, ResolveBundle, UnpackBundle, ApplyBundleWithOrbOperator
  - Registers finalizers (cleanup on CE deletion)
- Cache options: add COD, COS, COSL to the informer cache when the feature gate is enabled
- Controller builder: `WithOwns` for COD resources, watches for COS changes
- Feature gate selection logic alongside the existing Helm/Boxcutter check
- Integration with existing e2e test hooks for feature-gate-aware test setup

## Dependencies

- orb-operator-reconcile-step (for the step function and revision states getter)
- orb-operator-dependency (for scheme registration and deployment manifests)
