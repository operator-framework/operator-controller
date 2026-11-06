# Design: ClusterObjectDeployment adoption of existing ClusterObjectSets

**Date:** 2026-10-26
**Status:** Draft (pending approval)
**Branch:** `cod-adopt-existing-cos` (off `upstream/main` @ `5e8f2748`)

## Intent and why

A `ClusterObjectDeployment` (COD) manages a family of `ClusterObjectSet` (COS)
revisions, identified by `spec.group == COD.name`. Today the COD controller only
*creates* revisions it owns; it does not take ownership of COSes in its group that
it did not create. This design makes the COD **adopt the ClusterObjectSets in its
group** so it owns the exact revision family boxcutter already treats as siblings.

**Driving scenario — Helm→COD migration.** To migrate a Helm-based install to
COD/COS management, the migrator creates an **initial orphaned COS** (revision 1)
with `collisionProtection: None` *directly*. `None` lets that first revision adopt
the objects the Helm applier already created on the cluster (they are otherwise
"foreign" and would collide). Creating that COS explicitly **guarantees** the
None-initial-revision exists; relying on the COD to mint revision 1 from its
template would expose a race where a subsequent reconcile mutates the template's
`collisionProtection` around revision-1 creation, so the clean adopting revision
might not be produced. So: the migrator deterministically creates the orphan
revision 1, and the **COD adopts it**.

Once revision 1 (`None`) owns the Helm objects, they are inside the revision
family, so the COD rolling revision 2 from its template (`collisionProtection:
Prevent`) hands those objects over from the sibling revision 1 — no collision,
because Prevent only blocks *foreign* objects, not sibling-owned ones. So adoption
followed by normal rollout is correct by construction; no re-rollout suppression is
needed.

**Secondary scenario (narrow — see caveat):** a COD deleted while its COSes
survive (orphaned with *no* controller owner), then recreated, re-adopts them.
Caveat: this only holds when the surviving COSes are genuinely owner-less. If a
COD is deleted with its COSes left intact (no cascade), those COSes still carry a
controller ownerRef to the *old* COD's UID; a recreated same-named COD has a new
UID, so `SetControllerReference` returns `AlreadyOwnedError` and the COD reports
`RevisionOwnershipConflict` rather than re-adopting (we honor fail-on-different-
controller). True re-adoption across a delete/recreate with surviving owned COSes
is **out of scope** — in practice Kubernetes cascade deletion removes the COSes
with the COD, so the owner-less window is what the migrator (which creates
owner-less orphans) actually exercises.

## Goals

- The COD **controller-owns every COS in its group** (`spec.group == COD.name`) —
  the exact set boxcutter treats as the sibling family.
- **Adopt orphans** (COSes with no controller owner); **fail** on a COS owned by a
  *different* controller (anomaly — see below); **no-op** on COSes it already owns.
- **Revision numbering** accounts for adopted revisions: the next revision is
  `max(revision over all group COSes) + 1` (or 1 if none), so a freshly created
  revision never collides with an adopted one.
- Ownership uses the **native controller reference** — the same mechanism the COS's
  boxcutter configuration (`ownerhandling.NewNative`) uses for objects.

## Non-goals

- The **migrator** creating the orphan `collisionProtection: None` revision 1
  (operator-controller / CE side). This branch is the COD's adoption behavior only.
- Secret ownership / GC, and any ClusterExtension-side changes.
- Re-rollout suppression or template-hash stamping onto adopted revisions (not
  needed — see rollout mechanics above).

## Anti-goals (what would make this a failure)

- Stealing a COS that is controller-owned by a different controller (we fail
  loudly instead).
- Minting a new revision whose number/name collides with an adopted revision.
- Suppressing or special-casing rollout after adoption (the sibling hand-off makes
  the normal path correct).

## How boxcutter defines "siblings" (grounding)

Boxcutter does not discover siblings itself — the COS controller tells it. In
`buildClusterObjectSet`/`buildBoxcutterPhases` the COS:
1. lists COSes sharing `spec.group` via the group field index
   (`clusterObjectSetGroupIndex`), active and non-deleting, excluding itself; and
2. passes them to boxcutter as `boxcutter.WithSiblingOwners(siblingObjs)`.

Boxcutter then treats an object controlled by the current revision **or a sibling
owner** as legitimate (adopt / hand off), and an object controlled by a
**non-sibling foreign controller** as a collision (gated by `collisionProtection`).

So the sibling family is defined by `spec.group`. Because `spec.group == COD.name`,
**only one COD can ever own a given group** — which is why a foreign-controlled COS
in a COD's group is an anomaly, not a legitimate sibling, and adoption fails on it.

## Design

### Ownership reconciliation (`ensureRevisionOwnership`)

On every COD reconcile, before rollout and garbage collection, the COD ensures it
controller-owns every COS in its group:

1. List COSes with `spec.group == COD.name` (the same group index the COS uses for
   siblings; include active **and** archived — the COD owns its whole history).
2. For each such COS, call `controllerutil.SetControllerReference(cod, cos, scheme)`:
   - **no controller owner** → the ref is set; `Update` the COS (adopt).
   - **already owned by this COD** (same UID) → no change; skip the `Update`.
   - **owned by a different controller** → `SetControllerReference` returns
     `*controllerutil.AlreadyOwnedError`. The COD does not steal it and does not
     silently tolerate it: set **`Progressing=False` with reason
     `RevisionOwnershipConflict`** on the COD (message naming the conflicting COS and
     its controller), **skip rollout for this reconcile, persist status, and
     requeue** so the anomaly is both visible on the COD and retried. (A bare
     reconcile error would only surface in logs; the condition is the actionable
     signal.)
3. Only issue an `Update` when the ownerReferences actually changed (avoid write
   churn / needless reconciles).

A new condition reason is added to the API:
`ClusterObjectDeploymentReasonRevisionOwnershipConflict = "RevisionOwnershipConflict"`
(alongside the existing `Progressing` reasons in `clusterobjectdeployment_types.go`).

This runs **before** `createRevision`, so the COD never tries to create a revision
whose name/number collides with an unowned orphan it should have adopted first.

### Revision numbering

`nextRevisionNumber` returns `max(.spec.revision over all COSes in the group) + 1`,
or `1` when the group is empty. Counting **all** group revisions (adopted, created,
active, archived) guarantees a new revision never reuses an adopted number: after
adopting `ext-1`, the COD mints `ext-2`. This resolves the existing
`TODO: revisit AlreadyExists handling under the adoption use-case`.

### Rollout (unchanged, correct by construction)

After adoption the adopted revision is an ordinary COD-owned revision. Normal
rollout proceeds: if the template differs from the latest revision's template hash,
the COD cuts the next revision; its objects are handed over from the adopted
sibling revision. No special-casing.

## Edge cases

- **Multiple orphans in the group:** adopt all of them (each via
  `SetControllerReference` + `Update`).
- **Foreign-controlled COS in the group:** the COD reports
  `Progressing=False / RevisionOwnershipConflict` (message naming the COS and its
  controller), skips rollout, and requeues until the anomaly is resolved —
  intentional, since group membership is exclusive to one COD.
- **Archived orphan revisions:** adopted too (ownership spans the full history so
  GC/retention and cascade-on-COD-deletion manage them).
- **Adopted revision lacks a `template-hash` label:** fine — the COD rolls the next
  revision per normal template comparison; the adopted revision is retained/pruned
  as history.

## Testing

- **Unit (COD controller):**
  - adopts an orphan COS in the group (controllerRef set to the COD);
  - no-op when the COS is already COD-owned (no spurious `Update`);
  - when a group COS is owned by a different controller, the COD reports
    `Progressing=False / RevisionOwnershipConflict` and does not roll out (rather
    than silently proceeding or only logging);
  - adopts multiple orphans;
  - includes archived revisions;
  - ignores COSes outside the group (`spec.group != COD.name`).
  - `nextRevisionNumber` = max over all group revisions + 1 (and 1 when empty);
    a freshly created revision after adopting `ext-1` is `ext-2`.
- **envtest:** adoption `Update` is accepted against a real API server; a COS
  controller-owned by a different controller yields `AlreadyOwnedError` and is not
  modified.
- **e2e:** a COD created over an orphan COS in its group ends up owning it (COD is
  the controller owner), and a subsequent template change rolls the next revision
  that supersedes it. (Migration-driven e2e belongs with the migrator work.)

## What's left to the builder

- The exact placement of `ensureRevisionOwnership` in the reconcile sequence
  (before `garbageCollectArchivedRevisions` and `reconcileRollout`) and whether to
  reuse the existing group-list helper.
- Whether to detect "already owned / needs no update" by comparing ownerReferences
  before vs after `SetControllerReference` (to gate the `Update`), or via
  `metav1.GetControllerOf`.
- Error-type matching for `AlreadyOwnedError` (`errors.As`) and the exact reconcile
  error wrapping/condition surfaced.
