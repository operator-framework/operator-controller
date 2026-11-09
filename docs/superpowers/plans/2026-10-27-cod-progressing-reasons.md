# ClusterObjectDeployment Progressing reasons Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the ClusterObjectDeployment `Progressing` condition a COD-level reason vocabulary that distinguishes a normally-progressing rollout from one impeded by the head active ClusterObjectSet, and surfaces what the user must do.

**Architecture:** `reconcileRollout` reads the head active revision's `Ready` condition reason and maps it onto the COD `Progressing` condition. Hard stalls (`ObjectCollision`, `InvalidSpecification`) report `Progressing=False` and short-circuit the deadline; `PreconditionsNotMet` reports `Progressing=True` (still retrying) and is governed by the deadline; all other states fall through to the existing default progressing branch. No ClusterObjectSet-side changes.

**Tech Stack:** Go, controller-runtime, apimachinery `apimeta`, Godog e2e.

**Spec:** `docs/superpowers/specs/2026-10-27-cod-progressing-reasons-design.md`

## Global Constraints

- COD is an experimental, cluster-scoped API; changes are COD-controller-side only. No ClusterObjectSet controller, migrator, or spec changes.
- API domain for any generated label/annotation prefixes: `olm.operatorframework.io`.
- The `conditions` field docstring is the CRD schema field description; after editing it run `make manifests crd-ref-docs` and commit the regenerated CRD YAMLs and `docs/api-reference/olmv1-api-reference.md`.
- No new Go struct fields are added, so no DeepCopy regeneration is expected; `make verify` must still pass.
- Before opening the PR: `make lint`, `make test-unit`, `make verify`.
- New reason strings (exact, verbatim): `RevisionPreconditionsNotMet`, `RevisionObjectCollision`, `RevisionInvalidSpecification`.
- True/False mapping (fixed): `InvalidSpecification`→False, `ObjectCollision`→False, `PreconditionsNotMet`→True.
- Ordering (fixed): hard stalls are evaluated **before** the deadline; `PreconditionsNotMet` **after** the deadline.

## Review Focus

- **Head revision has no `Ready` condition yet** (freshly created COS): must fall through to `Progressing=True / NewClusterObjectSetProgressing`, never a false hard stall. Pinned in Task 2.
- **Unrecognized / future head-revision `Ready` reason** (e.g. `Pending`, `ObjectsNotReady`, or a reason added later): must fall through to the default progressing branch. Pinned in Task 2.
- **Hard stall arriving after the deadline has elapsed**: `ObjectCollision` / `InvalidSpecification` must still report their stall reason, not `ProgressDeadlineExceeded`. Pinned in Task 2.
- **`PreconditionsNotMet` after the deadline has elapsed**: must report `ProgressDeadlineExceeded` (deadline governs the retrying state), not `RevisionPreconditionsNotMet`. Pinned in Task 2.
- **A completed revision that later reports `ObjectCollision`/`InvalidSpecification`**: completion is historical and must still win (`NewClusterObjectSetProgressed`), the stall must not resurrect. Pinned in Task 2.

---

### Task 1: API reason constants, condition docstring, and regenerated artifacts

**Files:**
- Modify: `api/v1/clusterobjectdeployment_types.go` (reason consts block ~lines 31–35; `ClusterObjectDeploymentStatus.Conditions` docstring ~lines 142–157)
- Regenerate (do not hand-edit): `helm/olmv1/base/**/crd/experimental/*clusterobjectdeployments*.yaml`, `helm/olmv1/base/**/crd/standard/*` (if present), `manifests/*.yaml`, `docs/api-reference/olmv1-api-reference.md`

**Interfaces:**
- Produces: exported consts `ClusterObjectDeploymentReasonRevisionPreconditionsNotMet = "RevisionPreconditionsNotMet"`, `ClusterObjectDeploymentReasonRevisionObjectCollision = "RevisionObjectCollision"`, `ClusterObjectDeploymentReasonRevisionInvalidSpecification = "RevisionInvalidSpecification"` in package `v1`.

- [ ] **Step 1: Add the three reason constants** to the "Progressing condition reasons" block in `api/v1/clusterobjectdeployment_types.go`, keeping gofmt alignment with the existing consts:

```go
ClusterObjectDeploymentReasonRevisionPreconditionsNotMet  = "RevisionPreconditionsNotMet"
ClusterObjectDeploymentReasonRevisionObjectCollision      = "RevisionObjectCollision"
ClusterObjectDeploymentReasonRevisionInvalidSpecification = "RevisionInvalidSpecification"
```

- [ ] **Step 2: Extend the `conditions` field docstring** (`ClusterObjectDeploymentStatus.Conditions`) to document the new reasons. Add prose covering: `Progressing` is True with reason `RevisionPreconditionsNotMet` when the head active revision's preconditions (e.g. RBAC or a dependency CRD) are not yet met and the controller keeps retrying; False with reason `RevisionObjectCollision` when the head revision's objects are owned by another controller or already exist (the user must resolve ownership); False with reason `RevisionInvalidSpecification` when the head revision's objects are invalid (distinct from the deployment's own `InvalidSpecification`, which is set for invalid `spec.template.metadata` before a revision exists). Keep the existing sentences for the other reasons.

- [ ] **Step 3: Regenerate CRDs and docs**

Run: `make manifests crd-ref-docs`
Expected: the experimental COD CRD YAML(s) and `docs/api-reference/olmv1-api-reference.md` show the updated `conditions` description; no other unrelated diffs.

- [ ] **Step 4: Verify the tree builds and generation is in sync**

Run: `go build ./... && make verify`
Expected: build succeeds; `make verify` reports no uncommitted generated changes.

- [ ] **Step 5: Commit**

```bash
git add api/v1/clusterobjectdeployment_types.go helm/ manifests/ docs/api-reference/
git commit -m "✨ add COD Progressing reasons for impeded head revision rollouts"
```

---

### Task 2: Controller mapping of head revision Ready reason onto Progressing

**Files:**
- Modify: `internal/object-controller/controllers/clusterobjectdeployment_controller.go` (`reconcileRollout`, ~lines 128–156; add a small helper near the other helpers)
- Test: `internal/object-controller/controllers/clusterobjectdeployment_controller_test.go` (extend the tabular `Test_ClusterObjectDeploymentReconciler_NoNewRevisionWhenTemplateUnchanged`, ~lines 382–411)
- Test: `internal/object-controller/controllers/clusterobjectdeployment_deadline_test.go` (precedence cases)

**Interfaces:**
- Consumes: reason consts from Task 1; existing `setProgressing(cod, status, reason, message)`, `latestActiveRevision`, `durationUntilDeploymentDeadline`, `ocv1.ClusterObjectSetTypeReady`, and the COS reason consts `ClusterObjectSetReasonInvalidSpecification`/`ObjectCollision`/`PreconditionsNotMet`.
- Produces: unexported helper `headRevisionReadyReason(cos *ocv1.ClusterObjectSet) string` returning the head revision's `Ready` condition reason, or `""` when absent. The COD message is COD-owned generic text naming the revision (decision 4), not the COS message.

- [ ] **Step 1: Write the failing tests.** Add a test helper `setRevisionReadyReason(t, c, cosName, reason)` (sets the named COS's `Ready` condition to `status=False, reason=<reason>` with a non-empty message) alongside the existing `expireRevisionDeadline`/`getCOS` helpers. Then add cases to the tabular test:

```go
{
    name:       "head revision ObjectCollision reports RevisionObjectCollision and is not progressing",
    prep:       func(t *testing.T, c client.Client, cosName string) { setRevisionReadyReason(t, c, cosName, ocv1.ClusterObjectSetReasonObjectCollision) },
    wantStatus: metav1.ConditionFalse,
    wantReason: ocv1.ClusterObjectDeploymentReasonRevisionObjectCollision,
},
{
    name:       "head revision InvalidSpecification reports RevisionInvalidSpecification and is not progressing",
    prep:       func(t *testing.T, c client.Client, cosName string) { setRevisionReadyReason(t, c, cosName, ocv1.ClusterObjectSetReasonInvalidSpecification) },
    wantStatus: metav1.ConditionFalse,
    wantReason: ocv1.ClusterObjectDeploymentReasonRevisionInvalidSpecification,
},
{
    name:       "head revision PreconditionsNotMet reports RevisionPreconditionsNotMet and keeps progressing",
    prep:       func(t *testing.T, c client.Client, cosName string) { setRevisionReadyReason(t, c, cosName, ocv1.ClusterObjectSetReasonPreconditionsNotMet) },
    wantStatus: metav1.ConditionTrue,
    wantReason: ocv1.ClusterObjectDeploymentReasonRevisionPreconditionsNotMet,
},
{
    name:       "head revision with no Ready condition falls through to NewClusterObjectSetProgressing",
    prep:       nil, // default fixture COS has no Ready condition
    wantStatus: metav1.ConditionTrue,
    wantReason: ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing,
},
{
    name:       "head revision with unrecognized Ready reason falls through to NewClusterObjectSetProgressing",
    prep:       func(t *testing.T, c client.Client, cosName string) { setRevisionReadyReason(t, c, cosName, ocv1.ClusterObjectSetReasonObjectsNotReady) },
    wantStatus: metav1.ConditionTrue,
    wantReason: ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing,
},
{
    name: "completed revision that also reports ObjectCollision still reports Progressed",
    prep: func(t *testing.T, c client.Client, cosName string) {
        setRevisionReadyReason(t, c, cosName, ocv1.ClusterObjectSetReasonObjectCollision)
        cos, ok := getCOS(t, c, cosName); require.True(t, ok)
        cos.Status.CompletedAt = metav1.Now()
        require.NoError(t, c.Status().Update(t.Context(), cos))
    },
    wantStatus: metav1.ConditionFalse,
    wantReason: ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed,
},
```

Add a message-substring assertion to at least the `PreconditionsNotMet`, `ObjectCollision`, and `InvalidSpecification` cases proving the COD `Progressing` message is the COD-owned generic text that **names the head revision** and does **not** echo the COS message (decision 4): have `setRevisionReadyReason` write a distinctive COS message, then assert the COD message contains the revision name and does **not** contain that distinctive COS string.

In `clusterobjectdeployment_deadline_test.go`:
- Add the two precedence cases: (a) deadline expired **and** head `Ready` reason `ObjectCollision` → `ConditionFalse / RevisionObjectCollision` (hard stall wins); (b) deadline expired **and** head `Ready` reason `PreconditionsNotMet` → `ConditionFalse / ProgressDeadlineExceeded` (deadline governs the retrying state). Reuse `expireRevisionDeadline` + `setRevisionReadyReason`.
- **Flip the existing `invalid specification` subtest** in `TestClusterObjectDeploymentReconcilerDeadlineWithoutCOSDeadlineCondition` (~line 218): today a past-deadline COS with `Ready=False/InvalidSpecification` asserts `ProgressDeadlineExceeded`; under the new precedence it must assert `ConditionFalse / RevisionInvalidSpecification`. This is a behavior change to a currently-passing test, not a new case.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/object-controller/controllers/ -run ClusterObjectDeployment -v`
Expected: FAIL — new reason consts referenced but branches not implemented (wrong reason/status returned); compiles once Task 1 consts exist.

- [ ] **Step 3: Add the helper** `headRevisionReadyReason(cos *ocv1.ClusterObjectSet) string` using `apimeta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeReady)`, returning `""` when the condition is absent.

- [ ] **Step 4: Implement the branching in `reconcileRollout`.** After the `!latest.Status.CompletedAt.IsZero()` completed-branch and before the deadline check, evaluate hard stalls; after the deadline check, evaluate `PreconditionsNotMet`; otherwise keep the existing default. The COD message is **COD-owned generic text that names the head revision** and directs the user to inspect it — it must **not** echo the COS message (decision 4). Exact phrasing is the builder's call; the strings below are suggested defaults. Hard-stall branches return `ctrl.Result{}` (no bespoke timed requeue; the COS watch re-triggers). `PreconditionsNotMet` and the default branch keep the existing `RequeueAfter: remaining` when `hasDeadline`.

```go
switch headRevisionReadyReason(latest) {
case ocv1.ClusterObjectSetReasonInvalidSpecification:
    setProgressing(cod, metav1.ConditionFalse, ocv1.ClusterObjectDeploymentReasonRevisionInvalidSpecification,
        fmt.Sprintf("latest revision %q has an invalid specification; inspect the ClusterObjectSet for details", latest.Name))
    return ctrl.Result{}, nil
case ocv1.ClusterObjectSetReasonObjectCollision:
    setProgressing(cod, metav1.ConditionFalse, ocv1.ClusterObjectDeploymentReasonRevisionObjectCollision,
        fmt.Sprintf("latest revision %q cannot take ownership of one or more objects owned by another controller or already present; inspect the ClusterObjectSet for details", latest.Name))
    return ctrl.Result{}, nil
}

remaining, hasDeadline := durationUntilDeploymentDeadline(c.Clock, cod, latest)
if hasDeadline && remaining <= 0 {
    setProgressing(cod, metav1.ConditionFalse, ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded, "latest revision exceeded its progress deadline")
    return ctrl.Result{}, nil
}

if headRevisionReadyReason(latest) == ocv1.ClusterObjectSetReasonPreconditionsNotMet {
    setProgressing(cod, metav1.ConditionTrue, ocv1.ClusterObjectDeploymentReasonRevisionPreconditionsNotMet,
        fmt.Sprintf("latest revision %q is waiting for preconditions to be met; inspect the ClusterObjectSet for details", latest.Name))
    if hasDeadline {
        return ctrl.Result{RequeueAfter: remaining}, nil
    }
    return ctrl.Result{}, nil
}

setProgressing(cod, metav1.ConditionTrue, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing, "latest revision is progressing")
// existing requeue tail unchanged
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/object-controller/controllers/ -run ClusterObjectDeployment -v`
Expected: PASS (all new and existing cases).

- [ ] **Step 6: Commit**

```bash
git add internal/object-controller/controllers/clusterobjectdeployment_controller.go internal/object-controller/controllers/clusterobjectdeployment_controller_test.go internal/object-controller/controllers/clusterobjectdeployment_deadline_test.go
git commit -m "✨ map head revision Ready reason onto COD Progressing condition"
```

---

### Task 3: e2e coverage

**Files:**
- Create: `test/e2e/features/cod_progressing_reasons.feature`
- Modify (only if a new step is needed): the COD e2e step-definition file backing `test/e2e/features/cod_*.feature`

**Interfaces:**
- Consumes: the COD `Progressing` reasons from Task 1 and the behavior from Task 2.

- [ ] **Step 1: Inventory existing steps.** Run the `list-e2e-steps` skill (or read the step defs) to find existing steps for creating a COD, asserting a condition's `status`+`reason`, and pre-creating a foreign-owned object. Reuse them; add a new step only where none fits.

- [ ] **Step 2: Write scenarios** asserting the COD `Progressing` condition `status` and `reason` (and, where practical, that the message names the head revision):
  - `RevisionObjectCollision` (False): pre-create an object the head revision's phase collides with, apply a COD whose template renders that object.
  - `RevisionPreconditionsNotMet` (True): drive the head revision into `PreconditionsNotMet` (unsatisfied referenced-content precondition, mirroring the COS `revision.feature` referenced-content scenario); assert recovery to progressing/complete once satisfied.
  - Hard-stall-beats-deadline: a `RevisionObjectCollision` stall reports the stall reason rather than `ProgressDeadlineExceeded` after the deadline window.

  **No `RevisionInvalidSpecification` e2e** — that COS state is not reliably reachable from an admission-passing COD template; it is covered by the Task 2 unit test only (per the design's reachability note). If `PreconditionsNotMet` turns out impractical to reach deterministically, drop it and note in the PR that the unit test covers it, rather than shipping a flaky scenario.

- [ ] **Step 3: Run the feature against the experimental e2e cluster**

Run (per AGENTS.md / e2e local validation workflow): bring up the experimental cluster, then `make "e2e/cod_progressing_reasons" KUBECONFIG=<absolute experimental kubeconfig>` (adjust target name to the feature file).
Expected: all scenarios pass.

- [ ] **Step 4: Commit**

```bash
git add test/e2e/
git commit -m "✨ e2e for COD Progressing impeded-rollout reasons"
```

---

### Task 4: Concept documentation

**Files:**
- Modify: `docs/draft/concepts/clusterobjectsets.md` (or the COD-facing concept page if one exists; create a short COD rollout-status section if none covers it)

**Interfaces:**
- Consumes: the reason vocabulary and behavior from Tasks 1–2.

- [ ] **Step 1: Locate the COD/COS rollout-status discussion** in `docs/draft/concepts/clusterobjectsets.md` (and grep `docs/` for a COD `Progressing` mention).

- [ ] **Step 2: Document the impeded-rollout reasons** — a short table or list mapping `RevisionPreconditionsNotMet` / `RevisionObjectCollision` / `RevisionInvalidSpecification` to what each means and what the user should do, and noting hard stalls flip `Progressing=False` while `PreconditionsNotMet` keeps retrying.

- [ ] **Step 3: Verify docs build if the repo builds docs**

Run: `make docs` if such a target exists (otherwise skip).
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add docs/
git commit -m "📖 document COD impeded-rollout Progressing reasons"
```
