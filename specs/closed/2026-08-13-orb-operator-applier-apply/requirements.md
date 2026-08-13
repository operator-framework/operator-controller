# Requirements

- `Apply` uses the COD returned by `Externalize` (not the pre-externalization value)
- COSLs are applied via SSA before the COD
- The COD is applied via SSA with `FieldOwner` and `ForceOwnership`
- `Apply` returns `(true, "", nil)` when all SSA calls succeed
- `Apply` returns `(false, "", err)` on any SSA failure
- Orphaned COSLs are deleted after successful SSA, where "owned" means controlled by this CE via its `ownerReference` (`metav1.IsControlledBy`), not merely carrying the `OwnerNameKey` label; a foreign COSL sharing the label is preserved
- GC errors are non-fatal: logged and not returned
- When no externalization occurred, all previously owned COSLs are GC'd

## Acceptance Criteria

- Unit test: COSLs are applied before the COD (verify call ordering)
- Unit test: SSA error on a COSL returns `(false, "", err)` and does not apply the COD
- Unit test: SSA error on the COD returns `(false, "", err)`
- Unit test: successful apply returns `(true, "", nil)`
- Unit test: GC deletes owned COSLs not in the current slice set
- Unit test: GC with empty slice set deletes all owned COSLs
- Unit test: GC preserves a foreign COSL that shares the `OwnerNameKey` label but is controlled by a different owner
- Unit test: GC error does not fail the apply (returns `(true, "", nil)` still)
