# COD Progress-Deadline Durability E2E (OPRUN-4795) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Validate the delivered COD revision-level progress-deadline contract (OPRUN-4791) with three focused e2e scenarios: deadline durability across a controller restart, post-completion health regression, and stall-and-recover without an extra revision.

**Architecture:** godog BDD e2e scenarios under `test/e2e/features/`, backed by step helpers in `test/e2e/steps/`. Reuses existing registered steps; adds one new step to restart the object-controller manager (the existing `rollout restart` step is hardcoded to the scenario namespace and cannot target `olmv1-system`). Scenarios run against the experimental KIND cluster (object-controller enabled via `@BoxcutterRuntime`).

**Tech Stack:** Go, cucumber/godog, controller-runtime client, KIND.

**Spec:** OPRUN-4795 ("Add COD stalled-rollout, creation-failure and restart e2e scenarios"), realigned to the OPRUN-4791 delivered contract.

## Global Constraints

- Repo file cap: <=10 files across the branch; manual budget 2-4 files. This plan touches 2: `cod_progress_deadline.feature` (new) and `test/e2e/steps/steps.go` (one new step + registration).
- All scenarios carry `@BoxcutterRuntime`; deadline scenarios additionally carry `@ProgressDeadline` (matching existing `cod_progressing_reasons.feature` tagging).
- Deadline duration in tests: relax the CRD minimum with the existing step `min value for ClusterObjectDeployment .spec.template.spec.progressDeadlineMinutes is set to 1`, then use `progressDeadlineMinutes: 1`. This is a test-only CRD-validation relaxation, not a production timing override.
- Delivered contract (verbatim, OPRUN-4791): deadline is one budget per revision from the COS `metadata.creationTimestamp` to `status.completedAt`; creation time excluded; phases share one budget and progress never extends it; timing derives from durable COS timestamps via an injectable clock so a restart reconstructs the same deadline; `completedAt` latches and ends deadline timing even after later health regressions; `Available` stays True while an active revision is Ready.
- Controller target for restart: Deployment `object-controller-controller-manager` in namespace `olmv1-system`.
- Do not refactor existing steps; add only.

## Review Focus

- **Restart must not lose an exceeded deadline:** a controller that anchored the deadline on process start (not the durable `creationTimestamp`) would, after restart, treat the revision as freshly observed and flip Progressing back to True. Pinned in Task 2 by restarting *after* expiry and asserting `ProgressDeadlineExceeded` holds consistently.
- **Post-completion deadline must stay disabled:** once `completedAt` latches, a later health regression must not re-arm the deadline. Pinned in Task 1 by asserting Progressing stays `NewClusterObjectSetProgressed` while Available flips.
- **No spurious revision during a stall:** a blocked revision must not cause a second revision to be cut. Pinned in Task 3 by `owns 1 ClusterObjectSets` after recovery.

---

## File Structure

- `test/e2e/features/cod_progress_deadline.feature` (create): the three scenarios.
- `test/e2e/steps/steps.go` (modify): add `RestartObjectController` + one registration line.

Existing registered steps reused: `OLM is available`; `namespace "<ns>" is available`; `min value for ClusterObjectDeployment <path> is set to <n>`; `resource is applied`; `resource "<k>/<n>" is removed`; `ClusterObjectSet "<n>" reports <T> as <S> with Reason <R>`; `ClusterObjectDeployment "<n>" reports <T> as <S> with Reason <R>`; `ClusterObjectDeployment "<n>" consistently reports <T> as <S> with Reason <R> for "<dur>"`; `ClusterObjectDeployment "<n>" owns <N> ClusterObjectSets`; `ClusterObjectSet "<n>" has group "<g>"`.

---

### Task 1: Post-completion health regression scenario

**Files:**
- Create: `test/e2e/features/cod_progress_deadline.feature` (with this scenario first; later tasks append).

**Interfaces:**
- Consumes: existing registered steps only.

- [ ] **Step 1: Write the scenario** (`@BoxcutterRuntime`, `Background: Given OLM is available`). The deployment omits `progressDeadlineMinutes` (deadline disabled); readiness is gated by a FieldValue progressionProbe on a ConfigMap `data.ready` key that the template does NOT set, so boxcutter's SSA never owns it and the test can toggle it.
  - `Scenario: Health regression after completion flips Available but not Progressing`
    - `Given namespace "${TEST_NAMESPACE}" is available`
    - Apply a COD whose template has a single ConfigMap `cod-health-cm` (data `k: v`) and a progressionProbe: `selector` GroupKind `{group: "", kind: ConfigMap}`, `assertions: [{type: FieldValue, fieldValue: {fieldPath: data.ready, value: "true"}}]`.
    - Assert `ClusterObjectSet "${NAME}-1" reports Ready as False with Reason ObjectsNotReady` (the probe fails until `data.ready` is set; confirmed reason in revision.feature's progressionProbe scenarios).
    - Set readiness: `When resource is applied` a ConfigMap `cod-health-cm` with `data: {ready: "true"}` (SSA-merges the extra key; the COS keeps `data.k`).
    - Assert `ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady`, `ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason NewClusterObjectSetProgressed`, and `ClusterObjectDeployment "${NAME}" reports Available as True with Reason Available`.
    - Regress: `When resource is applied` the same ConfigMap with `data: {ready: "false"}`.
    - Assert `ClusterObjectDeployment "${NAME}" reports Available as False with Reason Unavailable` and `ClusterObjectDeployment "${NAME}" consistently reports Progressing as False with Reason NewClusterObjectSetProgressed for "15s"` (deadline stays disabled; completion is historical).

- [ ] **Step 2: Run; expect PASS**

Run (needs experimental cluster): `make e2e/cod_progress_deadline`
Expected: scenario passes. If a step is undefined, STOP and reword to the matching registered step (do not add tooling for this task).

- [ ] **Step 3: Commit**

```bash
git add test/e2e/features/cod_progress_deadline.feature
git commit -s -m "🌱 Add COD post-completion health-regression e2e scenario"
```

---

### Task 2: Controller-restart deadline-durability scenario

**Files:**
- Modify: `test/e2e/steps/steps.go` (new step `RestartObjectController` + registration)
- Modify: `test/e2e/features/cod_progress_deadline.feature` (append scenario)

**Interfaces:**
- Produces: step `the object-controller is restarted` -> `RestartObjectController(ctx context.Context) error`. Runs `kubectl rollout restart deployment/object-controller-controller-manager -n olmv1-system` then `kubectl rollout status` on the same Deployment to wait for the new pods to be ready. Uses `k8sClient(ctx, ...)` and `olmNamespace`.

- [ ] **Step 1: Write the failing scenario** (`@BoxcutterRuntime @ProgressDeadline`):
  - `Scenario: A restart does not clear an exceeded progress deadline`
    - `Given namespace "${TEST_NAMESPACE}" is available`
    - `And min value for ClusterObjectDeployment .spec.template.spec.progressDeadlineMinutes is set to 1`
    - Apply a COD with `progressDeadlineMinutes: 1`, a never-ready Deployment (`image: does-not-exist:latest`), and a progressionProbe `ConditionEqual {type: Available, status: "True"}` (mirror the existing `cod_progressing_reasons.feature` never-ready scenario).
    - Assert `ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason ProgressDeadlineExceeded`.
    - `When the object-controller is restarted`
    - Assert `ClusterObjectDeployment "${NAME}" consistently reports Progressing as False with Reason ProgressDeadlineExceeded for "30s"` (a controller that re-derived the deadline from process start would instead report Progressing=True after restart).

- [ ] **Step 2: Run; expect FAIL on the new step**

Run: `make e2e/cod_progress_deadline`
Expected: the restart scenario errors with an undefined step for `the object-controller is restarted`.

- [ ] **Step 3: Implement `RestartObjectController` in `steps.go`**

Signature: `func RestartObjectController(ctx context.Context) error`. Body: `k8sClient(ctx, "rollout", "restart", "deployment/object-controller-controller-manager", "-n", olmNamespace)`, returning a wrapped error with `stderrOutput(err)` on failure; then `k8sClient(ctx, "rollout", "status", "deployment/object-controller-controller-manager", "-n", olmNamespace, "--timeout=120s")`. Mirror the error-wrapping style of `RolloutRestartIsPerformed`.

- [ ] **Step 4: Register the step in `RegisterSteps`**

Add near the existing `rollout restart is performed on` registration:
```go
sc.Step(`^(?i)the object-controller is restarted$`, RestartObjectController)
```

- [ ] **Step 5: Run; expect PASS**

Run: `make e2e/cod_progress_deadline`
Expected: both scenarios pass.

- [ ] **Step 6: Commit**

```bash
git add test/e2e/steps/steps.go test/e2e/features/cod_progress_deadline.feature
git commit -s -m "🌱 Add COD restart deadline-durability e2e scenario"
```

---

### Task 3: Stall-and-recover without an extra revision scenario

**Files:**
- Modify: `test/e2e/features/cod_progress_deadline.feature` (append scenario)

**Interfaces:**
- Consumes: existing registered steps only.

- [ ] **Step 1: Write the scenario** (`@BoxcutterRuntime`). Reuse the collision block/unblock mechanic from `cod_progressing_reasons.feature`:
  - `Scenario: A stalled revision recovers without cutting an extra revision`
    - `Given namespace "${TEST_NAMESPACE}" is available`
    - Pre-create an unowned ConfigMap `cod-recover-cm` (data `k: pre-existing`).
    - Apply a COD (collisionProtection Prevent) whose template manages `cod-recover-cm` (data `k: managed`); the revision collides and stalls.
    - Assert `ClusterObjectSet "${NAME}-1" reports Ready as False with Reason ObjectCollision` and `ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason RevisionObjectCollision`.
    - `When resource "configmap/cod-recover-cm" is removed`
    - Assert `ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady`, `ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason NewClusterObjectSetProgressed`, and `ClusterObjectDeployment "${NAME}" owns 1 ClusterObjectSets` (the stall did not cause a second revision).

- [ ] **Step 2: Run; expect PASS**

Run: `make e2e/cod_progress_deadline`
Expected: all three scenarios pass.

- [ ] **Step 3: Commit**

```bash
git add test/e2e/features/cod_progress_deadline.feature
git commit -s -m "🌱 Add COD stall-and-recover no-extra-revision e2e scenario"
```

---

## Running

```
make experimental-e2e-setup
make e2e/cod_progress_deadline
make experimental-e2e-teardown
```

Full regression before PR: `make test-experimental-e2e`.

## Self-Review Notes

- Spec coverage: restart durability (Task 2), post-completion regression (Task 1), stall-and-recover without extra revision (Task 3). Creation-deadline, phase-progress-extension, and creation-failure scenarios are intentionally excluded (superseded / out of scope per the updated OPRUN-4795).
- One new step (`RestartObjectController`) is unavoidable because the existing `rollout restart` step is scoped to the scenario namespace. Everything else reuses registered steps.
- File budget: 2 manual files, within the 2-4 budget and 10-file cap.
