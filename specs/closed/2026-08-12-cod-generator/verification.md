# Verification

## Implementation Correctness

- [ ] `CODGenerator` interface defined in `internal/operator-controller/applier/codgen.go`
- [ ] `RegistryV1CODGenerator` uses `ManifestProvider.Get` for manifest rendering (no bundle parsing duplication)
- [ ] Phase sorting reuses `determinePhase` and `defaultPhaseOrder` from `phase.go`
- [ ] Object sanitization uses `sanitizedUnstructured` and `cache.ApplyStripAnnotationsTransform`
- [ ] Per-GVK assertions match the semantics of `defaultProgressionProbes`:
  - CRDs: Established=True
  - Deployments: updatedReplicas==replicas + Available=True
  - Namespaces: status.phase=Active
  - PVCs: status.phase=Bound
  - cert-manager Certificate/Issuer: Ready=True
  - Other objects: no assertions
- [ ] COD name equals `ext.Name`
- [ ] `progressDeadlineMinutes` set on COD spec when `ext.Spec.ProgressDeadlineMinutes > 0`
- [ ] Collision protection set to `Prevent` at template spec level
- [ ] Revision annotations on template metadata include bundle name, version, package, reference, and OLM properties
- [ ] Objects serialized as `runtime.RawExtension` JSON (not YAML)
- [ ] Object labels are NOT set by the generator (applier's responsibility)
- [ ] `OrbOperator` struct has `Generator CODGenerator` field
- [ ] `OrbOperator.Apply` calls `GenerateCOD` and logs the result (does not yet apply to cluster)
- [ ] `orbOperatorReconcilerConfigurator` constructs `RegistryV1CODGenerator` with existing `regv1ManifestProvider`
- [ ] Unit tests pass

## Project Conventions

- [ ] No `//nolint` comments added
- [ ] Code formatted with `make fmt`
- [ ] `make lint` passes
- [ ] `make test-unit` passes
- [ ] No unnecessary abstractions or helper functions beyond what the implementation requires
- [ ] Import aliases match project conventions (`orbac` for orb-operator apply configurations, `orbv1alpha1` for orb-operator API types)
- [ ] Mission principle: "do not fight Kubernetes" - using orb-operator's native apply configuration pattern
- [ ] Mission principle: "simple and predictable" - pure transformation, no side effects
