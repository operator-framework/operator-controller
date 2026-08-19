# Implementation Plan

1. **Define CODGenerator interface and RegistryV1CODGenerator struct**
   - Add `CODGenerator` interface to `internal/operator-controller/applier/codgen.go`
   - Add `RegistryV1CODGenerator` struct with `ManifestProvider` field
   - Import `orbac "github.com/joelanford/orb-operator/applyconfigurations/api/v1alpha1"`

2. **Implement object-to-PhaseObject conversion**
   - Add a helper that converts a `client.Object` into an `orbac.PhaseObjectApplyConfiguration`:
     - Convert to unstructured, sanitize with `sanitizedUnstructured`, strip large annotations with `cache.ApplyStripAnnotationsTransform`
     - Marshal to JSON as `runtime.RawExtension`
     - Look up GVK-based assertions using a mapping table (same logic as `defaultProgressionProbes` but producing `orbac.AssertionApplyConfiguration` values)
   - Add the GVK-to-assertions mapping as a package-level variable, built from the same GVKs as `defaultProgressionProbes`

3. **Implement GenerateCOD method**
   - Call `ManifestProvider.Get(bundleFS, ext)` to get `[]client.Object`
   - Get bundle annotations via `getBundleAnnotations(bundleFS)` and extract `olm.properties` if present
   - Merge caller-provided `revisionAnnotations` (bundle name, version, package, reference from resolver) with bundle-derived `olm.properties` annotation
   - For each object: determine phase via `determinePhase(gvk.GroupKind())`, convert to `PhaseObject`
   - Group by phase, sort phases by `defaultPhaseOrder`, sort objects within each phase deterministically
   - Build `ClusterObjectDeploymentApplyConfiguration`:
     - `WithName(ext.Name)`
     - Spec with `WithProgressDeadlineMinutes` (if set) and template
     - Template with metadata annotations (revision annotations) and spec (collision protection + phases)
   - Return the COD

4. **Write unit tests**
   - Test with a mock `ManifestProvider` that returns controlled sets of objects
   - Verify phase ordering matches `defaultPhaseOrder`
   - Verify assertions per GVK (CRD, Deployment, Namespace, cert-manager types, plain objects)
   - Verify deterministic object sorting within phases
   - Verify revision annotation propagation
   - Verify `progressDeadlineMinutes` passthrough
   - Verify error propagation from `ManifestProvider.Get` and `getBundleAnnotations`

5. **Wire CODGenerator into OrbOperator applier**
   - Add `Generator CODGenerator` field to the `OrbOperator` struct in `applier/orboperator.go`
   - Update `Apply` to call `o.Generator.GenerateCOD(ctx, contentFS, ext, revisionAnnotations)` and log the result
   - The Apply method generates the COD but does not yet apply it to the cluster (externalization, SSA, status reading are the applier spec's job) - return `(false, "", nil)` after successful generation
   - Construct `RegistryV1CODGenerator` in the `orbOperatorReconcilerConfigurator.Configure` method in `main.go`, passing the existing `regv1ManifestProvider`
   - Pass it to `OrbOperator{Generator: codGen, ...}`
