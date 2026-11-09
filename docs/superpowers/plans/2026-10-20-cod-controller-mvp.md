# ClusterObjectDeployment Controller MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the ClusterObjectDeployment (COD) controller create a new ClusterObjectSet (COS) revision on template change and report rollout progress via its `Progressing` condition.

**Architecture:** The COD reconciler (in the `object-controller` binary, `//go:build !standard`) lists COS in its group via the `.spec.group` index, detects template changes with a SHA-256 hash stored as a label on each COS, creates a new typed COS revision (`<cod>-<rev>`) owned by the COD when the hash differs, and sets `Progressing` from the latest matching COS's `completedAt`. Requeues come from the existing `Owns(COS)` watch.

**Tech Stack:** Go, controller-runtime, k8s apimachinery `meta`/`conditions`, gomock not needed (fake client), godog for e2e.

**Spec:** `docs/superpowers/specs/2026-10-20-cod-controller-mvp-design.md`

## Global Constraints

- COD controller code is experimental-only: every new controller file carries `//go:build !standard` (match `clusterobjectdeployment_controller.go`).
- API group / label prefix is `olm.operatorframework.io`.
- Child COS naming is `<cod-name>-<revision>`; COS `spec.group` = `cod.Name`.
- Label-value limit is 63 chars: the template-hash digest is `hex(sha256(...)[:16])` = 32 chars.
- COS `spec.collisionProtection` is `+required`; default to `Prevent` when the template omits it.
- New COS are created with plain typed `Client.Create`; `AlreadyExists` is treated as success.
- Status is written via the status subresource (`Status().Update`), only when it changed.
- Reuse existing constants: `ocv1.ClusterObjectDeploymentTypeProgressing`, `ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing`, `...Progressed`, `ocv1.ClusterObjectSetLifecycleStateActive`/`Archived`, `labels.OwnerKindKey`/`OwnerNameKey`.
- E2e feature files go in `test/e2e/features/` named `cod_<feature>.feature`, tagged `@BoxcutterRuntime` (Feature-level), run on the experimental cluster.

## Review Focus

- **Template change to a *mutable* COS field (`progressionProbes` only).** Must still produce a new revision (this is the whole reason for the hash approach). Covered by Task 1 hash-sensitivity test.
- **`collisionProtection` omitted in the template.** Must become `Prevent` on the COS or the COS is rejected (required field). Covered by Task 1 `collisionProtectionOrDefault` + BuildCOS tests.
- **Requeue race / duplicate create** (`<cod>-<rev>` already exists because `revs` was stale). Must not error the reconcile. Covered by Task 2 AlreadyExists test.
- **Latest active revision is Archived (or deleting).** `latestActiveRevision` must skip it so a fresh revision is created, and `nextRevisionNumber` must still count archived revisions so numbers never repeat. Covered by Task 1 `latestActiveRevision`/`nextRevisionNumber` tests and Task 2 "archived latest" test.
- **COD under deletion.** Reconcile must be a no-op (children cascade via owner ref); must not create a revision for a deleting COD. Covered by Task 2 deletion test.

---

### Task 1: Hash + COS-build helpers (pure functions)

**Files:**
- Modify: `internal/shared/labels/labels.go` (add `TemplateHashKey`)
- Modify: `internal/object-controller/controllers/clusterobjectdeployment_controller.go` (add helpers + `Scheme` field)
- Test: `internal/object-controller/controllers/clusterobjectdeployment_controller_internal_test.go` (extend existing internal test file)

**Interfaces:**
- Produces:
  - `labels.TemplateHashKey = "olm.operatorframework.io/template-hash"` (string const)
  - `func templateHash(template ocv1.ClusterObjectDeploymentTemplate) (string, error)` — `hex(sha256(json.Marshal(template))[:16])`
  - `func collisionProtectionOrDefault(cp ocv1.CollisionProtection) ocv1.CollisionProtection` — returns `ocv1.CollisionProtectionPrevent` when `cp == ""`
  - `func nextRevisionNumber(revisions []ocv1.ClusterObjectSet) int64` — `max(spec.revision)+1`, `1` when empty
  - `func latestActiveRevision(revisions []ocv1.ClusterObjectSet) *ocv1.ClusterObjectSet` — highest-revision COS with zero `DeletionTimestamp` and `LifecycleState != Archived`; `nil` if none
  - `func buildClusterObjectSet(cod *ocv1.ClusterObjectDeployment, revision int64, hash string) *ocv1.ClusterObjectSet` — sets name/labels/spec per design (no owner ref; the caller sets that)
  - `ClusterObjectDeploymentReconciler` gains field `Scheme *runtime.Scheme`

- [ ] **Step 1: Write failing tests** in `clusterobjectdeployment_controller_internal_test.go`:
  - `Test_templateHash_isStableAndSensitive`: same template → equal, non-empty, len 32; changing a phase object changes it; changing *only* `ProgressionProbes` changes it; result is lowercase hex.
  - `Test_collisionProtectionOrDefault`: `""` → `Prevent`; `None` → `None`; `IfNoController` → `IfNoController`.
  - `Test_nextRevisionNumber`: empty → 1; `{1,3}` (incl. an Archived rev 3) → 4.
  - `Test_latestActiveRevision`: picks highest non-archived/non-deleting; returns nil when all archived; skips a higher-revision COS with a non-zero `DeletionTimestamp`.
  - `Test_buildClusterObjectSet`: name `cod-1`, labels owner-kind=`ClusterObjectDeployment`/owner-name=`cod`/template-hash=`h`, spec group=`cod`, revision=1, lifecycleState=Active, phases+progressionProbes copied, collisionProtection defaulted to `Prevent` when template empty.

- [ ] **Step 2: Run tests, verify they fail** — `go test -tags containers_image_openpgp ./internal/object-controller/controllers/ -run 'templateHash|collisionProtectionOrDefault|nextRevisionNumber|latestActiveRevision|buildClusterObjectSet'` → FAIL (undefined). (envtest assets already set; `KUBEBUILDER_ASSETS` from `make envtest-k8s-bins`.)

- [ ] **Step 3: Add `TemplateHashKey` to `internal/shared/labels/labels.go`** next to `OwnerKindKey`/`OwnerNameKey`.

- [ ] **Step 4: Implement the helpers** in `clusterobjectdeployment_controller.go` with the signatures above and add the `Scheme *runtime.Scheme` field to the reconciler struct. `buildClusterObjectSet` copies `cod.Spec.Template.Spec.Phases`/`ProgressionProbes` by reference (fresh COS object, safe) and does not set the owner reference.

- [ ] **Step 5: Run tests, verify they pass.**

- [ ] **Step 6: Commit** — `git commit -s` "Add COD template hashing and COS build helpers".

---

### Task 2: Reconcile behavior (create-on-change + Progressing)

**Files:**
- Modify: `internal/object-controller/controllers/clusterobjectdeployment_controller.go` (Reconcile body, `reconcile`, `listRevisions`, `createRevision`, `setProgressing`)
- Modify: `cmd/object-controller/main.go` (pass `Scheme: mgr.GetScheme()` to the reconciler)
- Test: `internal/object-controller/controllers/clusterobjectdeployment_controller_test.go` (extend existing `controllers_test` file)

**Interfaces:**
- Consumes: all Task 1 helpers; `ocv1.ClusterObjectSet.Status.CompletedAt` (`metav1.Time`, `.IsZero()`), `controllers.SetupIndexes` (registers `.spec.group`), `testutil.WithIndexes`.
- Produces:
  - `func (c *ClusterObjectDeploymentReconciler) reconcile(ctx context.Context, cod *ocv1.ClusterObjectDeployment) error`
  - `func (c *ClusterObjectDeploymentReconciler) listRevisions(ctx context.Context, group string) ([]ocv1.ClusterObjectSet, error)` — `List` by `client.MatchingFields{".spec.group": group}`, sorted ascending by `spec.revision`
  - `func (c *ClusterObjectDeploymentReconciler) createRevision(ctx context.Context, cod *ocv1.ClusterObjectDeployment, revisions []ocv1.ClusterObjectSet, hash string) error` — builds COS, `controllerutil.SetControllerReference(cod, cos, c.Scheme)`, `Client.Create`, ignores `apierrors.IsAlreadyExists`
  - `func setProgressing(cod *ocv1.ClusterObjectDeployment, status metav1.ConditionStatus, reason, message string)` — `meta.SetStatusCondition` with `Type=Progressing`, `ObservedGeneration=cod.Generation`

- [ ] **Step 1: Write failing behavioral tests** in `clusterobjectdeployment_controller_test.go` (table-driven, `testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(testScheme), controllers.SetupIndexes).WithStatusSubresource(&ocv1.ClusterObjectDeployment{}).WithObjects(...).Build()`), each calling `(&controllers.ClusterObjectDeploymentReconciler{Client: c, Scheme: testScheme}).Reconcile(...)`:
  - no existing COS → a COS `cod-1` is created with group=cod, revision=1, lifecycleState=Active, owner-ref controller=COD, template-hash label set; COD `Progressing=True/NewClusterObjectSetProgressing`.
  - existing `cod-1` with matching hash label and `completedAt` set → no `cod-2`; COD `Progressing=False/NewClusterObjectSetProgressed`.
  - existing `cod-1` with matching hash label and zero `completedAt` → no `cod-2`; COD `Progressing=True/NewClusterObjectSetProgressing`.
  - existing `cod-1` with a *different* hash label (template changed) → `cod-2` created with revision=2; `Progressing=True`.
  - existing `cod-1` Archived (latest active = none) → `cod-2` created (revision=2).
  - existing `cod-1` already present when creating revision 1 (seed `cod-1` but force the create path via a stale list) → reconcile returns no error (AlreadyExists tolerated). *(Simplest: assert `createRevision` tolerates AlreadyExists by pre-seeding `cod-1` and calling reconcile on a COD whose latest active has a mismatching hash so revision 2 is attempted; then separately unit-test AlreadyExists by seeding the exact next-revision name.)*
  - COD with non-zero `DeletionTimestamp` → no COS created, no status change.

- [ ] **Step 2: Run tests, verify they fail** — `go test -tags containers_image_openpgp ./internal/object-controller/controllers/ -run ClusterObjectDeploymentReconciler_Reconcile -v` → FAIL.

- [ ] **Step 3: Implement the Reconcile flow** per the design's Reconcile/reconcile/createRevision/setProgressing pseudocode. Outer `Reconcile`: Get (IgnoreNotFound) → if `!DeletionTimestamp.IsZero()` return → DeepCopy → `reconcile` → if `!equality.Semantic.DeepEqual(existing.Status, reconciled.Status)` then `Status().Update`, joining errors. `reconcile`: list revisions → hash → latestActive → create-or-noop → setProgressing. Add imports (`crypto/sha256`, `encoding/json`, `encoding/hex`, `apierrors`, `apimeta`/`meta`, `controllerutil`, `runtime`, `equality`).

- [ ] **Step 4: Wire `Scheme` in `cmd/object-controller/main.go`** — add `Scheme: mgr.GetScheme(),` to the `ClusterObjectDeploymentReconciler{...}` literal.

- [ ] **Step 5: Run tests, verify they pass**; then `go build -tags containers_image_openpgp ./... ` and `golangci-lint run` on the touched packages.

- [ ] **Step 6: Commit** — "Implement COD create-on-change and Progressing status".

---

### Task 3: E2E step — assert a condition on a ClusterObjectDeployment

**Files:**
- Modify: `test/e2e/steps/steps.go` (register step + handler)

**Interfaces:**
- Consumes: existing `waitForCondition(ctx, resourceType, name, type, status, *reason, msgCmp)`, `substituteScenarioVars`, `scenarioCtx`.
- Produces:
  - Step regex `^ClusterObjectDeployment "([^"]*)" reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+)$`
  - `func ClusterObjectDeploymentReportsConditionWithoutMsg(ctx context.Context, name, conditionType, conditionStatus, conditionReason string) error` → `waitForCondition(ctx, "clusterobjectdeployment", substituteScenarioVars(name, scenarioCtx(ctx)), conditionType, conditionStatus, &conditionReason, nil)`

- [ ] **Step 1: Implement** the handler mirroring `ClusterObjectSetReportsConditionWithoutMsg` (steps.go:996) and register it in `RegisterSteps`.

- [ ] **Step 2: Verify the step is registered and the suite compiles** — `go test ./test/e2e/features_test.go -d 2>&1 | grep -i clusterobjectdeployment` shows the new regex; `go vet ./test/e2e/...`.

- [ ] **Step 3: Commit** — "Add e2e step for ClusterObjectDeployment conditions".

---

### Task 4: E2E feature — `cod_rollout.feature`

**Files:**
- Create: `test/e2e/features/cod_rollout.feature`

**Interfaces:**
- Consumes: `resource is applied` (DocString), `namespace "<ns>" is available`, `ClusterObjectSet "<name>" reports Ready as True with Reason AllObjectsReady`, `ClusterObjectSet "<name>" has group "<group>"`, `resource "clusterobjectset/<name>" is not found`, and the Task 3 COD condition step. COD named `${NAME}`; children `${NAME}-1`/`${NAME}-2`; objects in `${TEST_NAMESPACE}`.

- [ ] **Step 1: Write the feature** — Feature tagged `@BoxcutterRuntime`, `Background: Given OLM is available`. Scenarios:
  - *Initial rollout*: `namespace "${TEST_NAMESPACE}" is available`; apply a COD (`${NAME}`) with one phase containing a ConfigMap in `${TEST_NAMESPACE}`; then `ClusterObjectSet "${NAME}-1" has group "${NAME}"`, `ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady`, `ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason NewClusterObjectSetProgressed`.
  - *Template change creates a new revision*: given the above; re-apply `${NAME}` with a second ConfigMap added; then `ClusterObjectSet "${NAME}-2" reports Ready as True with Reason AllObjectsReady`, `ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason NewClusterObjectSetProgressed`.
  - *Unchanged template creates no new revision*: given the initial rollout; re-apply the identical `${NAME}`; then `resource "clusterobjectset/${NAME}-2" is not found`.

- [ ] **Step 2: Validate the feature parses and all steps are defined** — `go test ./test/e2e/features_test.go -d` lists the scenarios with no "undefined step". (Full execution needs the experimental cluster: `make experimental-e2e-setup` then `make e2e/cod_rollout KUBECONFIG=.kubeconfig/operator-controller-experimental-e2e.kubeconfig`; run if a cluster is available, otherwise note it as the reviewer/CI gate.)

- [ ] **Step 3: Commit** — "Add cod_rollout e2e feature".

---

## Notes for the executor

- `KUBEBUILDER_ASSETS` for unit tests: `export KUBEBUILDER_ASSETS="$(bin/… use -p path)"` — already downloaded via `make envtest-k8s-bins` to `bin/envtest-binaries/k8s/<ver>-<os>-<arch>`.
- After all tasks: `make generate` / `make manifests` should be a no-op (no API or RBAC changes); run them and confirm no diff before the final PR, reverting any unrelated controller-gen doc-comment drift (known gotcha).
- This work is on branch `cod-controller-mvp` (off `cod-controller-skeleton` / PR #3032); open the MVP PR against `cod-controller-skeleton` or `main` depending on merge order of #3032.
