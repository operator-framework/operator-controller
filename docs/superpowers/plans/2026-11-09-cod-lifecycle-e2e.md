# Standalone COD Lifecycle E2E (OPRUN-4794) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the remaining standalone ClusterObjectDeployment (COD) e2e gaps — idempotent reconcile, revision identity, multi-revision handoff, group isolation, and deletion teardown — by extending existing `cod_*.feature` files where topically aligned and adding focused new feature files otherwise.

**Architecture:** godog BDD e2e scenarios under `test/e2e/features/`, backed by step helpers in `test/e2e/steps/`. All assertions reuse existing registered steps — no new step helpers and no Go changes are required (verified against `steps.go`). Scenarios run against the experimental KIND cluster (object-controller enabled via the `@BoxcutterRuntime` tag).

**Tech Stack:** Go, cucumber/godog, controller-runtime client, KIND.

**Spec:** OPRUN-4794 ("Add standalone COD revision lifecycle e2e scenarios"). Companion tickets OPRUN-4849 (COS-only) and OPRUN-4795 (COD deadline/failure) are out of scope here.

## Global Constraints

- Repo file cap: <=10 files across the branch; manual budget 4-6 files. This plan touches 3 feature files: `cod_rollout.feature` (modify), `cod_handoff.feature` (new), `cod_lifecycle.feature` (new). No Go/step changes.
- All COD scenarios carry the `@BoxcutterRuntime` tag (object-controller is gated on it).
- Scenario placeholders: `${NAME}` is a per-scenario unique name; `${TEST_NAMESPACE}` a per-scenario namespace. Derive extra names as `${NAME}-a`, `${NAME}-1`, etc.
- Do not add or refactor steps. Every assertion must map to a step already registered in `test/e2e/steps/steps.go`.
- New revision semantics (verbatim from controller `reconcileRollout`/`templateHash`): a new revision is cut **iff** `templateHash(cod.Spec.Template)` differs from the latest active revision's `apps.kubernetes.io/template-hash` label. The hash covers `spec.template` only. Root `metadata` and root `spec.progressDeadlineMinutes` are NOT hashed. Revision numbers are `max+1` across the whole group including archived revisions and are never reused.

## Review Focus

- **Idempotency false-pass under timing:** asserting "no new revision" with a one-shot/eventual read can pass before a would-be successor appears. Pinned in Task 1 by holding the settled Progressing condition over a window (`consistently reports Progressing as False with Reason NewClusterObjectSetProgressed for "15s"`) and then asserting `${NAME}-2` is not found — the existing idiom from `cod_progressing_reasons.feature`.
- **A->B->A reuse assumption:** a reviewer may expect k8s-Deployment-style revision reuse; the controller cuts a fresh revision instead. Pinned by Task 1's A->B->A scenario asserting `${NAME}-3` exists and the COD owns 3 revisions.
- **Handoff UID identity:** a shared managed object must keep its UID across the revision handoff (adopt, not recreate). Pinned by Task 2 via `resource ... UID is remembered` + `keeps its UID`.
- **Orphaned object cleanup:** an object dropped from the template across a handoff must be deleted, not leaked. Pinned by Task 2 via `resource ... is eventually not found`.
- **Group isolation:** one COD must not adopt another COD's revision. Pinned by Task 3 asserting each COD owns exactly its own revision (the `owns N` step verifies controller UID).

---

## File Structure

- `test/e2e/features/cod_rollout.feature` (modify): add idempotency, root-edit, A->B->A, child-COS UID scenarios.
- `test/e2e/features/cod_handoff.feature` (create): blocked-successor handoff, shared-object UID survival, dropped-object GC.
- `test/e2e/features/cod_lifecycle.feature` (create): group isolation, COD deletion teardown.

Existing registered steps reused (verified in `steps.go`): `resource is applied`; `resource "<k>/<n>" is removed`; `resource "<k>/<n>" is (eventually) not found`; `resource "<k>/<n>" UID is remembered`; `resource "<k>/<n>" keeps its UID`; `ClusterObjectSet "<n>" reports <T> as <S> with Reason <R>`; `ClusterObjectSet "<n>" has group "<g>"`; `ClusterObjectSet "<n>" is archived`; `ClusterObjectSet "<n>" is eventually deleted`; `ClusterObjectDeployment "<n>" owns <N> ClusterObjectSets`; `ClusterObjectDeployment "<n>" reports <T> as <S> with Reason <R>`; `ClusterObjectDeployment "<n>" consistently reports <T> as <S> with Reason <R> for "<dur>"`.

---

### Task 1: Rollout idempotency and revision-identity scenarios

**Files:**
- Modify: `test/e2e/features/cod_rollout.feature`

**Interfaces:**
- Consumes: existing registered steps only (see File Structure). No step or Go code is created.

- [ ] **Step 1: Write the new scenarios in `cod_rollout.feature`** (all tagged `@BoxcutterRuntime`, each `Given namespace "${TEST_NAMESPACE}" is available`):
  - `Scenario: Reapplying an unchanged template creates no new revision` — apply a COD with one ConfigMap; wait `ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady`; reapply the byte-identical template; assert `ClusterObjectDeployment "${NAME}" consistently reports Progressing as False with Reason NewClusterObjectSetProgressed for "15s"` and `resource "clusterobjectset/${NAME}-2" is not found`.
  - `Scenario: Editing only root metadata or deadline creates no new revision` — apply COD; wait rev1 Ready; reapply with an added root `metadata.labels` entry and a root `spec.progressDeadlineMinutes` value, `spec.template` unchanged; assert `ClusterObjectDeployment "${NAME}" consistently reports Progressing as False with Reason NewClusterObjectSetProgressed for "15s"` and `resource "clusterobjectset/${NAME}-2" is not found`.
  - `Scenario: Reverting template content cuts a fresh revision without reuse (A to B to A)` — apply content A (rev1 Ready); apply content B (wait `ClusterObjectSet "${NAME}-2" reports Ready as True with Reason AllObjectsReady`); reapply content A; assert `ClusterObjectSet "${NAME}-3" has group "${NAME}"`, `ClusterObjectSet "${NAME}-3" reports Ready as True with Reason AllObjectsReady`, and `ClusterObjectDeployment "${NAME}" owns 3 ClusterObjectSets` (confirms rev1 was not reused or renumbered).
  - Extend the existing `Scenario: ClusterObjectDeployment rolls out its initial revision` with `And ClusterObjectDeployment "${NAME}" owns 1 ClusterObjectSets` (this step already verifies each revision's controller owner is the COD with the COD's UID — the child-COS controller-UID assertion).

- [ ] **Step 2: Run the feature; expect PASS**

Run (requires a running experimental cluster — see "Running"): `make e2e/cod_rollout`
Expected: all `cod_rollout` scenarios pass. Every step resolves to an already-registered step; if any step is reported undefined, STOP — the scenario wording does not match an existing step and must be reworded to match one (do not add steps in this plan).

- [ ] **Step 3: Commit**

```bash
git add test/e2e/features/cod_rollout.feature
git commit -m "test(e2e): COD idempotent reconcile and A->B->A revision identity"
```

---

### Task 2: Multi-revision handoff scenarios

**Files:**
- Create: `test/e2e/features/cod_handoff.feature`

**Interfaces:**
- Consumes: existing registered steps only (collision-block trick from `cod_progressing_reasons.feature`, `resource ... UID is remembered`/`keeps its UID`, `ClusterObjectSet ... is archived`, `resource ... is eventually not found`, `ClusterObjectSet ... reports Ready as False`, `ClusterObjectDeployment ... owns N ClusterObjectSets`).

- [ ] **Step 1: Write `cod_handoff.feature`** (`@BoxcutterRuntime`, `Background: Given OLM is available`). Scenarios:
  - `Scenario: A blocked successor keeps the predecessor active until it is unblocked` — apply COD rev1 with a shared ConfigMap `cod-shared-cm` (data v1); wait rev1 Ready; `resource "configmap/cod-shared-cm" UID is remembered`. Pre-create a conflicting plain object the successor will collide on (ConfigMap `cod-block-cm` with `k: pre-existing`). Update the template to add `cod-block-cm` (managed) alongside the shared CM (data v2); the successor `${NAME}-2` is cut but blocked. Assert `ClusterObjectSet "${NAME}-2" reports Ready as False with Reason ObjectCollision`, `ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason RevisionObjectCollision`, and `ClusterObjectDeployment "${NAME}" owns 2 ClusterObjectSets` (both active during handoff). Then unblock: `resource "configmap/cod-block-cm" is removed`. Assert `ClusterObjectSet "${NAME}-2" reports Ready as True with Reason AllObjectsReady`, `ClusterObjectSet "${NAME}-1" is archived`, and the shared object survived the handoff via `resource "configmap/cod-shared-cm" keeps its UID`.
  - `Scenario: An object dropped across a revision is garbage collected` — apply COD rev1 with two ConfigMaps (`cod-keep-cm`, `cod-drop-cm`); wait rev1 Ready; update the template removing `cod-drop-cm` (keep `cod-keep-cm`); wait `ClusterObjectSet "${NAME}-2" reports Ready as True with Reason AllObjectsReady`; assert `resource "configmap/cod-drop-cm" is eventually not found`. (Rev2 Ready with `cod-keep-cm` in its phase proves the retained object was not collaterally removed.)

- [ ] **Step 2: Run; expect PASS**

Run: `make e2e/cod_handoff`
Expected: both scenarios pass. If any step is reported undefined, STOP and reword the scenario to match an existing step.

- [ ] **Step 3: Commit**

```bash
git add test/e2e/features/cod_handoff.feature
git commit -m "test(e2e): COD multi-revision handoff, shared-object UID survival, dropped-object GC"
```

---

### Task 3: Group isolation and deletion teardown scenarios

**Files:**
- Create: `test/e2e/features/cod_lifecycle.feature`

**Interfaces:**
- Consumes: existing registered steps only (`ClusterObjectDeployment ... owns N ClusterObjectSets`, `ClusterObjectSet ... has group`, `resource "clusterobjectdeployment/..." is removed`, `ClusterObjectSet ... is eventually deleted`, `resource ... is eventually not found`).

- [ ] **Step 1: Write `cod_lifecycle.feature`** (`@BoxcutterRuntime`, `Background: Given OLM is available`). Scenarios:
  - `Scenario: Two ClusterObjectDeployments do not adopt each other's revisions` — `Given namespace "${TEST_NAMESPACE}" is available`; apply COD `${NAME}-a` (own ConfigMap) and COD `${NAME}-b` (different ConfigMap); wait each rev1 Ready; assert `ClusterObjectSet "${NAME}-a-1" has group "${NAME}-a"`, `ClusterObjectSet "${NAME}-b-1" has group "${NAME}-b"`, `ClusterObjectDeployment "${NAME}-a" owns 1 ClusterObjectSets`, `ClusterObjectDeployment "${NAME}-b" owns 1 ClusterObjectSets` (the `owns` step verifies controller UID, proving neither COD adopted the other's revision).
  - `Scenario: Deleting a ClusterObjectDeployment tears down its revisions and managed objects` — `Given namespace "${TEST_NAMESPACE}" is available`; apply COD with one ConfigMap `cod-teardown-cm`; wait rev1 Ready; `resource "clusterobjectdeployment/${NAME}" is removed`; assert `ClusterObjectSet "${NAME}-1" is eventually deleted` and `resource "configmap/cod-teardown-cm" is eventually not found`.

- [ ] **Step 2: Run; expect PASS**

Run: `make e2e/cod_lifecycle`
Expected: both scenarios pass. If any step is reported undefined, STOP and reword the scenario to match an existing step.

- [ ] **Step 3: Commit**

```bash
git add test/e2e/features/cod_lifecycle.feature
git commit -m "test(e2e): COD group isolation and deletion teardown"
```

---

## Running

Scenarios need the experimental cluster (object-controller enabled). Bring it up once, then iterate per-feature:

```
make experimental-e2e-setup        # one-time cluster + experimental manifests
make e2e/<feature>                 # run a single feature (e.g. e2e/cod_handoff)
make experimental-e2e-teardown     # when done
```

Full suite regression before PR: `make test-experimental-e2e`.

## Self-Review Notes

- Spec coverage: idempotent reapply + root edit (Task 1), A->B->A revision identity (Task 1), child-COS controller UID (Task 1), blocked-successor handoff + multiple-active + archival + shared-object UID survival + dropped-object GC (Task 2), group isolation (Task 3), COD deletion teardown (Task 3). COS-level items intentionally excluded (OPRUN-4849).
- No new steps or Go changes: every assertion maps to a step already registered in `steps.go` (verified). If a scenario cannot be expressed with an existing step, that is a signal to reword the scenario, not to add tooling in this plan.
- File budget: 3 manual files, within the 4-6 budget and 10-file cap.
