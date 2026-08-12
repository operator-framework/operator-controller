# Requirements

- Add `OrbOperatorRuntime` feature gate (alpha, default off)
- `OrbOperatorRuntime` and `BoxcutterRuntime` are mutually exclusive; enabling both must fail at startup
- Add `orbOperatorReconcilerConfigurator` following the existing configurator pattern
- Create stub `OrbOperator` applier type implementing the `Applier` interface
- Create stub `OrbOperatorRevisionStatesGetter` implementing `RevisionStatesGetter`
- Configure informer cache for COD, COS, and COSL types when `OrbOperatorRuntime` is enabled
- Add controller builder option to own COD resources
- The stub applier's `Apply` method must return an error indicating it is not yet implemented
- Helm and Boxcutter paths must be completely unaffected when `OrbOperatorRuntime` is not enabled

## Acceptance Criteria

- `make build` succeeds
- `make test-unit` passes
- Enabling `--feature-gates=OrbOperatorRuntime=true` starts the controller without panic (reconciliation returns the "not implemented" error on each attempt)
- Enabling both `--feature-gates=BoxcutterRuntime=true,OrbOperatorRuntime=true` fails at startup with a clear error
- The default path (both gates off) still uses Helm, unchanged
