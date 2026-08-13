# Implementation Plan

1. Add `runPreflights` and `extractObjectsFromCOD` in `internal/operator-controller/applier/orboperator.go`
   - `extractObjectsFromCOD(*orbac.ClusterObjectDeploymentApplyConfiguration) ([]client.Object, error)` iterates phases and deserializes each `PhaseObject.Object` RawExtension into `*unstructured.Unstructured`
   - `runPreflights(ctx, ext, cod, preflights) error` calls `extractObjectsFromCOD`, then loops over preflights calling `shouldSkipPreflight` and `preflight.Upgrade`
   - Wire up `runPreflights` in `OrbOperator.Apply` between COD generation and COD application

2. Add unit tests in `internal/operator-controller/applier/orboperator_test.go`
   - Test `extractObjectsFromCOD` with multi-phase COD, empty COD, nil RawExtension entries, invalid JSON
   - Test `runPreflights`: skip logic (real `crdupgradesafety.Preflight`), and error propagation / empty object list via the generated `MockPreflight`
