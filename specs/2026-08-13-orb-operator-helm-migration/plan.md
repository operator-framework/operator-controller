# Implementation Plan

0. Generalize the externalizer (`internal/operator-controller/applier/orb/`):
   - Extract a shared phases-level core (or add a COS entry point) so both the COD path and the migrator's COS can externalize; keep the existing `ExternalizeCOD(cod)` behavior intact

1. COS-from-Helm generator (`internal/operator-controller/applier/`):
   - Add a method that builds an `orbac.ClusterObjectSetApplyConfiguration` from a `*release.Release` and `ext`, parallel to `SimpleRevisionGenerator.GenerateRevisionFromHelmRelease`
   - Reuse `splitManifestDocuments` and object sanitization, but place all objects in a **single assertion-free phase** (chunk at 50 only when necessary); do NOT reuse the COD generator's kind-based phase assignment or per-GVK assertions
   - Set `spec.group = ext.Name`, `lifecycleState: Active`, `collisionProtection: None` (revision number set by the migrator)
   - Set owner labels, bundle-metadata annotations, and a **non-controller** ownerReference to the ClusterExtension; do not set `LabelTemplateHash`

2. `OrbStorageMigrator` (`internal/operator-controller/applier/`):
   - Fields: `ActionClientGetter`, COS generator, client, `Scheme`, `FieldOwner`
   - `Migrate`: find deployed Helm release (with history fallback); if none, no-op
   - Build the desired adopting COS; externalize it (create slices first)
   - List existing revisions for the group; if the desired spec is a `equality.Semantic.DeepDerivative` of the latest, reuse it; otherwise assign the next revision number and create it (never update in place)
   - Report adoption state (adopting revision `completedAt`) so the step can gate/requeue
   - Once `completedAt`, delete the Helm release storage: list the release's `helm.sh/release.v1` secrets and delete them oldest-to-newest (ascending version); do not run a Helm uninstall
   - No manual adopting-revision cleanup: orb's `adoptOrphans`/`archiveSuperseded`/`pruneArchived` take over, archive (after the `Prevent` revision is available), and prune it

3. Reconcile step (dedicated orb step; leave the shared `StorageMigrator`/`MigrateStorage` untouched):
   - Orb migrator `Migrate` returns `(*ctrl.Result, error)`; the step returns a requeue result during phase 1 and nil afterward
   - Wire it as the first step after `ValidateClusterExtension` (before `RetrieveRevisionStates` and before resolution/`ResolveBundle`)

4. Wiring (`cmd/operator-controller/main.go`):
   - Construct a Helm `ActionClientGetter` in `orbOperatorReconcilerConfigurator` (mirror the Boxcutter/Helm configurators)
   - Construct `OrbStorageMigrator` and prepend the migration step as the first step after `ValidateClusterExtension` (before resolution)

5. Tests:
   - Unit tests for the generator and migrator covering the acceptance criteria (fake client + fake `ActionClientGetter`; seed COS with/without `completedAt`)
   - An upgrade/regression e2e (under `test/`) proving a Helm-backed CE migrates to an orb `Prevent` revision with objects preserved
