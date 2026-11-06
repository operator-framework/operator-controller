# ClusterObjectSet `Ready` condition — completion (stacked PRs)

**Date:** 2026-10-12
**Status:** Design (approved decisions, pending spec review)
**Stack base:** `main` (PR #2985 merged as `f47aaefa`)
**Builds on:** `docs/superpowers/specs/2026-09-30-cos-ready-condition-blueprint.md`

## Purpose

Finish the `ClusterObjectSet` (COS) `Ready` condition vocabulary started in the
2026-09-30 blueprint and the #2985 `ValidationFailure` consolidation. Four
independently-reviewable, stacked PRs refine the remaining readiness reasons so
the condition reads correctly standalone and when reconstructed onto the
`ClusterExtension` (CE).

This is experimental-API-only. Reasons are Go string constants on the status
condition (not CRD-enum validated), so no CRD schema/compat gate is involved;
each PR still regenerates manifests, apply configurations, and reference docs for
doc-comment/printcolumn churn.

## End-state vocabulary (after all four PRs)

COS `Ready`:

| Status | Reason | Trigger |
| --- | --- | --- |
| True | `AllObjectsReady` | `rres.IsComplete()` — all phases done, every object passes its progress probe |
| False | `Superseded` | ≥1 object adopted by a newer sibling revision (`machinery.ActionProgressed`) |
| False | `ObjectsNotReady` | not complete, deadline not exceeded: ≥1 object still applying or probe pending/failing (absorbs the former `RollingOut` + `ProbeFailure`) |
| False | `ObjectCollision` | ≥1 object reports `machinery.ActionCollision` (foreign owner) |
| False | `ValidationFailure` | classified preflight/phase validation failure (from #2985; unchanged here) |
| False | `Archiving` | `spec.lifecycleState=Archived` and teardown in progress |
| False | `Archived` | archived and teardown complete (terminal) |
| False | `ProgressDeadlineExceeded` | deadline exceeded; wins over the readiness reasons |
| *(unchanged)* | *(prior reason)* | transient internal error — condition left untouched, error returned for backoff |

**Precedence (Ready=False, when multiple hold):** `ValidationFailure` is reported
first and is **not** overridden by the progress deadline (it wins because it is the
most actionable). The remaining not-ready reasons — `ObjectCollision`, `Superseded`,
`ObjectsNotReady` — are relabeled to `ProgressDeadlineExceeded` once the deadline is
exceeded; below the deadline they rank `ObjectCollision` → `Superseded` →
`ObjectsNotReady`. So the progress deadline **intentionally overrides**
`ObjectCollision` (unlike `ValidationFailure`): past the deadline a collision reads
as `ProgressDeadlineExceeded`. This asymmetry is a deliberate, documented choice,
not a bug; see the living reference doc. `Archiving`/`Archived` are on the archival
path and mutually exclusive with the active-rollout reasons.

`RetryableError` is removed entirely.

## Confirmed decisions

1. **Superseded is partial.** Fire as soon as ≥1 object has `ActionProgressed`,
   evaluated before `ObjectsNotReady`. A partial hand-off reads as superseded
   rather than masquerading as `ObjectsNotReady`.
2. **CE goes neutral on transient COS errors.** With `RetryableError` gone, a
   transient COS error leaves `Ready` untouched and returns an error for backoff.
   CE no longer flips `Installed=Failed` or `Progressing=Retrying` for it; CE
   reflects the unchanged COS `Ready` reason (→ `Absent` / `RollingOut`). On a
   first-ever reconcile that hits a transient error before `Ready` is set, `Ready`
   stays absent (consumers read absent as `Unknown`). Accepted.
3. **Stack on `main`.** #2985 has merged (`f47aaefa`); the four PRs stack on
   `main` rather than the old `cos-ready-validation` branch.

## PR decomposition

Each PR is TDD'd and self-contained: controller change + CE-mapping update in
`internal/operator-controller/controllers/common_controller.go` + unit tests
(both controllers) + `make manifests crd-ref-docs`.

**Per-PR consistency pass (definition of done).** After the functional change of
each PR lands and tests pass, before opening the PR run a cleanup pass:

1. **Names:** do any variable / function / method names still reflect a retired
   reason or old behavior (e.g. `setRetryableErrorConditions`, `probeFailureMsgs`)?
   Rename to match the new vocabulary.
2. **Test names/descriptions:** do test function names, `t.Run` case names, and
   assertion messages still describe the old reason/behavior? Update them.
3. **Tabular folding:** can any unit test added in this PR be folded into an
   existing table test (e.g. `RevisionReconciliation`, `ForeignRevisionCollision`)
   rather than standing alone? Fold where it fits the table's shape.
4. **Docs:** update every doc the change touches — `docs/draft/concepts/clusterobjectsets.md`
   (status-condition tables) and the living reference
   `docs/superpowers/specs/clusterobjectset-ready-condition.md` (reasons, precedence,
   CE roll-up). Keep both in step with the code as of that PR.

Primary controller file:
`internal/object-controller/controllers/clusterobjectset_controller.go`.

### PR 1 — `ObjectsNotReady` (remove `ProbeFailure`)

- Collapse the in-transition `RollingOut` branch (`:298-299`) and the
  `ProbeFailure` branch (`:367`) into a single `Ready=False/ObjectsNotReady`.
  Keep the per-object probe detail as the message; fall back to a generic
  "Objects are not yet ready." before any probe reports. The deadline branch
  (`:361-365`) still wins and still summarizes via "Last status".
- Add `ClusterObjectSetReasonObjectsNotReady`; remove
  `ClusterObjectSetReasonProbeFailure`. COS stops emitting the shared
  `ocv1.ReasonRollingOut`.
- CE: `progressingFromReady` — map `ObjectsNotReady` explicitly to
  `Progressing=True/RollingOut` (replaces the `default`/`ProbeFailure` case);
  CE Progressing contract (reason `RollingOut`) is preserved. `determineFailureReason`
  needs no change (`ObjectsNotReady` is not an error reason → `Absent`).

### PR 2 — `Archiving`

- In `archive()` (`:390-409`), replace the two teardown-in-progress
  `setRetryableErrorConditions` calls (teardown error `:394`; owned resources not
  yet removed `:398`) with `Ready=False/Archiving`. `markAsArchived` (→ `Archived`)
  stays terminal. The teardown-error path still returns the error for backoff.
- Add `ClusterObjectSetReasonArchiving`.
- CE: `Archiving`/`Archived` are archival-path reasons on old revisions;
  `determineFailureReason` only inspects the latest rolling revision, so no
  `Failed` mapping. Add `Archiving` to `progressingFromReady` as neutral
  (`Progressing=True/RollingOut`) for completeness.

### PR 3 — `Superseded`

- In the not-complete branch, **after** the `ProgressDeadlineExceeded` check and
  **before** the `ObjectsNotReady`/probe-detail determination, scan
  `rres.GetPhases()` objects for `machinery.ActionProgressed`. If any (and the
  deadline is not exceeded), set `Ready=False/Superseded` with a message naming the
  superseding direction ("Object(s) have been adopted by a newer revision of the
  object set."). This honors the precedence `ProgressDeadlineExceeded` →
  `Superseded` → `ObjectsNotReady`.
- Siblings are already wired (`WithSiblingOwners`, `:578`); boxcutter emits
  `ActionProgressed` only for newer-sibling adoption, so no revision comparison is
  needed on our side.
- Add `ClusterObjectSetReasonSuperseded`.
- CE: `Superseded` is on the old revision; map to neutral in
  `progressingFromReady`; not an error in `determineFailureReason`.

### PR 4 — remove `RetryableError`

- Replace every `setRetryableErrorConditions(...)` call with: return the wrapped
  error (controller-runtime requeues with backoff) **without** mutating `Ready`.
  Sites: `:153, :176, :193, :219, :229` (and the archive-error path if still
  present after PR 2). Delete `setRetryableErrorConditions` and
  `ClusterObjectSetReasonRetryableError`.
- The `LifecycleStateArchived` TrackingCache.Free error (`:207`) currently marks
  `RetryableError`; switch to returning the error without touching `Ready`.
- CE: remove `RetryableError` from `determineFailureReason` (`:170`) and the
  `Retrying` branch of `progressingFromReady` (`:276`). `ObjectCollision` stays an
  error reason in `determineFailureReason`.

## Testing

Per PR, extend the existing table tests in
`internal/object-controller/controllers/clusterobjectset_controller_test.go`
(status-condition table `RevisionReconciliation`, collision table
`ForeignRevisionCollision`) and the CE reconstruction tests in
`internal/operator-controller/controllers/*_test.go`. Add e2e scenario updates
where the retired reasons are asserted. Verify with `make test-unit` and
`make lint` each PR; regenerate and commit generated artifacts.

## Non-goals

- `MutableReferencedSecret` / `DigestMismatch` split from the blueprint — #2985
  deliberately consolidated these under `ValidationFailure`; not reopened here.
- Message polish beyond what each reason requires (tracked separately).
- `ClusterObjectDeployment` layering and moving `ProgressDeadlineExceeded` off COS
  (future work once COD lands).
- Concept-doc `Progressing`/`Succeeded` section cleanup beyond the reason rows
  these PRs touch (pre-existing follow-up).
