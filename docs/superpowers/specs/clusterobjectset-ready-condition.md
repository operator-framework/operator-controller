# ClusterObjectSet `Ready` condition

This document describes the single `Ready` status condition published on
`ClusterObjectSet` (COS), every reason it can carry, what triggers each reason,
and how the controller re-enqueues the object afterwards.

It reflects the implementation in
`internal/object-controller/controllers/clusterobjectset_controller.go`.

## Overview

A `ClusterObjectSet` is an immutable snapshot of Kubernetes objects. The
controller rolls those objects out phase-by-phase and reports the result on a
**single** `Ready` condition (type `Ready`). Rollout *completion* is recorded
separately on `status.completedAt`; `Ready` reports *health*, not progress.

```
status:
  conditions:
    - type: Ready
      status: "True" | "False"        # Unknown before the first reconcile
      reason: <see table below>
      message: <human-readable detail>
      observedGeneration: <generation the condition was computed for>
```

- `status: Unknown` is the implicit initial state — before the first
  reconciliation has evaluated the object set there is no `Ready` condition at
  all.
- `status: True` means every object is ready and passes its probes.
- `status: False` means the object set is not (yet) healthy, or cannot progress.
  The `reason` distinguishes a transient hiccup from a state that needs the user
  to act.

## How re-enqueue works

The controller's re-enqueue behavior is determined entirely by what `reconcile`
returns to controller-runtime:

| Return | Effect |
|--------|--------|
| `return ctrl.Result{}, err` (non-nil error) | **Rate-limited requeue** — controller-runtime re-enqueues with exponential backoff (default workqueue rate limiter). The error is also logged. |
| `return ctrl.Result{RequeueAfter: N}, nil` | **Timed requeue** — re-enqueued after exactly `N`. |
| `return ctrl.Result{}, nil` | **No scheduled requeue** — the object is only reconciled again when a watched object changes (the tracking cache watches the objects the COS manages) or on the informer resync. |

Two terms used below:

- **Retryable** — the controller expects the situation to clear on its own and
  arranges (or relies on) another reconcile. No user action is strictly
  required.
- **Blocking** — the situation will not clear without a change the controller
  cannot make itself (fix a referenced resource, or create a new object set).
  The controller does not keep retrying on a timer; it re-evaluates on the next
  reconcile trigger (spec change, watched-object change, or resync).

## The progress deadline overlay

`spec.progressDeadlineMinutes` sets a deadline for the object set to become
ready. When the deadline is exceeded, most *not-ready* reasons are **relabelled**
to `ProgressDeadlineExceeded` by `setReadyWithDeadline`, with the message:

```
The object set did not become ready within <N> minute(s). Last status: <original message>
```

`ProgressDeadlineExceeded` is an **overlay**, not a terminal state: the deadline
does not stop reconciliation. If the objects later become ready, the next
reconcile flips `Ready` back to `True`/`AllObjectsReady`. The re-enqueue behavior
of a deadline-exceeded reconcile is inherited from the underlying path (see the
"under deadline" column below).

**Precedence.** Two reasons are checked *before* the deadline and therefore take
precedence over it — they are never relabelled to `ProgressDeadlineExceeded`:

1. `MutableReferencedSecret`
2. `ResolvedContentChanged`

Both are more actionable than "deadline exceeded", so they win.

## Reasons

### `AllObjectsReady`

- **Status:** `True`
- **Class:** Terminal (success)
- **Trigger:** The revision reconcile is complete (`rres.IsComplete()`) and every
  object passes its progress probe. On the first success the controller also
  stamps `status.completedAt`.
- **Message:** `All objects are ready and pass their probes.`
- **Re-enqueue:** None (`Result{}`). The object is reconciled again only when a
  watched object changes, so a later health regression is still caught.
- **Under deadline:** n/a — the deadline overlay only applies to not-ready states.

### `ObjectsNotReady`

- **Status:** `False`
- **Class:** Retryable (health)
- **Trigger:** Rollout is not complete — one or more objects have not yet passed
  their progress probe.
- **Message:** `<N> object(s) are not yet ready.` (count only; per-object detail is
  logged at `V(1)`), or the generic `Objects are not yet ready.` before any probe
  reports.
- **Re-enqueue:** If `spec.progressDeadlineMinutes` is set,
  `RequeueAfter: <time remaining until the deadline>`. Otherwise none
  (`Result{}`) — recovery is driven by the tracking-cache watches on the managed
  objects (a probe flipping to ready re-triggers the reconcile).
- **Under deadline:** Relabelled to `ProgressDeadlineExceeded`, the message
  collapses to the generic `Objects are not yet ready.`, and the reconcile
  returns `Result{}` (no timer — watch-driven from here).

### `Superseded`

- **Status:** `False`
- **Class:** Retryable (health)
- **Trigger:** One or more objects have been adopted by a newer sibling revision
  (`machinery.ActionProgressed`). boxcutter emits `ActionProgressed` only for a
  newer sibling (a foreign owner is an `ObjectCollision`), so no revision
  comparison is needed. A *partial* hand-off counts; evaluated before
  `ObjectsNotReady`.
- **Message:** `Object(s) have been adopted by a newer revision of the object set.`
- **Re-enqueue:** If `spec.progressDeadlineMinutes` is set,
  `RequeueAfter: <time remaining until the deadline>`; otherwise none (`Result{}`).
- **Under deadline:** Not reached — the deadline is checked first, so an exceeded
  deadline reports `ProgressDeadlineExceeded` instead.

### `ValidationFailed`

- **Status:** `False`
- **Class:** Retryable
- **Trigger:** Revision- or phase-level preflight validation failed
  (`rres.GetValidationError()` or `pres.GetValidationError()`).
- **Message:** `Object set manifests are invalid: <err>` (revision level) or
  `Phase <i> is invalid: <err>` (phase level).
- **Re-enqueue:** `RequeueAfter: 10s`.
- **Under deadline:** Relabelled to `ProgressDeadlineExceeded`; still
  `RequeueAfter: 10s`.

### `ObjectCollision`

- **Status:** `False`
- **Class:** Retryable
- **Trigger:** An object the COS would apply already exists and is **not** managed
  by this object set — owned by another controller/operator, or a pre-existing
  unowned object — and `CollisionProtection` prevents adoption.
- **Message:** `One or more objects in phase <i> already exist and are not managed
  by this object set.` (The full list of colliding objects is written to the
  controller log.)
- **Re-enqueue:** `RequeueAfter: 10s`.
- **Under deadline:** Relabelled to `ProgressDeadlineExceeded`; still
  `RequeueAfter: 10s`.

### `MutableReferencedSecret`

- **Status:** `False`
- **Class:** Blocking
- **Trigger:** A referenced `Secret` is not immutable
  (`verifyReferencedSecretsImmutable` fails). This check runs first, so it blocks
  before anything else is evaluated.
- **Message:** Lists the offending Secrets (sorted) and explains that they must
  set `immutable: true` (or that immutable Secrets should be referenced instead).
- **Re-enqueue:** None (`Result{}`, nil error). The controller does not retry on a
  timer; it re-evaluates on the next reconcile trigger (e.g. a spec change or a
  change to the referenced Secret).
- **Under deadline:** **Not** overridden — because it is checked before the
  deadline logic, it always wins over `ProgressDeadlineExceeded`.

### `ResolvedContentChanged`

- **Status:** `False`
- **Class:** Blocking (effectively terminal)
- **Trigger:** Current phase content digests differ from those recorded in
  `status.observedPhases` (`verifyObservedPhases` fails) — a referenced object
  source was changed, or deleted and recreated with different content.
- **Message:** Names the affected phase(s) and explains that the resolved content
  no longer matches the digest recorded in `status.observedPhases`, instructing
  the user to create a new object set to apply the changes.
- **Re-enqueue:** None (`Result{}`, nil error). A COS snapshot is immutable, so
  this does not self-heal — the user must create a new object set.
- **Under deadline:** **Not** overridden — checked before the deadline logic, so
  it always wins over `ProgressDeadlineExceeded`.

### Transient errors (no reason)

A transient internal error — building the boxcutter phases, creating the revision
engine, freeing informers, establishing the watch, the revision reconcile itself,
reading a referenced Secret, listing sibling revisions, or an archive teardown
error — does **not** write the `Ready` condition. The reconcile returns a non-nil
error, so controller-runtime re-enqueues with rate-limited exponential backoff, and
`Ready` keeps whatever value it had (absent on a first reconcile, read as
`Unknown`). There is no dedicated transient-error reason.

### `Archiving`

- **Status:** `False`
- **Class:** Retryable
- **Trigger:** Archive teardown is in progress but not yet complete
  (`tdres != nil && !tdres.IsComplete()`), reached via
  `spec.lifecycleState: Archived`. A teardown *error* is a transient error and
  leaves `Ready` unchanged (see "Transient errors"), not `Archiving`.
- **Message:** `The object set is being archived; removing owned objects.`
- **Re-enqueue:** `RequeueAfter: 5s`.
- **Under deadline:** n/a — the archive path has no deadline overlay.

### `Archived`

- **Status:** `False`
- **Class:** Terminal
- **Trigger:** Archive teardown is complete — all owned objects have been torn
  down. Set just before the teardown finalizer is removed.
- **Message:** `The object set is archived.`
- **Re-enqueue:** None (`Result{}`).
- **Under deadline:** n/a.

### `ProgressDeadlineExceeded`

- **Status:** `False`
- **Class:** Retryable (overlay)
- **Trigger:** `spec.progressDeadlineMinutes` elapsed before the object set became
  ready. This is an *overlay* reason that relabels `ObjectsNotReady` or
  `ObjectCollision` — it is not a distinct code path and does not stop
  reconciliation. (Transient errors set no reason, so there is nothing to relabel.)
- **Message:** `The object set did not become ready within <N> minute(s). Last
  status: <original message>`.
- **Re-enqueue:** Inherits the re-enqueue behavior of whichever path triggered it
  (see each reason's "Under deadline" note above).
- **Under deadline:** n/a — this *is* the deadline state.

## Precedence summary

Within a single reconcile, the checks run in this order; the first one that
fires wins:

1. **`MutableReferencedSecret`** — referenced Secrets must be immutable.
   (blocking; beats the deadline)
2. **`ResolvedContentChanged`** — observed-phase digests must still match.
   (blocking; beats the deadline)
3. Transient errors → **`Ready` left unchanged, no reason** (building phases,
   engine creation, freeing informers, establishing the watch, revision reconcile);
   the error is returned for backoff.
4. **`ValidationFailed`** — revision/phase preflight validation. Like the blocking
   checks (1–2), this is reported via `markAsNotReady` and is **not** overridden by
   the deadline — a failed pre-apply check is more actionable than "deadline
   exceeded", so it wins.
5. **`ObjectCollision`** — per-phase object collisions.
6. Rollout result:
   - complete → **`AllObjectsReady`** (`True`)
   - not complete, ≥1 object progressed to a newer revision → **`Superseded`**
   - not complete → **`ObjectsNotReady`**
7. **`ProgressDeadlineExceeded`** overlays steps 5–6 (`ObjectCollision`,
   `Superseded`, `ObjectsNotReady`) once the deadline has passed — but **not**
   steps 1–2 or 4. Note the asymmetry within the actionable reasons: the progress
   deadline **intentionally overrides `ObjectCollision`** (past the deadline a
   collision reads as `ProgressDeadlineExceeded`), whereas `ValidationFailed` wins
   over it. This is a deliberate, documented choice.

The archive path (`spec.lifecycleState: Archived`) is separate and produces
**`Archiving`** (teardown in progress), **`Archived`** (teardown complete), or a
transient error reason (teardown error).

## ClusterExtension roll-up (for reference)

The `ClusterExtension` controller reads the COS `Ready` condition and derives its
own `Progressing` / `Installed` conditions from it
(`progressingFromAvailable` in `common_controller.go`). In brief:

- `MutableReferencedSecret` / `ResolvedContentChanged` → CE `Progressing=False`
  with reason `Blocked` (message passed through verbatim).
- `ProgressDeadlineExceeded` → CE `Progressing=False` with reason
  `ProgressDeadlineExceeded`.
- `ObjectCollision` → CE `Progressing=True` with reason `Retrying`; it and
  `ValidationFailed` can drive the `Installed` failure reason `Failed`.
- A transient error leaves COS `Ready` unchanged, so the CE goes neutral — it does
  not map to `Retrying` or `Failed`.
- Other reasons on a not-yet-complete revision → CE `Progressing=True` with
  reason `RollingOut`; on a completed revision, `Progressing` stays `Succeeded`.
