---
status: done
---
# orb-operator Feature Gate and Wiring Stub

## Summary

Add the `OrbOperatorRuntime` feature gate and wire it into `main.go` following the existing Helm/Boxcutter configurator pattern. Create a stub `OrbOperator` applier type with the shared-infrastructure fields it will need (client, scheme, preflights, field owner) but no actual implementation yet. OrbOperator-specific fields (CODGenerator, etc.) will be added in later phases when the applier is implemented.

This establishes the feature gate, cache configuration, controller watches, and reconcile step pipeline so that subsequent phases can focus purely on applier logic without touching wiring.

## Design

### Feature gate

Add `OrbOperatorRuntime` to `internal/operator-controller/features/features.go`:

```go
OrbOperatorRuntime featuregate.Feature = "OrbOperatorRuntime"
```

Alpha, default off, not locked. `OrbOperatorRuntime` and `BoxcutterRuntime` are mutually exclusive - enabling both is a startup error.

### Mutual exclusivity check

In `run()`, after feature gate flags are parsed but before any wiring, validate:

```go
if features.OperatorControllerFeatureGate.Enabled(features.BoxcutterRuntime) &&
    features.OperatorControllerFeatureGate.Enabled(features.OrbOperatorRuntime) {
    return fmt.Errorf("BoxcutterRuntime and OrbOperatorRuntime feature gates are mutually exclusive")
}
```

### OrbOperator applier stub

Create `internal/operator-controller/applier/orboperator.go` with a stub type:

```go
type OrbOperator struct {
    Client     client.Client
    Scheme     *runtime.Scheme
    Preflights []Preflight
    FieldOwner string
}

func (o *OrbOperator) Apply(ctx context.Context, contentFS fs.FS, ext *ocv1.ClusterExtension,
    objectLabels, revisionAnnotations map[string]string) (bool, string, error) {
    return false, "", fmt.Errorf("OrbOperatorRuntime applier not yet implemented")
}
```

This implements the existing `Applier` interface. The shared fields (Client, Scheme, Preflights, FieldOwner) come from the same infrastructure already set up in main.go for Helm/Boxcutter. OrbOperator-specific fields (CODGenerator, etc.) are added in later phases.

### Configurator

Add `orbOperatorReconcilerConfigurator` to `main.go` following the existing pattern:

```go
type orbOperatorReconcilerConfigurator struct {
    mgr                   manager.Manager
    preflights            []applier.Preflight
    regv1ManifestProvider applier.ManifestProvider
    resolver              resolve.Resolver
    imageCache            imageutil.Cache
    imagePuller           imageutil.Puller
    finalizers            crfinalizer.Finalizers
}
```

No `trackingCache` field - the orb-operator COD controller handles drift detection, so the tracking cache is not needed for this path.

The `Configure` method:
1. Registers a no-op finalizer for `ClusterExtensionCleanupContentManagerCacheFinalizer` (same pattern as Boxcutter - orb-operator doesn't use contentmanager either).
2. Constructs the `OrbOperator` applier with the shared fields.
3. Sets reconcile steps using `ApplyBundle(appl)` - the existing step function works since `OrbOperator` implements `Applier`. (A custom step function can replace this in a later phase if needed.)
4. For `RevisionStatesGetter`, uses a stub that returns empty states (no installed/rolling-out state). This is temporary until the orb-operator-specific revision states getter is implemented.

### Cache options

When `OrbOperatorRuntime` is enabled, add to the informer cache with label selectors scoped to resources managed by the CE controller:

- `orbv1alpha1.ClusterObjectDeployment` - label selector: `olm.operatorframework.io/owner-kind=ClusterExtension` (set on COD top-level metadata by the applier)
- `orbv1alpha1.ClusterObjectSet` - label selector: `olm.operatorframework.io/owner-kind=ClusterExtension` (propagated from COD `template.metadata.labels` by orb-operator's `template.BuildCOS`, which clones template labels onto each COS revision)
- `orbv1alpha1.ClusterObjectSlice` - label selector: `olm.operatorframework.io/owner-kind=ClusterExtension` (set directly by the applier when creating slices)

Note: the applier must set the owner-kind label in both the COD's top-level metadata and the COD template's metadata. Top-level labels filter CODs in the cache; template labels propagate to COSs.

### Field indexer

Register a field indexer on `spec.group` for `ClusterObjectSet` in the `Configure` method:

```go
mgr.GetFieldIndexer().IndexField(ctx, &orbv1alpha1.ClusterObjectSet{}, "spec.group",
    func(obj client.Object) []string {
        return []string{obj.(*orbv1alpha1.ClusterObjectSet).Spec.Group}
    })
```

Since `spec.group` equals the COD name (which equals the CE name), this enables efficient lookups: `client.MatchingFields{"spec.group": ext.Name}` instead of listing all COSs and filtering in-process.

### Controller builder options

When `OrbOperatorRuntime` is enabled:
- `WithOwns(&orbv1alpha1.ClusterObjectDeployment{})` - re-reconcile CE when its COD changes
- `WithOwns(&orbv1alpha1.ClusterObjectSlice{})` - re-reconcile CE when its COSLs change

### Feature gate selection

The three-way selection in `main.go`:
```go
if features.OperatorControllerFeatureGate.Enabled(features.OrbOperatorRuntime) {
    cerCfg = &orbOperatorReconcilerConfigurator{...}
} else if features.OperatorControllerFeatureGate.Enabled(features.BoxcutterRuntime) {
    cerCfg = &boxcutterReconcilerConfigurator{...}
} else {
    cerCfg = &helmReconcilerConfigurator{...}
}
```

### Stub RevisionStatesGetter

A minimal implementation in the controllers package:

```go
type OrbOperatorRevisionStatesGetter struct{}

func (o *OrbOperatorRevisionStatesGetter) GetRevisionStates(ctx context.Context, ext *ocv1.ClusterExtension) (*RevisionStates, error) {
    return &RevisionStates{}, nil
}
```

This returns empty revision states. The real implementation will read COD/COS status in a later phase.

### What this does NOT include

- Actual `Apply` implementation (returns error)
- CODGenerator, externalizer, or COSL GC
- Custom reconcile step function (uses existing `ApplyBundle`)
- Real revision states getter (uses stub)
- Storage migration from Helm/Boxcutter
