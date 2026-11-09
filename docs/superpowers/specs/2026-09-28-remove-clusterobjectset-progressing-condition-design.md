# Design: Remove the `Progressing` condition from ClusterObjectSet

**Status:** Draft — awaiting review
**Date:** 2026-09-28
**Scope:** experimental channel only (ClusterObjectSet is experimental-only)

## Goal & intent

Remove the `Progressing` condition type from `ClusterObjectSet` (COS). Progress /
retry / deadline semantics should be owned at the `ClusterExtension` (CE) layer;
the lower-level COS API should expose only **health** (`Available`) and a
**done** latch (`status.completedAt`).

This continues the direction of commit `85255103` ("Use completedAt in lieu of
Succeeded condition on ClusterObjectSet"), which already removed the COS
`Succeeded` condition in favour of `completedAt`.

## Decisions (agreed during brainstorming)

1. **Approach 2 — express everything through `Available`.** COS keeps its
   deadline and block-detection logic internally, but stops publishing a
   `Progressing` condition. The rollout/retry/block/deadline states that
   `Progressing` used to carry are folded into an expanded reason set on the
   single `Available` condition.
2. **Preserve the ClusterExtension condition contract.** The CE's `Progressing`
   condition (types, reasons, semantics) must not change for users or e2e. The
   operator-controller now *reconstructs* the CE `Progressing` condition from
   COS `Available` + `completedAt` instead of mirroring COS `Progressing`.
3. **Fold `Blocked` / `ProgressDeadlineExceeded` into `Available`, accepting a
   small health-signal loss.** These are rollout concerns, not health concerns,
   but to keep the logic in COS with only one condition left, COS sets
   `Available=False` with reason `Blocked` / `ProgressDeadlineExceeded`. In those
   states a blocked-but-running rolling revision no longer surfaces an
   independent health value. This is a documented, accepted deviation.
4. **Preserve current top-level CE `Available` mirroring.** Today the
   operator-controller mirrors the installed revision's `Available` onto the
   top-level CE conditions (undocumented, untested). Keep that behavior to avoid
   any observable change.

## Background: what COS `Progressing` encodes today

`internal/object-controller/controllers/clusterobjectset_controller.go` sets
`Progressing` in five distinct ways:

| `Progressing` state | Trigger (call site) | `Available` at that time |
|---|---|---|
| `True/RollingOut` | `rres.InTransition()` (L229); not-complete branch (L291) | set separately (L287-289) or untouched |
| `True/Succeeded` | `rres.IsComplete()` (L249) — also sets `completedAt` | `True/ProbesSucceeded` (L250) |
| `True/Retrying` | reconcile errors via `setRetryingConditions` (L144,158,184,194,202,209,222,651) | `Unknown/Reconciling` — **only if `Available` already exists** (L652 guard) |
| `False/Blocked` | `verifyReferencedSecretsImmutable` (L138), `verifyObservedPhases` (L152) — terminal | **untouched** (health preserved) |
| `False/ProgressDeadlineExceeded` | `durationUntilDeadline` inside `markAsProgressing` (L670-673) | varies |
| `False/Archived` | `markAsArchived` (L727) | `Unknown/Archived` (L728) |

Key facts that make removal feasible:

- `status.completedAt` already encodes "rollout complete" (== old `Succeeded`),
  and `progress_deadline.go` (`deadlineAwareRateLimiter` / `durationUntilDeadline`)
  computes deadlines from `spec.progressDeadlineMinutes` + `creationTimestamp` +
  `completedAt` — it **does not read the `Progressing` condition**. Requeue
  timing therefore needs no change.
- Only the *reporting* of `ProgressDeadlineExceeded` (currently via
  `markAsProgressing`) needs a new home: it moves onto `Available`.

## Current condition mapping (COS → CE)

`internal/operator-controller/controllers/boxcutter_reconcile_steps.go`:

- `GetRevisionStates` copies the full COS `Status.Conditions` into each
  `RevisionMetadata`, and classifies a revision as `Installed` iff
  `completedAt` is set, else `RollingOut` (L64-85).
- `ApplyBundleWithBoxcutter` mirrors both `Available` and `Progressing`:
  - installed revision → top-level CE conditions + `activeRevisions[0]` (L139-150),
  - each rolling revision → its `RevisionStatus.Conditions` (L151-158),
  - latest rolling revision's `Progressing` → top-level CE conditions (L160-165).
- On apply error, `setStatusProgressing(ext, err)` sets CE `Progressing` directly
  (L124-134) — **unchanged by this work**.

`internal/operator-controller/controllers/common_controller.go`:

- `determineFailureReason` (L91-109) reads the **latest rolling revision's
  `Progressing` reason**; `Retrying` → CE `Installed` reason `Failed`, else
  `Absent`. This must switch to reading `Available`.

## Target design

### 1. COS `Available` reason taxonomy (expanded)

`api/v1/clusterobjectset_types.go` — remove `ClusterObjectSetTypeProgressing`;
`Available` becomes the single status condition with these `(status, reason)`
tuples:

| status | reason | meaning |
|---|---|---|
| `True` | `ProbesSucceeded` | rolled out, all probes pass (paired with `completedAt`) |
| `False` | `ProbeFailure` | rolling out, objects failing probes |
| `False` | `RollingOut` | rolling out, not yet complete, no probe failures |
| `Unknown` | `Reconciling` | reconcile error prevented observing probes (was `Retrying`) |
| `False` | `Blocked` | terminal error, manual intervention required (**new on `Available`**) |
| `False` | `ProgressDeadlineExceeded` | deadline exceeded before rollout (**new on `Available`**) |
| `Unknown` | `Archived` | archived / torn down |
| `Unknown` | `Migrated` | migrated, probes not yet observed |

Notes:
- `Reconciling` (already an `Available` reason) absorbs the old `Retrying`
  semantics. The L652 "only if Available already exists" guard is **removed** so
  first-reconcile retries are visible.
- Deadline enforcement (`if isDeadlineExceeded → ProgressDeadlineExceeded`),
  currently centralized in `markAsProgressing`, moves into the `Available`-setting
  path for non-complete states.

### 2. COS controller changes

`internal/object-controller/controllers/clusterobjectset_controller.go`:

- Delete `markAsProgressing`, `markAsNotProgressing`, and the `Progressing`
  half of `setRetryingConditions` / `markAsArchived`.
- Introduce a single helper for in-progress / not-complete states that applies
  the deadline check, e.g.
  `markProgressing(l, cos, reason, message, isDeadlineExceeded)` which sets
  `Available=False/ProgressDeadlineExceeded` when the deadline is exceeded, else
  `Available` with the supplied reason.
- Rewire call sites:
  - L138 / L152 (Blocked): `markAsUnavailable(Blocked, …)`.
  - L144/158/184/194/202/209/222 (Retrying): `markAsAvailableUnknown(Reconciling, …)` (unconditionally).
  - L229 / L291 (RollingOut): set `Available=False/RollingOut` (or `ProbeFailure`
    per L287-289) through the deadline-aware helper.
  - L249-250 (Succeeded): drop the `Progressing` line; keep
    `markAsAvailable(ProbesSucceeded)` + `completedAt`.
  - `markAsArchived`: keep `Available=Unknown/Archived` only.
- `progress_deadline.go`: **no change**.

### 3. Operator-controller reconstruction (COS `Available` → CE `Progressing`)

`internal/operator-controller/controllers/common_controller.go` — add a pure
helper:

```go
// progressingFromAvailable derives the ClusterExtension Progressing condition
// for a revision from that revision's COS Available condition and whether it has
// completed (completedAt set).
func progressingFromAvailable(available *metav1.Condition, completed bool) metav1.Condition
```

Mapping:

| input | CE `Progressing` |
|---|---|
| `completed == true` | `True/Succeeded` |
| `Available` reason `Blocked` | `False/Blocked` |
| `Available` reason `ProgressDeadlineExceeded` | `False/ProgressDeadlineExceeded` |
| `Available` status `Unknown` (`Reconciling`) | `True/Retrying` |
| otherwise (`ProbeFailure` / `RollingOut` / not-complete) | `True/RollingOut` |

Message is best-effort: reuse the `Available` condition's message.
`ObservedGeneration` is rewritten to the CE generation, as today.

`boxcutter_reconcile_steps.go` — in `ApplyBundleWithBoxcutter`:
- Mirror COS `Available` onto CE `Available` (top-level for installed; per-revision
  for all) exactly as today.
- Replace every place that mirrored COS `Progressing` with a call to
  `progressingFromAvailable(...)` and `SetStatusCondition`:
  - installed revision → top-level CE `Progressing` + `activeRevisions[0]`,
  - each rolling revision → its `RevisionStatus.Conditions`,
  - latest rolling revision → top-level CE `Progressing`.

`common_controller.go` — `determineFailureReason`: read the latest rolling
revision's `Available` condition instead of `Progressing`; treat
`Available=Unknown/Reconciling` (the new home of `Retrying`) as the error signal
→ `ReasonFailed`, else `ReasonAbsent`.

### 4. CRDs, manifests, docs (generated)

- `api/v1/clusterobjectset_types.go`: remove the `Progressing` printcolumn
  (L565); update the `ClusterObjectSetStatus.Conditions` doc block (L495-515) to
  describe only `Available` and its expanded reasons.
- `make manifests` regenerates
  `helm/olmv1/base/operator-controller/crd/experimental/…clusterobjectsets.yaml`
  and `manifests/experimental.yaml` / `manifests/experimental-e2e.yaml`.
- `make crd-ref-docs` regenerates `docs/api-reference/olmv1-api-reference.md`.
- No Go struct shape change → `zz_generated.deepcopy.go` and
  `applyconfigurations/**` are unaffected (conditions remain `[]metav1.Condition`).

### 5. Tests

- `clusterobjectset_controller_test.go`, `clusterobjectset_controller_internal_test.go`,
  `progress_deadline_test.go`: migrate assertions from `Progressing` to the
  expanded `Available` reasons (RollingOut, Reconciling, Blocked,
  ProgressDeadlineExceeded, Archived).
- `boxcutter_reconcile_steps_test.go`, `common_controller_test.go`: assert the
  reconstruction produces the same CE `Progressing` tuples as before from COS
  `Available` inputs.
- E2E `test/e2e/steps/steps.go`: `ClusterObjectSetIsArchived` (L975-978) waits on
  COS `Progressing=False/Archived` → change to `Available=Unknown/Archived`. CE
  Progressing steps (L684, L698-713) are unchanged (contract preserved).
- Check `hack/tools/migrate/` and `internal/operator-controller/migration/` for
  any code that sets/reads COS `Progressing`; migrate to `Available`.

## Accepted deviations / risks

- **Health loss in Blocked/DeadlineExceeded** (decision 3): a blocked-but-running
  rolling revision no longer exposes an independent health value on `Available`;
  `Available` shows the rollout-blocking reason instead. Documented as intended.
- **Condition messages** may differ byte-for-byte on the reconstructed CE
  `Progressing` (reasons and status match; messages are best-effort reused from
  `Available`). If strict message parity is required, it is achievable but adds
  coupling; not planned unless a test demands it.
- Experimental-only surface, so no standard-channel CRD/manifest changes.

## Out of scope

- Moving deadline computation or block-detection to the operator-controller
  (that was Approach 1, not chosen).
- Any change to the CE `Progressing`/`Installed` public contract.

## Verification

- `make generate manifests crd-ref-docs` then `make verify` (generated code in sync).
- `make lint` / `make lint-api-diff`.
- `make test-unit`.
- `make test-experimental-e2e` (COS is experimental).
