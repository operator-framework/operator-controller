# Verification

## Implementation Correctness

- [ ] `extractObjectsFromCOD` correctly deserializes RawExtension JSON into Unstructured objects with GVK preserved
- [ ] `runPreflights` calls `shouldSkipPreflight` with state `"NeedsUpgrade"` for each preflight
- [ ] `runPreflights` calls `preflight.Upgrade` (not Install) for non-skipped preflights
- [ ] Phase objects with nil RawExtension are skipped without error
- [ ] Invalid JSON in RawExtension produces a descriptive error

## Project Conventions

- [ ] Function placed in `internal/operator-controller/applier/preflight.go` alongside existing preflight code
- [ ] Tests use testify assertions and table-driven patterns consistent with existing tests
- [ ] No new interfaces introduced - uses existing `Preflight` interface
- [ ] `make test-unit` passes
- [ ] `make lint` passes
- [ ] Commit message uses `:sparkles:` prefix
