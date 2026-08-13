---
status: in-progress
---
# orb-operator Preflight Check

## Summary

Add a preflight runner for the orb-operator applier path that extracts `[]client.Object` from a `ClusterObjectDeploymentApplyConfiguration`'s phases and runs them through the existing `Preflight` interface. This bridges the COD generator's `runtime.RawExtension`-based output with the CRD upgrade safety checks that Helm and Boxcutter appliers already perform.

## Design

### Function signature

```go
func runPreflights(
    ctx context.Context,
    ext *ocv1.ClusterExtension,
    cod *orbac.ClusterObjectDeploymentApplyConfiguration,
    preflights []Preflight,
) error
```

Lives in `internal/operator-controller/applier/` alongside the existing `Preflight` interface and `shouldSkipPreflight` helper.

### Object extraction

Each phase in the COD contains `PhaseObject` entries with `Object *runtime.RawExtension`. The runner deserializes each into an `*unstructured.Unstructured` via `json.Unmarshal`, collecting all objects across all phases into a flat `[]client.Object` slice. Objects without a `RawExtension` (e.g. ref-only entries, though the COD generator does not produce those) are skipped.

### Preflight execution

The runner always calls `preflight.Upgrade(ctx, objs)` for each preflight. The existing CRD upgrade safety implementation handles both install and upgrade scenarios internally - when no existing CRD is found on the cluster, it returns nil (the "nothing to break" case). There is no need to determine install-vs-upgrade state or call `preflight.Install` separately.

Before calling Upgrade, the runner checks `shouldSkipPreflight(ctx, preflight, ext, "NeedsUpgrade")` and skips the preflight if it returns true. This preserves the existing behavior where CRD upgrade safety enforcement set to `None` bypasses the check.

### Error handling

Preflight errors are returned directly to the caller (the orb-operator Apply method). The applier is responsible for classifying errors as terminal vs retryable and mapping them to ClusterExtension status conditions.

### Why not a shared runner for all appliers

The Helm and Boxcutter appliers have their own state detection logic interleaved with preflight execution (Helm's server-side dry-run, Boxcutter's SSA patch comparison). Extracting a shared runner would require untangling that state detection, which is out of scope and unnecessary - the orb-operator path simplifies by always calling Upgrade.
