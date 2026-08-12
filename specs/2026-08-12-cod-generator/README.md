---
status: idea
---
# COD Generator

Implement the `CODGenerator` interface and `RegistryV1CODGenerator` that converts a bundle `fs.FS` and ClusterExtension into an inline `ClusterObjectDeploymentApplyConfiguration`. This is the translation layer between OLM's registry+v1 bundle format and orb-operator's phased object model.

## Deliverables

- `CODGenerator` interface with `GenerateCOD(ctx, bundleFS, ext, revisionAnnotations) (*orbac.ClusterObjectDeploymentApplyConfiguration, error)`
- `RegistryV1CODGenerator` implementation that uses the existing `ManifestProvider` to render bundle manifests, then organizes them into orb-operator phases with appropriate assertions and collision protection
- Phasing logic: CRDs/namespaces in early phases, workloads in later phases, with progression assertions (e.g., CRD Established=True before proceeding)
- Unit tests covering phase ordering, assertion generation, and edge cases
- Reference: speed-run branch commits `sn` and `xuk`
