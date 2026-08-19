---
status: done
---
# orb-operator Applier Apply Step

## Summary

Complete `OrbOperator.Apply` by wiring the externalized COD and COSLs through server-side apply (SSA), and garbage-collecting COSLs orphaned by content changes. This is the final step that makes the orb-operator applier functional on-cluster.

## Design

### What already exists

The `Apply` method stub already:
1. Generates the inline COD via `CODGenerator`
2. Runs preflight checks on the inline objects
3. Externalizes the COD (moves objects to COSLs if needed)

The remaining work picks up after externalization.

### Apply pipeline completion

After `Externalize(cod)` returns the (possibly rewritten) COD and any COSLs. The COSLs already carry the owner labels (`OwnerKindKey`, `OwnerNameKey`) and the COD's controller `ownerReference` (which points at this `ClusterExtension`), both propagated from the COD by the externalizer. The label makes them listable during GC; the controller `ownerReference` is what authorizes deletion (single-owner semantics).

**Step 1 - Apply COSLs**: SSA each COSL with `client.FieldOwner(o.FieldOwner)` and `client.ForceOwnership`. COSLs must be applied before the COD since the COD's objectRefs point to them. Order within the COSL list doesn't matter.

**Step 2 - Apply COD**: SSA the COD with the same field owner options.

**Step 3 - GC orphaned COSLs**: After successful COD apply, delete any COSLs that are owned by this CE but not in the current `slices` set (these are leftovers from a previous reconcile where the content was larger or different). See below.

**Step 4 - Return**: `(true, "", nil)` on success. Any SSA error returns `(false, "", err)`. GC errors are logged but do not fail the reconcile (they will be retried next cycle).

### GarbageCollectOrphanedSlices

Private helper called from `Apply`. Receives the current `slices` list from `Externalize`.

Algorithm:
1. Build a `set[string]` of current slice names from `slices`
2. List `ClusterObjectSlice` resources with label `OwnerNameKey = ext.Name` (the label only narrows the list; it is not proof of ownership)
3. Delete a listed COSL only when it is both (a) not in the current set **and** (b) controlled by this `ClusterExtension` (`metav1.IsControlledBy(cosl, ext)`). A foreign COSL that merely shares the `OwnerNameKey` label but is controlled by something else is never deleted.

When externalization was not needed (`slices` is nil/empty), the current set is empty and all COSLs controlled by this CE are deleted.

### Return semantics

`OrbOperator.Apply` returns `(bool, string, error)` matching the `Applier` interface. The bool indicates whether apply succeeded (not whether Available=True - that is read separately by the reconcile step via `OrbOperatorRevisionStatesGetter`). The string is empty; status messages come from COD conditions read by the reconcile step.

| Case | Return |
|---|---|
| SSA of COSL fails | `false, "", err` |
| SSA of COD fails | `false, "", err` |
| SSA succeeds | `true, "", nil` |
