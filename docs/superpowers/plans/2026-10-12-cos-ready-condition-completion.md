let's review let# ClusterObjectSet Ready Condition Completion — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish the `ClusterObjectSet` (COS) `Ready` condition vocabulary across four stacked PRs: introduce `ObjectsNotReady` (retire `ProbeFailure`), `Archiving`, and `Superseded`, and remove `RetryableError`.

**Architecture:** Each PR is one gh-stack branch on top of the previous, based on `main`. The COS controller computes the `Ready` condition in `reconcile()`/`archive()`; the ClusterExtension (CE) controller reconstructs its own `Progressing`/`Installed` conditions from the COS `Ready` reason in `common_controller.go`. Each PR changes the COS reason emission **and** the CE mapping together so the two never drift.

**Tech Stack:** Go, controller-runtime, boxcutter (`pkg.package-operator.run/boxcutter@v0.15.0`), gomock, testify, kubebuilder/controller-gen. Build tag `containers_image_openpgp`.

**Spec:** `docs/superpowers/specs/2026-10-12-cos-ready-condition-completion-design.md`

## Global Constraints

- Reasons are Go string constants in `api/v1/clusterobjectset_types.go` (status-condition reasons, **not** CRD-enum validated). No CRD compat gate; COS is experimental-only.
- After any change under `api/v1/` or helm charts, run `make manifests crd-ref-docs` and commit generated artifacts (`helm/olmv1/base/**/crd/experimental/*.yaml`, `manifests/experimental*.yaml`, `applyconfigurations/**`, `docs/api-reference/olmv1-api-reference.md`).
- Each PR must pass `make test-unit` and `make lint` before it is opened.
- Message style: capitalized, complete sentence, terminal period. COS self-describes as "the object set"; per-object messages say "object(s)"; never mention bundle version.
- **Per-PR consistency pass (definition of done for every task):** (1) rename vars/functions/methods that still reflect retired vocabulary; (2) update test names, `t.Run` case names, and assertion messages; (3) fold any newly added unit test into an existing table test where it fits; (4) update `docs/draft/concepts/clusterobjectsets.md` and `docs/superpowers/specs/clusterobjectset-ready-condition.md` to match the code as of that PR.
- Commit style: title prefix per repo convention — `✨` minor/compatible, `⚠` breaking. DCO sign-off and the attribution trailer `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

## Review Focus

- **ObjectsNotReady deadline overlay:** when the deadline is exceeded while objects are not ready, `Ready` must still relabel to `ProgressDeadlineExceeded` (not `ObjectsNotReady`). Covered in Task 1.
- **Superseded vs ObjectsNotReady precedence:** a revision with ≥1 `ActionProgressed` object **and** probe-pending objects must report `Superseded`, not `ObjectsNotReady`. Covered in Task 3.
- **Superseded vs deadline precedence:** `ActionProgressed` objects while the deadline is exceeded must report `ProgressDeadlineExceeded` (deadline wins over `Superseded`). Covered in Task 3.
- **Archiving → Archived in one reconcile:** when teardown completes, the final reason must be `Archived`, never left on `Archiving`. Covered in Task 2.
- **First-reconcile transient error (no prior Ready):** a transient error before `Ready` is ever set must leave the condition absent and must NOT make the CE report `Installed=Failed`. Covered in Task 4.

---

## Task 1 (PR 1): `ObjectsNotReady` — retire `ProbeFailure`

**Files:**
- Modify: `api/v1/clusterobjectset_types.go:33` (reasons block)
- Modify: `internal/object-controller/controllers/clusterobjectset_controller.go:297-300,358-370`
- Modify: `internal/operator-controller/controllers/common_controller.go:253-283` (`progressingFromReady`)
- Test: `internal/object-controller/controllers/clusterobjectset_controller_test.go`
- Test: `internal/operator-controller/controllers/common_controller_test.go`
- Docs: `docs/draft/concepts/clusterobjectsets.md`, `docs/superpowers/specs/clusterobjectset-ready-condition.md`

**Interfaces:**
- Consumes: existing `setReadyWithDeadline(cos, status, reason, message, isDeadlineExceeded)`, `rres.IsComplete()`, `rres.GetPhases()`, `machinery.ObjectResult`.
- Produces: `ocv1.ClusterObjectSetReasonObjectsNotReady = "ObjectsNotReady"`. Removes `ocv1.ClusterObjectSetReasonProbeFailure`. COS no longer emits `ocv1.ReasonRollingOut`.

- [ ] **Step 1: Add the new reason constant, keep the old one for now**

In `api/v1/clusterobjectset_types.go` reasons block, add:
```go
	ClusterObjectSetReasonObjectsNotReady   = "ObjectsNotReady"
```

- [ ] **Step 2: Write the failing controller test (not-ready → ObjectsNotReady)**

Add a case to the `RevisionReconciliation` table in `clusterobjectset_controller_test.go` (fold, don't create a standalone test):
```go
{
	name:                    "rolling out with a failing probe reports ObjectsNotReady",
	reconcilingRevisionName: clusterObjectSetName,
	revisionResult: newMockRevisionResult(mockCtrl, revisionResultConfig{
		isComplete:   false,
		inTransition: true,
		phases: []machinery.PhaseResult{
			newMockPhaseResult(mockCtrl, phaseResultConfig{
				name:       "deploy",
				isComplete: false,
				objects: []machinery.ObjectResult{
					newMockObjectResult(mockCtrl, objectResultConfig{
						action: machinery.ActionUpdated,
						object: &unstructured.Unstructured{Object: map[string]interface{}{
							"apiVersion": "apps/v1", "kind": "Deployment",
							"metadata": map[string]interface{}{"name": "d", "namespace": "ns"},
						}},
						probes: machinerytypes.ProbeResultContainer{
							boxcutter.ProgressProbeType: {Status: machinerytypes.ProbeStatusFalse, Messages: []string{"not ready"}},
						},
					}),
				},
			}),
		},
	}),
	existingObjs: func() []client.Object {
		ext := newTestClusterExtension()
		return []client.Object{ext, newTestClusterObjectSet(t, clusterObjectSetName, ext, testScheme)}
	},
	validate: func(t *testing.T, c client.Client) {
		rev := &ocv1.ClusterObjectSet{}
		require.NoError(t, c.Get(t.Context(), client.ObjectKey{Name: clusterObjectSetName}, rev))
		cond := meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
		require.NotNil(t, cond)
		require.Equal(t, metav1.ConditionFalse, cond.Status)
		require.Equal(t, ocv1.ClusterObjectSetReasonObjectsNotReady, cond.Reason)
	},
},
```

- [ ] **Step 3: Run it, verify it fails**

Run: `go test ./internal/object-controller/controllers/ -run 'RevisionReconciliation/rolling_out_with_a_failing_probe' -count=1`
Expected: FAIL (reason is `ProbeFailure`, not `ObjectsNotReady`).

- [ ] **Step 4: Rewrite the not-ready emission to a single `ObjectsNotReady`**

In `clusterobjectset_controller.go`, delete the in-transition block at `:297-300` entirely:
```go
	revisionNumber := cos.Spec.Revision
	if rres.InTransition() {
		setReadyWithDeadline(cos, metav1.ConditionFalse, ocv1.ReasonRollingOut, fmt.Sprintf("Revision %d is rolling out.", revisionNumber), isDeadlineExceeded)
	}
```
`revisionNumber` is used only in the two rolling-out messages (`:299` and `:358`), both removed in this task, so remove its declaration too. Then replace the `else`-branch switch at `:358-370`:
```go
		switch {
		case isDeadlineExceeded:
			setReadyWithDeadline(cos, metav1.ConditionFalse, ocv1.ReasonRollingOut, rollingOutMsg, true)
		case len(probeFailureMsgs) > 0:
			setReadyWithDeadline(cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonProbeFailure, strings.Join(probeFailureMsgs, "\n"), false)
		default:
			setReadyWithDeadline(cos, metav1.ConditionFalse, ocv1.ReasonRollingOut, rollingOutMsg, false)
		}
```
with:
```go
		notReadyMsg := "Objects are not yet ready."
		if len(probeFailureMsgs) > 0 {
			notReadyMsg = fmt.Sprintf("%d object(s) are not yet ready:\n%s", len(probeFailureMsgs), strings.Join(probeFailureMsgs, "\n"))
		}
		setReadyWithDeadline(cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonObjectsNotReady, notReadyMsg, isDeadlineExceeded)
```
Rename the local `probeFailureMsgs` → `notReadyMsgs` (consistency pass). Delete the now-unused `rollingOutMsg` line if it becomes dead.

- [ ] **Step 5: Run it, verify it passes**

Run: `go test ./internal/object-controller/controllers/ -run 'RevisionReconciliation' -count=1`
Expected: PASS.

- [ ] **Step 6: Write the failing deadline-overlay test**

Add a `RevisionReconciliation` case: same as Step 2 but set `spec.progressDeadlineMinutes` on the COS (via `newTestClusterObjectSet` + a deadline in the past using the reconciler's injected `Clock`), and assert `cond.Reason == ocv1.ReasonProgressDeadlineExceeded`. Reuse the existing deadline-exceeded helper pattern already present in the table (grep the test file for `ProgressDeadlineExceeded` to copy the clock setup).

- [ ] **Step 7: Run it, verify it fails then passes after Step 4 already handles it**

Run: `go test ./internal/object-controller/controllers/ -run 'RevisionReconciliation' -count=1`
Expected: PASS (the `isDeadlineExceeded` arg to `setReadyWithDeadline` already relabels).

- [ ] **Step 8: Update the CE mapping + its failing test**

In `common_controller.go` `progressingFromReady`, change the `default` comment and add an explicit case so intent is clear:
```go
	case ocv1.ClusterObjectSetReasonObjectsNotReady:
		cond.Reason = ocv1.ReasonRollingOut
	default:
		// AllObjectsReady-but-not-yet-complete, or any other non-error reason.
		cond.Reason = ocv1.ReasonRollingOut
```
Add a `common_controller_test.go` case asserting `progressingFromReady(&metav1.Condition{Type: ocv1.ClusterObjectSetTypeReady, Reason: ocv1.ClusterObjectSetReasonObjectsNotReady, Message: "x"}, false)` returns `Status=True, Reason=RollingOut`. Run the CE test package to confirm pass.

- [ ] **Step 9: Remove the `ProbeFailure` constant**

Delete `ClusterObjectSetReasonProbeFailure` from `api/v1/clusterobjectset_types.go`. Run `grep -rn ProbeFailure internal api docs` — update/remove every remaining reference (there should be none in code after Step 4).

- [ ] **Step 10: Consistency pass + docs + regen**

- `grep -rn 'ReasonRollingOut' internal/object-controller` — confirm COS no longer emits it.
- Update `docs/draft/concepts/clusterobjectsets.md` Ready table: replace the `ProbeFailure` row with `ObjectsNotReady`.
- Update `docs/superpowers/specs/clusterobjectset-ready-condition.md` if any `ProbeFailure` wording remains.
- Run `make manifests crd-ref-docs`.

- [ ] **Step 11: Verify and commit**

Run: `make test-unit && make lint`
Expected: PASS, 0 issues.
```bash
git add -A
git commit -s -m "✨ report ClusterObjectSet rollout as Ready=False/ObjectsNotReady

Collapse the in-transition RollingOut path and the ProbeFailure path into a
single ObjectsNotReady reason on the ClusterObjectSet Ready condition, keeping
per-object probe detail. Remove ClusterObjectSetReasonProbeFailure; COS no
longer emits the shared RollingOut reason. CE Progressing still reports
RollingOut while rolling out.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2 (PR 2): `Archiving`

**Files:**
- Modify: `api/v1/clusterobjectset_types.go` (reasons block)
- Modify: `internal/object-controller/controllers/clusterobjectset_controller.go:390-409` (`archive`), add `markAsArchiving` helper near `markAsArchived:785`
- Modify: `internal/operator-controller/controllers/common_controller.go` (`progressingFromReady` — neutral case)
- Test: `internal/object-controller/controllers/clusterobjectset_controller_test.go` (archival table — grep `ArchivalAndDeletion`)
- Docs: `docs/draft/concepts/clusterobjectsets.md`, `docs/superpowers/specs/clusterobjectset-ready-condition.md`

**Interfaces:**
- Consumes: `revisionEngine.Teardown(...)`, `tdres.IsComplete()`, `markAsNotReady`, `markAsArchived`.
- Produces: `ocv1.ClusterObjectSetReasonArchiving = "Archiving"`, helper `markAsArchiving(cos *ocv1.ClusterObjectSet) bool`.

- [ ] **Step 1: Add the reason + helper**

In `clusterobjectset_types.go` add `ClusterObjectSetReasonArchiving = "Archiving"`. In the controller, next to `markAsArchived`:
```go
func markAsArchiving(cos *ocv1.ClusterObjectSet) bool {
	const msg = "The object set is being archived; removing owned objects."
	return markAsNotReady(cos, ocv1.ClusterObjectSetReasonArchiving, msg)
}
```

- [ ] **Step 2: Write the failing test (teardown in progress → Archiving)**

In the archival table test (`Test_ClusterObjectSetReconciler_Reconcile_ArchivalAndDeletion`, grep to confirm name), add a case: `spec.lifecycleState=Archived`, Teardown returns a `revisionTeardownResultConfig{isComplete: false}` mock, assert `result == ctrl.Result{RequeueAfter: 5 * time.Second}` and `Ready` reason `ocv1.ClusterObjectSetReasonArchiving`.

- [ ] **Step 3: Run it, verify it fails**

Run: `go test ./internal/object-controller/controllers/ -run 'ArchivalAndDeletion' -count=1`
Expected: FAIL (reason is `RetryableError`).

- [ ] **Step 4: Switch the archive teardown-in-progress paths to Archiving**

In `archive()` replace both `setRetryableErrorConditions` calls (`:394` teardown error, `:398` owned resources not yet removed) with `markAsArchiving(cos)`:
```go
	tdres, err := revisionEngine.Teardown(ctx, revision, machinerytypes.WithObserveAfterIncomplete{})
	if err != nil {
		markAsArchiving(cos)
		return ctrl.Result{}, fmt.Errorf("error archiving revision: %v", err)
	}
	if tdres != nil && !tdres.IsComplete() {
		markAsArchiving(cos)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
```
(The teardown-error path still returns the error → backoff requeue.)

- [ ] **Step 5: Run it, verify it passes**

Run: `go test ./internal/object-controller/controllers/ -run 'ArchivalAndDeletion' -count=1`
Expected: PASS.

- [ ] **Step 6: Write the failing "teardown completes → Archived, not Archiving" test**

Add an archival-table case: Teardown returns `isComplete: true`; assert final `Ready` reason is `ocv1.ClusterObjectSetReasonArchived` and `result == ctrl.Result{}`.

- [ ] **Step 7: Run it, verify it passes**

Run: `go test ./internal/object-controller/controllers/ -run 'ArchivalAndDeletion' -count=1`
Expected: PASS (existing `markAsArchived` path unchanged).

- [ ] **Step 8: CE mapping + test**

In `progressingFromReady`, add `case ocv1.ClusterObjectSetReasonArchiving:` → `cond.Reason = ocv1.ReasonRollingOut` (neutral). Add a `common_controller_test.go` assertion for it. Run the CE test package.

- [ ] **Step 9: Consistency pass + docs + regen**

- Update the Ready table in `docs/draft/concepts/clusterobjectsets.md` and `clusterobjectset-ready-condition.md` to reflect `Archiving` on the archive path.
- `make manifests crd-ref-docs`.

- [ ] **Step 10: Verify and commit**

Run: `make test-unit && make lint`
```bash
git add -A
git commit -s -m "✨ report ClusterObjectSet teardown as Ready=False/Archiving

While spec.lifecycleState=Archived and teardown is in progress, report a
dedicated Archiving reason instead of RetryableError; Archived stays terminal.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3 (PR 3): `Superseded`

**Files:**
- Modify: `api/v1/clusterobjectset_types.go` (reasons block)
- Modify: `internal/object-controller/controllers/clusterobjectset_controller.go` (not-complete `else` branch, before the `ObjectsNotReady` emission from Task 1)
- Modify: `internal/operator-controller/controllers/common_controller.go` (`progressingFromReady` neutral case)
- Test: `internal/object-controller/controllers/clusterobjectset_controller_test.go` (`RevisionReconciliation` table)
- Docs: `docs/draft/concepts/clusterobjectsets.md`, `docs/superpowers/specs/clusterobjectset-ready-condition.md`

**Interfaces:**
- Consumes: `rres.GetPhases()`, `machinery.ObjectResult.Action()`, `machinery.ActionProgressed`, `isDeadlineExceeded`, `setReadyWithDeadline`.
- Produces: `ocv1.ClusterObjectSetReasonSuperseded = "Superseded"`.

- [ ] **Step 1: Add the reason constant**

Add `ClusterObjectSetReasonSuperseded = "Superseded"` to `clusterobjectset_types.go`.

- [ ] **Step 2: Write the failing test (any progressed object → Superseded, over ObjectsNotReady)**

Add a `RevisionReconciliation` case: not complete; one phase with two objects — one `action: machinery.ActionProgressed`, one with a failing probe (`ProbeStatusFalse`). Assert `cond.Reason == ocv1.ClusterObjectSetReasonSuperseded` (Superseded wins over ObjectsNotReady).

- [ ] **Step 3: Run it, verify it fails**

Run: `go test ./internal/object-controller/controllers/ -run 'RevisionReconciliation' -count=1`
Expected: FAIL (reason is `ObjectsNotReady`).

- [ ] **Step 4: Add the Superseded check in the not-complete branch**

In the `else` branch (after `probeFailureMsgs`/`notReadyMsgs` are gathered, before the `setReadyWithDeadline(..., ObjectsNotReady, ...)` emission from Task 1), insert:
```go
		superseded := false
		for _, pres := range rres.GetPhases() {
			for _, ores := range pres.GetObjects() {
				if ores.Action() == machinery.ActionProgressed {
					superseded = true
					break
				}
			}
			if superseded {
				break
			}
		}
		if superseded && !isDeadlineExceeded {
			setReadyWithDeadline(cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonSuperseded,
				"Object(s) have been adopted by a newer revision of the object set.", false)
			if hasDeadline {
				return ctrl.Result{RequeueAfter: remaining}, nil
			}
			return ctrl.Result{}, nil
		}
```
Place it so the deadline-exceeded path (handled by the existing `ObjectsNotReady`/`setReadyWithDeadline(..., isDeadlineExceeded)` emission) still wins — i.e. the `&& !isDeadlineExceeded` guard ensures `ProgressDeadlineExceeded` precedence.

- [ ] **Step 5: Run it, verify it passes**

Run: `go test ./internal/object-controller/controllers/ -run 'RevisionReconciliation' -count=1`
Expected: PASS.

- [ ] **Step 6: Write the failing "Superseded + deadline exceeded → ProgressDeadlineExceeded" test**

Add a `RevisionReconciliation` case: a progressed object **and** a past deadline. Assert `cond.Reason == ocv1.ReasonProgressDeadlineExceeded`.

- [ ] **Step 7: Run it, verify it passes**

Run: `go test ./internal/object-controller/controllers/ -run 'RevisionReconciliation' -count=1`
Expected: PASS (the `!isDeadlineExceeded` guard defers to the deadline path).

- [ ] **Step 8: CE mapping + test**

In `progressingFromReady`, add `case ocv1.ClusterObjectSetReasonSuperseded:` → `cond.Reason = ocv1.ReasonRollingOut` (neutral; the old revision is handing off). Add a `common_controller_test.go` assertion. Confirm `determineFailureReason` does NOT treat `Superseded` as an error (it isn't in the error list — add a test asserting a latest revision with `Superseded` yields `ReasonAbsent`). Run the CE test package.

- [ ] **Step 9: Consistency pass + docs + regen**

- Add the `Superseded` row to the Ready tables in `docs/draft/concepts/clusterobjectsets.md` and `clusterobjectset-ready-condition.md`, and to the precedence summary (after `ProgressDeadlineExceeded`, before `ObjectsNotReady`).
- `make manifests crd-ref-docs`.

- [ ] **Step 10: Verify and commit**

Run: `make test-unit && make lint`
```bash
git add -A
git commit -s -m "✨ report superseded ClusterObjectSet as Ready=False/Superseded

When one or more objects have been adopted by a newer sibling revision
(machinery.ActionProgressed), report Superseded instead of ObjectsNotReady.
Partial supersession counts; the progress deadline still takes precedence.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4 (PR 4): remove `RetryableError`

**Files:**
- Modify: `internal/object-controller/controllers/clusterobjectset_controller.go` (sites `:153,:176,:193,:219,:229,:381`; delete `setRetryableErrorConditions:761`)
- Modify: `api/v1/clusterobjectset_types.go` (remove the constant)
- Modify: `internal/operator-controller/controllers/common_controller.go` (`determineFailureReason:170`, `progressingFromReady:276`)
- Test: both controllers' `_test.go`
- Docs: `docs/draft/concepts/clusterobjectsets.md`, `docs/superpowers/specs/clusterobjectset-ready-condition.md`

**Interfaces:**
- Consumes: controller-runtime requeue-on-error behavior.
- Produces: removes `ocv1.ClusterObjectSetReasonRetryableError` and `setRetryableErrorConditions`.

- [ ] **Step 1: Write the failing controller test (transient error leaves Ready untouched)**

Add a `RevisionReconciliation` case: pre-seed the COS with an existing `Ready=True/AllObjectsReady` condition (via `existingObjs`), then force a transient error (e.g. a factory error using the table's `factoryErr` field). Assert the returned error is non-nil AND the `Ready` condition is **unchanged** (`Reason == AllObjectsReady`, `Status == True`).

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./internal/object-controller/controllers/ -run 'RevisionReconciliation' -count=1`
Expected: FAIL (reason flipped to `RetryableError`).

- [ ] **Step 3: Remove every `setRetryableErrorConditions` call**

At each site, drop the status mutation and just return the wrapped error:
- `:153` verifying referenced Secrets (read error): `return ctrl.Result{}, werr`
- `:176` converting to boxcutter revision: `return ctrl.Result{}, fmt.Errorf("converting to boxcutter revision: %v", err)`
- `:193` create revision engine: `return ctrl.Result{}, fmt.Errorf("failed to create revision engine: %v", err)`
- `:219` establish watch: `return ctrl.Result{}, werr`
- `:229` revision reconcile: `return ctrl.Result{}, fmt.Errorf("revision reconcile: %v", err)`
- `:381` `delete()` TrackingCache.Free: drop `markAsNotReady(..., RetryableError, ...)`, keep `return ctrl.Result{}, fmt.Errorf("error stopping informers: %v", err)`
- `:207` `LifecycleStateArchived` TrackingCache.Free: drop the `markAsNotReady(..., RetryableError, ...)`, keep returning the error.

- [ ] **Step 4: Delete the helper and constant**

Delete `setRetryableErrorConditions` (`:761`). Remove `ClusterObjectSetReasonRetryableError` from `clusterobjectset_types.go`. Run `grep -rn RetryableError internal api docs` — expect only CE mapping + docs left (handled next).

- [ ] **Step 5: Run it, verify it passes**

Run: `go test ./internal/object-controller/controllers/ -run 'RevisionReconciliation' -count=1`
Expected: PASS.

- [ ] **Step 6: Write the failing CE test (transient → not Failed)**

In `common_controller_test.go`, add/adjust a `determineFailureReason` case: a latest rolling revision whose `Ready` condition is **absent** (or `ObjectsNotReady`) must return `ocv1.ReasonAbsent`, not `ReasonFailed`. Add a `progressingFromReady` case: `ready == nil` already yields `RollingOut` (keep); remove reliance on the `RetryableError` branch.

- [ ] **Step 7: Run it, verify it fails**

Run: `go test ./internal/operator-controller/controllers/ -run 'FailureReason|Progressing' -count=1`
Expected: FAIL to compile or assert until Step 8.

- [ ] **Step 8: Update the CE mapping**

In `determineFailureReason` (`:170-172`) remove `ocv1.ClusterObjectSetReasonRetryableError` from the error-reason condition (keep `ObjectCollision` and `ValidationFailure`). In `progressingFromReady` (`:276`) remove `ocv1.ClusterObjectSetReasonRetryableError` from the `Retrying` case; if that leaves only `ObjectCollision`, keep the case as `case ocv1.ClusterObjectSetReasonObjectCollision:`. Update the doc comments at `:147,:155,:168,:279` that mention `RetryableError`.

- [ ] **Step 9: Run it, verify it passes**

Run: `go test ./internal/operator-controller/controllers/ -count=1`
Expected: PASS.

- [ ] **Step 10: Consistency pass + docs + regen**

- `grep -rn RetryableError .` (excluding vendor) — zero hits outside historical specs.
- Update the Ready tables in `docs/draft/concepts/clusterobjectsets.md` and `clusterobjectset-ready-condition.md`: remove `RetryableError`; document that transient errors leave `Ready` unchanged and requeue with backoff (and the first-reconcile absent case).
- `make manifests crd-ref-docs`.

- [ ] **Step 11: Verify and commit**

Run: `make test-unit && make lint`
```bash
git add -A
git commit -s -m "⚠ remove ClusterObjectSet Ready RetryableError reason

Transient errors now leave the Ready condition unchanged and return an error
for rate-limited backoff requeue, instead of overwriting Ready with
RetryableError. The ClusterExtension no longer maps transient COS errors to
Installed=Failed or Progressing=Retrying; it goes neutral. Remove the
RetryableError reason constant and setRetryableErrorConditions helper.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## gh-stack setup (after Task 4)

Each task's commit is its own branch stacked on the prior (`main` → PR1 → PR2 → PR3 → PR4). Create branches per commit and submit with `gh stack` so each PR targets the one below it. Confirm the stack base is `main` (`f47aaefa` or later) before submitting.
