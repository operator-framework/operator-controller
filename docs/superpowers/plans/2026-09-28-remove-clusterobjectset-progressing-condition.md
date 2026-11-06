# Remove ClusterObjectSet `Progressing` Condition — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the `Progressing` condition type from `ClusterObjectSet`, folding its states into an expanded `Available` reason set, while the operator-controller reconstructs the `ClusterExtension` `Progressing` condition byte-for-byte from `Available` + `completedAt`.

**Architecture:** ClusterObjectSet (COS) publishes only `Available` (health, with an expanded reason set) plus the existing `status.completedAt` latch. The operator-controller stops mirroring a COS `Progressing` condition and instead derives the ClusterExtension (CE) `Progressing` condition via a new pure helper `progressingFromAvailable`. COS keeps its deadline and block-detection logic internally, now expressed through `Available`.

**Tech Stack:** Go, controller-runtime, `k8s.io/apimachinery` meta conditions, kubebuilder markers, gomock/testify, godog (e2e). Experimental channel only.

**Spec:** `docs/superpowers/specs/2026-09-28-remove-clusterobjectset-progressing-condition-design.md`

## Global Constraints

- Experimental channel only — `ClusterObjectSet` has no standard-channel CRD/manifest. Do not touch `crd/standard/**` or `manifests/standard*.yaml`.
- The CE public contract is frozen: CE `Progressing` must keep the same `(status, reason)` tuples — `True/Succeeded`, `True/RollingOut`, `True/Retrying`, `False/Blocked`, `False/ProgressDeadlineExceeded`. CE `Installed` unchanged.
- No new API condition-reason constants are required: reuse existing `ocv1.ReasonRollingOut` ("RollingOut"), `ocv1.ReasonRetrying` ("Retrying"), `ocv1.ReasonBlocked` ("Blocked"), `ocv1.ReasonProgressDeadlineExceeded` ("ProgressDeadlineExceeded"), `ocv1.ReasonSucceeded` ("Succeeded"), and COS `ClusterObjectSetReasonBlocked`/`ClusterObjectSetReasonReconciling`/`ClusterObjectSetReasonProbeFailure`/`ClusterObjectSetReasonProbesSucceeded`/`ClusterObjectSetReasonArchived`.
- Generated files (`zz_generated.deepcopy.go`, `applyconfigurations/**`, `crd/experimental/**`, `manifests/experimental*.yaml`, `docs/api-reference/olmv1-api-reference.md`) are produced by `make`, never hand-edited.
- All shell used in steps must work on macOS and Linux.
- Build tag for experimental code paths: files under `internal/object-controller/**` and `progress_deadline.go` carry `//go:build !standard`. Run object-controller tests without the `standard` tag.

## Review Focus

- **First-reconcile retry with no prior `Available`:** COS used to set `Available=Unknown/Reconciling` only if `Available` already existed; the retry signal must now always be set so the CE can show `Progressing=True/Retrying` on the very first failing reconcile. → Task 2 test.
- **Deadline exceeded while probes are failing:** `Available` must become `False/ProgressDeadlineExceeded` (not `False/ProbeFailure`) so the CE reconstructs `Progressing=False/ProgressDeadlineExceeded`. → Task 2 test.
- **`determineFailureReason` on a `Blocked` latest revision:** a latest rolling revision with `Available` reason `Blocked` must yield CE `Installed` reason `Absent` (not `Failed`), preserving today's behavior where only the retry reason mapped to `Failed`. → Task 3 test.
- **Latest-rolling-revision selection with multiple rolling revisions:** only the last (highest `Spec.Revision`) rolling revision drives top-level CE `Progressing`. → Task 3 test.
- **Reconstruction when a rolling revision has no `Available` condition (nil):** must default to `Progressing=True/RollingOut`, never panic. → Task 1 test.

---

## Task 1: Add the `progressingFromAvailable` reconstruction helper

**Files:**
- Modify: `internal/operator-controller/controllers/common_controller.go`
- Test: `internal/operator-controller/controllers/common_controller_test.go`

**Interfaces:**
- Consumes: `ocv1` condition type/reason constants; `metav1.Condition`.
- Produces: `func progressingFromAvailable(available *metav1.Condition, completed bool) metav1.Condition` — returns a `metav1.Condition` with `Type == ocv1.TypeProgressing` and no `ObservedGeneration` set (callers set it). Used by Task 3.

- [ ] **Step 1: Write the failing test**

Add to `internal/operator-controller/controllers/common_controller_test.go`:

```go
func TestProgressingFromAvailable(t *testing.T) {
	for _, tc := range []struct {
		name      string
		available *metav1.Condition
		completed bool
		expected  metav1.Condition
	}{
		{
			name:      "completed revision maps to Succeeded",
			available: &metav1.Condition{Type: ocv1.ClusterObjectSetTypeAvailable, Status: metav1.ConditionTrue, Reason: ocv1.ClusterObjectSetReasonProbesSucceeded, Message: "ok"},
			completed: true,
			expected:  metav1.Condition{Type: ocv1.TypeProgressing, Status: metav1.ConditionTrue, Reason: ocv1.ReasonSucceeded, Message: "Desired state reached"},
		},
		{
			name:      "nil available on rolling revision defaults to RollingOut",
			available: nil,
			completed: false,
			expected:  metav1.Condition{Type: ocv1.TypeProgressing, Status: metav1.ConditionTrue, Reason: ocv1.ReasonRollingOut, Message: "Revision is rolling out."},
		},
		{
			name:      "probe failure maps to RollingOut",
			available: &metav1.Condition{Type: ocv1.ClusterObjectSetTypeAvailable, Status: metav1.ConditionFalse, Reason: ocv1.ClusterObjectSetReasonProbeFailure, Message: "probe failed"},
			completed: false,
			expected:  metav1.Condition{Type: ocv1.TypeProgressing, Status: metav1.ConditionTrue, Reason: ocv1.ReasonRollingOut, Message: "probe failed"},
		},
		{
			name:      "reconciling maps to Retrying",
			available: &metav1.Condition{Type: ocv1.ClusterObjectSetTypeAvailable, Status: metav1.ConditionUnknown, Reason: ocv1.ClusterObjectSetReasonReconciling, Message: "boom"},
			completed: false,
			expected:  metav1.Condition{Type: ocv1.TypeProgressing, Status: metav1.ConditionTrue, Reason: ocv1.ReasonRetrying, Message: "boom"},
		},
		{
			name:      "blocked maps to Blocked",
			available: &metav1.Condition{Type: ocv1.ClusterObjectSetTypeAvailable, Status: metav1.ConditionFalse, Reason: ocv1.ClusterObjectSetReasonBlocked, Message: "manual fix needed"},
			completed: false,
			expected:  metav1.Condition{Type: ocv1.TypeProgressing, Status: metav1.ConditionFalse, Reason: ocv1.ReasonBlocked, Message: "manual fix needed"},
		},
		{
			name:      "deadline exceeded maps to ProgressDeadlineExceeded",
			available: &metav1.Condition{Type: ocv1.ClusterObjectSetTypeAvailable, Status: metav1.ConditionFalse, Reason: ocv1.ReasonProgressDeadlineExceeded, Message: "too slow"},
			completed: false,
			expected:  metav1.Condition{Type: ocv1.TypeProgressing, Status: metav1.ConditionFalse, Reason: ocv1.ReasonProgressDeadlineExceeded, Message: "too slow"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := progressingFromAvailable(tc.available, tc.completed)
			require.Equal(t, tc.expected.Type, got.Type)
			require.Equal(t, tc.expected.Status, got.Status)
			require.Equal(t, tc.expected.Reason, got.Reason)
			require.Equal(t, tc.expected.Message, got.Message)
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/operator-controller/controllers/ -run TestProgressingFromAvailable -v`
Expected: FAIL — `undefined: progressingFromAvailable`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/operator-controller/controllers/common_controller.go`:

```go
// progressingFromAvailable reconstructs the ClusterExtension Progressing condition
// for a single revision from that revision's ClusterObjectSet Available condition
// and whether the revision has completed its rollout (status.completedAt set).
//
// This replaces the previous behavior of mirroring the ClusterObjectSet Progressing
// condition, which no longer exists. ObservedGeneration is left unset; callers set it.
func progressingFromAvailable(available *metav1.Condition, completed bool) metav1.Condition {
	cond := metav1.Condition{
		Type:   ocv1.TypeProgressing,
		Status: metav1.ConditionTrue,
		Reason: ocv1.ReasonRollingOut,
	}
	if completed {
		cond.Reason = ocv1.ReasonSucceeded
		cond.Message = "Desired state reached"
		return cond
	}
	if available == nil {
		cond.Message = "Revision is rolling out."
		return cond
	}
	cond.Message = available.Message
	switch available.Reason {
	case ocv1.ClusterObjectSetReasonBlocked:
		cond.Status = metav1.ConditionFalse
		cond.Reason = ocv1.ReasonBlocked
	case ocv1.ReasonProgressDeadlineExceeded:
		cond.Status = metav1.ConditionFalse
		cond.Reason = ocv1.ReasonProgressDeadlineExceeded
	case ocv1.ClusterObjectSetReasonReconciling:
		cond.Reason = ocv1.ReasonRetrying
	default:
		// ProbeFailure, RollingOut, or ProbesSucceeded-but-not-yet-complete.
		cond.Reason = ocv1.ReasonRollingOut
	}
	return cond
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/operator-controller/controllers/ -run TestProgressingFromAvailable -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/operator-controller/controllers/common_controller.go internal/operator-controller/controllers/common_controller_test.go
git commit -s -m "✨ add progressingFromAvailable reconstruction helper"
```

---

## Task 2: Express ClusterObjectSet state via `Available` (drop internal `Progressing` writes)

**Files:**
- Modify: `internal/object-controller/controllers/clusterobjectset_controller.go`
- Test: `internal/object-controller/controllers/clusterobjectset_controller_test.go`
- Test: `internal/object-controller/controllers/clusterobjectset_controller_internal_test.go`
- Test: `internal/object-controller/controllers/progress_deadline_test.go` (only if it asserts on `Progressing`)

**Interfaces:**
- Consumes: existing `ocv1` reason constants; `durationUntilDeadline` from `progress_deadline.go` (unchanged).
- Produces: COS reconcile now only ever writes the `Available` condition (+ `completedAt`). New helper `setAvailableWithDeadline(cos *ocv1.ClusterObjectSet, status metav1.ConditionStatus, reason, message string, isDeadlineExceeded bool)`; `setRetryingConditions(cos *ocv1.ClusterObjectSet, message string, isDeadlineExceeded bool)` (drops the `logr.Logger` parameter). Functions `markAsProgressing`, `markAsNotProgressing`, and the `nonTerminalProgressingReasons` map are deleted.

- [ ] **Step 1: Write the failing tests**

In `internal/object-controller/controllers/clusterobjectset_controller_test.go`, add two focused reconcile tests (adapt setup helpers already used by the neighboring tests in this file — reuse the same fake client / reconciler builder the existing `Test_..._Reconcile*` tests use):

```go
func Test_ClusterObjectSet_Reconcile_FirstReconcileRetrySetsAvailableReconciling(t *testing.T) {
	// A revision whose FIRST reconcile fails (e.g. buildBoxcutterPhases error) must
	// set Available=Unknown/Reconciling even though Available did not previously exist.
	// Arrange a reconciler whose phase build / engine creation returns an error,
	// reconcile once, then assert:
	cos := /* freshly created COS with no conditions */ nil
	_ = cos
	// after reconcile:
	// avail := meta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeAvailable)
	// require.NotNil(t, avail)
	// require.Equal(t, metav1.ConditionUnknown, avail.Status)
	// require.Equal(t, ocv1.ClusterObjectSetReasonReconciling, avail.Reason)
	// require.Nil(t, meta.FindStatusCondition(cos.Status.Conditions, "Progressing"))
	t.Skip("replace scaffold with concrete arrange/act using this file's existing reconciler test harness")
}

func Test_ClusterObjectSet_Reconcile_DeadlineExceededDuringProbeFailureSetsAvailableDeadline(t *testing.T) {
	// A rolling revision with failing probes AND an exceeded progress deadline must
	// set Available=False/ProgressDeadlineExceeded (deadline wins over ProbeFailure).
	// Arrange spec.progressDeadlineMinutes small + creationTimestamp in the past + probe failure,
	// reconcile, then assert:
	// avail := meta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeAvailable)
	// require.Equal(t, metav1.ConditionFalse, avail.Status)
	// require.Equal(t, ocv1.ReasonProgressDeadlineExceeded, avail.Reason)
	t.Skip("replace scaffold with concrete arrange/act using this file's existing reconciler test harness")
}
```

Also migrate every existing assertion in these test files from the `Progressing` condition to `Available`, using this translation table (old COS `Progressing` → new COS `Available`):

| Old `Progressing` `(status/reason)` | New `Available` `(status/reason)` |
|---|---|
| `True/RollingOut` | `False/RollingOut` |
| `True/Retrying` | `Unknown/Reconciling` |
| `True/Succeeded` | `True/ProbesSucceeded` (plus `completedAt` set) |
| `False/Blocked` | `False/Blocked` |
| `False/ProgressDeadlineExceeded` | `False/ProgressDeadlineExceeded` |
| `False/Archived` | `Unknown/Archived` |

Concrete example — the archived test (`clusterobjectset_controller_test.go` around L794-825) changes from:

```go
prog := meta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeProgressing)
require.Equal(t, metav1.ConditionFalse, prog.Status)
require.Equal(t, ocv1.ClusterObjectSetReasonArchived, prog.Reason)
```

to:

```go
avail := meta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeAvailable)
require.Equal(t, metav1.ConditionUnknown, avail.Status)
require.Equal(t, ocv1.ClusterObjectSetReasonArchived, avail.Reason)
require.Nil(t, meta.FindStatusCondition(cos.Status.Conditions, "Progressing"))
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/object-controller/controllers/ -run Test_ClusterObjectSet_Reconcile -v`
Expected: FAIL — new assertions reference `Available` states the controller does not yet produce (and it still writes `Progressing`).

- [ ] **Step 3: Implement — rewrite the condition helpers**

In `internal/object-controller/controllers/clusterobjectset_controller.go`:

Delete `markAsProgressing`, `markAsNotProgressing`, and `var nonTerminalProgressingReasons`. Add:

```go
// setAvailableWithDeadline sets the Available condition to the supplied status/reason/message,
// unless the progress deadline has been exceeded — in which case it reports
// Available=False/ProgressDeadlineExceeded instead. This centralises the deadline
// enforcement that previously lived in markAsProgressing.
func setAvailableWithDeadline(cos *ocv1.ClusterObjectSet, status metav1.ConditionStatus, reason, message string, isDeadlineExceeded bool) {
	if isDeadlineExceeded {
		markAsUnavailable(cos, ocv1.ReasonProgressDeadlineExceeded,
			fmt.Sprintf("Revision has not rolled out for %d minute(s). Last status: %s", cos.Spec.ProgressDeadlineMinutes, message))
		return
	}
	meta.SetStatusCondition(&cos.Status.Conditions, metav1.Condition{
		Type:               ocv1.ClusterObjectSetTypeAvailable,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cos.Generation,
	})
}
```

Rewrite `setRetryingConditions` (drop the `logr.Logger` param; always set Available):

```go
func setRetryingConditions(cos *ocv1.ClusterObjectSet, message string, isDeadlineExceeded bool) {
	setAvailableWithDeadline(cos, metav1.ConditionUnknown, ocv1.ClusterObjectSetReasonReconciling, message, isDeadlineExceeded)
}
```

Rewrite `markAsArchived` to only touch Available:

```go
func markAsArchived(cos *ocv1.ClusterObjectSet) bool {
	const msg = "revision is archived"
	return markAsAvailableUnknown(cos, ocv1.ClusterObjectSetReasonArchived, msg)
}
```

- [ ] **Step 4: Implement — rewire the reconcile call sites**

In `func (c *ClusterObjectSetReconciler) reconcile(...)`:

- Blocked (secret immutability, ~L138) and Blocked (phase digest, ~L152): replace `markAsNotProgressing(cos, ocv1.ClusterObjectSetReasonBlocked, err.Error())` with:
  ```go
  markAsUnavailable(cos, ocv1.ClusterObjectSetReasonBlocked, err.Error())
  ```
- Every `setRetryingConditions(l, cos, msg, isDeadlineExceeded)` call (~L144, L158, L184, L194, L202, L209, L222) drops the `l` argument:
  ```go
  setRetryingConditions(cos, msg, isDeadlineExceeded)
  ```
- InTransition branch (~L229): replace `markAsProgressing(l, cos, ocv1.ReasonRollingOut, msg, isDeadlineExceeded)` with:
  ```go
  setAvailableWithDeadline(cos, metav1.ConditionFalse, ocv1.ReasonRollingOut, fmt.Sprintf("Revision %s is rolling out.", revVersion), isDeadlineExceeded)
  ```
- Complete branch (~L249-250): delete the `markAsProgressing(l, cos, ocv1.ReasonSucceeded, ...)` line. Keep `markAsAvailable(cos, ocv1.ClusterObjectSetReasonProbesSucceeded, "Objects are available and pass all probes.")` and the `completedAt` assignment unchanged.
- Not-complete else branch (~L287-291): replace the two `markAsUnavailable(...)` calls and delete the trailing `markAsProgressing(...)` line so deadline enforcement applies to the Available write:
  ```go
  if len(probeFailureMsgs) > 0 {
      setAvailableWithDeadline(cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonProbeFailure, strings.Join(probeFailureMsgs, "\n"), isDeadlineExceeded)
  } else {
      setAvailableWithDeadline(cos, metav1.ConditionFalse, ocv1.ReasonRollingOut, fmt.Sprintf("Revision %s is rolling out.", revVersion), isDeadlineExceeded)
  }
  ```
- In the archive/delete paths that call `markAsAvailableUnknown(cos, ocv1.ClusterObjectSetReasonReconciling, ...)` (~L172, L302): leave unchanged.

Fill in the two scaffolded tests from Step 1 with concrete arrange/act (remove the `t.Skip`), matching this file's existing reconciler harness.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/object-controller/controllers/ -v`
Expected: PASS (all migrated + new tests).

- [ ] **Step 6: Build check (unused const is fine; unused imports are not)**

Run: `go build ./internal/object-controller/...`
Expected: builds. If `logr` becomes unused in the file, remove its import; if still used elsewhere, leave it.

- [ ] **Step 7: Commit**

```bash
git add internal/object-controller/controllers/
git commit -s -m "✨ express ClusterObjectSet rollout state via Available condition"
```

---

## Task 3: Reconstruct CE `Progressing` from COS `Available` in the operator-controller

**Files:**
- Modify: `internal/operator-controller/controllers/boxcutter_reconcile_steps.go:137-170`
- Modify: `internal/operator-controller/controllers/common_controller.go` (`determineFailureReason`, ~L91-109)
- Test: `internal/operator-controller/controllers/common_controller_test.go`

**Interfaces:**
- Consumes: `progressingFromAvailable` (Task 1); `RevisionStates`/`RevisionMetadata`.
- Produces: CE `Progressing` derived from `Available`; `determineFailureReason` reads `Available` instead of `Progressing`.

- [ ] **Step 1: Write the failing test for `determineFailureReason`**

Add to `internal/operator-controller/controllers/common_controller_test.go`:

```go
func TestDetermineFailureReason(t *testing.T) {
	availCond := func(status metav1.ConditionStatus, reason string) []metav1.Condition {
		return []metav1.Condition{{Type: ocv1.ClusterObjectSetTypeAvailable, Status: status, Reason: reason}}
	}
	for _, tc := range []struct {
		name     string
		rolling  []*RevisionMetadata
		expected string
	}{
		{name: "no rolling revisions -> Failed", rolling: nil, expected: ocv1.ReasonFailed},
		{
			name:     "latest reconciling -> Failed",
			rolling:  []*RevisionMetadata{{Conditions: availCond(metav1.ConditionUnknown, ocv1.ClusterObjectSetReasonReconciling)}},
			expected: ocv1.ReasonFailed,
		},
		{
			name:     "latest rolling out -> Absent",
			rolling:  []*RevisionMetadata{{Conditions: availCond(metav1.ConditionFalse, ocv1.ReasonRollingOut)}},
			expected: ocv1.ReasonAbsent,
		},
		{
			name:     "latest blocked -> Absent (Blocked is terminal, handled elsewhere)",
			rolling:  []*RevisionMetadata{{Conditions: availCond(metav1.ConditionFalse, ocv1.ClusterObjectSetReasonBlocked)}},
			expected: ocv1.ReasonAbsent,
		},
		{
			name: "only the latest revision matters",
			rolling: []*RevisionMetadata{
				{Conditions: availCond(metav1.ConditionUnknown, ocv1.ClusterObjectSetReasonReconciling)},
				{Conditions: availCond(metav1.ConditionFalse, ocv1.ReasonRollingOut)},
			},
			expected: ocv1.ReasonAbsent,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, determineFailureReason(tc.rolling))
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/operator-controller/controllers/ -run TestDetermineFailureReason -v`
Expected: FAIL — `determineFailureReason` still reads the `Progressing` condition, so the `Reconciling`/`Blocked` cases return the wrong reason.

- [ ] **Step 3: Implement — `determineFailureReason` reads `Available`**

Replace the body of `determineFailureReason` in `common_controller.go`:

```go
func determineFailureReason(rollingRevisions []*RevisionMetadata) string {
	if len(rollingRevisions) == 0 {
		return ocv1.ReasonFailed
	}
	// Latest revision is the last element (sorted ascending by Spec.Revision).
	latestRevision := rollingRevisions[len(rollingRevisions)-1]
	availableCond := apimeta.FindStatusCondition(latestRevision.Conditions, ocv1.ClusterObjectSetTypeAvailable)
	// Reconciling is the new home of the old Retrying signal: it indicates an error occurred.
	if availableCond != nil && availableCond.Reason == ocv1.ClusterObjectSetReasonReconciling {
		return ocv1.ReasonFailed
	}
	return ocv1.ReasonAbsent
}
```

Update the doc comment above the function to refer to the `Available`/`Reconciling` signal instead of `Progressing`/`Retrying`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/operator-controller/controllers/ -run TestDetermineFailureReason -v`
Expected: PASS.

- [ ] **Step 5: Implement — rewrite the mirror in `ApplyBundleWithBoxcutter`**

Replace the block `internal/operator-controller/controllers/boxcutter_reconcile_steps.go:137-167` (from `ext.Status.ActiveRevisions = []ocv1.RevisionStatus{}` through the end of the `for idx, r := range ...` loop) with:

```go
			ext.Status.ActiveRevisions = []ocv1.RevisionStatus{}
			// Installed revision: mirror Available (health) and reconstruct Progressing.
			if i := state.revisionStates.Installed; i != nil {
				avail := apimeta.FindStatusCondition(i.Conditions, ocv1.ClusterObjectSetTypeAvailable)
				if avail != nil {
					a := *avail
					a.ObservedGeneration = ext.GetGeneration()
					apimeta.SetStatusCondition(&ext.Status.Conditions, a)
				}
				prog := progressingFromAvailable(avail, true)
				prog.ObservedGeneration = ext.GetGeneration()
				apimeta.SetStatusCondition(&ext.Status.Conditions, prog)

				ext.Status.Install = &ocv1.ClusterExtensionInstallStatus{
					Bundle: i.BundleMetadata,
				}
				ext.Status.ActiveRevisions = []ocv1.RevisionStatus{{Name: i.RevisionName}}
			}
			// Rolling revisions: mirror Available and reconstruct Progressing per revision;
			// the latest rolling revision also drives top-level Progressing.
			for idx, r := range state.revisionStates.RollingOut {
				rs := ocv1.RevisionStatus{Name: r.RevisionName}
				avail := apimeta.FindStatusCondition(r.Conditions, ocv1.ClusterObjectSetTypeAvailable)
				if avail != nil {
					a := *avail
					a.ObservedGeneration = ext.GetGeneration()
					apimeta.SetStatusCondition(&rs.Conditions, a)
				}
				prog := progressingFromAvailable(avail, false)
				prog.ObservedGeneration = ext.GetGeneration()
				apimeta.SetStatusCondition(&rs.Conditions, prog)

				if idx == len(state.revisionStates.RollingOut)-1 {
					topProg := progressingFromAvailable(avail, false)
					topProg.ObservedGeneration = ext.GetGeneration()
					apimeta.SetStatusCondition(&ext.Status.Conditions, topProg)
				}
				ext.Status.ActiveRevisions = append(ext.Status.ActiveRevisions, rs)
			}
```

- [ ] **Step 6: Run the package tests + build**

Run: `go test ./internal/operator-controller/controllers/ -v && go build ./...`
Expected: PASS and clean build. (`ocv1.ClusterObjectSetTypeProgressing` is no longer referenced anywhere in this package.)

- [ ] **Step 7: Commit**

```bash
git add internal/operator-controller/controllers/
git commit -s -m "✨ reconstruct ClusterExtension Progressing from ClusterObjectSet Available"
```

---

## Task 4: Remove the `Progressing` type, regenerate artifacts, update e2e

**Files:**
- Modify: `api/v1/clusterobjectset_types.go` (remove const L30, printcolumn L565, update status doc L495-515)
- Modify: `test/e2e/steps/steps.go` (`ClusterObjectSetIsArchived`, ~L975-978)
- Regenerated (via make): `helm/olmv1/base/operator-controller/crd/experimental/olm.operatorframework.io_clusterobjectsets.yaml`, `manifests/experimental.yaml`, `manifests/experimental-e2e.yaml`, `docs/api-reference/olmv1-api-reference.md`

**Interfaces:**
- Consumes: nothing new.
- Produces: `ClusterObjectSetTypeProgressing` no longer exists; CRD printcolumn/doc reflect `Available`-only.

- [ ] **Step 1: Confirm the constant is unused before removal**

Run: `grep -rn "ClusterObjectSetTypeProgressing" --include=*.go .`
Expected: only the definition in `api/v1/clusterobjectset_types.go`. If any other hit remains, that file was missed in Task 2/3 — fix it before proceeding.

- [ ] **Step 2: Remove the constant and printcolumn; update the status doc**

In `api/v1/clusterobjectset_types.go`:
- Delete `ClusterObjectSetTypeProgressing = "Progressing"` (L30).
- Delete the kubebuilder printcolumn line for `Progressing` (L565): `// +kubebuilder:printcolumn:name="Progressing",type=string,JSONPath=...`.
- Rewrite the `ClusterObjectSetStatus.Conditions` doc (L495-515) so it documents a single `Available` condition and its full reason set. Replace the `Progressing`-specific paragraph and expand the `Available` paragraph, e.g.:

```go
	// conditions is an optional list of status conditions describing the state of the
	// ClusterObjectSet.
	//
	// The Available condition represents the current health and rollout state of the revision:
	//   - True with reason ProbesSucceeded: the revision has rolled out and all objects pass their readiness probes.
	//   - False with reason ProbeFailure: one or more objects are failing their readiness probes during rollout.
	//   - False with reason RollingOut: the revision is actively rolling out and has not yet become available.
	//   - False with reason Blocked: the revision has encountered an error that requires manual intervention for recovery.
	//   - False with reason ProgressDeadlineExceeded: the revision did not roll out within spec.progressDeadlineMinutes.
	//   - Unknown with reason Reconciling: the revision encountered an error that prevented it from observing the probes.
	//   - Unknown with reason Archived: the revision has been archived and its objects have been torn down.
	//   - Unknown with reason Migrated: the revision was migrated from an existing release and probe results have not yet been observed.
	//
	// Rollout completion is recorded separately by status.completedAt.
```

- [ ] **Step 3: Update the e2e archived step**

In `test/e2e/steps/steps.go`, `ClusterObjectSetIsArchived` (~L975-978) — change the waited-on condition from `Progressing/False/Archived` to `Available/Unknown/Archived`, and update its doc comment:

```go
// ClusterObjectSetIsArchived waits for the named ClusterObjectSet to have Available=Unknown
// with reason Archived. Polls with timeout.
func ClusterObjectSetIsArchived(ctx context.Context, revisionName string) error {
	return waitForCondition(ctx, "clusterobjectset", substituteScenarioVars(revisionName, scenarioCtx(ctx)), "Available", "Unknown", ptr.To("Archived"), nil)
}
```

Run: `grep -rn "Progressing" test/e2e/` and confirm every remaining hit refers to the CE (or ClusterCatalog) `Progressing`, not COS.

- [ ] **Step 4: Regenerate CRDs, manifests, and docs**

Run: `make generate manifests crd-ref-docs`
Expected: updates the experimental COS CRD (drops the `Progressing` printcolumn, updates the condition description), `manifests/experimental*.yaml`, and `docs/api-reference/olmv1-api-reference.md`. Confirm no `crd/standard/**` or `manifests/standard*.yaml` files changed:

Run: `git status --porcelain helm/olmv1/base/*/crd/standard manifests/standard.yaml manifests/standard-e2e.yaml`
Expected: empty output.

- [ ] **Step 5: Verify everything is in sync and lints**

Run: `make verify && make lint`
Expected: PASS (generated code up to date, linters clean).

- [ ] **Step 6: Run the full unit suite**

Run: `make test-unit`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add api/v1/clusterobjectset_types.go test/e2e/steps/steps.go helm/olmv1 manifests docs/api-reference
git commit -s -m "⚠ remove Progressing condition from ClusterObjectSet"
```

---

## Final verification (whole branch)

- [ ] `make verify` — generated code in sync.
- [ ] `make lint` and `make lint-api-diff` — no new violations (removing a condition type and printcolumn on an experimental CRD is expected; confirm crd-diff tolerance).
- [ ] `make test-unit` — all unit tests pass.
- [ ] `make test-experimental-e2e` — COS is experimental; exercises rollout, archive, and CE `Progressing` reconstruction end-to-end.
- [ ] `grep -rn "ClusterObjectSetTypeProgressing\|\"Progressing\"" internal/object-controller` returns nothing — COS no longer emits `Progressing`.
