---
status: in-progress
---
# COD Generator

## Summary

Implement a `CODGenerator` interface and `RegistryV1CODGenerator` that converts a bundle `fs.FS` plus `ClusterExtension` into an inline `ClusterObjectDeploymentApplyConfiguration`. This is the translation layer between OLM's registry+v1 bundle format and orb-operator's phased object model.

The COD generator reuses the existing `ManifestProvider` to render manifests, then organizes them into orb-operator phases with per-object assertions and collision protection. It produces an inline-only COD (no externalization to slices) - the caller (the applier, in a later phase) handles externalization.

## Design

### Interface

```go
// CODGenerator produces a ClusterObjectDeployment apply configuration
// from an unpacked bundle filesystem and a ClusterExtension.
type CODGenerator interface {
    GenerateCOD(
        ctx context.Context,
        bundleFS fs.FS,
        ext *ocv1.ClusterExtension,
        revisionAnnotations map[string]string,
    ) (*orbac.ClusterObjectDeploymentApplyConfiguration, error)
}
```

The interface lives in `internal/operator-controller/applier/` alongside the existing `ManifestProvider` and `ClusterObjectSetGenerator` interfaces.

The return is a `*orbac.ClusterObjectDeploymentApplyConfiguration` (from `github.com/joelanford/orb-operator/applyconfigurations/api/v1alpha1`). The COD is populated with:
- `WithName(ext.Name)` - COD name matches ClusterExtension name (1:1 mapping)
- `WithSpec(codSpec)` where codSpec has:
  - `WithProgressDeadlineMinutes(ext.Spec.ProgressDeadlineMinutes)` if set
  - `WithTemplate(template)` where template has:
    - `WithMetadata(templateMeta)` with revision annotations (bundle name, version, package, reference)
    - `WithSpec(templateSpec)` with collision protection and phased objects

### RegistryV1CODGenerator

```go
type RegistryV1CODGenerator struct {
    ManifestProvider ManifestProvider
}
```

Takes the existing `ManifestProvider` (shared with Boxcutter/Helm paths) to render manifests from the bundle FS, then converts the flat `[]client.Object` into orb-operator phases.

### Phase sorting and assertions

Reuse the existing `phase.go` infrastructure (`determinePhase`, `defaultPhaseOrder`, `gkPhaseMap`) to assign each object to a well-known phase. The phase names and ordering are identical to what Boxcutter uses today.

Objects are serialized to `runtime.RawExtension` (JSON) for the orb-operator `PhaseObject.Object` field. Each object is sanitized (status stripped, metadata limited to name/namespace/labels/annotations) using the existing `sanitizedUnstructured` helper.

Per-object assertions are set based on GVK, matching the existing `defaultProgressionProbes` logic but expressed as orb-operator assertions on individual `PhaseObject` entries rather than top-level `ProgressionProbe` selectors. The assertion mapping:

| GVK | Assertion |
|---|---|
| CRD (`apiextensions.k8s.io/CustomResourceDefinition`) | `ConditionEqual{Type: "Established", Status: "True"}` |
| cert-manager Certificate | `ConditionEqual{Type: "Ready", Status: "True"}` |
| cert-manager Issuer | `ConditionEqual{Type: "Ready", Status: "True"}` |
| Namespace | `FieldValue{FieldPath: "status.phase", Value: "Active"}` |
| Deployment | `FieldsEqual{FieldA: "status.updatedReplicas", FieldB: "status.replicas"}` + `ConditionEqual{Type: "Available", Status: "True"}` |

The mapping only covers kinds a registry+v1 bundle can actually produce (see operator-registry's supported resources).

Objects without a matching assertion rule get no assertions (available immediately after apply).

### Collision protection

Set at the template spec level: `CollisionProtection: "Prevent"` (default for fresh installs). The applier can override per-object if needed in a later phase.

### Revision annotations on template metadata

Bundle metadata is propagated via `template.metadata.annotations`:
- `olm.operatorframework.io/bundle-name`
- `olm.operatorframework.io/bundle-version`
- `olm.operatorframework.io/package-name`
- `olm.operatorframework.io/bundle-reference`
- `olm.properties` (OLM properties from bundle annotations, if present)

These are the same annotations that Boxcutter puts on COS revision annotations. By placing them on the COD template metadata, they propagate to COS revisions created by the orb-operator COD controller.

### Object labels

Object labels (e.g., `olm.operatorframework.io/owner-kind`, `olm.operatorframework.io/owner-name`) are NOT set by the COD generator. They are applied by the caller (the applier) on the COD's top-level metadata and template metadata for cache filtering. This keeps the generator focused on content translation.

### Error handling

The generator returns errors for:
- Bundle parse failures (via `ManifestProvider.Get`)
- Object serialization failures (JSON marshaling)
- Missing bundle annotations (via `getBundleAnnotations`)

No terminal vs retryable distinction needed here - the generator is a pure transformation. Error classification is the applier's responsibility.
