# Verification

## Implementation Correctness

- [ ] `OrbOperatorRuntime` feature gate exists in `features.go` (alpha, default off, not locked)
- [ ] Mutual exclusivity check prevents enabling both `BoxcutterRuntime` and `OrbOperatorRuntime`
- [ ] `OrbOperator` struct in `applier/orboperator.go` has fields: `Client`, `Scheme`, `Preflights`, `FieldOwner`
- [ ] `OrbOperator.Apply` returns an error (not a panic)
- [ ] `OrbOperatorRevisionStatesGetter` returns empty `RevisionStates`
- [ ] `orbOperatorReconcilerConfigurator` struct does not include `trackingCache`
- [ ] `Configure` method registers a no-op finalizer for `ClusterExtensionCleanupContentManagerCacheFinalizer`
- [ ] Reconcile steps use `ApplyBundle(appl)` (the existing Applier-interface step)
- [ ] Cache options include COD, COS, COSL when `OrbOperatorRuntime` is enabled, each with a label selector filtering on `olm.operatorframework.io/owner-kind=ClusterExtension`
- [ ] Field indexer on `spec.group` is registered for COS in the `Configure` method
- [ ] Controller builder uses `WithOwns(&orbv1alpha1.ClusterObjectDeployment{})` and `WithOwns(&orbv1alpha1.ClusterObjectSlice{})` when `OrbOperatorRuntime` is enabled
- [ ] Three-way feature gate selection: OrbOperator -> Boxcutter -> Helm

## Build Verification

- [ ] `make build` succeeds
- [ ] `make test-unit` passes

## Project Conventions

- [ ] Commit message uses `:seedling:` prefix (infrastructure/wiring change)
- [ ] No unnecessary code changes beyond what is specified
- [ ] Feature gate follows existing patterns (same struct, same registration)
- [ ] Configurator follows existing patterns (same fields, same Configure signature)
