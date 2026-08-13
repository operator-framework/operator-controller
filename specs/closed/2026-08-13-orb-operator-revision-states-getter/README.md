---
status: done
---
# orb-operator RevisionStatesGetter

## Summary

Replace the `OrbOperatorRevisionStatesGetter` stub (which returns an empty
`RevisionStates{}`) with real logic that both (a) reports the installed and
rolling-out revisions and (b) drives the `ClusterExtension`'s `Installed`,
`Progressing`, and `Available` conditions from the true orb rollout state. The
getter queries orb-operator's `ClusterObjectSet` (COS) and
`ClusterObjectDeployment` (COD) APIs and synthesizes OLM-vocabulary conditions,
mirroring how the Boxcutter runtime already works.

This fixes two coupled bugs:

1. **Reconcile loop.** The empty stub makes `ResolveBundle` and `ApplyBundle`
   disagree on the installed bundle name, flapping the `BundleDeprecated`
   condition's `lastTransitionTime` every reconcile and causing an unbounded
   ClusterExtension write / reconcile loop.
2. **Premature success.** Under the `OrbOperatorRuntime` feature gate, a
   `ClusterExtension` reports `Installed=True/Succeeded` and
   `Progressing=True/Succeeded` the moment its COD is created, even when the
   revision is wedged and its objects were never applied - the CE lies about
   being installed. It surfaces in `test/e2e/features/recover.feature:55`
   ("Install ClusterExtension after conflicting resource is removed"), which
   waits for `Progressing=True/Retrying` on a resource collision and instead
   sees `Succeeded`.

All status derivation lives in one testable place, `OrbOperatorRevisionStatesGetter`;
the orb applier stays "dumb" (its `(bool, string)` return is ignored, while a
non-nil `error` still surfaces as `Retrying`).

## Background: how Boxcutter already does this

Status mapping is not done in the applier. For Boxcutter it is a three-layer flow:

1. The in-tree `ClusterObjectSet` controller sets `.status.conditions`
   (`Progressing`, `Available`, `Succeeded`) on each revision.
2. `BoxcutterRevisionStatesGetter.GetRevisionStates` lists the revisions, decides
   Installed vs RollingOut, and copies `rev.Status.Conditions` into
   `RevisionMetadata.Conditions`.
3. The revision-state-driven apply step discards the applier's `(bool, string)`
   return and mirrors each revision's `ocv1.ClusterObjectSetTypeAvailable` /
   `...TypeProgressing` conditions onto the CE, using the latest rolling-out
   revision's `Progressing` as the CE's `Progressing`.

The orb getter mirrors this, with two differences: orb's COS has no
OLM-vocabulary `Progressing`/`Succeeded` conditions, so the getter synthesizes
them from COS `observedPhases` plus the COD `Progressing` condition; and
completion is keyed off COS `completedAt` rather than a `Succeeded` condition.

## Design

### Root cause being fixed (reconcile loop)

Per reconcile, `ResolveBundle` sets `BundleDeprecated` from
`state.revisionStates.Installed` (nil -> `Unknown/Absent`), then `ApplyBundle`
sets it again from the resolved bundle name (-> `False`). The status flap
rewrites `lastTransitionTime` each cycle, so
`DeepEqual(existing.Status, reconciled.Status)` is always false -> a status write
-> a watch event -> another reconcile. Populating `Installed` stably makes both
call sites agree, so the condition stops flapping.

### Getter shape

Give `OrbOperatorRevisionStatesGetter` a `Reader client.Reader` field (like
`BoxcutterRevisionStatesGetter`) and wire it from the manager client in `main.go`:

```go
type OrbOperatorRevisionStatesGetter struct {
	Reader client.Reader
}
```

`main.go` currently constructs `&controllers.OrbOperatorRevisionStatesGetter{}`;
change it to `{Reader: c.mgr.GetClient()}`.

### Querying revisions and the COD

orb-operator's model (verified against `github.com/joelanford/orb-operator@v0.0.3`):
- The applier creates one `ClusterObjectDeployment` (COD) named `ext.Name`. The
  orb COD controller stamps out `ClusterObjectSet` revisions named
  `<cod.Name>-<revision>` with `spec.group == cod.Name` (i.e. `== ext.Name`).
- Each COS inherits the COD template metadata: the template labels (owner labels)
  and annotations (the bundle metadata the applier set). So bundle identity is
  readable directly off each COS's annotations.

List COS via the existing `spec.group` field indexer (registered in `main.go`)
for a cache-served lookup, and `Get` the COD by name for its deployment-level
`Progressing` signal:

```go
list := &orbv1alpha1.ClusterObjectSetList{}
r.Reader.List(ctx, list, client.MatchingFields{"spec.group": ext.Name})

cod := &orbv1alpha1.ClusterObjectDeployment{}
r.Reader.Get(ctx, client.ObjectKey{Name: ext.Name}, cod) // may not exist yet
```

### Building RevisionStates

Mirror `BoxcutterRevisionStatesGetter.GetRevisionStates`:

1. List COS by `spec.group == ext.Name`.
2. Sort ascending by `Spec.Revision`.
3. Skip revisions whose `Spec.LifecycleState == LifecycleStateArchived`.
4. Build a `RevisionMetadata` per live revision from the COS annotations:

   | RevisionMetadata field | Source |
   |---|---|
   | `RevisionName` | COS `metadata.name` |
   | `Package` | `labels.PackageNameKey` annotation |
   | `Image` | `labels.BundleReferenceKey` annotation |
   | `BundleMetadata.Name` | `labels.BundleNameKey` annotation |
   | `BundleMetadata.Version` | `labels.BundleVersionKey` annotation |
   | `Release` | `labels.BundleReleaseKey` annotation, only if the key is present |
   | `Conditions` | **synthesized** OLM-vocabulary `Available` + `Progressing` (see below) |

5. Classify installed vs rolling-out using COS `status.completedAt` (set once when
   all phases first complete, never cleared):
   - `completedAt != nil` -> `Installed` (last one wins in ascending order)
   - otherwise -> append to `RollingOut`

6. Return the `RevisionStates`.

### Synthesizing the CE conditions

`RevisionStates` is the deliberate interface for telling the CE what to display;
it does not need to be a faithful projection of orb's model. The getter populates
`RevisionMetadata.Conditions` with:

**Available**: the COS `Available` condition passes through as
`ocv1.ClusterObjectSetTypeAvailable` (orb and OLM share the
`Available`/`Unavailable` reason strings).

**Progressing**: synthesized as `ocv1.ClusterObjectSetTypeProgressing` from the
active revision's COS `status` (primarily `observedPhases`) plus the COD
`Progressing` condition, in priority order:

| Signal | CE status | CE reason (`ocv1`) |
|---|---|---|
| revision completed (`completedAt != nil`) | `True` | `ReasonSucceeded` |
| COD `Progressing` reason `ProgressDeadlineExceeded` | `False` | `ReasonProgressDeadlineExceeded` (reconcile continues) |
| a phase `Status == Invalid`, OR a phase with `synced < total` and non-empty `objectDetails` | `True` | `ClusterObjectSetReasonRetrying` |
| COD `Progressing` reason in {`ReconcileError`, `InternalError`, `InvalidRevision`, `TeardownError`} | `True` | `ClusterObjectSetReasonRetrying` |
| otherwise in progress (`WaitingForAssertions`, or clean `Reconciling`) | `True` | `ReasonRollingOut` |

Attach the synthesized conditions to the `RevisionMetadata` that drives CE
`Progressing`: the latest rolling-out revision when one exists, otherwise the
installed revision.

Terminology: "terminal" means the reconcile sense (a `reconcile.TerminalError`
that stops requeue/backoff). None of the reasons above are reconcile-terminal;
`ProgressDeadlineExceeded` sets `Progressing=False` but the controller keeps
retrying.

The discriminator between `RollingOut` and `Retrying` is structured, not
free-text: phase `Status == Invalid` and the `synced < total` count are the cues;
`objectDetails` presence confirms the not-synced object is genuinely blocked (as
opposed to `synced == total` with `available < total`, which is a probe/assertion
still pending -> `RollingOut`). The `recover.feature:55` collision (immutable
`Deployment.spec.selector`) is caught by orb's per-object preflight dry-run, which
reports the `deploy` phase as `Status == Invalid` with `objectCounts {total:1,
synced:0}` and an `objectDetails` entry naming the immutable-selector error. That
`Invalid` is retryable (it clears when the conflicting object is removed), so it
maps to `Retrying`, letting `recover.feature:55` pass unchanged.

### Rename the revision-state-driven apply step (runtime-neutral)

Rename `ApplyBundleWithBoxcutter` to a runtime-neutral name
(`ApplyBundleWithRevisions`) and reuse it for both Boxcutter and orb. It has no
Boxcutter-specific coupling; it is generic over `RevisionStates` and only looks
for conditions of type `ocv1.ClusterObjectSetTypeAvailable` / `...TypeProgressing`.
Update the Boxcutter and orb configurators in `cmd/operator-controller/main.go` to
use it; the Helm configurator keeps the generic bool-driven `ApplyBundle`.

### Type note

orb's COS/COD (`orbv1alpha1`) are distinct types from Boxcutter's `ocv1`
equivalents; the logic is parallel but operates on the orb API types, reads
`CompletedAt` instead of a `Succeeded` condition, and synthesizes `Progressing`
from `observedPhases`.

### Out of scope

- Controller watches/predicates for COD/COS - existing wiring is unchanged.
