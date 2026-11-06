# ClusterObjectDeployment Controller MVP — Design

**Date:** 2026-10-20
**Status:** approved for planning
**Branch:** `cod-controller-mvp` (off `cod-controller-skeleton`, PR #3032)

## Intent

Give the `ClusterObjectDeployment` (COD) controller its first real behavior. Today it is a
skeleton (PR #3032): it watches COD + owned ClusterObjectSet (COS) but does nothing. This MVP
makes it roll out template changes as new COS revisions and report rollout progress.

The COD lives in the `object-controller` binary, is experimental-only (`//go:build !standard`,
experimental-channel CRD), and reuses the existing COS sub-types verbatim.

## Goals

1. On a detected template change (or when no revision exists), create a new COS revision owned
   by the COD.
2. Reflect the latest revision's rollout progress in the COD's single `Progressing` condition.

## Non-goals (handled elsewhere or deferred)

- **Archival / supersession** of older revisions — done today by the COS controller
  (`clusterobjectset_controller.go:325`, `TODO(COD)`); not moved in this MVP.
- **Progress deadlines** — a COS concern; the cut-down COD CRD has no deadline field.
- **Pruning / revision-history limits** — the cut-down COD CRD has no limit field.
- **Orphan adoption** — the group→COD watch exists (skeleton) but COD only manages COS it creates.
- **Object externalization to Secrets** — the applier does this for bundle content; COD copies
  user-authored template phases verbatim (inline objects stay inline).

## Key facts (from recon)

- **COD CRD is cut down.** `Spec.Template.Spec` = `{Phases, ProgressionProbes, CollisionProtection}`
  (no template metadata). `Status` = `{Conditions}` only. Condition type `Progressing`; reasons
  `NewClusterObjectSetProgressed` / `NewClusterObjectSetProgressing` (constants already defined in
  `api/v1/clusterobjectdeployment_types.go`). COD name is a valid COS group (≤52 chars).
- **COS sub-types are identical** to the COD template's, so BuildCOS is a direct typed field copy.
- **Revision ancestry is runtime-derived**, not stored: all COS sharing `spec.group` form the
  ordered sequence (sort by `spec.revision`). There is no `Previous` field. `spec.group` has a
  field index registered by `controllers.SetupIndexes` (already wired in `main.go`).
- **COS readiness/completion**: `status.completedAt` (set-once, immutable) marks "rolled out".
  The live `Ready` condition is `True`/`AllObjectsReady` when ready. This repo's own
  `GetRevisionStates` uses `completedAt` for "Installed"; the COD uses it the same way.
- **Naming**: child COS name = `<cod-name>-<revision>` (matches applier convention).
- **Owner labels** on COS: `olm.operatorframework.io/owner-kind` + `owner-name`
  (`internal/shared/labels`).

## Decisions

- **Change detection: template-hash label (orb-style), not SSA-diff.** Compute a SHA-256 over the
  marshaled `cod.Spec.Template`, store a truncated hex digest as a label on each COS, and compare
  the latest active COS's label to the current hash. Chosen because it honors the CRD contract
  "any template change → new revision" (the SSA-diff approach silently mutates a revision on a
  change to a *mutable* COS field such as `progressionProbes`), is self-contained, and is unit-
  testable with the fake client. New label constant:
  `olm.operatorframework.io/template-hash`. Digest truncated to 32 hex chars (sha256[:16]) to
  stay within the 63-char label-value limit.
- **Create mechanism: plain typed `Client.Create`** (not SSA). Revisions are immutable and the
  controller only ever creates them; `AlreadyExists` is treated as success (requeue race).
- **Completion signal: COS `status.completedAt`** (not the live `Ready` condition) so a later
  availability regression does not un-complete the rollout.
- **No finalizer.** COS children are cleaned up by Kubernetes cascading delete via the controller
  owner reference.

## Behavior

### Reconcile (outer)
Get the COD (ignore not-found). If `DeletionTimestamp` is set, no-op (children cascade).
Deep-copy, run the inner reconcile, and if `Status` changed, write it via the status subresource,
joining any update error.

### reconcile (inner)
1. List COS in the group (`cod.Name`) via the `.spec.group` index; sort ascending by `spec.revision`.
2. `hash = templateHash(cod.Spec.Template)`.
3. `latest = latestActiveRevision(revs)` — highest-revision COS that is not Archived and not deleting.
4. If `latest == nil` **or** `latest.Labels[template-hash] != hash`: create a new revision
   (`nextRevision = max(revision over all revs)+1`), then set `Progressing=True /
   NewClusterObjectSetProgressing` ("waiting for new revision to be created").
5. Else (latest matches current template):
   - `completedAt` set → `Progressing=False / NewClusterObjectSetProgressed` ("latest revision has progressed").
   - else → `Progressing=True / NewClusterObjectSetProgressing` ("latest revision is progressing").

Requeues are driven by the existing `Owns(COS)` watch (COS status changes re-trigger the COD); no timer.

### BuildCOS
Name `<cod>-<rev>`; labels owner-kind=`ClusterObjectDeployment`, owner-name=`<cod>`,
template-hash=`<hash>`; spec `group=<cod>`, `revision=<rev>`, `lifecycleState=Active`,
`phases`/`progressionProbes` copied from template, `collisionProtection` from template defaulting
to `Prevent` when empty; controller owner reference to the COD (needs a `Scheme` on the reconciler).

## Testing

- **Unit** (fake client + `.spec.group` index via `testutil.WithIndexes`): create-first-revision,
  no-op-when-hash-matches (completed → Progressing False; not-completed → Progressing True),
  new-revision-on-template-change, new-revision-when-latest-archived, deletion no-op; plus pure-
  function tests for hash determinism/sensitivity, BuildCOS field mapping, collisionProtection
  default, nextRevision, latestActiveRevision.
- **E2E** (`test/e2e/features/cod_rollout.feature`, `@BoxcutterRuntime`, experimental cluster):
  initial rollout → COD Progressing False; template change → revision 2 ready → Progressing False;
  unchanged template → no revision 2. Requires a new Godog step asserting a condition directly on
  a ClusterObjectDeployment.

## Out-of-repo-convention notes

The ClusterExtension applier uses SSA-diff for change detection and externalizes objects to
Secrets; COD intentionally diverges (hash label, inline copy) for MVP simplicity. The applier is
itself marked `TODO(COD)` for eventual replacement, so COD is free to define its own clean
mechanism.
