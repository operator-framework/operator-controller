# Implementation Plan

1. Add feature gate
   - Add `OrbOperatorRuntime` constant and feature spec in `internal/operator-controller/features/features.go`
   - Add mutual exclusivity check in `run()` in `main.go`, after feature gate flags are parsed

2. Create stub applier
   - Create `internal/operator-controller/applier/orboperator.go` with the `OrbOperator` struct and stub `Apply` method
   - Fields: `Client`, `Scheme`, `Preflights`, `FieldOwner`
   - `Apply` returns `false, "", fmt.Errorf("OrbOperatorRuntime applier not yet implemented")`

3. Create stub revision states getter
   - Add `OrbOperatorRevisionStatesGetter` in the controllers package (e.g., in a new file `orboperator_reconcile_steps.go` or alongside existing step code)
   - Returns `&RevisionStates{}, nil`

4. Add configurator and wiring in main.go
   - Add `orbOperatorReconcilerConfigurator` struct (without `trackingCache` field)
   - Implement `Configure` method:
     - Register no-op finalizer for `ClusterExtensionCleanupContentManagerCacheFinalizer`
     - Construct `applier.OrbOperator` with shared fields
     - Set reconcile steps: HandleFinalizers, ValidateClusterExtension, RetrieveRevisionStates, ResolveBundle, UnpackBundle, ApplyBundle
   - Update cache options: add COD, COS, COSL to informer cache when `OrbOperatorRuntime` is enabled
     - Register field indexer on `spec.group` for COS in the `Configure` method
   - Update controller builder options: `WithOwns(&orbv1alpha1.ClusterObjectDeployment{})` when `OrbOperatorRuntime` is enabled
   - Update feature gate selection: three-way if/else (OrbOperator, Boxcutter, Helm)

5. Verify
   - `make build`
   - `make test-unit`
