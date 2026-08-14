# Requirements

- Add an `OrbStorageMigrator` in `internal/operator-controller/applier/` with a Helm `ActionClientGetter`, a COS-from-Helm generator, a client, `Scheme`, and `FieldOwner`
- The deployed Helm release is the migration trigger: when absent, migration is a no-op; when present, migration is in progress
- Look up the most-recent **deployed** Helm release (fall back through history when the latest release is not `deployed`), matching `BoxcutterStorageMigrator` behavior
- Provide a COS-from-Helm generator method (parallel to `GenerateRevisionFromHelmRelease`) that builds an `orbac.ClusterObjectSetApplyConfiguration` from a Helm release: sanitized objects placed in a **single, assertion-free phase** (chunked at the 50-object cap only when necessary; no kind-based ordering, no per-GVK assertions - mimicking Helm apply semantics), `spec.group = ext.Name`, `lifecycleState: Active`, `collisionProtection: None`, owner labels, bundle-metadata annotations, a **non-controller** ownerReference to the ClusterExtension (so orb's `adoptOrphans` can later set the COD as controller), and no `LabelTemplateHash`
- Provide an `ExternalizeCOS` entry point in the externalizer (shared phases-level core with `ExternalizeCOD(cod)`) for externalizing the adopting revision
- Externalize the built revision through the (generalized) externalizer: pack large object sets into ClusterObjectSlices with `objectRef`s, creating the slices before the revision, reusing the same externalizer used by the COD apply path
- Revision numbering respects COS spec immutability: compare the desired COS against the latest existing revision for the group via `equality.Semantic.DeepDerivative`; reuse it when already reflected, otherwise create a new revision with the next revision number (never update an existing revision in place)
- Phase 1: create the adopting (`None`) revision when the desired spec has no equivalent existing revision
- Gate the pipeline (return a non-nil `ctrl.Result` requeue) while revision 1 exists but `status.completedAt` is nil, so `ApplyBundle` does not create the COD during adoption
- Delete the Helm release storage only after the adopting revision reaches `completedAt`; delete only the Helm bookkeeping secrets (never a Helm uninstall, which would tear down the now-adopted resources)
- When multiple release-history secrets exist, delete them oldest-to-newest (ascending version) so a partial-delete failure leaves the newest deployed release present (no "rewind" on the next reconcile)
- Phase 2: after the gate lifts, the normal apply path creates the COD with `collisionProtection: Prevent` (no change to codgen's default), triggering orb to stamp revision 2 which takes over via sibling handoff
- Do NOT manually clean up the adopting revision: rely on orb's `adoptOrphans` -> `archiveSuperseded` (only after the `Prevent` revision `IsAvailable`) -> `pruneArchived` to take over, archive, and prune it
- Wire a Helm `ActionClientGetter` and the migration step into `orbOperatorReconcilerConfigurator`, ordered as the first step after `ValidateClusterExtension` - before `RetrieveRevisionStates` and, critically, before resolution (`ResolveBundle`) so adoption needs no catalog access and gates ahead of resolve/unpack
- Migration is idempotent and resumable across controller restarts

## Acceptance Criteria

- Unit test: no deployed Helm release -> migrator is a no-op (pipeline proceeds)
- Unit test: deployed release found, no COS -> revision 1 created with `None`, `spec.group = ext.Name`, revision 1, bundle annotations, owner labels, and CE ownerReference
- Unit test: desired COS equivalent to the latest existing revision -> no new revision created (idempotent)
- Unit test: desired COS differs from the latest existing revision -> a new revision is created with the next revision number (existing revision left unchanged)
- Unit test: a Helm release large enough to exceed the size threshold -> revision phases use `objectRef`s and ClusterObjectSlices are produced (and created before the revision)
- Unit test: latest release not `deployed` -> falls back to the most-recent `deployed` release in history; none found -> no-op
- Unit test: revision 1 exists but `completedAt` nil -> step returns a requeue result (pipeline gated) and the Helm release is NOT deleted
- Unit test: adopting revision has `completedAt` -> Helm release storage is deleted (bookkeeping secrets only) and the step no longer gates
- Unit test: multiple release-history secrets -> deleted oldest-to-newest; a simulated mid-delete failure leaves the newest deployed release present
- Unit test: COS-from-Helm generator places all objects in a single assertion-free phase (no per-GVK assertions, no kind-based ordering); a >50-object release splits into additional no-assertion phases only to satisfy the cap
- Unit test: the migration step is ordered before `ResolveBundle` in the orb pipeline
- Unit test: the adopting revision is created with a non-controller CE ownerReference and no `LabelTemplateHash` (so orb can adopt it and will stamp the `Prevent` revision)
- Unit/e2e: a Helm-backed ClusterExtension ends up managed by an orb COD with a `Prevent` revision, its objects preserved (not recreated), and reported installed by `OrbOperatorRevisionStatesGetter`
