# Verification

## Implementation Correctness

- [ ] `Apply` captures the COD returned by `Externalize` (not the pre-externalization value)
- [ ] Each COSL is SSA'd before the COD is SSA'd
- [ ] COSL SSA error short-circuits: COD is not applied, returns `(false, "", err)`
- [ ] COD SSA error returns `(false, "", err)`
- [ ] Successful SSA returns `(true, "", nil)`
- [ ] `garbageCollectOrphanedSlices` lists COSLs by owner label but deletes only those not in the current set **and** controlled by this CE (`metav1.IsControlledBy`)
- [ ] GC preserves a foreign COSL that shares the `OwnerNameKey` label but is controlled by a different owner
- [ ] GC with no current slices deletes all COSLs controlled by this CE
- [ ] GC error is logged, not returned - apply still returns `(true, "", nil)`
- [ ] All unit tests pass

## Project Conventions

- [ ] Code follows Go style and passes `make lint`
- [ ] No `//nolint` comments added
- [ ] GC lists COSLs using the `labels.OwnerNameKey` constant (not a string)
- [ ] Uses `client.ForceOwnership` for all SSA calls
- [ ] Follows design principles from specs/mission.md (simple, predictable)
- [ ] Unit tests cover both happy path and error cases
