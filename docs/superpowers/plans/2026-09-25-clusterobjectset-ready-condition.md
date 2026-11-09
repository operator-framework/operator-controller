# ClusterObjectSet single `Ready` condition — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the `ClusterObjectSet` `Available` + `Progressing` status conditions with a single `Ready` condition, and derive the ClusterExtension's `Available`/`Progressing` conditions from it.

**Architecture:** The COS reconciler publishes one `Ready` condition via two helpers (`setReady`, `setReadyProgressing`). The ClusterExtension controller stops mirroring COS conditions verbatim: it retypes the COS `Ready` condition as its own `Available` and derives its `Progressing` from `Ready`'s reason. All changes are gated behind the experimental build tag (`//go:build !standard`) and confined to the experimental CRD variant.

**Tech Stack:** Go, controller-runtime, `k8s.io/apimachinery/pkg/api/meta`, kubebuilder markers, envtest (`make test-unit`), godog e2e feature files.

**Spec:** `docs/superpowers/specs/2026-09-25-clusterobjectset-ready-condition-design.md`

## Global Constraints

- Experimental only. `ClusterObjectSet` lives behind `//go:build !standard`; only the experimental CRD/manifests change (`.../crd/experimental/...`, `manifests/experimental.yaml`, `manifests/experimental-e2e.yaml`).
- Label/annotation/condition prefixes use `olm.operatorframework.io`.
- Never hand-edit generated files (`zz_generated.deepcopy.go`, `applyconfigurations/**`, `helm/olmv1/base/**/crd/**`, `manifests/*.yaml`, `docs/api-reference/olmv1-api-reference.md`); regenerate via `make generate manifests crd-ref-docs`.
- Commit messages: title prefix `⚠` (breaking experimental API change) for the API/removal commits, `✨`/`🐛`/`🌱` as appropriate otherwise. End every commit body with:
  `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`
- Do not commit to `main`. Work on branch `cos-status-remove-progressing` (already checked out).
- The COS `Ready` condition is the single source of truth. Reason constants (exact strings): `Ready`, `Incomplete`, `Blocked`, `Invalid`, `Archived`, `ProgressDeadlineExceeded`, `ReconcileError`, `TeardownError`, `InternalError`. No `Superseded`.

## Review Focus

- **Deadline vs Blocked precedence:** a `Blocked` revision past its `progressDeadlineMinutes` must stay `Ready=False/Blocked`, NOT flip to `ProgressDeadlineExceeded` (Blocked bypasses `setReadyProgressing`). Pinned in Task 3.
- **CompletedAt latch preserved:** once `status.completedAt` is set, the deadline no longer applies and `Ready` must not regress to `ProgressDeadlineExceeded` on a later transient error. Pinned in Task 3.
- **Probe-failure message survives folding:** the probe-failure detail message must be carried into `Ready=False/Incomplete`'s `message` so `revision.feature` `... and Message:` assertions still match. Pinned in Task 3.
- **determineFailureReason latest-revision-wins:** with `Ready`, `Unknown/*` and `False/{Invalid,Blocked,ProgressDeadlineExceeded}` on the LATEST rolling revision → `Failed`; `False/Incomplete` or absent → `Absent`. Pinned in Task 4.
- **Handover with multiple active revisions:** CE `Available` comes from the `Installed` revision; CE `Progressing` from the LATEST `RollingOut` revision. Pinned in Task 5.

---

## Task 1: Add `Ready` API constants, printer columns, status doc; regenerate

**Files:**
- Modify: `api/v1/clusterobjectset_types.go` (constants block lines 28-39; status `Conditions` doc lines 494-515; printcolumn markers lines 564-565)
- Regenerate: `helm/olmv1/base/operator-controller/crd/experimental/olm.operatorframework.io_clusterobjectsets.yaml`, `manifests/experimental.yaml`, `manifests/experimental-e2e.yaml`, `docs/api-reference/olmv1-api-reference.md`

**Interfaces:**
- Produces: `ocv1.ClusterObjectSetTypeReady = "Ready"` and reason constants `ClusterObjectSetReasonReady`, `ClusterObjectSetReasonIncomplete`, `ClusterObjectSetReasonBlocked`, `ClusterObjectSetReasonInvalid`, `ClusterObjectSetReasonArchived`, `ClusterObjectSetReasonProgressDeadlineExceeded`, `ClusterObjectSetReasonReconcileError`, `ClusterObjectSetReasonTeardownError`, `ClusterObjectSetReasonInternalError`.
- Note: the OLD constants (`ClusterObjectSetTypeAvailable`, `ClusterObjectSetTypeProgressing`, `ClusterObjectSetReasonProbeFailure`, `ClusterObjectSetReasonProbesSucceeded`, `ClusterObjectSetReasonReconciling`, `ClusterObjectSetReasonRetrying`) STAY defined until Task 6 so downstream packages keep compiling. `ClusterObjectSetReasonArchived`/`ClusterObjectSetReasonBlocked` already exist — reuse, do not duplicate.

- [ ] **Step 1: Add the new type + reason constants.**

In `api/v1/clusterobjectset_types.go`, extend the `const` block (keep existing entries):

```go
const (
	ClusterObjectSetKind = "ClusterObjectSet"

	// Condition Types
	ClusterObjectSetTypeAvailable   = "Available"   // Deprecated: removed in Task 6, kept temporarily for compilation
	ClusterObjectSetTypeProgressing = "Progressing" // Deprecated: removed in Task 6, kept temporarily for compilation
	ClusterObjectSetTypeReady       = "Ready"

	// Ready condition Reasons
	ClusterObjectSetReasonReady                    = "Ready"
	ClusterObjectSetReasonIncomplete               = "Incomplete"
	ClusterObjectSetReasonBlocked                  = "Blocked"
	ClusterObjectSetReasonInvalid                  = "Invalid"
	ClusterObjectSetReasonArchived                 = "Archived"
	ClusterObjectSetReasonProgressDeadlineExceeded = "ProgressDeadlineExceeded"
	ClusterObjectSetReasonReconcileError           = "ReconcileError"
	ClusterObjectSetReasonTeardownError            = "TeardownError"
	ClusterObjectSetReasonInternalError            = "InternalError"

	// Deprecated reasons (removed in Task 6, kept temporarily for compilation)
	ClusterObjectSetReasonProbeFailure    = "ProbeFailure"
	ClusterObjectSetReasonProbesSucceeded = "ProbesSucceeded"
	ClusterObjectSetReasonReconciling     = "Reconciling"
	ClusterObjectSetReasonRetrying        = "Retrying"
)
```

- [ ] **Step 2: Rewrite the status `Conditions` doc comment** (replace lines 494-515 block describing Progressing/Available):

```go
	// conditions is an optional list of status conditions describing the state of the
	// ClusterObjectSet.
	//
	// The Ready condition represents whether the revision has fully rolled out:
	//   - True / Ready: all phases are complete and all objects pass their probes.
	//   - False / Incomplete: one or more phases are not yet complete.
	//   - False / Blocked: reconciliation requires manual intervention (e.g. a mutable
	//     referenced Secret, resolved-content drift, or an object ownership collision).
	//   - False / Invalid: a preflight validation error was encountered.
	//   - False / Archived: the revision is archived; teardown is in progress or complete.
	//   - False / ProgressDeadlineExceeded: the revision did not complete within
	//     spec.progressDeadlineMinutes.
	//   - Unknown / ReconcileError: reconciliation returned an error.
	//   - Unknown / TeardownError: teardown returned an error.
	//   - Unknown / InternalError: an internal controller error occurred.
```

- [ ] **Step 3: Replace the printcolumn markers** (lines 564-565) with:

```go
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].reason`
// +kubebuilder:printcolumn:name="Completed",type=string,JSONPath=`.status.completedAt`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
```

(`Completed` uses `type=string` so kubectl prints the RFC3339 timestamp, not a relative age.)

- [ ] **Step 4: Regenerate.**

Run: `make generate manifests crd-ref-docs`
Expected: only the experimental COS CRD (printer columns + conditions description) and `olmv1-api-reference.md` change; `zz_generated.deepcopy.go` and `applyconfigurations/**` are unchanged (no struct fields changed).

- [ ] **Step 5: Verify build + generated columns.**

Run:
```bash
go build -mod=mod ./...
grep -A1 'name: Ready' helm/olmv1/base/operator-controller/crd/experimental/olm.operatorframework.io_clusterobjectsets.yaml
grep 'completedAt' helm/olmv1/base/operator-controller/crd/experimental/olm.operatorframework.io_clusterobjectsets.yaml | grep -i jsonpath
```
Expected: build passes; the CRD `additionalPrinterColumns` include `Ready`, `Reason`, and a `Completed` column with `jsonPath: .status.completedAt` and `type: string`.

- [ ] **Step 6: Commit.**

```bash
git add api/v1/clusterobjectset_types.go helm/olmv1 manifests docs/api-reference
git commit -s -m "$(cat <<'EOF'
✨ Add ClusterObjectSet Ready condition constants and printer columns

Introduce the Ready condition type and its reason constants, add Ready/Reason/
Completed printer columns, and document the Ready state machine. Old Available/
Progressing constants remain temporarily to keep downstream packages compiling.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: `setReady` / `setReadyProgressing` helpers (+ unit tests)

**Files:**
- Modify: `internal/object-controller/controllers/clusterobjectset_controller.go` (add helpers; leave existing `markAs*` in place for now)
- Test: `internal/object-controller/controllers/clusterobjectset_controller_test.go` (append a focused test)

**Interfaces:**
- Consumes: `ocv1.ClusterObjectSetTypeReady`, reason constants (Task 1).
- Produces:
  - `func setReady(cos *ocv1.ClusterObjectSet, status metav1.ConditionStatus, reason, message string) bool`
  - `func setReadyProgressing(l logr.Logger, cos *ocv1.ClusterObjectSet, status metav1.ConditionStatus, reason, message string, isDeadlineExceeded bool) bool`
  - Both return the `meta.SetStatusCondition` "changed" bool. `setReadyProgressing`, when `isDeadlineExceeded` is true, overrides to `False`/`ProgressDeadlineExceeded`.

- [ ] **Step 1: Write the failing tests.**

Append to `clusterobjectset_controller_test.go`:

```go
func Test_setReady(t *testing.T) {
	cos := &ocv1.ClusterObjectSet{}
	cos.Generation = 7

	changed := setReady(cos, metav1.ConditionTrue, ocv1.ClusterObjectSetReasonReady, "all done")
	require.True(t, changed)

	got := meta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
	require.NotNil(t, got)
	require.Equal(t, metav1.ConditionTrue, got.Status)
	require.Equal(t, ocv1.ClusterObjectSetReasonReady, got.Reason)
	require.Equal(t, "all done", got.Message)
	require.Equal(t, int64(7), got.ObservedGeneration)
	// Ready is the only condition.
	require.Len(t, cos.Status.Conditions, 1)
}

func Test_setReadyProgressing(t *testing.T) {
	l := logr.Discard()
	t.Run("passes through when deadline not exceeded", func(t *testing.T) {
		cos := &ocv1.ClusterObjectSet{}
		setReadyProgressing(l, cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonIncomplete, "rolling out", false)
		got := meta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
		require.NotNil(t, got)
		require.Equal(t, metav1.ConditionFalse, got.Status)
		require.Equal(t, ocv1.ClusterObjectSetReasonIncomplete, got.Reason)
	})
	t.Run("overrides to ProgressDeadlineExceeded when deadline exceeded", func(t *testing.T) {
		cos := &ocv1.ClusterObjectSet{}
		cos.Spec.ProgressDeadlineMinutes = 10
		setReadyProgressing(l, cos, metav1.ConditionUnknown, ocv1.ClusterObjectSetReasonReconcileError, "boom", true)
		got := meta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
		require.NotNil(t, got)
		require.Equal(t, metav1.ConditionFalse, got.Status)
		require.Equal(t, ocv1.ClusterObjectSetReasonProgressDeadlineExceeded, got.Reason)
	})
}
```

- [ ] **Step 2: Run to verify failure.**

Run: `go test -mod=mod ./internal/object-controller/controllers/ -run 'Test_setReady|Test_setReadyProgressing' -v`
Expected: FAIL — `setReady`/`setReadyProgressing` undefined.

- [ ] **Step 3: Implement the helpers.**

Add to `clusterobjectset_controller.go`:

```go
// setReady writes the single Ready condition on the ClusterObjectSet.
func setReady(cos *ocv1.ClusterObjectSet, status metav1.ConditionStatus, reason, message string) bool {
	return meta.SetStatusCondition(&cos.Status.Conditions, metav1.Condition{
		Type:               ocv1.ClusterObjectSetTypeReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cos.Generation,
	})
}

// setReadyProgressing writes a not-yet-Ready state that is subject to the
// progress deadline. When the deadline is exceeded it overrides the given
// (status, reason) with False/ProgressDeadlineExceeded. Callers that must take
// precedence over the deadline (Blocked, Archived) call setReady directly.
func setReadyProgressing(l logr.Logger, cos *ocv1.ClusterObjectSet, status metav1.ConditionStatus, reason, message string, isDeadlineExceeded bool) bool {
	if isDeadlineExceeded {
		l.V(1).Info("progress deadline exceeded", "priorReason", reason)
		return setReady(cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonProgressDeadlineExceeded,
			fmt.Sprintf("Revision has not rolled out for %d minute(s). Last status: %s", cos.Spec.ProgressDeadlineMinutes, message))
	}
	return setReady(cos, status, reason, message)
}
```

- [ ] **Step 4: Run to verify pass.**

Run: `go test -mod=mod ./internal/object-controller/controllers/ -run 'Test_setReady|Test_setReadyProgressing' -v`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/object-controller/controllers/clusterobjectset_controller.go internal/object-controller/controllers/clusterobjectset_controller_test.go
git commit -s -m "$(cat <<'EOF'
✨ Add setReady/setReadyProgressing helpers for ClusterObjectSet

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Rewire the COS reconciler to publish only `Ready`

**Files:**
- Modify: `internal/object-controller/controllers/clusterobjectset_controller.go` (all `markAs*`/`setRetryingConditions` call sites + delete those helpers and `nonTerminalProgressingReasons`)
- Modify (tests): `internal/object-controller/controllers/clusterobjectset_controller_test.go`, `internal/object-controller/controllers/progress_deadline_test.go`

**Interfaces:**
- Consumes: `setReady`, `setReadyProgressing` (Task 2).
- Produces: the COS reconciler emits exactly one `Ready` condition. `markAsProgressing`, `markAsNotProgressing`, `markAsAvailable`, `markAsUnavailable`, `markAsAvailableUnknown`, `markAsArchived`, `setRetryingConditions`, and `nonTerminalProgressingReasons` no longer exist.

- [ ] **Step 1: Rewrite each reconcile exit point.** Apply this exact mapping in `clusterobjectset_controller.go`:

| Current line | Current call | Replace with |
| --- | --- | --- |
| 139 | `markAsNotProgressing(cos, ...Blocked, err.Error())` | `setReady(cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonBlocked, err.Error())` |
| 145 | `setRetryingConditions(l, cos, err.Error(), isDeadlineExceeded)` | `setReadyProgressing(l, cos, metav1.ConditionUnknown, ocv1.ClusterObjectSetReasonReconcileError, err.Error(), isDeadlineExceeded)` |
| 153 | `markAsNotProgressing(cos, ...Blocked, err.Error())` | `setReady(cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonBlocked, err.Error())` |
| 159 | `setRetryingConditions(...)` | `setReadyProgressing(l, cos, metav1.ConditionUnknown, ocv1.ClusterObjectSetReasonReconcileError, err.Error(), isDeadlineExceeded)` |
| 173 | `markAsAvailableUnknown(cos, ...Reconciling, err.Error())` | `setReady(cos, metav1.ConditionUnknown, ocv1.ClusterObjectSetReasonTeardownError, err.Error())` |
| 185 | `setRetryingConditions(l, cos, werr.Error(), isDeadlineExceeded)` | `setReadyProgressing(l, cos, metav1.ConditionUnknown, ocv1.ClusterObjectSetReasonReconcileError, werr.Error(), isDeadlineExceeded)` |
| 195 | `setRetryingConditions(...)` | `setReadyProgressing(l, cos, metav1.ConditionUnknown, ocv1.ClusterObjectSetReasonReconcileError, err.Error(), isDeadlineExceeded)` |
| 203 | `setRetryingConditions(l, cos, fmt.Sprintf("revision validation error: %s", verr), isDeadlineExceeded)` | `setReadyProgressing(l, cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonInvalid, fmt.Sprintf("revision validation error: %s", verr), isDeadlineExceeded)` |
| 210 | `setRetryingConditions(l, cos, fmt.Sprintf("phase %d validation error: %s", i, verr), isDeadlineExceeded)` | `setReadyProgressing(l, cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonInvalid, fmt.Sprintf("phase %d validation error: %s", i, verr), isDeadlineExceeded)` |
| 223 | `setRetryingConditions(l, cos, fmt.Sprintf("revision object collisions in phase %d\n%s", ...), isDeadlineExceeded)` | `setReady(cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonBlocked, fmt.Sprintf("revision object collisions in phase %d\n%s", i, strings.Join(collidingObjs, "\n\n")))` |
| 230 | `markAsProgressing(l, cos, ...RollingOut, ..., isDeadlineExceeded)` | delete this line (the IsComplete/else block below sets Ready) |
| 250-251 | `markAsProgressing(...Succeeded...)` + `markAsAvailable(...ProbesSucceeded...)` | `setReady(cos, metav1.ConditionTrue, ocv1.ClusterObjectSetReasonReady, fmt.Sprintf("Revision %s has rolled out.", revVersion))` |
| 288 | `markAsUnavailable(cos, ...ProbeFailure, strings.Join(probeFailureMsgs, "\n"))` | (handled in Step 2 — fold into single setReadyProgressing with the probe message) |
| 290 | `markAsUnavailable(cos, ...RollingOut, ...)` | (handled in Step 2) |
| 292 | `markAsProgressing(l, cos, ...RollingOut, ..., isDeadlineExceeded)` | (handled in Step 2) |
| 317 | `setRetryingConditions(l, cos, err.Error(), false)` | `setReady(cos, metav1.ConditionUnknown, ocv1.ClusterObjectSetReasonTeardownError, err.Error())` |
| 321 | `setRetryingConditions(l, cos, "removing revision resources...", false)` | `setReady(cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonArchived, "removing revision resources that are not owned by another revision")` |
| 325 | `if markAsArchived(cos) {` | `if setReady(cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonArchived, "revision is archived") {` |
| 303 | `markAsAvailableUnknown(cos, ...Reconciling, err.Error())` | `setReady(cos, metav1.ConditionUnknown, ocv1.ClusterObjectSetReasonTeardownError, err.Error())` |

- [ ] **Step 2: Collapse the not-complete `else` block (lines 260-296)** so probe failures fold into `Incomplete` while preserving the probe message. Replace the body of the `else` branch with:

```go
	} else {
		message := fmt.Sprintf("Revision %s is rolling out.", revVersion)
		var probeFailureMsgs []string
		for _, pres := range rres.GetPhases() {
			if pres.IsComplete() {
				continue
			}
			for _, ores := range pres.GetObjects() {
				pr := ores.ProbeResults()[boxcutter.ProgressProbeType]
				if pr.Status == machinerytypes.ProbeStatusTrue {
					continue
				}
				obj := ores.Object()
				gvk := obj.GetObjectKind().GroupVersionKind()
				probeFailureMsgs = append(probeFailureMsgs, fmt.Sprintf(
					"Object %s.%s %s/%s: %v",
					gvk.Kind, gvk.GroupVersion().String(),
					obj.GetNamespace(), obj.GetName(), strings.Join(pr.Messages, " and "),
				))
				break
			}
		}
		if len(probeFailureMsgs) > 0 {
			message = strings.Join(probeFailureMsgs, "\n")
		}
		setReadyProgressing(l, cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonIncomplete, message, isDeadlineExceeded)
		if hasDeadline && !isDeadlineExceeded {
			return ctrl.Result{RequeueAfter: remaining}, nil
		}
	}
```

- [ ] **Step 3: Delete dead helpers.** Remove `setRetryingConditions` (651-656), `nonTerminalProgressingReasons` (658-661), `markAsProgressing` (663-684), `markAsNotProgressing` (686-694), `markAsAvailable` (696-704), `markAsUnavailable` (706-714), `markAsAvailableUnknown` (716-724), `markAsArchived` (726-730). Remove now-unused imports if any (`logr` is still used by `setReadyProgressing`).

- [ ] **Step 4: Run the COS controller tests — expect failures to fix.**

Run: `go test -mod=mod ./internal/object-controller/controllers/ 2>&1 | head -40`
Expected: compile-level failures in the test file referencing removed helpers/old condition types — proceed to migrate them.

- [ ] **Step 5: Migrate assertions in `clusterobjectset_controller_test.go`.** Replace every `meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterObjectSetTypeAvailable|TypeProgressing|ocv1.TypeProgressing)` lookup with `ocv1.ClusterObjectSetTypeReady`, and update the expected `Status`/`Reason` per the Task 3 mapping. Concrete per-site substitutions (line numbers from the pre-edit file):

| Test line(s) | Old expectation | New expectation |
| --- | --- | --- |
| 85-100 (rollout complete) | `Available True / ProbesSucceeded` | `Ready True / Ready` |
| 112, 135, 223, 311, 417 (rolling out / probe fail) | `Available False / ProbeFailure` or `RollingOut` | `Ready False / Incomplete` |
| 334, 359, 380, 392 (`TypeProgressing`, RollingOut/Retrying) | `Progressing True / RollingOut` or `Retrying` | `Ready False / Incomplete` (RollingOut) or `Ready Unknown / ReconcileError` (Retrying) |
| 424, 794, 825, 859, 893, 926 (Progressing during archive/blocked) | `Progressing ... / Blocked|Archived|Retrying` | `Ready False / Blocked` or `Ready False / Archived` per site |
| 947-956 (archived: Available+Progressing Archived) | two conditions, both `Archived` | single `Ready False / Archived`; delete the second assertion |
| 1611 (collision) | `Progressing True / Retrying` | `Ready False / Blocked` |

For the paired archived assertion (947-956), collapse to one condition:

```go
			cond := meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
			require.NotNil(t, cond)
			require.Equal(t, metav1.ConditionFalse, cond.Status)
			require.Equal(t, ocv1.ClusterObjectSetReasonArchived, cond.Reason)
			require.Len(t, rev.Status.Conditions, 1)
```

- [ ] **Step 6: Migrate `progress_deadline_test.go`.** Update the deadline cases (lines 1049-1150 in the controller test are the `_Reconcile_ProgressDeadline` cases; `progress_deadline_test.go` covers the rate limiter). In `clusterobjectset_controller_test.go` `Test_..._ProgressDeadline`:
  - not-exceeded rolling case (1059) → `Ready False / Incomplete`
  - exceeded case (1084, 1097-1099) → `Ready False / ProgressDeadlineExceeded`
  - completed case (1115, 1130-1132) → `Ready True / Ready`
  - Add a **new** case pinning Review-Focus item "Deadline vs Blocked": a `Blocked` revision past deadline stays `Blocked`:

```go
		{
			name:  "blocked revision past deadline stays Blocked",
			// reuse the fixture that triggers markAsNotProgressing(Blocked): a mutable referenced Secret
			clock: clocktesting.NewFakeClock(time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)),
			checkResult: func(t *testing.T, rev *ocv1.ClusterObjectSet) {
				cnd := meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
				require.NotNil(t, cnd)
				require.Equal(t, metav1.ConditionFalse, cnd.Status)
				require.Equal(t, ocv1.ClusterObjectSetReasonBlocked, cnd.Reason)
			},
		},
```

  Wire this case using the existing "blocked" fixture pattern in `Test_..._RevisionReconciliation` (the mutable-Secret path that reaches line 139). If the deadline table test does not support a blocked fixture, add the assertion instead to the `RevisionReconciliation` test with a fake clock far past the deadline.

- [ ] **Step 7: Add a CompletedAt-latch regression assertion** (Review-Focus). In `Test_..._CompletedAt`, after `completedAt` is set, advance the clock past the deadline and reconcile again; assert `Ready` does NOT become `ProgressDeadlineExceeded`:

```go
	t.Run("completedAt latch: no ProgressDeadlineExceeded after ready", func(t *testing.T) {
		// after a successful rollout stamped completedAt, a later reconcile past the
		// deadline must keep Ready=True/Ready (durationUntilDeadline returns hasDeadline=false).
		// Reuse the CompletedAt fixture; advance clock beyond ProgressDeadlineMinutes.
		cnd := meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
		require.NotNil(t, cnd)
		require.Equal(t, metav1.ConditionTrue, cnd.Status)
		require.Equal(t, ocv1.ClusterObjectSetReasonReady, cnd.Reason)
	})
```

- [ ] **Step 8: Run all object-controller tests.**

Run: `go test -mod=mod ./internal/object-controller/...`
Expected: PASS.

- [ ] **Step 9: Commit.**

```bash
git add internal/object-controller/controllers/
git commit -s -m "$(cat <<'EOF'
⚠ Publish only the Ready condition from the ClusterObjectSet reconciler

Replace the Available/Progressing setters with setReady/setReadyProgressing at
every reconcile exit point. Probe failures fold into Ready=False/Incomplete
(message preserved); object collisions map to Blocked; deadline overrides all
not-Ready states except Blocked.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Derive the ClusterExtension `Installed` reason from `Ready`

**Files:**
- Modify: `internal/operator-controller/controllers/common_controller.go` (`determineFailureReason`, lines 91-109)
- Test: `internal/operator-controller/controllers/common_controller_test.go` (`TestSetInstalledStatusFromRevisionStates_ConfigValidationError`, table starting line 301)

**Interfaces:**
- Consumes: `ocv1.ClusterObjectSetTypeReady` and reason constants (Task 1).
- Produces: `determineFailureReason` reads the latest rolling revision's `Ready` condition.

- [ ] **Step 1: Update the failing tests first.** In `common_controller_test.go`, change the table cases (lines 320-429) that build `Conditions` with `ocv1.ClusterObjectSetTypeProgressing`:
  - "rolling revision with error (Retrying) - uses Failed" → condition `Type: ClusterObjectSetTypeReady, Status: ConditionUnknown, Reason: ClusterObjectSetReasonReconcileError`; still expect `ReasonFailed`.
  - "multiple rolling revisions with one Retrying - uses Failed" → last revision `Ready Unknown / ReconcileError`; earlier `Ready False / Incomplete`; expect `Failed`.
  - "rolling revision with RollingOut reason - uses Absent" → `Ready False / Incomplete`; expect `Absent`.
  - "old revision with Retrying superseded by latest healthy - uses Absent" → rev-1 `Ready Unknown / ReconcileError`, rev-2 `Ready False / Incomplete`; expect `Absent`.
  - Add a new case (Review-Focus): latest revision `Ready False / Invalid` → expect `Failed`; and `Ready False / Blocked` → expect `Failed`.

- [ ] **Step 2: Run to verify failure.**

Run: `go test -mod=mod ./internal/operator-controller/controllers/ -run TestSetInstalledStatusFromRevisionStates_ConfigValidationError -v`
Expected: FAIL (still keying off Progressing/Retrying).

- [ ] **Step 3: Implement.** Replace `determineFailureReason` body (lines 91-109):

```go
func determineFailureReason(rollingRevisions []*RevisionMetadata) string {
	if len(rollingRevisions) == 0 {
		return ocv1.ReasonFailed
	}
	// Only the LATEST rolling revision matters (sorted ascending by Spec.Revision).
	latest := rollingRevisions[len(rollingRevisions)-1]
	ready := apimeta.FindStatusCondition(latest.Conditions, ocv1.ClusterObjectSetTypeReady)
	if ready == nil {
		return ocv1.ReasonAbsent
	}
	switch {
	case ready.Status == metav1.ConditionUnknown:
		// ReconcileError / TeardownError / InternalError all indicate an error.
		return ocv1.ReasonFailed
	case ready.Status == metav1.ConditionFalse &&
		ready.Reason != ocv1.ClusterObjectSetReasonIncomplete:
		// Invalid, Blocked, ProgressDeadlineExceeded, Archived are error/terminal states.
		return ocv1.ReasonFailed
	default:
		// Incomplete (healthy rollout) or True → not an error yet.
		return ocv1.ReasonAbsent
	}
}
```

Ensure `metav1` is imported (`metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"`); `apimeta` alias already used in this package.

- [ ] **Step 4: Run to verify pass.**

Run: `go test -mod=mod ./internal/operator-controller/controllers/ -run TestSetInstalledStatusFromRevisionStates_ConfigValidationError -v`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/operator-controller/controllers/common_controller.go internal/operator-controller/controllers/common_controller_test.go
git commit -s -m "$(cat <<'EOF'
✨ Derive ClusterExtension Installed reason from ClusterObjectSet Ready

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: ClusterExtension condition translator (Available = retyped Ready; Progressing derived)

**Files:**
- Modify: `internal/operator-controller/controllers/boxcutter_reconcile_steps.go` (mirror block, lines 137-167; add translator helper + `ceAvailableConditionType` const)
- Modify: `api/v1/clusterextension_types.go` (`RevisionStatus.conditions` doc comment, line ~519)
- Test: `internal/operator-controller/controllers/boxcutter_reconcile_steps_apply_test.go` (and `clusterextension_controller_test.go` if it asserts mirrored conditions)
- Regenerate: `docs/api-reference/olmv1-api-reference.md`, `manifests/experimental*.yaml` (RevisionStatus doc)

**Interfaces:**
- Consumes: `ocv1.ClusterObjectSetTypeReady`, COS reason constants, CE reason constants from `common_types.go` (`ReasonSucceeded`, `ReasonRollingOut`, `ReasonRetrying`, `ReasonBlocked`, `ReasonInvalidConfiguration`, `ReasonProgressDeadlineExceeded`).
- Produces:
  - `const ceAvailableConditionType = "Available"` (package `controllers`)
  - `func ceProgressingFromReady(ready metav1.Condition) (metav1.ConditionStatus, string)`

- [ ] **Step 1: Write the failing test** for the translator. Append to `boxcutter_reconcile_steps_apply_test.go`:

```go
func Test_ceProgressingFromReady(t *testing.T) {
	cases := []struct {
		reason     string
		wantStatus metav1.ConditionStatus
		wantReason string
	}{
		{ocv1.ClusterObjectSetReasonReady, metav1.ConditionTrue, ocv1.ReasonSucceeded},
		{ocv1.ClusterObjectSetReasonIncomplete, metav1.ConditionTrue, ocv1.ReasonRollingOut},
		{ocv1.ClusterObjectSetReasonProgressDeadlineExceeded, metav1.ConditionFalse, ocv1.ReasonProgressDeadlineExceeded},
		{ocv1.ClusterObjectSetReasonBlocked, metav1.ConditionFalse, ocv1.ReasonBlocked},
		{ocv1.ClusterObjectSetReasonInvalid, metav1.ConditionFalse, ocv1.ReasonInvalidConfiguration},
		{ocv1.ClusterObjectSetReasonReconcileError, metav1.ConditionTrue, ocv1.ReasonRetrying},
		{ocv1.ClusterObjectSetReasonTeardownError, metav1.ConditionTrue, ocv1.ReasonRetrying},
		{ocv1.ClusterObjectSetReasonInternalError, metav1.ConditionTrue, ocv1.ReasonRetrying},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			gotStatus, gotReason := ceProgressingFromReady(metav1.Condition{
				Type:   ocv1.ClusterObjectSetTypeReady,
				Reason: tc.reason,
			})
			require.Equal(t, tc.wantStatus, gotStatus)
			require.Equal(t, tc.wantReason, gotReason)
		})
	}
}
```

- [ ] **Step 2: Run to verify failure.**

Run: `go test -mod=mod ./internal/operator-controller/controllers/ -run Test_ceProgressingFromReady -v`
Expected: FAIL — `ceProgressingFromReady` undefined.

- [ ] **Step 3: Implement the translator and rewrite the mirror block.** In `boxcutter_reconcile_steps.go`:

```go
const ceAvailableConditionType = "Available"

// ceProgressingFromReady maps a revision's Ready condition reason onto the
// ClusterExtension's Progressing (status, reason).
func ceProgressingFromReady(ready metav1.Condition) (metav1.ConditionStatus, string) {
	switch ready.Reason {
	case ocv1.ClusterObjectSetReasonReady:
		return metav1.ConditionTrue, ocv1.ReasonSucceeded
	case ocv1.ClusterObjectSetReasonIncomplete:
		return metav1.ConditionTrue, ocv1.ReasonRollingOut
	case ocv1.ClusterObjectSetReasonProgressDeadlineExceeded:
		return metav1.ConditionFalse, ocv1.ReasonProgressDeadlineExceeded
	case ocv1.ClusterObjectSetReasonBlocked:
		return metav1.ConditionFalse, ocv1.ReasonBlocked
	case ocv1.ClusterObjectSetReasonInvalid:
		return metav1.ConditionFalse, ocv1.ReasonInvalidConfiguration
	default: // ReconcileError, TeardownError, InternalError
		return metav1.ConditionTrue, ocv1.ReasonRetrying
	}
}

// ceConditionsFromReady returns the ClusterExtension Available (Ready retyped)
// and Progressing (derived) conditions for a revision's Ready condition.
func ceConditionsFromReady(ready metav1.Condition, generation int64) (available, progressing metav1.Condition) {
	available = ready
	available.Type = ceAvailableConditionType
	available.ObservedGeneration = generation

	ps, pr := ceProgressingFromReady(ready)
	progressing = metav1.Condition{
		Type:               ocv1.TypeProgressing,
		Status:             ps,
		Reason:             pr,
		Message:            ready.Message,
		ObservedGeneration: generation,
	}
	return available, progressing
}
```

Replace the mirror block (lines 137-167) with:

```go
	ext.Status.ActiveRevisions = []ocv1.RevisionStatus{}
	gen := ext.GetGeneration()
	if i := state.revisionStates.Installed; i != nil {
		if ready := apimeta.FindStatusCondition(i.Conditions, ocv1.ClusterObjectSetTypeReady); ready != nil {
			rs := ocv1.RevisionStatus{Name: i.RevisionName}
			r := *ready
			r.ObservedGeneration = gen
			apimeta.SetStatusCondition(&rs.Conditions, r)

			avail, prog := ceConditionsFromReady(*ready, gen)
			apimeta.SetStatusCondition(&ext.Status.Conditions, avail)
			apimeta.SetStatusCondition(&ext.Status.Conditions, prog)

			ext.Status.Install = &ocv1.ClusterExtensionInstallStatus{Bundle: i.BundleMetadata}
			ext.Status.ActiveRevisions = []ocv1.RevisionStatus{rs}
		} else {
			ext.Status.Install = &ocv1.ClusterExtensionInstallStatus{Bundle: i.BundleMetadata}
			ext.Status.ActiveRevisions = []ocv1.RevisionStatus{{Name: i.RevisionName}}
		}
	}
	for idx, rr := range state.revisionStates.RollingOut {
		rs := ocv1.RevisionStatus{Name: rr.RevisionName}
		if ready := apimeta.FindStatusCondition(rr.Conditions, ocv1.ClusterObjectSetTypeReady); ready != nil {
			r := *ready
			r.ObservedGeneration = gen
			apimeta.SetStatusCondition(&rs.Conditions, r)
			// The latest rolling revision drives the ClusterExtension Progressing condition.
			if idx == len(state.revisionStates.RollingOut)-1 {
				_, prog := ceConditionsFromReady(*ready, gen)
				apimeta.SetStatusCondition(&ext.Status.Conditions, prog)
			}
		}
		ext.Status.ActiveRevisions = append(ext.Status.ActiveRevisions, rs)
	}
```

- [ ] **Step 4: Update the `RevisionStatus.conditions` doc comment** in `api/v1/clusterextension_types.go` (line ~519):

```go
	// conditions optionally exposes the Ready condition of the revision, in case
	// when it is not yet marked as successfully installed (completedAt is not set).
	// Given that a ClusterExtension should remain available during upgrades, an observer may use these conditions
	// to get more insights about reasons for its current state.
```

- [ ] **Step 5: Add a handover test** (Review-Focus: multiple active revisions). Append to `boxcutter_reconcile_steps_apply_test.go` a case with BOTH an `Installed` revision (`Ready True/Ready`) and a later `RollingOut` revision (`Ready False/Incomplete`), then assert:

```go
	// CE Available comes from the installed revision (Ready=True/Ready retyped).
	avail := apimeta.FindStatusCondition(ext.Status.Conditions, "Available")
	require.NotNil(t, avail)
	require.Equal(t, metav1.ConditionTrue, avail.Status)
	require.Equal(t, ocv1.ClusterObjectSetReasonReady, avail.Reason)
	// CE Progressing comes from the LATEST rolling revision (Incomplete -> RollingOut).
	prog := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
	require.NotNil(t, prog)
	require.Equal(t, metav1.ConditionTrue, prog.Status)
	require.Equal(t, ocv1.ReasonRollingOut, prog.Reason)
	// Each active revision mirrors its own single Ready condition.
	require.Len(t, ext.Status.ActiveRevisions, 2)
```

- [ ] **Step 6: Find and update all operator-controller tests referencing old COS constants.**

Run: `grep -rln 'ClusterObjectSetTypeAvailable\|ClusterObjectSetTypeProgressing\|ClusterObjectSetReasonProbeFailure\|ClusterObjectSetReasonProbesSucceeded\|ClusterObjectSetReasonReconciling\|ClusterObjectSetReasonRetrying' internal/operator-controller/`
Expected match set to migrate: `boxcutter_reconcile_steps_apply_test.go`, `internal/operator-controller/applier/boxcutter_test.go`, and any `clusterextension_controller_test.go` cases. In each, replace mirrored `Available`/`Progressing` expectations with the retyped-`Ready` (CE Available) / derived-`Progressing` values per the §4b mapping.

- [ ] **Step 7: Run translator + apply tests.**

Run: `go test -mod=mod ./internal/operator-controller/... -run 'Test_ceProgressingFromReady|Boxcutter|ApplyBundle|RevisionStates' -v`
Expected: PASS.

- [ ] **Step 8: Regenerate docs/manifests for the RevisionStatus comment.**

Run: `make manifests crd-ref-docs`
Then: `go build -mod=mod ./...`
Expected: `manifests/experimental*.yaml` + `olmv1-api-reference.md` reflect the new RevisionStatus description; build passes.

- [ ] **Step 9: Commit.**

```bash
git add internal/operator-controller/ api/v1/clusterextension_types.go manifests docs/api-reference
git commit -s -m "$(cat <<'EOF'
✨ Derive ClusterExtension Available/Progressing from ClusterObjectSet Ready

CE Available is the revision's Ready condition retyped; CE Progressing is derived
from the Ready reason. Per-revision status now mirrors the single Ready condition.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Remove the deprecated Available/Progressing constants

**Files:**
- Modify: `api/v1/clusterobjectset_types.go` (delete the temporary constants)

**Interfaces:**
- Consumes: nothing new.
- Produces: `ClusterObjectSetTypeAvailable`, `ClusterObjectSetTypeProgressing`, `ClusterObjectSetReasonProbeFailure`, `ClusterObjectSetReasonProbesSucceeded`, `ClusterObjectSetReasonReconciling`, `ClusterObjectSetReasonRetrying` no longer exist.

- [ ] **Step 1: Confirm no remaining Go references.**

Run: `grep -rn "ClusterObjectSetTypeAvailable\|ClusterObjectSetTypeProgressing\|ClusterObjectSetReasonProbeFailure\|ClusterObjectSetReasonProbesSucceeded\|ClusterObjectSetReasonReconciling\|ClusterObjectSetReasonRetrying" --include=*.go .`
Expected: matches ONLY in `api/v1/clusterobjectset_types.go` (the definitions). If any other file matches, fix it before deleting (it belongs to Tasks 3-5).

- [ ] **Step 2: Delete the temporary constants** from the const block in `clusterobjectset_types.go` (the lines marked "removed in Task 6"). Keep `ClusterObjectSetReasonArchived` and `ClusterObjectSetReasonBlocked` (still used).

- [ ] **Step 3: Build + full unit tests.**

Run:
```bash
go build -mod=mod ./...
make test-unit
```
Expected: PASS.

- [ ] **Step 4: Verify generated code is in sync.**

Run: `make verify`
Expected: no diff.

- [ ] **Step 5: Commit.**

```bash
git add api/v1/clusterobjectset_types.go
git commit -s -m "$(cat <<'EOF'
⚠ Remove ClusterObjectSet Available/Progressing condition constants

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Update e2e feature files and archival step

**Files:**
- Modify: `test/e2e/steps/steps.go` (`ClusterObjectSetIsArchived`, line 975-978)
- Modify: `test/e2e/features/revision.feature`, `update.feature`, `status.feature`, `install.feature`

**Interfaces:** none (test data + one helper). No unit-test cycle — validated by build + `test-experimental-e2e` (not run here unless a cluster is available).

- [ ] **Step 1: Update `ClusterObjectSetIsArchived`** (steps.go:975-978):

```go
// ClusterObjectSetIsArchived waits for the named ClusterObjectSet to have Ready=False
// with reason Archived.
func ClusterObjectSetIsArchived(ctx context.Context, revisionName string) error {
	return waitForCondition(ctx, "clusterobjectset", substituteScenarioVars(revisionName, scenarioCtx(ctx)), "Ready", "False", ptr.To("Archived"), nil)
}
```

- [ ] **Step 2: Rewrite COS assertions in feature files.** Apply these exact edits (each `ClusterObjectSet "X" reports ...`):

  `revision.feature`:
  - `:63`, `:208`: `reports Available as False with Reason ProbeFailure and Message:` → `reports Ready as False with Reason Incomplete and Message:`
  - `:150`+`:151`, `:326`+`:327`, `:423`+`:424`, `:519`+`:520`, `:578`, `:710`: replace the `Progressing as True with Reason Succeeded` line with `reports Ready as True with Reason Ready`, and DELETE the paired `Available as True with Reason ProbesSucceeded` line.
  - `:471`: `reports Progressing as False with Reason Blocked and Message:` → `reports Ready as False with Reason Blocked and Message:`
  - `:548`: `reports Progressing as False with Reason Blocked and Message includes:` → `reports Ready as False with Reason Blocked and Message includes:`
  - `:640`, `:709`: `reports Progressing as False with Reason ProgressDeadlineExceeded` → `reports Ready as False with Reason ProgressDeadlineExceeded`

  `update.feature`:
  - `:318`+`:319`: replace `Progressing as True with Reason Succeeded` with `Ready as True with Reason Ready`; delete the `Available as True with Reason ProbesSucceeded` line.
  - `:347`+`:348`: replace `Progressing as True with Reason RollingOut` + `Available as False with Reason ProbeFailure` with a single `Ready as False with Reason Incomplete`.

  `status.feature`:
  - `:37`, `:43`: `ClusterObjectSet "..." reports Available as False with Reason ProbeFailure` → `... reports Ready as False with Reason Incomplete`
  - `:46`: `... reports Available as True with Reason ProbesSucceeded` → `... reports Ready as True with Reason Ready`
  - `:36`, `:42`: `ClusterExtension reports Available as False with Reason ProbeFailure` → `ClusterExtension reports Available as False with Reason Incomplete` (CE Available now mirrors the Ready reason)

  `install.feature`:
  - `:373`, `:407`: `ClusterObjectSet "..." reports Progressing as False with Reason ProgressDeadlineExceeded` → `... reports Ready as False with Reason ProgressDeadlineExceeded`

- [ ] **Step 3: Verify no stale COS Available/Progressing assertions remain.**

Run: `grep -rn 'ClusterObjectSet.*reports \(Available\|Progressing\)' test/e2e/features/`
Expected: no matches.

- [ ] **Step 4: Build the e2e package.**

Run: `go build -mod=mod -tags containers_image_openpgp ./test/e2e/...`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add test/e2e/
git commit -s -m "$(cat <<'EOF'
🌱 Update e2e feature files for ClusterObjectSet Ready condition

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Update hand-written docs

**Files:**
- Modify: `docs/draft/concepts/clusterobjectsets.md` (Progressing/Available section, ~lines 156-168)
- Modify: `docs/draft/concepts/large-bundle-support.md` (narrative refs, lines 153, 400)

**Interfaces:** none (prose).

- [ ] **Step 1: Rewrite the status-conditions section** in `clusterobjectsets.md` to a single `Ready` table matching the spec:

```markdown
### Status Conditions

The `ClusterObjectSet` exposes a single `Ready` status condition:

| Status  | Reason                   | When                                                    |
| ------- | ------------------------ | ------------------------------------------------------- |
| True    | Ready                    | All phases are complete                                 |
| False   | Incomplete               | One or more phases are not yet complete                 |
| False   | Blocked                  | Reconciliation requires manual intervention            |
| False   | Invalid                  | A preflight validation error was encountered            |
| False   | Archived                 | Teardown is in progress or complete                     |
| False   | ProgressDeadlineExceeded | The revision did not complete within the deadline       |
| Unknown | ReconcileError           | Reconciliation returned an error                        |
| Unknown | TeardownError            | Teardown returned an error                              |
| Unknown | InternalError            | An internal controller error occurred                   |
```

- [ ] **Step 2: Fix `large-bundle-support.md` refs** (lines 153, 400): change any `Progressing=False, Reason=Blocked` phrasing to `Ready=False, Reason=Blocked`.

- [ ] **Step 3: Sanity-check for stale references.**

Run: `grep -rn 'Progressing\|Available' docs/draft/concepts/clusterobjectsets.md docs/draft/concepts/large-bundle-support.md`
Expected: no remaining COS `Progressing`/`Available` condition references (ClusterExtension mentions are fine).

- [ ] **Step 4: Commit.**

```bash
git add docs/draft/concepts/
git commit -s -m "$(cat <<'EOF'
📖 Document the ClusterObjectSet Ready condition

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

## Final verification

- [ ] Run: `make generate manifests crd-ref-docs && make verify` — Expected: no diff.
- [ ] Run: `make lint` — Expected: PASS.
- [ ] Run: `make test-unit` — Expected: PASS.
- [ ] Run: `make lint-api-diff` — Expected: flags the COS condition removal as a breaking change (expected for the ⚠ experimental change).
- [ ] (If a cluster is available) Run: `make test-experimental-e2e` — Expected: PASS.
