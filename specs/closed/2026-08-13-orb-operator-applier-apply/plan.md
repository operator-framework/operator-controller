# Implementation Plan

1. Complete `OrbOperator.Apply` in `internal/operator-controller/applier/orboperator.go`:
   - Fix the TODO: capture `cod` and `slices` from `orb.Externalize(cod)` (COSLs already carry owner labels from the externalizer)
   - Apply each COSL with `o.Client.Apply(ctx, cosl, client.FieldOwner(o.FieldOwner), client.ForceOwnership)`; return `(false, "", err)` on error
   - Apply the COD with the same options; return `(false, "", err)` on error
   - Call `o.garbageCollectOrphanedSlices(ctx, ext, slices)`, log any error but do not return it
   - Return `(true, "", nil)`

2. Add `garbageCollectOrphanedSlices` private method to `OrbOperator`:
   - Build `current` set from slice names in `slices`
   - List `ClusterObjectSliceList` with `client.MatchingLabels{labels.OwnerNameKey: ext.Name}` (label narrows the list only)
   - For each listed COSL not in `current` **and** controlled by this CE (`metav1.IsControlledBy(cosl, ext)`), call `o.Client.Delete`; skip foreign slices; collect errors with `errors.Join`
   - Return joined error (caller logs and discards)

3. Add unit tests in `internal/operator-controller/applier/orboperator_test.go`:
   - Use a fake `client.Client` (or mock) to verify call ordering and arguments
   - Cover all acceptance criteria scenarios
