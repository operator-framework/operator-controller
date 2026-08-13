# Requirements

- Extract all objects from a COD apply configuration's phases, deserializing `runtime.RawExtension` entries to `*unstructured.Unstructured`
- Run each configured preflight's `Upgrade` method with the extracted objects
- Respect `shouldSkipPreflight` logic (skip CRD upgrade safety when enforcement is `None`)
- Return preflight errors without classifying them (caller's responsibility)
- Skip phase objects that have no `RawExtension` data

## Acceptance Criteria

- `runPreflights` with a COD containing CRDs and a strict CRD upgrade safety preflight calls the preflight's `Upgrade` method with the full object list
- `runPreflights` with enforcement set to `None` skips the CRD upgrade safety preflight
- `runPreflights` with an empty COD (no phases or no objects) returns nil
- `runPreflights` with a COD containing an object with invalid JSON in the `RawExtension` returns an error
- `runPreflights` with multiple preflights runs all non-skipped preflights and returns all errors joined
