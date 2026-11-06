# ClusterObjectDeployment adoption of existing ClusterObjectSets — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the ClusterObjectDeployment (COD) controller adopt (take controller ownership of) the ClusterObjectSets (COS) in its group, failing loudly when a COS is owned by a different controller.

**Architecture:** On each reconcile, before GC and rollout, the COD ensures it is the controller owner of every COS in its group (`spec.group == COD.name`). Orphans are adopted via `controllerutil.SetControllerReference`; a COS already owned by a different controller yields `*controllerutil.AlreadyOwnedError`, which the COD surfaces as `Progressing=False / RevisionOwnershipConflict` (and skips rollout). Ownership spans the full history (active + archived). Revision numbering already counts the whole group, so a freshly minted revision never collides with an adopted one.

**Tech Stack:** Go, controller-runtime (`controllerutil`), the object-controller COD controller, envtest, Godog (e2e).

**Spec:** `docs/superpowers/specs/2026-10-26-cod-adopt-existing-cos-design.md`

## Global Constraints

- The family is `spec.group == COD.name`, discovered via the existing `clusterObjectSetGroupIndex` (same index the COS uses for siblings). Include **active and archived** revisions.
- Adoption uses the **native controller reference** (`controllerutil.SetControllerReference(cod, cos, scheme)`): orphan → set + `Update`; already COD-owned (same UID) → **no-op, no Update** (avoid write churn); different controller → `*controllerutil.AlreadyOwnedError`.
- A different-controller COS is an **anomaly** (group membership is exclusive to one COD): the COD sets **`Progressing=False` with reason `RevisionOwnershipConflict`** (message naming the COS and its controller), **skips rollout and GC for that reconcile, persists status, and requeues** (the COS watch re-triggers when the conflict is resolved). It must **not** steal the COS and must **not** only log.
- Ownership reconciliation runs **before** `garbageCollectArchivedRevisions` and `reconcileRollout`.
- `nextRevisionNumber` already returns `max(.spec.revision over the group)+1`; **do not change it** — adoption placing orphans in the listed group is what makes it correct, resolving the `TODO: revisit AlreadyExists handling under the adoption use-case` in `createRevision`.
- Scope is COD-side only: do **not** touch the migrator, secret ownership, or the COS controller's sibling discovery.

## Review Focus

- **Different-controller COS in the group:** COD reports `Progressing=False/RevisionOwnershipConflict` and does not roll out (not stolen, not log-only). → Task 1 test.
- **Mixed group (orphan + foreign):** orphans are still adopted, and the conflict is still reported. → Task 1 test.
- **Already-COD-owned COS:** no spurious `Update` (idempotent; avoids a reconcile hot-loop). → Task 1 test.
- **Archived orphan revision:** adopted too (ownership spans history). → Task 1 test.
- **COS in a different group (`spec.group != COD.name`):** never touched. → Task 1 test (and inherent to the group-indexed list).

---

### Task 1: Adopt group ClusterObjectSets (`ensureRevisionOwnership`) + conflict condition

**Files:**
- Modify: `api/v1/clusterobjectdeployment_types.go` (add the reason constant)
- Modify: `internal/object-controller/controllers/clusterobjectdeployment_controller.go` (add `ensureRevisionOwnership`, a typed conflict error, wire into `reconcile`; drop the stale `AlreadyExists` TODO on `createRevision`)
- Test: `internal/object-controller/controllers/clusterobjectdeployment_controller_test.go` (uses `codFixture`, `newCODClient`, `reconcileCOD`, `getCOS`, `progressing`, `revisionCOS`)

**Interfaces:**
- Consumes: `clusterObjectSetGroupIndex`; `controllerutil.SetControllerReference`, `*controllerutil.AlreadyOwnedError`; `metav1.GetControllerOf`; `setProgressing`; the `revisions []ocv1.ClusterObjectSet` already fetched in `reconcile`.
- Produces:
  - `ClusterObjectDeploymentReasonRevisionOwnershipConflict = "RevisionOwnershipConflict"` in `api/v1/clusterobjectdeployment_types.go`.
  - `func (c *ClusterObjectDeploymentReconciler) ensureRevisionOwnership(ctx context.Context, cod *ocv1.ClusterObjectDeployment, revisions []ocv1.ClusterObjectSet) error` — adopts every orphan in `revisions`; returns a `*revisionOwnershipConflictError` (holding the offending COS name + owning controller) if any revision is owned by a different controller; returns a plain error on a transient `Update` failure; `nil` otherwise.

- [ ] **Step 1: Write the failing unit tests**

Add tests driving `reconcileCOD` (which calls `reconcile`) with group COSes seeded via `newCODClient`. Use `revisionCOS(n, lifecycle)` for orphans (it sets `Group == clusterObjectDeploymentName`, no owner), and set `OwnerReferences` for the owned/foreign cases.

```
- "adopts an orphan revision in the group":
    seed revisionCOS(1, Active) (orphan). reconcile. getCOS("test-cod-1-1"):
    GetControllerOf == the COD (Kind=ClusterObjectDeployment, Name=test-cod-1, UID=cod.UID).
- "no-op when already owned by this COD" (no spurious write):
    seed a revision already controller-owned by the COD; reconcile via a client with a
    write interceptor counting Updates on ClusterObjectSet; assert zero Updates to that COS.
- "reports RevisionOwnershipConflict when a group revision is owned by a different controller":
    seed revisionCOS(1, Active) with a controller ownerRef to a foreign owner (different UID/Kind).
    reconcile. progressing(): Status=False, Reason=RevisionOwnershipConflict. And no new revision
    is created (getCOS("test-cod-1-2") absent) — rollout was skipped.
- "adopts orphans even when another group revision is foreign-owned":
    seed orphan revisionCOS(1) + foreign-owned revisionCOS(2). reconcile.
    getCOS("...-1") is COD-owned; progressing() == False/RevisionOwnershipConflict.
- "adopts an archived orphan revision":
    seed revisionCOS(1, Archived) (orphan). reconcile. getCOS is COD-owned.
- "ignores a ClusterObjectSet in a different group":
    seed a COS with Spec.Group="other" and no owner. reconcile. that COS is left orphaned
    (GetControllerOf == nil).
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -tags "!standard,containers_image_openpgp" -run 'ClusterObjectDeploymentReconciler' ./internal/object-controller/controllers/`
Expected: the new cases FAIL (no adoption happens yet; no conflict condition).

- [ ] **Step 3: Add the reason constant**

In `api/v1/clusterobjectdeployment_types.go`, add alongside the other `Progressing` reasons:
`ClusterObjectDeploymentReasonRevisionOwnershipConflict = "RevisionOwnershipConflict"`.

- [ ] **Step 4: Implement `ensureRevisionOwnership` + the typed conflict error**

In the controller: define `type revisionOwnershipConflictError struct{ cos string; owner string }` with an `Error()` method. Implement `ensureRevisionOwnership`: for each `rev` in `revisions`, if `metav1.GetControllerOf(&rev)` is the COD → skip; else `controllerutil.SetControllerReference(cod, &rev, c.Scheme)` and, if it returns `*AlreadyOwnedError`, remember a conflict (COS name + existing controller) and continue; otherwise `c.Client.Update(ctx, &rev)` (adopt). After the loop, return the remembered `*revisionOwnershipConflictError` if any; else nil. (Transient `Update` errors are returned immediately.)

- [ ] **Step 5: Wire into `reconcile` and drop the stale TODO**

In `reconcile`, after `listRevisions` and before `garbageCollectArchivedRevisions`:
```go
if err := c.ensureRevisionOwnership(ctx, cod, revisions); err != nil {
    var conflict *revisionOwnershipConflictError
    if errors.As(err, &conflict) {
        setProgressing(cod, metav1.ConditionFalse, ocv1.ClusterObjectDeploymentReasonRevisionOwnershipConflict, conflict.Error())
        return nil // skip GC + rollout; status is persisted by Reconcile; COS watch re-triggers on resolution
    }
    return err
}
```
Remove the now-resolved `TODO: revisit AlreadyExists handling under the adoption use-case` comment on `createRevision` (numbering already counts adopted revisions).

- [ ] **Step 6: Run tests + build**

Run: `go test -tags "!standard,containers_image_openpgp" -run 'ClusterObjectDeploymentReconciler' ./internal/object-controller/controllers/` → PASS
Run: `go build -tags containers_image_openpgp ./...` → succeeds. Run `make generate manifests`; expect no diff (no API struct change, only a string const).

- [ ] **Step 7: Commit**

```bash
git add api/v1/clusterobjectdeployment_types.go internal/object-controller/controllers/clusterobjectdeployment_controller.go internal/object-controller/controllers/clusterobjectdeployment_controller_test.go
git commit -s -m "✨ ClusterObjectDeployment adopts existing ClusterObjectSets in its group"
```

---

### Task 2: envtest — ownership write against a real API server

**Files:**
- Test: `internal/object-controller/controllers/clusterobjectdeployment_controller_test.go` (or the existing envtest file in this package; use the suite `config`)

**Interfaces:**
- Consumes: `ensureRevisionOwnership` behavior via `Reconcile` on a real-client-backed reconciler (the envtest `config` / group-index-aware client, as `revision_engine_factory_test.go` sets up).

- [ ] **Step 1: Write the test**

Build a real client from the envtest `config` with the group index registered (mirror the existing envtest setup in this package). Create a COD and, in its group, (a) an orphan COS and (b) a COS controller-owned by a different controller. Drive `Reconcile`. Assert:
- the orphan COS is now controller-owned by the COD (re-Get; `GetControllerOf` == COD);
- the foreign-owned COS is **unchanged** (its controller ownerRef is intact), and the COD reports `Progressing=False/RevisionOwnershipConflict`.
Use unique names + `t.Cleanup`.

- [ ] **Step 2: Run**

Run: `go test -tags "!standard,containers_image_openpgp" -run '<envtest name>' ./internal/object-controller/controllers/` → PASS (envtest spins up the API server; GC is not needed — this checks the ownerRef write).

- [ ] **Step 3: Commit**

```bash
git commit -s -am "🌱 envtest: COD adopts orphan COS and reports conflict on foreign-owned"
```

---

### Task 3: E2E — standalone COD adopts an orphan ClusterObjectSet

**Files:**
- Create: `test/e2e/features/cod_orphan_adoption.feature` (experimental `@BoxcutterRuntime`). No suitable file exists — `cos_object_collision_and_adoption.feature` covers the COS adopting *objects*, a different concern; this is the COD adopting *ClusterObjectSets*. The name follows the existing `cod_*.feature` convention.
- Modify (if needed): `test/e2e/steps/steps.go` — a step asserting a COS's controller owner is a given ClusterObjectDeployment (and, if not already available, applying a bare orphan COS).

**Scope ruling:** do **not** spin up a KIND cluster; deliver compiling scenarios where every Gherkin step resolves to a registered step. Actual cluster execution is deferred to CI/`make test-experimental-e2e`.

- [ ] **Step 1: Write the scenario**

```gherkin
@BoxcutterRuntime
Scenario: A ClusterObjectDeployment adopts a pre-existing orphan ClusterObjectSet in its group
  # Given an orphan ClusterObjectSet (no controller owner) with spec.group == <cod>
  # And a ClusterObjectDeployment named <cod> with a template
  # Then the ClusterObjectDeployment is the controller owner of that ClusterObjectSet
  # And a subsequent template change rolls the next revision (ext-2), which supersedes the adopted one
```

- [ ] **Step 2: Add any missing step + verify resolution + compile**

Add a "ClusterObjectSet `<name>` is controller-owned by ClusterObjectDeployment `<name>`" step if absent (kubectl get + assert `GetControllerOf`). Reuse existing apply/Ready/not-found steps. Verify: `go test -c -tags "e2e,containers_image_openpgp" -o /dev/null ./test/e2e/` compiles, and each scenario step matches a registered step regex.

- [ ] **Step 3: Commit**

```bash
git add test/e2e/features/cod_orphan_adoption.feature test/e2e/steps/steps.go
git commit -s -m "✨ e2e: ClusterObjectDeployment adopts an orphan ClusterObjectSet in its group"
```

---

## Not in scope / future work

- The migrator creating the orphan `collisionProtection: None` revision 1 (operator-controller / CE side).
- Secret ownership / GC and ClusterExtension-side changes.
- Actual experimental-cluster execution of the Task 3 e2e (deferred to CI).
