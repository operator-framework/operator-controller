# Requirements

- Define a `CODGenerator` interface in `internal/operator-controller/applier/`
- Implement `RegistryV1CODGenerator` that converts a registry+v1 bundle FS and ClusterExtension into a `ClusterObjectDeploymentApplyConfiguration`
- Reuse `ManifestProvider` for manifest rendering (no duplication of bundle parsing, config validation, or rendering logic)
- Reuse the existing phase sorting infrastructure (`determinePhase`, `defaultPhaseOrder`, `gkPhaseMap` from `phase.go`)
- Map per-GVK assertions onto individual `PhaseObject` entries matching the existing `defaultProgressionProbes` semantics
- Serialize objects as `runtime.RawExtension` JSON with sanitized metadata (existing `sanitizedUnstructured` helper)
- Propagate bundle metadata (name, version, package, reference, OLM properties) as COD template metadata annotations
- Set collision protection at the template spec level (`Prevent`)
- COD name equals `ext.Name`
- Support `progressDeadlineMinutes` from ClusterExtension spec
- Wire `CODGenerator` into `OrbOperator` struct and have `Apply` call `GenerateCOD`
- Construct `RegistryV1CODGenerator` in the `orbOperatorReconcilerConfigurator` in `main.go`

## Acceptance Criteria

- `RegistryV1CODGenerator.GenerateCOD` produces a COD with phases matching the same ordering as `PhaseSort`
- Objects within each phase are sorted deterministically (by GVK, namespace, name) matching `compareClusterObjectSetObjectApplyConfigurations` ordering
- CRDs get `ConditionEqual{Established, True}` assertions; Deployments get `FieldsEqual{updatedReplicas, replicas}` + `ConditionEqual{Available, True}` assertions; Namespaces get `FieldValue{status.phase, Active}`. Only kinds a registry+v1 bundle can produce are assigned assertions.
- Objects without assertion rules have empty assertions (no assertions = immediately available)
- Bundle revision annotations appear on template metadata
- `progressDeadlineMinutes` from ClusterExtension is set on the COD spec when non-zero
- `OrbOperator.Apply` calls `GenerateCOD` with the bundle FS and revision annotations
- `orbOperatorReconcilerConfigurator` constructs `RegistryV1CODGenerator` with the shared `ManifestProvider`
- Unit tests cover: phase ordering, assertion generation per GVK, deterministic object sorting, revision annotation propagation, error cases (bad bundle, serialization failure)
