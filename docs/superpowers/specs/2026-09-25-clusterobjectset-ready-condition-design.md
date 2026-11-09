# ClusterObjectSet: single `Ready` status condition

**Date:** 2026-09-25
**Status:** Design — pending review
**Scope:** experimental only (`ClusterObjectSet` is an experimental API)
**Change class:** ⚠ breaking API change (experimental channel)

## Goal

Replace the two status conditions currently published on `ClusterObjectSet`
(`Available` + `Progressing`) with a **single `Ready` condition**. `Ready` is the
sole condition on the resource, with the following state table:

| Status  | Reason                   | When                                                   |
| ------- | ------------------------ | ------------------------------------------------------ |
| True    | Ready                    | All phases complete                                    |
| False   | Incomplete               | One or more phases not yet complete                    |
| False   | Blocked                  | Requires manual intervention (see below)               |
| False   | Invalid                  | Preflight validation error                             |
| False   | Archived                 | Teardown in progress or complete                       |
| False   | ProgressDeadlineExceeded | Not complete within the progression deadline           |
| Unknown | ReconcileError           | Reconcile returned an error                            |
| Unknown | TeardownError            | Teardown returned an error                             |
| Unknown | InternalError            | Internal controller error                              |

### Decisions captured during design

- **`Superseded` is dropped.** The original table included a `Superseded` reason
  ("objects adopted by a newer revision"). There is no equivalent in today's
  controller (a newer revision simply archives older ones on completion), and
  detecting it is net-new work. It is out of scope for this change and not added
  as a constant.
- **Object collisions map to `Blocked`.** A collision (another controller owns a
  managed object) requires manual intervention, so it resolves to
  `Ready=False/Blocked` rather than a transient error.
- **`Blocked` folds in** secret-immutability failures, resolved-phase content
  drift, and object collisions.
- **Probe failures fold into `Incomplete`.** A failing readiness probe means a
  phase is not complete; there is no separate probe reason.
- **Deadline wins.** For an active (non-archived) revision past its
  `progressDeadlineMinutes`, any not-yet-`Ready` state resolves to
  `Ready=False/ProgressDeadlineExceeded`, **except `Blocked`**, which continues to
  take precedence (preserving the existing "Blocked is more actionable" behavior).
- **The ClusterExtension keeps both `Available` and `Progressing`.** To avoid
  churning e2e assertions now, the CE-side translator derives the CE's existing
  `Available` and `Progressing` conditions from the COS `Ready` condition
  (mapping in §4). The COS itself still exposes only `Ready`.

## Non-goals

- No change to the standard (non-experimental) channel.
- No `Superseded` detection.
- No change to `CompletedAt`, `ObservedPhases`, or the progress-deadline
  rate-limiter mechanics — these already key off `CompletedAt`, not conditions.
- No restructuring of the ClusterExtension's public condition set (still
  `Installed` + `Progressing` + deprecation types in `conditionsets`, with
  `Available` continuing to be emitted by the experimental mirror as today).

---

## 1. API changes (`api/v1/clusterobjectset_types.go`)

**Remove** condition-type constants `ClusterObjectSetTypeAvailable`,
`ClusterObjectSetTypeProgressing`, and reason constants
`ClusterObjectSetReasonArchived/Blocked/ProbeFailure/ProbesSucceeded/Reconciling/Retrying`.

**Add:**

```go
// Condition Types
ClusterObjectSetTypeReady = "Ready"

// Condition Reasons
ClusterObjectSetReasonReady                    = "Ready"
ClusterObjectSetReasonIncomplete               = "Incomplete"
ClusterObjectSetReasonBlocked                  = "Blocked"
ClusterObjectSetReasonInvalid                  = "Invalid"
ClusterObjectSetReasonArchived                 = "Archived"
ClusterObjectSetReasonProgressDeadlineExceeded = "ProgressDeadlineExceeded"
ClusterObjectSetReasonReconcileError           = "ReconcileError"
ClusterObjectSetReasonTeardownError            = "TeardownError"
ClusterObjectSetReasonInternalError            = "InternalError"
```

**Doc comment** on `ClusterObjectSetStatus.Conditions` (currently lines 494-515):
rewrite to describe only `Ready` and its status/reason combinations from the
table above.

**Printer columns** (currently lines 564-565):

```go
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].reason`
// +kubebuilder:printcolumn:name="Completed",type=string,JSONPath=`.status.completedAt`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
```

The `Completed` column uses `type=string` (not `type=date`) deliberately:
`kubectl` renders `type=date` columns as a relative age (e.g. "3d"), whereas
`type=string` against `.status.completedAt` prints the absolute RFC3339
timestamp. `completedAt` is omitted from the status until the revision first
becomes ready, so the column is empty (`<none>`) until then.

---

## 2. Controller state machine (`internal/object-controller/controllers/clusterobjectset_controller.go`)

**Delete:** `markAsProgressing`, `markAsNotProgressing`, `markAsAvailable`,
`markAsUnavailable`, `markAsAvailableUnknown`, `markAsArchived`,
`setRetryingConditions`, and the `nonTerminalProgressingReasons` map.

**Add** two helpers:

```go
// setReady writes the single Ready condition unconditionally.
func setReady(cos *ocv1.ClusterObjectSet, status metav1.ConditionStatus, reason, message string) bool

// setReadyProgressing handles the "still working / erroring" states that are
// subject to the progress deadline. If the deadline is exceeded it overrides to
// Ready=False/ProgressDeadlineExceeded; otherwise it writes the given
// (status, reason). Callers pass Incomplete / Invalid / ReconcileError /
// TeardownError / InternalError. Blocked and Archived do NOT go through this
// helper (they bypass the deadline override).
func setReadyProgressing(l logr.Logger, cos *ocv1.ClusterObjectSet, status metav1.ConditionStatus, reason, message string, isDeadlineExceeded bool) bool
```

**Exit-point mapping** (replacing current calls):

| Location (current line) | Current call | New call |
| --- | --- | --- |
| secret not immutable (139) | `markAsNotProgressing(Blocked)` | `setReady(False, Blocked, …)` |
| build phases error (145) | `setRetryingConditions` | `setReadyProgressing(Unknown, ReconcileError, …, deadline)` |
| observedPhases drift (153) | `markAsNotProgressing(Blocked)` | `setReady(False, Blocked, …)` |
| create engine error (159) | `setRetryingConditions` | `setReadyProgressing(Unknown, ReconcileError, …, deadline)` |
| archive: Free error (173) | `markAsAvailableUnknown(Reconciling)` | `setReady(Unknown, TeardownError, …)` |
| establish watch error (185) | `setRetryingConditions` | `setReadyProgressing(Unknown, ReconcileError, …, deadline)` |
| revision reconcile error (195) | `setRetryingConditions` | `setReadyProgressing(Unknown, ReconcileError, …, deadline)` |
| preflight validation error (203) | `setRetryingConditions` | `setReadyProgressing(False, Invalid, …, deadline)` |
| phase validation error (210) | `setRetryingConditions` | `setReadyProgressing(False, Invalid, …, deadline)` |
| object collision (223) | `setRetryingConditions` | `setReady(False, Blocked, …)` |
| in transition (230) | `markAsProgressing(RollingOut)` | `setReadyProgressing(False, Incomplete, …, deadline)` |
| complete (250-251) | `markAsProgressing(Succeeded)` + `markAsAvailable(ProbesSucceeded)` | `setReady(True, Ready, …)` |
| not complete, probe fail (288) | `markAsUnavailable(ProbeFailure)` | (folded) `setReadyProgressing(False, Incomplete, …, deadline)` |
| not complete, rolling (290-292) | `markAsUnavailable` + `markAsProgressing` | `setReadyProgressing(False, Incomplete, …, deadline)` |
| archive teardown in progress (317, 321) | `setRetryingConditions` | `setReady(False, Archived, …)` |
| archive teardown complete (325 `markAsArchived`) | Progressing/Available Archived | `setReady(False, Archived, …)` |
| delete: Free error (303) | `markAsAvailableUnknown(Reconciling)` | `setReady(Unknown, TeardownError, …)` |

`CompletedAt` set (257-259) is unchanged. The `IsComplete` branch sets
`Ready=True/Ready` and stamps `CompletedAt` exactly as today.

Note the not-complete branch collapses to a single `setReadyProgressing(...,
Incomplete, ...)` call — the probe-failure message can be preserved in the
condition `message` for debuggability, but the reason is always `Incomplete`.

---

## 3. Progress deadline

No change to `progress_deadline.go` or `durationUntilDeadline` — they already
derive the deadline from spec/metadata/`CompletedAt`. The deadline-override
behavior (today inside `markAsProgressing`, lines 664-676) moves into
`setReadyProgressing`: when `isDeadlineExceeded` is true, write
`Ready=False/ProgressDeadlineExceeded` instead of the caller's
`(status, reason)`. `Blocked` paths bypass `setReadyProgressing`, preserving the
"Blocked takes precedence over ProgressDeadlineExceeded" comment at line 136.

---

## 4. ClusterExtension consumer translation

### 4a. `common_controller.go` — `determineFailureReason` (line 91)

Read the latest rolling revision's `Ready` condition instead of `Progressing`:

- `Ready=Unknown/*` (ReconcileError/TeardownError/InternalError) → `ReasonFailed`
- `Ready=False/{Invalid, Blocked, ProgressDeadlineExceeded}` → `ReasonFailed`
- `Ready=False/Incomplete` or condition absent → `ReasonAbsent`

### 4b. `boxcutter_reconcile_steps.go` (lines 138-166)

Stop mirroring `Available`+`Progressing` verbatim. Introduce a translator that
derives the CE conditions from a revision's `Ready` condition.

**CE `Available` = the COS `Ready` condition retyped as `Available`.** Status,
reason, and message are mirrored 1:1 from `Ready`; only `Type` changes to
`Available`. (Decision: "mirror COS Ready reason" — probe failures no longer
produce a distinct `ProbeFailure` reason, so CE `Available` carries the `Ready`
reason instead.)

**CE `Progressing`** is derived from `Ready`:

| COS `Ready` | CE `Progressing` |
| --- | --- |
| True / Ready | True / `Succeeded` |
| False / Incomplete | True / `RollingOut` |
| False / ProgressDeadlineExceeded | False / `ProgressDeadlineExceeded` |
| False / Blocked | False / `Blocked` |
| False / Invalid | False / `InvalidConfiguration` |
| Unknown / {ReconcileError, TeardownError, InternalError} | True / `Retrying` |
| False / Archived | not set by the translator (leave as-is) |

CE `Progressing` reasons use the existing `common_types.go` constants
(`ReasonSucceeded`, `ReasonRollingOut`, `ReasonRetrying`, `ReasonBlocked`,
`ReasonInvalidConfiguration`, `ReasonProgressDeadlineExceeded`).

- `RevisionStatus.Conditions` (per-revision, surfaced in
  `ext.Status.ActiveRevisions[].conditions`): mirror the single `Ready`
  condition. Update the `RevisionStatus.conditions` doc comment
  (`clusterextension_types.go:519`).
- CE top-level: emit `Available` (retyped `Ready`) + derived `Progressing` from
  the installed revision, and derived `Progressing` from the latest rolling
  revision (as today).

Note: many CE `Progressing` assertions (e.g. `InvalidConfiguration` in
`install.feature`) originate from CE-level reconcile logic (`setStatusProgressing`
on apply errors), not the COS mirror, and are unaffected by this translator.

---

## 5. Generated + peripheral artifacts

- `make generate` — `zz_generated.deepcopy.go`, apply-config
  (`applyconfigurations/api/v1/clusterobjectsetstatus.go`, `revisionstatus.go`).
- `make manifests` — helm base CRD
  (`helm/olmv1/base/operator-controller/crd/experimental/olm.operatorframework.io_clusterobjectsets.yaml`)
  and rendered `manifests/experimental.yaml` + `manifests/experimental-e2e.yaml`
  (printer columns + conditions description).
- `make crd-ref-docs` — `docs/api-reference/olmv1-api-reference.md`.
- Hand-written docs: rewrite the `Progressing`/`Available` section of
  `docs/draft/concepts/clusterobjectsets.md` (≈ lines 156-168) to a single
  `Ready` table; adjust narrative refs in
  `docs/draft/concepts/large-bundle-support.md` (`Blocked` still valid).

---

## 6. Tests

- `internal/object-controller/controllers/clusterobjectset_controller_test.go` —
  rewrite all `Available`/`Progressing` assertions to `Ready` per §2.
- `internal/object-controller/controllers/progress_deadline_test.go` — update
  deadline-override assertions to `Ready=False/ProgressDeadlineExceeded`.
- `internal/operator-controller/controllers/common_controller_test.go` — update
  `determineFailureReason` cases to drive off `Ready`.
- `internal/operator-controller/applier/boxcutter_test.go` — update mirror/
  translation expectations.
- `test/e2e/steps/steps.go` — `ClusterObjectSetIsArchived` (975-978) now waits
  for `Ready=False, Reason=Archived`. The generic `ClusterObjectSetReportsCondition*`
  step defs are type-agnostic and need no code change.
- **e2e feature files** — all COS condition assertions must be rewritten (COS
  loses `Available` and `Progressing`). ~24 lines across:
  - `revision.feature`: `:63,208` `Available False/ProbeFailure and Message:` →
    `Ready False/Incomplete and Message:`; `:150+151, 326+327, 423+424, 519+520,
    578, 710` `Progressing True/Succeeded` (+ paired `Available True/ProbesSucceeded`)
    → single `Ready True/Ready`; `:471,548` `Progressing False/Blocked` →
    `Ready False/Blocked`; `:640,709` `Progressing False/ProgressDeadlineExceeded`
    → `Ready False/ProgressDeadlineExceeded`.
  - `update.feature`: `:318+319` → `Ready True/Ready`; `:347+348`
    (`Progressing True/RollingOut` + `Available False/ProbeFailure`) →
    `Ready False/Incomplete`.
  - `status.feature`: `:37,43` COS `Available False/ProbeFailure` →
    `Ready False/Incomplete`; `:46` COS `Available True/ProbesSucceeded` →
    `Ready True/Ready`; `:36,42` **CE** `Available False/ProbeFailure` →
    `Available False/Incomplete` (CE Available now mirrors the Ready reason).
  - `install.feature`: `:373,407` COS `Progressing False/ProgressDeadlineExceeded`
    → `Ready False/ProgressDeadlineExceeded`.
  - CE `Progressing` assertions in `recover.feature`, `install.feature`,
    `proxy.feature`, `update.feature` (`Retrying`/`Succeeded`/`RollingOut`/
    `InvalidConfiguration`/`ProgressDeadlineExceeded`) are preserved by the §4b
    translator + CE-level logic and need no change.

---

## 7. Verification

```
make generate manifests crd-ref-docs
make lint-api-diff        # expects breaking-change flags on the experimental CRD
make test-unit
make verify
```

CI `crd-diff` / `go-apidiff` will flag the condition removal — expected for a ⚠
experimental change.

The `test-experimental-e2e` suite exercises the feature-file changes in §6.
