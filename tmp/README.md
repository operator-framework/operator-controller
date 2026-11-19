# GA API shape — review copies

These are the complete Go API type definitions for ClusterObjectDeployment,
ClusterObjectSet, and ClusterExtension (plus the shared condition constants in
`common_types.go`), copied from the `refactor/clusterobjectset-status-api` branch.
They are copied here so the proposed GA shape can be reviewed in its entirety
rather than as a diff against the current types.

This directory is for review only and will not be merged. The files are otherwise
unmodified; they rely only on the unchanged `groupversion_info.go` boilerplate.
Channel-specific descriptions are marked with `<opcon:standard:...>` and
`<opcon:experimental:...>` directives; CRD schemas, printer columns, and
validation are generated from the kubebuilder markers shown here.

## Differences from the current upstream/main experimental CRDs

Note: "removed" below means the field is not part of the proposed standard GA
API, or is not relevant under the `BoxcutterRuntime` feature gate — not
necessarily that the capability is gone from the product.

### ClusterObjectDeployment (`clusterobjectdeployment_types.go`)

- Removed `spec.progressDeadlineMinutes` and the `ProgressDeadlineExceeded`
  Progressing reason.
- `Available` redefined around per-object probe health instead of revision
  readiness: True when any active, non-deleting, owned revision reports
  `presentObjects == totalObjects` and `healthyObjects == presentObjects`;
  reasons `Available`, `Unavailable`, `Unknown`, `NoActiveRevision`.
- `status.conditions` documentation restructured per Progressing reason, without
  controller internals; `NewClusterObjectSetProgressing` now also covers revision
  creation and adoption.
- `spec.template.metadata` documents its reserved labels
  (`olm.operatorframework.io/template-hash`, `app.kubernetes.io/managed-by`) and
  the label/annotation entry limits and validation.

### ClusterObjectSet (`clusterobjectset_types.go`)

- Object sources: `secretRef` (Secret reference) replaced by `objectSliceRef`
  (name + key into a cluster-scoped `ClusterObjectSlice`, whose CRD is provided
  externally and is not included here); the object `type` enum is now
  `InlineManifest`/`ObjectSliceRef`.
- Probes: revision-level `progressionProbes` (selector +
  `ConditionEqual`/`FieldsEqual`/`FieldValue` assertions) replaced by per-object
  `progressionProbes` supporting `ConditionEqual` and `CEL` rules.
- Status counts: `status.objectCounts.{total,present,synced,ready}` replaced by
  top-level `totalObjects`, `presentObjects`, `syncedObjects`, `healthyObjects`,
  `unhealthyObjects` (new), `supersededObjects`, and `collidingObjects`;
  `unhealthyObjects` distinguishes failed probes from undetermined ones.
- `status.observedPhases` replaced by `status.phases`: pinned `contentDigest`,
  lifecycle `status`, machine-readable `reason`, and sparse per-object
  `diagnostics` categorized by reason.
- `spec.lifecycleState` documentation updated to assign archival responsibility:
  the owning ClusterObjectDeployment decides when to archive older revisions (on
  recorded rollout completion), the ClusterObjectSet controller tears down a
  revision once `lifecycleState` is set to `Archived`, and standalone callers
  archive explicitly.
- New printer columns: `Unhealthy`, `Superseded`, `Colliding`.

### ClusterExtension (`clusterextension_types.go`)

- `spec.namespace` is optional at GA: when omitted, operator-controller
  resolves and creates a managed namespace from the bundle's metadata. Whether
  namespace is set or omitted is fixed at creation time; the value itself is
  immutable and DNS-label validated. (The standard CRD previously required it.)
- Removed `spec.config` (inline bundle configuration),
  `spec.progressDeadlineMinutes`, and `status.install.bundle.release`.
- Conditions documentation: the `Available` condition mirrors the managing
  ClusterObjectDeployment's `Available` condition.

The only other change is the `controller-gen.kubebuilder.io/version` annotation
(v0.21.0 → v0.23.0), from regenerating with the current toolchain.

To reproduce a full diff for a kind:

```console
git diff upstream/main:api/v1/<kind>_types.go tmp/<kind>_types.go
```
