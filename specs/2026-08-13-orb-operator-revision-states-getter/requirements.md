# Requirements

## Getter / RevisionStates

- `OrbOperatorRevisionStatesGetter` has a `Reader client.Reader` field, wired from
  the manager client in `main.go`
- `GetRevisionStates` lists `orbv1alpha1.ClusterObjectSet` via
  `client.MatchingFields{"spec.group": ext.Name}` (using the existing `spec.group`
  indexer) and `Get`s the `ClusterObjectDeployment` named `ext.Name` (tolerating
  NotFound on the first reconcile)
- Revisions are sorted ascending by `Spec.Revision`
- Revisions with `Spec.LifecycleState == Archived` are skipped
- Each live revision yields a `RevisionMetadata` populated from the COS annotations
  (bundle name, version, package, image); `Release` is set only when the
  `BundleReleaseKey` annotation is present
- A revision with `status.completedAt != nil` is recorded as `Installed` (last wins
  in ascending order); others are appended to `RollingOut`
- A List/Get error is returned wrapped; no partial results on error
- When no revisions exist, an empty (non-nil) `RevisionStates` is returned with no
  error

## Status mapping

- Under `OrbOperatorRuntime`, a `ClusterExtension` MUST NOT report
  `Installed=True/Succeeded` or `Progressing=True/Succeeded` while its active orb
  revision has not completed (`completedAt` unset). Completion is keyed solely off
  COS `status.completedAt` (current per-phase `Available` is orthogonal).
- `RevisionMetadata.Conditions` carries synthesized
  `ocv1.ClusterObjectSetTypeAvailable` (passed through from the COS `Available`
  condition) and `ocv1.ClusterObjectSetTypeProgressing` conditions, attached to the
  CE-Progressing-driving revision (latest rolling-out, else installed)
- Progressing classification, in priority order:
  - completed -> `True/Succeeded`
  - COD `ProgressDeadlineExceeded` -> `False/ProgressDeadlineExceeded` (status only;
    the reconciler keeps retrying, NOT a `reconcile.TerminalError`)
  - phase `Status == Invalid`, OR a phase with `synced < total` and non-empty
    `objectDetails` -> `True/Retrying`
  - COD reason in {`ReconcileError`, `InternalError`, `InvalidRevision`,
    `TeardownError`} -> `True/Retrying`
  - otherwise (`WaitingForAssertions` / clean `Reconciling`) -> `True/RollingOut`
- The CE `Available` condition MUST reflect the orb COS `Available` condition
  (`Available` / `Unavailable`)
- The orb applier's `Apply` return `bool`/`string` MUST NOT be relied on for CE
  status; a non-nil `error` MUST still surface as `Progressing=Retrying` via the
  apply step's existing error handling

## Apply step

- `ApplyBundleWithBoxcutter` is renamed to a runtime-neutral
  `ApplyBundleWithRevisions`; the Boxcutter and orb configurators in
  `cmd/operator-controller/main.go` both use it; the Helm configurator keeps the
  bool-driven `ApplyBundle`
- The Boxcutter runtime's status behavior is unchanged (the rename is a pure
  refactor for it)

## Constraints

- All status mapping logic lives in `OrbOperatorRevisionStatesGetter` (single,
  unit-testable location); the applier stays free of CE-status concerns
- No new API types or CRD changes; consumes existing `ocv1` condition vocabulary
  and `github.com/joelanford/orb-operator/api/v1alpha1` constants
- No `//nolint` suppressions; fix underlying issues
- Follows `specs/mission.md` principle 1 (work with Kubernetes condition patterns)
  and principle 3 (simple, predictable, eventually-consistent status)

## Acceptance Criteria

- Unit test: single completed revision -> `Installed` set with correct bundle
  metadata, `Progressing=True/Succeeded`, `RollingOut` empty
- Unit test: single revision with nil `completedAt` -> `RollingOut` has it,
  `Installed` nil
- Unit test: mixed revisions (older completed, newer not) -> `Installed` is the
  completed one, newer is in `RollingOut`
- Unit test: two completed revisions -> the higher-revision one wins as `Installed`
- Unit test: archived revisions are skipped
- Unit test: `Release` populated only when the release annotation key is present
- Unit test: no revisions -> empty `RevisionStates`, no error
- Unit test: List error is propagated
- Unit test: Progressing classification covers every row: phase `Invalid` ->
  Retrying; `synced < total` + `objectDetails` -> Retrying; `WaitingForAssertions`
  / clean progress -> RollingOut; COD `ProgressDeadlineExceeded` ->
  False/ProgressDeadlineExceeded; COD error reasons -> Retrying; completed ->
  Succeeded; `Available` passthrough
- e2e: `test/e2e/features/recover.feature:55` passes under
  `make test-experimental-e2e` **unchanged** (collision -> `Progressing=True/Retrying`
  via the phase-`Invalid` cue, matching the Helm/Boxcutter assertion)
- e2e: `install.feature` and `update.feature` happy-path scenarios still pass
- Live/e2e: after install completes, repeated reconciles do not rewrite the CE
  status (no `BundleDeprecated` flap), and a stuck revision (COS
  `completedAt == nil`) never shows premature `Succeeded`
