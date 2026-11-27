# GA API shape — review copies

These are the complete Go API type definitions for ClusterObjectDeployment,
ClusterObjectSet, ClusterExtension, and ClusterObjectSlice (with their kind-scoped condition
constants), copied from the `refactor/clusterobjectset-status-api` branch.
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

- `spec.progressDeadlineMinutes` and the `ProgressDeadlineExceeded` Progressing
  reason are retained as experimental (omitted from standard-channel CRDs).
- `Available` renamed to `Ready` and redefined around the active revision's own
  Ready condition instead of per-object probe health: True when the single
  active revision is Ready (`RevisionReady`), False when it is not
  (`RevisionNotReady`) or when no active revision is controlled
  (`NoActiveRevision`), Unknown while more than one active revision exists
  (`MultipleActiveRevisions`) or when its readiness is undetermined
  (`Unknown`).
- `status.conditions` documentation restructured per Progressing reason, without
  controller internals; `NewClusterObjectSetProgressing` now also covers revision
  creation and adoption.
- `spec.template.metadata` documents its reserved labels
  (`olm.operatorframework.io/template-hash`, `app.kubernetes.io/managed-by`) and
  the label/annotation entry limits and validation.

### ClusterObjectSet (`clusterobjectset_types.go`)

- Object sources: `secretRef` (Secret reference) replaced by `objectSliceRef`
  (slice name plus entry identity — apiVersion, kind, name, namespace — into a
  cluster-scoped `ClusterObjectSlice`); the object `type` enum is now
  `InlineManifest`/`ObjectSliceRef`.
- `ClusterObjectSlice` (started from #3195, reworked here): the spec is a
  discriminated union with a single content type, `inlineManifests`, whose
  `manifests` list stores embedded Kubernetes manifests inline, each carrying
  its own target identity (replacing the encoded-envelope entry design).
- Probes: revision-level `progressionProbes` (selector +
  `ConditionEqual`/`FieldsEqual`/`FieldValue` assertions) replaced by per-object
  `progressionProbes` supporting `ConditionEqual` and `CEL` rules.
- Status counts: remain grouped under `status.objectCounts`, now with
  `{total,present,synced,superseded,colliding}`; `ready` and the health counts
  (`healthyObjects`, `unhealthyObjects`) are removed.
- `status.observedPhases` replaced by `status.phases`: pinned `contentDigest`,
  lifecycle `status`, machine-readable `reason`, and sparse per-object
  `diagnostics` categorized by reason; object entries are identified by
  apiVersion, kind, name, and namespace, matching the spec's object
  references. The phase `status`, `reason`, `message`,
  and `lastTransitionTime` fields are experimental (standard-channel CRDs omit
  them); `completedAt`, `contentDigest`, and `objectDetails` are standard.
- `spec.lifecycleState` documentation updated to assign archival responsibility:
  the owning ClusterObjectDeployment decides when to archive older revisions (on
  recorded rollout completion), the ClusterObjectSet controller tears down a
  revision once `lifecycleState` is set to `Archived`, and standalone callers
  archive explicitly.
- New printer columns: `Superseded` and `Colliding` (counts now served from
  `status.objectCounts`).

### ClusterExtension (`clusterextension_types.go`)

- `spec.namespace` is optional at GA: when omitted, operator-controller
  resolves and creates a managed namespace from the bundle's metadata. Whether
  namespace is set or omitted is fixed at creation time; the value itself is
  immutable and DNS-label validated. (The standard CRD previously required it.)
- `spec.config` (inline bundle configuration) is part of the standard API.
- Retained as experimental (omitted from standard-channel CRDs):
  `spec.progressDeadlineMinutes` and `status.install.bundle.release`.
- Conditions documentation: the `Ready` condition (replacing `Available`)
  mirrors the managing ClusterObjectDeployment's `Ready` condition.
- Condition constants are kind-scoped (`ClusterExtensionType*`,
  `ClusterExtensionReason*`) and live in `clusterextension_types.go`;
  `common_types.go` is removed (see #3205).

The only other change is the `controller-gen.kubebuilder.io/version` annotation
(v0.21.0 → v0.23.0), from regenerating with the current toolchain.

To reproduce a full diff for a kind:

```console
git diff upstream/main:api/v1/<kind>_types.go tmp/<kind>_types.go
```
