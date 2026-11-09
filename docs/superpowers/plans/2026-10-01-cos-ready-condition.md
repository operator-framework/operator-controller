# ClusterObjectSet `Ready` Condition Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename the `ClusterObjectSet` (COS) health condition from `Available` to `Ready` and replace its overloaded reason set with specific, actionable reasons, without changing the ClusterExtension-visible roll-up behavior.

**Architecture:** COS is experimental-only. The COS controller (`internal/object-controller/controllers/clusterobjectset_controller.go`) is the sole writer of the condition; `common_controller.go` in the operator-controller package is the sole reader, reconstructing the ClusterExtension `Progressing`/`Installed` conditions from it. We rename the condition type, split the catch-all `Reconciling`/`Blocked` reasons into specific ones, merge `RollingOut`+`ProbeFailure` into a single readiness reason, drop bundle-version/"revision" verbiage from messages, then regenerate the experimental CRD, manifests, and API reference docs.

**Tech Stack:** Go, controller-runtime, boxcutter (`pkg.package-operator.run/boxcutter v0.14.0`), gomock, testify, kubebuilder markers (controller-gen), bingo-managed tooling.

**Spec:** `docs/superpowers/specs/2026-09-30-cos-ready-condition-blueprint.md` — the plan implements that blueprint; executors read both.

## Global Constraints

- **Experimental-only API.** COS has no standard-channel CRD. The controller file carries `//go:build !standard`. No compatibility gate blocks the rename; there is no standard manifest/CRD to update.
- **API group / label prefix:** `olm.operatorframework.io` for all reasons, labels, annotations.
- **Reason values are CamelCase** machine identifiers; **messages are complete sentences**, capitalized, ending with a period (OpenShift convention). Avoid raw `err.Error()` jargon except in the transient `Reconciling` catch-all.
- **No bundle version, no "revision" verbiage in COS messages.** Bundle version is a ClusterExtension concept; COS self-describes as "the object set"; per-object messages say "object(s)".
- **Determinism:** any list interpolated into a message (secret names, phase names, per-object detail) must be sorted so the message does not churn across reconciles.
- **Generated files are committed.** After API-marker/doc changes, regenerate with the project's make targets (bingo-managed tools) and commit the output. Never hand-edit generated CRD YAML, `manifests/*.yaml`, or `docs/api-reference/olmv1-api-reference.md`.
- **Do not touch** `internal/operator-controller/applier/boxcutter.go` `availableConditionAssertion` — it asserts a managed **Deployment's** `Available` condition (`appsv1.DeploymentAvailable`), unrelated to the COS condition.
- **Commits:** DCO sign-off (`git commit -s`). If opening a PR, title prefix `✨` (minor/compatible change). End commit messages with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

## Review Focus

- **CE condition type stays `Available`.** The ClusterExtension's public contract exposes a condition named `Available` (both the top-level roll-up and each `status.activeRevisions[].conditions[]` entry — `clusterextension_types.go:522`). The reader currently copies the COS condition **verbatim, including `.Type`** (`common_controller.go:84`, `:97`), so a naive constant rename would silently flip the CE's condition to `Ready`. The reader must be decoupled: look up the COS `Ready` condition but re-emit onto the CE as `Available`. → Task 1 tests assert the CE emits type `Available`.
- **ClusterExtension roll-up equivalence.** The resplit must preserve the CE `Progressing`/`Installed` **reasons** (messages are allowed to change — they flow through from the reworded COS messages via `progressingFromAvailable` at `common_controller.go:248`, and that is intended): `ValidationFailed`/`ObjectCollision` must roll up like the old `Reconciling` (CE `Progressing=Retrying`, `Installed` failure reason `Failed`); `MutableReferencedSecret`/`DigestMismatch` must roll up like the old `Blocked` (CE `Progressing=False/Blocked`). → Task 4 tests.
- **Deadline precedence vs. blocked.** Blocked-class reasons (`MutableReferencedSecret`, `DigestMismatch`) must NOT be overridden by `ProgressDeadlineExceeded`; `ObjectsNotReady`/`ValidationFailed`/`ObjectCollision`/`Reconciling` MUST remain deadline-overridable. → Task 3 tests.
- **Empty per-object detail.** When the revision is not complete but no per-object probe detail is available yet, `ObjectsNotReady` must emit the generic `Objects are not yet ready.`, never `0 object(s) are not yet ready:`. → Task 3 test.
- **completedAt stamped once.** `status.completedAt` must still be stamped exactly once, on first `AllObjectsReady`, and never cleared/re-set. → Task 3 test.
- **Message hygiene / determinism.** `DigestMismatch` must not leak raw digests; `MutableReferencedSecret` and `DigestMismatch` must list names sorted. → Task 3 tests.

---

### Task 1: Rename the COS condition type `Available` → `Ready`, keeping the CE's `Available` condition

Rename the **ClusterObjectSet** condition type to `Ready`, but **decouple the ClusterExtension reader** so the CE keeps publishing a condition literally named `Available`. The reader looks up the COS `Ready` condition and re-emits it onto the CE (top-level and per-revision) as `Available`. No reason changes here. The tree must compile and all unit tests pass at the end, and the CE must still emit type `Available`.

**Files:**
- Modify: `api/v1/clusterobjectset_types.go:29` (constant), `:563` (printcolumn)
- Modify: `api/v1/common_types.go:20-21` (add CE-owned `TypeAvailable`)
- Modify: `internal/object-controller/controllers/clusterobjectset_controller.go` (`:665`, `:679`, `:689` — the three writer `Type:` fields)
- Modify: `internal/operator-controller/controllers/common_controller.go` (lookups `:80`, `:93`, `:112`, `:115`, `:152`; emission overrides `:84`, `:97`)
- Test: `internal/object-controller/controllers/clusterobjectset_controller_test.go` (all `ocv1.ClusterObjectSetTypeAvailable` uses → `ClusterObjectSetTypeReady`)
- Test: `internal/operator-controller/controllers/common_controller_test.go` (revision-condition **fixtures** → `ClusterObjectSetTypeReady`; add CE-emission assertions)

**Interfaces:**
- Produces: `ocv1.ClusterObjectSetTypeReady = "Ready"` (replaces `ClusterObjectSetTypeAvailable`) — the COS writer's condition type.
- Produces: `ocv1.TypeAvailable = "Available"` (new, in `common_types.go`) — the CE-owned condition type the reader emits. Consumed by Task 4 tests.
- Rule established here: the COS **writes** `Ready`; the CE **reads** `Ready` from revisions but **emits** `Available`. Later tasks preserve this split.

- [ ] **Step 1: Rename the COS condition-type constant and its value**

In `api/v1/clusterobjectset_types.go`, change line 29:

```go
	// Condition Types
	ClusterObjectSetTypeReady = "Ready"
```

- [ ] **Step 2: Add the CE-owned `TypeAvailable` constant**

In `api/v1/common_types.go`, add alongside `TypeInstalled`/`TypeProgressing` (lines 20-21):

```go
	TypeInstalled   = "Installed"
	TypeProgressing = "Progressing"
	// TypeAvailable is the ClusterExtension condition that surfaces the health of the
	// active revision's object set. It is published by the operator-controller reader
	// (decoupled from the ClusterObjectSet's own Ready condition) so the ClusterExtension
	// contract stays stable across the ClusterObjectSet/ClusterObjectDeployment layering.
	TypeAvailable = "Available"
```

- [ ] **Step 3: Update the printcolumn marker**

In `api/v1/clusterobjectset_types.go` replace the printcolumn at `:563`:

```go
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`
```

- [ ] **Step 4: Update the COS writer's condition type**

In `clusterobjectset_controller.go`, replace every `ocv1.ClusterObjectSetTypeAvailable` with `ocv1.ClusterObjectSetTypeReady` (the three `Type:` fields at `:665`, `:679`, `:689`). Run:

```bash
grep -n ClusterObjectSetTypeAvailable internal/object-controller/controllers/clusterobjectset_controller.go
```

Expected after edit: no matches.

- [ ] **Step 5: Decouple the CE reader — look up `Ready`, emit `Available`**

In `common_controller.go`:

Change the three **lookups** to read the COS `Ready` condition — `:80`, `:93`, `:112`, `:115`, `:152` — e.g.:

```go
	avail := apimeta.FindStatusCondition(r.Conditions, ocv1.ClusterObjectSetTypeReady)
```

Then at the two **emission** sites override the copied condition's type to the CE-owned `Available` before setting it. In `setActiveRevisionsFromRevisionStates` (`:80-85`):

```go
		avail := apimeta.FindStatusCondition(r.Conditions, ocv1.ClusterObjectSetTypeReady)
		if avail != nil {
			a := *avail
			a.Type = ocv1.TypeAvailable
			a.ObservedGeneration = ext.GetGeneration()
			apimeta.SetStatusCondition(&rs.Conditions, a)
		}
```

And in `setAvailableFromRevisionStates` (`:93-98`):

```go
		avail := apimeta.FindStatusCondition(i.Conditions, ocv1.ClusterObjectSetTypeReady)
		if avail != nil {
			a := *avail
			a.Type = ocv1.TypeAvailable
			a.ObservedGeneration = ext.GetGeneration()
			apimeta.SetStatusCondition(&ext.Status.Conditions, a)
		}
```

Verify no stray references remain:

```bash
grep -n ClusterObjectSetTypeAvailable internal/operator-controller/controllers/common_controller.go
```

Expected: no matches.

- [ ] **Step 6: Flip the reader test fixtures to `Ready`, keep CE assertions on `Available`**

In `common_controller_test.go`, the revision-condition **fixtures** represent the COS condition the reader consumes, so replace `ocv1.ClusterObjectSetTypeAvailable` with `ocv1.ClusterObjectSetTypeReady` in every fixture (`:328`, `:352`, `:363`, `:387`, `:411`, `:422`, the `availCond` helper `:460`, and the `TestProgressingFromAvailable` cases `:507`, `:519`, `:525`, `:531`, `:537`). Run:

```bash
grep -n ClusterObjectSetTypeAvailable internal/operator-controller/controllers/common_controller_test.go
```

Expected: no matches.

- [ ] **Step 7: Add a test asserting the CE emits type `Available` (not `Ready`)**

Add a focused test next to the existing reader tests:

```go
func TestCEEmitsAvailableConditionType(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "test-ext", Generation: 1}}
	states := &RevisionStates{
		Installed: &RevisionMetadata{
			RevisionName: "rev-1",
			Conditions: []metav1.Condition{{
				Type:   ocv1.ClusterObjectSetTypeReady,
				Status: metav1.ConditionTrue,
				Reason: ocv1.ClusterObjectSetReasonProbesSucceeded,
			}},
		},
	}
	setAvailableFromRevisionStates(ext, states)

	require.NotNil(t, meta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeAvailable),
		"CE must expose an Available condition")
	require.Nil(t, meta.FindStatusCondition(ext.Status.Conditions, ocv1.ClusterObjectSetTypeReady),
		"CE must not expose the COS Ready condition type directly")
}
```

(Uses `ClusterObjectSetReasonProbesSucceeded`, which still exists until Task 5; Task 4 updates it to `AllObjectsReady`.)

- [ ] **Step 8: Build and test**

Run: `go build ./... && go test ./internal/object-controller/... ./internal/operator-controller/... ./api/...`
Expected: PASS. CE behavior is preserved (still emits `Available`); only the COS condition type changed to `Ready`.

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -s -m "rename ClusterObjectSet condition to Ready; keep ClusterExtension Available

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: Add the new reason constants (additive)

Purely additive — introduce the new reason identifiers alongside the existing ones so later tasks can reference them while the tree stays green.

**Files:**
- Modify: `api/v1/clusterobjectset_types.go:31-38` (reason block)

**Interfaces:**
- Produces these new constants (consumed by Tasks 3 and 4):
  - `ClusterObjectSetReasonAllObjectsReady = "AllObjectsReady"`
  - `ClusterObjectSetReasonObjectsNotReady = "ObjectsNotReady"`
  - `ClusterObjectSetReasonValidationFailed = "ValidationFailed"`
  - `ClusterObjectSetReasonObjectCollision = "ObjectCollision"`
  - `ClusterObjectSetReasonMutableReferencedSecret = "MutableReferencedSecret"`
  - `ClusterObjectSetReasonDigestMismatch = "DigestMismatch"`
  - `ClusterObjectSetReasonArchiving = "Archiving"`
- Keeps: `ClusterObjectSetReasonReconciling`, `ClusterObjectSetReasonArchived`.
- Still present (removed in Task 5): `ClusterObjectSetReasonProbesSucceeded`, `ClusterObjectSetReasonProbeFailure`, `ClusterObjectSetReasonBlocked`, `ClusterObjectSetReasonRetrying`.
- `ProgressDeadlineExceeded` continues to come from `api/v1/common_types.go` as `ocv1.ReasonProgressDeadlineExceeded` (shared; unchanged).

- [ ] **Step 1: Add the constants**

In `api/v1/clusterobjectset_types.go`, replace the reason block (lines 31-38) with:

```go
	// Condition Reasons
	ClusterObjectSetReasonArchived  = "Archived"
	ClusterObjectSetReasonArchiving = "Archiving"

	ClusterObjectSetReasonAllObjectsReady         = "AllObjectsReady"
	ClusterObjectSetReasonObjectsNotReady         = "ObjectsNotReady"
	ClusterObjectSetReasonValidationFailed        = "ValidationFailed"
	ClusterObjectSetReasonObjectCollision         = "ObjectCollision"
	ClusterObjectSetReasonMutableReferencedSecret = "MutableReferencedSecret"
	ClusterObjectSetReasonDigestMismatch          = "DigestMismatch"
	ClusterObjectSetReasonReconciling             = "Reconciling"

	// Deprecated: removed after the Ready-condition rework; retained only until
	// all references are migrated.
	ClusterObjectSetReasonBlocked         = "Blocked"
	ClusterObjectSetReasonProbeFailure    = "ProbeFailure"
	ClusterObjectSetReasonProbesSucceeded = "ProbesSucceeded"
	ClusterObjectSetReasonRetrying        = "Retrying"
```

- [ ] **Step 2: Build**

Run: `go build ./... && go vet ./api/...`
Expected: PASS (nothing references the new constants yet; no removals).

- [ ] **Step 3: Commit**

```bash
git add api/v1/clusterobjectset_types.go
git commit -s -m "add new ClusterObjectSet Ready condition reason constants

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: Rewire the COS controller writer to the new reasons and messages

Restructure the condition setters and all call sites in `clusterobjectset_controller.go`. This is the behavioral core. Update the controller tests in lockstep.

**Files:**
- Modify: `internal/object-controller/controllers/clusterobjectset_controller.go` (`reconcile` body `:131-302`, `archive` `:316-335`, `delete` `:305-314`, setters `:654-700`, `verifyObservedPhases` `:719-746`, `verifyReferencedSecretsImmutable` `:748-796`)
- Test: `internal/object-controller/controllers/clusterobjectset_controller_test.go`

**Interfaces:**
- Consumes the Task 2 reason constants and `ocv1.ClusterObjectSetTypeReady`.
- Produces this setter API (consumed only within this file):
  - `setReadyWithDeadline(cos *ocv1.ClusterObjectSet, status metav1.ConditionStatus, reason, message string, isDeadlineExceeded bool)` — sets `Ready`; when `isDeadlineExceeded`, overrides to `Ready=False`/`ProgressDeadlineExceeded`.
  - `markReady(cos, reason, message string) bool` — `Ready=True`.
  - `markNotReady(cos, reason, message string) bool` — `Ready=False`, no deadline override (for blocked-class reasons).
  - `setReconciling(cos, detail string, isDeadlineExceeded bool)` — `Ready=False`/`Reconciling`, deadline-overridable, prefixes the transient message.
- Condition-reason emission after this task (the only reasons the controller writes):
  `AllObjectsReady`, `ObjectsNotReady`, `ValidationFailed`, `ObjectCollision`, `MutableReferencedSecret`, `DigestMismatch`, `Archiving`, `Reconciling`, `Archived`, and `ReasonProgressDeadlineExceeded` (via the deadline override).

#### 3a. Rework the setters

- [ ] **Step 1: Rename/rewrite the setter helpers**

Replace `setAvailableWithDeadline` (`:654-671`), `setRetryingConditions` (`:673-675`), `markAsAvailable` (`:677-685`), `markAsUnavailable` (`:687-695`) with:

```go
// setReadyWithDeadline sets the Ready condition to the supplied status/reason/message,
// unless the progress deadline has been exceeded — in which case it reports
// Ready=False/ProgressDeadlineExceeded instead.
func setReadyWithDeadline(cos *ocv1.ClusterObjectSet, status metav1.ConditionStatus, reason, message string, isDeadlineExceeded bool) {
	if isDeadlineExceeded {
		markNotReady(cos, ocv1.ReasonProgressDeadlineExceeded,
			fmt.Sprintf("The object set did not become ready within %d minute(s). Last status: %s", cos.Spec.ProgressDeadlineMinutes, message))
		return
	}
	meta.SetStatusCondition(&cos.Status.Conditions, metav1.Condition{
		Type:               ocv1.ClusterObjectSetTypeReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cos.Generation,
	})
}

// setReconciling reports a transient internal error on the Ready condition.
func setReconciling(cos *ocv1.ClusterObjectSet, detail string, isDeadlineExceeded bool) {
	setReadyWithDeadline(cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonReconciling,
		fmt.Sprintf("Encountered a transient error, retrying: %s", detail), isDeadlineExceeded)
}

func markReady(cos *ocv1.ClusterObjectSet, reason, message string) bool {
	return meta.SetStatusCondition(&cos.Status.Conditions, metav1.Condition{
		Type:               ocv1.ClusterObjectSetTypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cos.Generation,
	})
}

func markNotReady(cos *ocv1.ClusterObjectSet, reason, message string) bool {
	return meta.SetStatusCondition(&cos.Status.Conditions, metav1.Condition{
		Type:               ocv1.ClusterObjectSetTypeReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cos.Generation,
	})
}
```

- [ ] **Step 2: Update `markAsArchived`**

Replace the body of `markAsArchived` (`:697-700`):

```go
func markAsArchived(cos *ocv1.ClusterObjectSet) bool {
	return markNotReady(cos, ocv1.ClusterObjectSetReasonArchived, "The object set is archived.")
}
```

#### 3b. Rework the blocked-class checks (messages)

- [ ] **Step 3: Reword `verifyReferencedSecretsImmutable`'s error (sorted, actionable)**

In `verifyReferencedSecretsImmutable` (`:790-793`), sort the names and reword:

```go
	if len(mutableSecrets) > 0 {
		sort.Strings(mutableSecrets)
		return fmt.Errorf("one or more referenced Secrets are not immutable (must set immutable: true): %s; make them immutable or reference immutable Secrets",
			strings.Join(mutableSecrets, ", "))
	}
```

Add `"sort"` to the import block.

- [ ] **Step 4: Reword `verifyObservedPhases`'s mismatch error (no raw digests, sorted phase names)**

Replace the mismatch accumulation and final error (`:732-744`) so it names only the phases, sorted, and omits digests:

```go
	var mismatched []string
	for _, c := range current {
		if prev, ok := storedMap[c.Name]; ok && prev != c.Digest {
			mismatched = append(mismatched, fmt.Sprintf("%q", c.Name))
		}
	}
	if len(mismatched) > 0 {
		sort.Strings(mismatched)
		return fmt.Errorf(
			"the resolved content for phase(s) %s no longer matches the digest recorded in status.observedPhases; "+
				"a referenced object source may have been deleted and recreated with different content; "+
				"create a new object set to apply the changes",
			strings.Join(mismatched, ", "))
	}
	return nil
```

(The two earlier guards in this function — empty stored, phase-count change — stay as-is; they also surface as `DigestMismatch`.)

#### 3c. Rewire the `reconcile` call sites

- [ ] **Step 5: Blocked-class call sites → `markNotReady`**

In `reconcile`, update the two blocked paths:
- `:137` → `markNotReady(cos, ocv1.ClusterObjectSetReasonMutableReferencedSecret, err.Error())`
- `:151` → `markNotReady(cos, ocv1.ClusterObjectSetReasonDigestMismatch, err.Error())`

- [ ] **Step 6: Transient-error call sites → `setReconciling`**

Replace each `setRetryingConditions(cos, X, isDeadlineExceeded)` / `markAsUnavailable(cos, ...Reconciling, err.Error())` with `setReconciling`:
- `:143` → `setReconciling(cos, err.Error(), isDeadlineExceeded)`
- `:157` → `setReconciling(cos, err.Error(), isDeadlineExceeded)`
- `:171` (archived path, `TrackingCache.Free`) → `setReconciling(cos, err.Error(), false)`
- `:183` (establish watch) → `setReconciling(cos, werr.Error(), isDeadlineExceeded)`
- `:193` (revision reconcile) → `setReconciling(cos, err.Error(), isDeadlineExceeded)`
- `:307` (`delete` path, `TrackingCache.Free`) → `setReconciling(cos, err.Error(), false)`
- `:320` (`archive` path, teardown error) → `setReconciling(cos, err.Error(), false)`

- [ ] **Step 7: Archiving-in-progress call site → `markNotReady`**

In `archive` (`:323-325`), replace the teardown-incomplete `setRetryingConditions` with:

```go
	if tdres != nil && !tdres.IsComplete() {
		markNotReady(cos, ocv1.ClusterObjectSetReasonArchiving, "The object set is being archived; removing owned objects.")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
```

- [ ] **Step 8: Validation + collision call sites (deadline-overridable)**

Replace the preflight validation (`:199-203`), per-phase validation (`:205-210`), and collision (`:219-223`) blocks:

```go
	if verr := rres.GetValidationError(); verr != nil {
		l.Error(fmt.Errorf("%w", verr), "preflight validation failed, retrying after 10s")
		setReadyWithDeadline(cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonValidationFailed,
			fmt.Sprintf("Object set manifests are invalid: %s", verr), isDeadlineExceeded)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	for i, pres := range rres.GetPhases() {
		if verr := pres.GetValidationError(); verr != nil {
			l.Error(fmt.Errorf("%w", verr), "phase preflight validation failed, retrying after 10s", "phase", i)
			setReadyWithDeadline(cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonValidationFailed,
				fmt.Sprintf("Phase %d is invalid: %s", i, verr), isDeadlineExceeded)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		var collidingObjs []string
		for _, ores := range pres.GetObjects() {
			if ores.Action() == machinery.ActionCollision {
				collidingObjs = append(collidingObjs, ores.String())
			}
		}

		if len(collidingObjs) > 0 {
			sort.Strings(collidingObjs)
			l.Error(fmt.Errorf("object collision detected"), "object collision, retrying after 10s", "phase", i, "collisions", collidingObjs)
			setReadyWithDeadline(cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonObjectCollision,
				fmt.Sprintf("Object(s) in phase %d are managed by another object set:\n%s", i, strings.Join(collidingObjs, "\n")), isDeadlineExceeded)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	}
```

- [ ] **Step 9: Drop the bundle version + the bare in-transition RollingOut; set AllObjectsReady / ObjectsNotReady**

Remove the `revVersion := ...` line (`:226`) and the `if rres.InTransition() { ... }` block (`:227-229`). Replace the completion branch and the else branch (`:232-300`):

```go
	//nolint:nestif
	if rres.IsComplete() {
		// Archive previous revisions
		previous, err := c.listPreviousRevisions(ctx, cos)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("listing previous revisions: %v", err)
		}
		for _, a := range previous {
			patch := []byte(`{"spec":{"lifecycleState":"Archived"}}`)
			if err := c.Client.Patch(ctx, client.Object(a), client.RawPatch(types.MergePatchType, patch)); err != nil {
				return ctrl.Result{}, fmt.Errorf("archive previous Revision: %w", err)
			}
		}

		markReady(cos, ocv1.ClusterObjectSetReasonAllObjectsReady, "All objects are ready and pass their probes.")

		if cos.Status.CompletedAt.IsZero() {
			cos.Status.CompletedAt = metav1.NewTime(c.Clock.Now())
		}
	} else {
		var notReadyMsgs []string
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
				notReadyMsgs = append(notReadyMsgs, fmt.Sprintf(
					"Object %s.%s %s/%s: %v",
					gvk.Kind, gvk.GroupVersion().String(),
					obj.GetNamespace(), obj.GetName(), strings.Join(pr.Messages, " and "),
				))
				break
			}
		}

		if isDeadlineExceeded {
			// Deadline wins; use the generic summary as the "last status" so the
			// reconstructed ClusterExtension ProgressDeadlineExceeded message is stable.
			setReadyWithDeadline(cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonObjectsNotReady, "Objects are not yet ready.", true)
		} else {
			sort.Strings(notReadyMsgs)
			msg := "Objects are not yet ready."
			if len(notReadyMsgs) > 0 {
				msg = fmt.Sprintf("%d object(s) are not yet ready:\n%s", len(notReadyMsgs), strings.Join(notReadyMsgs, "\n"))
			}
			setReadyWithDeadline(cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonObjectsNotReady, msg, false)
		}
		if hasDeadline && !isDeadlineExceeded {
			return ctrl.Result{RequeueAfter: remaining}, nil
		}
	}

	return ctrl.Result{}, nil
```

- [ ] **Step 10: Remove the now-unused `labels` import if orphaned**

Run: `go build ./internal/object-controller/...`
If the build reports `labels` imported and not used (the `BundleVersionKey` reference was the only use in this file besides `OwnerNameKey`), keep it — `labels.OwnerNameKey` is still used at `:443`, `:531`. Expected: builds (import still needed). If it errors, remove the unused import.

#### 3d. Update the controller tests

- [ ] **Step 11: Update existing test expectations to the new reasons/messages**

In `clusterobjectset_controller_test.go`, update the assertions:
- Reconcile/factory error cases (`:89`, `:120`, `:145`) now expect `ocv1.ClusterObjectSetReasonReconciling` with message `"Encountered a transient error, retrying: some error"` (and `"...: failed to create revision engine"` for the factory case). Update the message asserts accordingly.
- The seeded condition at `:103-108` should use `ocv1.ClusterObjectSetReasonAllObjectsReady`.
- RollingOut rollout cases (`:167-168`, `:368-369`) now expect `ocv1.ClusterObjectSetReasonObjectsNotReady` and message `"Objects are not yet ready."` (no per-object probe detail configured → generic fallback).
- ProbeFailure cases (`:255`, `:343`) now expect `ocv1.ClusterObjectSetReasonObjectsNotReady`; assert the message starts with `"1 object(s) are not yet ready:\n"` via `require.Contains`.
- The `ProbesSucceeded` success case (`:374...` "set Available:True:ProbesSucceeded...") now expects `ocv1.ClusterObjectSetReasonAllObjectsReady` and message `"All objects are ready and pass their probes."`
- Rename the cosmetic `name:` strings from `Available:...` to `Ready:...` for readability.

- [ ] **Step 12: Add a test — completedAt stamped once on AllObjectsReady**

Add a table case (mirroring the existing success case harness: `revisionResultConfig{isComplete: true}`) that reconciles twice with a `clocktesting.NewFakeClock` and asserts `status.completedAt` is non-zero after the first reconcile and byte-identical after the second (clock advanced). Assertion sketch:

```go
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterObjectSet{}
				require.NoError(t, c.Get(t.Context(), client.ObjectKey{Name: clusterObjectSetName}, rev))
				cond := meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
				require.Equal(t, ocv1.ClusterObjectSetReasonAllObjectsReady, cond.Reason)
				require.False(t, rev.Status.CompletedAt.IsZero())
			},
```

- [ ] **Step 13: Add a test — deadline override for ObjectsNotReady but not for blocked-class**

Add two cases:
1. A not-complete reconcile with `spec.progressDeadlineMinutes` set and the fake clock advanced past it → expect `Ready=False`, reason `ocv1.ReasonProgressDeadlineExceeded`, message containing `"did not become ready within"` and `"Last status: Objects are not yet ready."`.
2. A `verifyReferencedSecretsImmutable` failure (mutable referenced Secret) with the deadline also exceeded → expect reason `ocv1.ClusterObjectSetReasonMutableReferencedSecret` (NOT `ProgressDeadlineExceeded`), proving blocked wins. Seed a mutable `corev1.Secret` referenced by a `ref` object and an exceeded deadline.

- [ ] **Step 14: Run the controller tests**

Run: `go test ./internal/object-controller/... -run Test_ClusterObjectSetReconciler -v`
Expected: PASS.

- [ ] **Step 15: Commit**

```bash
git add internal/object-controller/
git commit -s -m "rewire ClusterObjectSet controller to specific Ready reasons

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: Update the ClusterExtension roll-up reader

`common_controller.go` reconstructs the CE `Progressing`/`Installed` conditions from the COS condition by switching on its reason. Preserve the existing CE-visible **reasons** under the new reason set via a single classification helper. (The condition-type lookups already read the COS `Ready` condition and the CE still emits `Available` — that decoupling was done in Task 1 and is not touched here. Messages flow through from the reworded COS messages by design.)

**Files:**
- Modify: `internal/operator-controller/controllers/common_controller.go` (`determineFailureReason` `:146-158`, `progressingFromAvailable` `:233-263`)
- Test: `internal/operator-controller/controllers/common_controller_test.go`

**Interfaces:**
- Consumes the Task 2 reason constants.
- Produces (file-local): `func isClusterObjectSetErrorReason(reason string) bool` — true for `Reconciling`, `ValidationFailed`, `ObjectCollision` (the reasons that roll up to a CE failure, as the old `Reconciling` did).

- [ ] **Step 1: Add the error-reason classifier**

Add near `determineFailureReason`:

```go
// isClusterObjectSetErrorReason reports whether a ClusterObjectSet Ready-condition
// reason represents an error (as the former single "Reconciling" reason did), which
// the ClusterExtension surfaces as an installation failure.
func isClusterObjectSetErrorReason(reason string) bool {
	switch reason {
	case ocv1.ClusterObjectSetReasonReconciling,
		ocv1.ClusterObjectSetReasonValidationFailed,
		ocv1.ClusterObjectSetReasonObjectCollision:
		return true
	default:
		return false
	}
}
```

- [ ] **Step 2: Update `determineFailureReason`**

Replace the Reconciling check (`:153-156`) and refresh the doc comment wording from "Available" to "Ready":

```go
	// An error reason on the Ready condition indicates an error occurred.
	if availableCond != nil && isClusterObjectSetErrorReason(availableCond.Reason) {
		return ocv1.ReasonFailed
	}
	return ocv1.ReasonAbsent
```

- [ ] **Step 3: Update the `progressingFromAvailable` switch**

Replace the switch (`:249-261`) so blocked-class reasons map to CE `Blocked`, error reasons to `Retrying`, and everything else to `RollingOut`:

```go
	switch {
	case available.Reason == ocv1.ClusterObjectSetReasonMutableReferencedSecret,
		available.Reason == ocv1.ClusterObjectSetReasonDigestMismatch:
		cond.Status = metav1.ConditionFalse
		cond.Reason = ocv1.ReasonBlocked
	case available.Reason == ocv1.ReasonProgressDeadlineExceeded:
		cond.Status = metav1.ConditionFalse
		cond.Reason = ocv1.ReasonProgressDeadlineExceeded
	case isClusterObjectSetErrorReason(available.Reason):
		cond.Reason = ocv1.ReasonRetrying
	default:
		// ObjectsNotReady, AllObjectsReady-but-not-yet-complete, Archiving.
		cond.Reason = ocv1.ReasonRollingOut
	}
```

- [ ] **Step 4: Update the reader tests**

In `common_controller_test.go`:
- Replace `ocv1.ClusterObjectSetReasonBlocked` cases (`:480`, `:531`) with `ocv1.ClusterObjectSetReasonMutableReferencedSecret` (and add a `ClusterObjectSetReasonDigestMismatch` case) — both must still yield CE `Progressing=False/Blocked` and `Installed` reason `Absent` (digest/secret are not error reasons).
- Replace `ocv1.ClusterObjectSetReasonProbeFailure` case (`:519`) with `ocv1.ClusterObjectSetReasonObjectsNotReady` → still `RollingOut`.
- Replace the success `ocv1.ClusterObjectSetReasonProbesSucceeded` (`:507`) with `ocv1.ClusterObjectSetReasonAllObjectsReady`.
- Add cases asserting `ClusterObjectSetReasonValidationFailed` and `ClusterObjectSetReasonObjectCollision` roll up like `Reconciling`: `determineFailureReason` → `Failed`, and `progressingFromAvailable` → `Retrying`.
- Update `TestCEEmitsAvailableConditionType` (added in Task 1 Step 7) to use `ocv1.ClusterObjectSetReasonAllObjectsReady` in its fixture now that `ProbesSucceeded` is going away; the assertions (CE emits `Available`, not `Ready`) stay unchanged.

- [ ] **Step 5: Run the reader tests**

Run: `go test ./internal/operator-controller/controllers/... -v`
Expected: PASS — including `TestCEEmitsAvailableConditionType` (confirms the CE still exposes `Available` after the reason resplit).

- [ ] **Step 6: Commit**

```bash
git add internal/operator-controller/
git commit -s -m "map new ClusterObjectSet reasons in ClusterExtension roll-up

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: Remove obsolete reason constants and rewrite the status doc block

With no remaining references, delete the dead constants and finalize the API documentation.

**Files:**
- Modify: `api/v1/clusterobjectset_types.go` (reason block; status doc block `:493-514`)

- [ ] **Step 1: Confirm no references to the obsolete constants**

Run:

```bash
grep -rn --include='*.go' 'ClusterObjectSetReasonBlocked\|ClusterObjectSetReasonProbeFailure\|ClusterObjectSetReasonProbesSucceeded\|ClusterObjectSetReasonRetrying' .
```

Expected: no matches.

- [ ] **Step 2: Delete the obsolete constants**

Remove the four `Deprecated:` constants (`Blocked`, `ProbeFailure`, `ProbesSucceeded`, `Retrying`) added in Task 2, leaving the active reason set.

- [ ] **Step 3: Rewrite the status `conditions` doc block**

Replace the `Available`-oriented description (`:497-509`) with the `Ready` vocabulary:

```go
	// conditions is an optional list of status conditions describing the state of the
	// ClusterObjectSet.
	//
	// The Ready condition represents the health of the object set.
	// True means all objects are ready; False means one or more objects are not
	// ready or the object set cannot progress; Unknown is the initial state, before
	// the first reconciliation has evaluated the object set.
	//   - True with reason AllObjectsReady: all objects are ready and pass their probes.
	//   - False with reason ObjectsNotReady: one or more objects are not yet ready.
	//   - False with reason ValidationFailed: the object set manifests failed preflight validation.
	//   - False with reason ObjectCollision: one or more objects are managed by another object set.
	//   - False with reason MutableReferencedSecret: a referenced Secret is not immutable.
	//   - False with reason DigestMismatch: resolved content changed after creation.
	//   - False with reason ProgressDeadlineExceeded: the object set did not become ready within spec.progressDeadlineMinutes.
	//   - False with reason Archiving: the object set is being archived.
	//   - False with reason Archived: the object set has been archived and its objects torn down.
	//   - False with reason Reconciling: a transient error prevented reconciliation.
	//
	// Rollout completion is recorded separately by status.completedAt.
```

- [ ] **Step 4: Build and test**

Run: `go build ./... && go test ./api/... ./internal/object-controller/... ./internal/operator-controller/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api/v1/clusterobjectset_types.go
git commit -s -m "remove obsolete ClusterObjectSet reasons; document Ready condition

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: Regenerate CRD, manifests, and API reference; verify

The printcolumn marker and doc-comment changes must be propagated into the generated experimental CRD YAML, the rendered manifests, and the API reference docs.

**Files (generated — do not hand-edit):**
- `helm/olmv1/base/operator-controller/crd/experimental/olm.operatorframework.io_clusterobjectsets.yaml`
- `manifests/experimental.yaml`, `manifests/experimental-e2e.yaml`
- `docs/api-reference/olmv1-api-reference.md`

- [ ] **Step 1: Regenerate**

Run: `make generate manifests crd-ref-docs`
(DeepCopy is unaffected — no struct shape changes — but running `generate` is harmless and matches the documented API-change workflow.)

- [ ] **Step 2: Sanity-check the regenerated CRD printcolumn**

Run: `grep -n "name: Ready" helm/olmv1/base/operator-controller/crd/experimental/olm.operatorframework.io_clusterobjectsets.yaml`
Expected: the additionalPrinterColumns entry now reads `Ready` with JSONPath `.status.conditions[?(@.type=='Ready')].status`; no `Available` printcolumn remains for this CRD.

- [ ] **Step 3: Verify everything is in sync**

Run: `make verify`
Expected: PASS (no uncommitted generated diffs, lint clean). If `make verify` reports drift, commit the regenerated files it points to.

- [ ] **Step 4: Full unit suite**

Run: `make test-unit`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add helm/ manifests/ docs/api-reference/
git commit -s -m "regenerate CRD, manifests, and docs for Ready condition

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Notes carried from the spec (deferred — out of scope here)

- **`Superseded` reason** (objects adopted by a newer sibling revision) is intentionally **not** implemented in this plan; boxcutter support is confirmed and documented in the spec for a later addition.
- When `ClusterObjectDeployment` lands, `ProgressDeadlineExceeded` moves to `COD.Progressing` and the ClusterExtension should observe COD rather than reading COS directly in `common_controller.go`. Not part of this plan.
