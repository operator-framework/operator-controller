# Implementation Plan

Prerequisite: the orb-runtime resource-gathering fix for the e2e harness
(`:seedling: Make e2e resource-gathering helpers orb-runtime aware`) so the
experimental e2e suite reaches these scenarios instead of panicking.

## 1. Rename the revision-state-driven apply step (pure refactor)

- Rename `ApplyBundleWithBoxcutter` -> `ApplyBundleWithRevisions` in
  `internal/operator-controller/controllers/boxcutter_reconcile_steps.go`
  (no logic change).
- Update the Boxcutter configurator in `cmd/operator-controller/main.go` to call
  the renamed function; update any references in tests/mocks.
- Confirm no behavior change: `make test-unit` for the controllers package.

## 2. Implement the getter in `internal/operator-controller/controllers/orboperator_reconcile_steps.go`

- Add `Reader client.Reader` to `OrbOperatorRevisionStatesGetter`.
- `GetRevisionStates`:
  - List `orbv1alpha1.ClusterObjectSetList` with
    `client.MatchingFields{"spec.group": ext.Name}`; `Get` the COD named
    `ext.Name` (tolerate NotFound).
  - Sort ascending by `Spec.Revision`; skip `LifecycleStateArchived`.
  - Build `RevisionMetadata` from COS annotations (using the `labels.*` key
    constants); set `Release` only when the key exists.
  - `completedAt != nil` -> `Installed` (last wins); else append to `RollingOut`.
  - Synthesize `RevisionMetadata.Conditions`:
    - Pass through the COS `Available` condition as
      `ocv1.ClusterObjectSetTypeAvailable`.
    - Produce `ocv1.ClusterObjectSetTypeProgressing` in the README priority order
      (completed / deadline / blocked-phase / COD-error / rolling-out), reading
      the COS `status.observedPhases` and the COD `Progressing` condition.
- Wire the reader in `cmd/operator-controller/main.go`:
  `&controllers.OrbOperatorRevisionStatesGetter{Reader: c.mgr.GetClient()}`.

## 3. Wire the orb runtime to the revision-state-driven step

- In the orb configurator in `cmd/operator-controller/main.go`, replace
  `controllers.ApplyBundle(appl)` with
  `controllers.ApplyBundleWithRevisions(appl.Apply)`.
- Leave the orb applier (`applier/orboperator.go`) unchanged; add a brief comment
  noting its `(bool, string)` return is intentionally ignored by this step, while
  a non-nil `error` still drives `Retrying`.

## 4. Unit tests in `orboperator_reconcile_steps_test.go`

- Seed a fake `client.Reader` with `orbv1alpha1.ClusterObjectSet` /
  `ClusterObjectDeployment` objects (register the orb scheme and the `spec.group`
  index on the fake client via `WithIndex` so `MatchingFields` works).
- Cover the getter classification (installed, rolling-out, mixed,
  multiple-completed, archived-skip, release-present/absent, empty, list-error)
  and the full Progressing reason mapping (each row, using crafted COD/COS objects).

## 5. Empirical validation on the live experimental cluster

- Confirm a healthy install goes `Progressing=RollingOut -> Succeeded` with
  `status.install.bundle` and `status.activeRevisions` populated.
- Reproduce the `recover.feature:55` collision and confirm the CE reaches
  `Progressing=True/Retrying`.
- Confirm a stuck revision (no `completedAt`) never shows premature `Succeeded`,
  and that after a completed install the CE `resourceVersion` stops incrementing
  and `BundleDeprecated.lastTransitionTime` is stable (no flap).

## 6. Confirm `recover.feature:55` passes unchanged

- The collision maps to `Progressing=True/Retrying` via the phase-`Invalid` cue,
  matching the existing Helm/Boxcutter assertion, so no test edit is expected.

## 7. Finalize

- `make lint`, `make test-unit`, `make verify`.
