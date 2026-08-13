---
status: in-progress
---
# orb-operator Helm Storage Migration

## Summary

Enable ClusterExtensions installed under the legacy Helm runtime to move to the orb-operator runtime without an uninstall/reinstall. A new migration reconcile step generates a first `ClusterObjectSet` (COS) revision directly from the deployed Helm release, with `collisionProtection: None`, so the orb COS controller **adopts** the existing Helm-managed objects. Once that revision completes, the normal apply path creates the `ClusterObjectDeployment` (COD) with `collisionProtection: Prevent`, and the orb COD controller stamps a second revision that takes over the adopted objects via orb's sibling handoff. This mirrors the role `BoxcutterStorageMigrator` plays for the Boxcutter runtime.

## Design

### Why adoption needs a hand-built first COS

orb's COD controller keeps every COS equal to its COD template: on a template-hash match it runs `ensureFieldOwnership`, which force-applies the COD template onto the COS; on a mismatch it stamps a new revision. `collisionProtection` is part of the template hash. Consequences:

- You **cannot** hold a `None` COS under a `Prevent` COD - the controller would either force the COS back to `Prevent` (hash match) or stamp a competing `Prevent` revision (hash mismatch). Either breaks adoption of still-unowned Helm objects.
- `None` is only needed for the **first** adoption of the externally-owned (Helm) objects. Once a revision owns them, a later revision takes them over via orb's group sibling handoff (not gated by collision protection), so `Prevent` is safe for every subsequent revision.

Therefore the migrator creates the first COS **directly** (standalone, no COD yet) with `None`, lets it adopt and reach `completedAt`, and only then allows the COD (`Prevent`) to be created.

### The deployed Helm release is the migration trigger

Rather than keying idempotency off "does a COD/COS exist," the migrator keys off the **deployed Helm release**: its presence means migration has not yet completed, its absence means there is nothing to migrate (fresh install, or migration already finished). Crucially, the Helm release secret is **not deleted until the adopting COS (revision 1) reaches `completedAt`**. This makes migration resumable: if the controller restarts mid-adoption, the release is still present, so the migration step keeps running - re-ensuring revision 1 and holding the pipeline gated - until adoption genuinely completes.

### Sequence

1. **Skip when nothing to migrate**: if there is no deployed Helm release, return and let the normal pipeline run.
2. **Phase 1 - adopt.** When a deployed Helm release is found, build the desired adopting COS from the release and reconcile it into a revision:
   - `spec.group = ext.Name`, `lifecycleState: Active`, `spec.collisionProtection: None`
   - **all objects in a single phase, with no assertions/availability probes** - this mimics Helm's semantics (apply everything at once, no ordering, no readiness gating) in orb syntax. Since the objects are already running (Helm deployed them), the goal is fast, low-risk ownership takeover, not a fresh phased rollout: no probes means the revision reaches `completedAt` as soon as the objects are synced rather than waiting on per-object conditions that a phased/asserted layout (like the COD generator's) could stall on. Objects are still sanitized (status stripped, metadata trimmed) the same way. If a release exceeds the orb per-phase cap (50 objects), split into additional no-assertion phases purely to satisfy the limit - not for ordering.
   - **externalize** the built revision through the externalizer: pack large object sets into ClusterObjectSlices and rewrite phases to `objectRef`s, so an oversized Helm release does not exceed etcd limits (same treatment the COD apply path gets)
   - owner labels (`objectLabels`) and bundle-metadata annotations (bundle name/version, package, reference) so the `OrbOperatorRevisionStatesGetter` reports it correctly
   - a **non-controller** ownerReference to the ClusterExtension (for GC during the pre-COD window). It must NOT be a controller ref: orb's `adoptOrphans` only adopts COSs that have no controller owner (`GetControllerOf(cos) == nil`), so a non-controller ref lets orb later set the COD as controller.
   - **no `LabelTemplateHash`** (leave it unset). The label only matters for COD-owned revisions; an unset value guarantees a mismatch against the COD's `Prevent` template hash, ensuring the COD controller stamps the `Prevent` revision rather than running `ensureFieldOwnership` on the adopting revision.
   - **revision numbering (COS spec is immutable).** Compare the desired COS against the latest existing revision for the group using `equality.Semantic.DeepDerivative(desiredSpec, existingSpec)` (same pattern as the applier's `alreadyApplied`): if the desired is already reflected, reuse it (idempotent no-op); otherwise create a **new** revision with the next revision number rather than updating in place. First migration produces `<ext.Name>-1`; a changed desired spec produces `<ext.Name>-2`, etc. (orb continues numbering from the highest existing revision.)
3. **Gate the pipeline.** While the adopting (`None`) revision exists but `status.completedAt` is nil, the migration step returns a non-nil `ctrl.Result` (requeue) so the pipeline stops before `ApplyBundle` - preventing the COD from being created while adoption is still in progress (which would stamp a premature `Prevent` revision that collides with the unowned objects).
4. **Release the trigger.** Once the adopting revision has `completedAt`, adoption has succeeded: delete the Helm release **storage** for the extension. This deletes only the Helm bookkeeping secrets (`helm.sh/release.v1`), **not** a Helm uninstall - the managed resources are now orb-adopted and must not be torn down. Delete the history secrets **oldest to newest** (ascending release version): the newest deployed release - the one the migrator keys off - is removed last, so a partial-delete failure leaves it present and the next reconcile resumes against the same release rather than "rewinding" to an older one. From here the migration step becomes a no-op on subsequent reconciles (no deployed release), and the normal pipeline runs. (Adopting-revision cleanup is independent of this deletion - orb handles it automatically; see step 6.)
5. **Phase 2 - hand off.** With the gate lifted, `ApplyBundle` -> `OrbOperator.Apply` -> codgen produces the COD with `collisionProtection: Prevent`. The orb COD controller sees no owned COS matching its (Prevent) template hash and stamps the next revision (`Prevent`), which takes over the adopted objects from the adopting revision via sibling handoff, then reaches `completedAt` itself.
6. **Cleanup of the adopting revision is automatic.** Once the COD exists, orb's COD controller `adoptOrphans` claims the standalone adopting revision (it has no controller owner) by setting the COD as its controller. `archiveSuperseded` then archives it - but only after the `Prevent` revision `IsAvailable`, i.e. only after the takeover has succeeded (orb enforces the safe ordering for us) - and `pruneArchived` removes it per the COD's `revisionHistoryLimit`. The migrator does no manual cleanup.

### Components

- **`OrbStorageMigrator`** (in `internal/operator-controller/applier/`): holds a Helm `ActionClientGetter`, a COS-from-Helm generator, a client, `Scheme`, and `FieldOwner`. Looks up the most-recent **deployed** Helm release (falling back through history if the latest is not `deployed`, as the Boxcutter migrator does), reconciles the adopting revision (create/reuse with next-revision-number semantics, externalizing large releases), reports whether adoption has completed, and deletes the Helm release storage once it has. It does **not** clean up the adopting revision - orb does that automatically (see step 6).
- **COS-from-Helm generator**: a method (parallel to `SimpleRevisionGenerator.GenerateRevisionFromHelmRelease`) that builds an `orbac.ClusterObjectSetApplyConfiguration` from a Helm release - splitting the manifest into objects, sanitizing them, and placing them all in a **single, assertion-free phase** (chunked at the 50-object cap only when necessary), with `None` collision protection plus bundle annotations. It deliberately does **not** reuse the COD generator's kind-based phase assignment or per-GVK assertions.
- **Externalizer reuse**: add an `ExternalizeCOS(cos) (cos, []cosl, error)` entry point alongside the existing `ExternalizeCOD(cod)`, refactoring the shared logic into a private phases-level core (size probe + `pack(phases)` + replace-inline-with-refs + label/ownerRef propagation). `slicePacker.pack` already operates on `[]PhaseApplyConfiguration`, and `ClusterObjectSetSpec` embeds the same `Phases`, so the refactor is mechanical. The migrator applies the returned ClusterObjectSlices before the revision.
- **Reconcile step**: a dedicated orb step (do **not** overload the shared `StorageMigrator`/`MigrateStorage`, which is synchronous for Boxcutter). The orb migrator's `Migrate` returns `(*ctrl.Result, error)` so the step can requeue while adoption is in progress. It is wired as the first step after `ValidateClusterExtension` - **before `RetrieveRevisionStates`, `ResolveBundle`, `UnpackBundle`, and `ApplyBundle`**, i.e. before resolution. Adoption of an already-running workload needs no catalog access, so ordering it ahead of resolution lets migration proceed even when the catalog is unavailable, and its gating avoids doing resolve/unpack work while adoption is still in progress.
- **Wiring** in `orbOperatorReconcilerConfigurator`: construct a Helm `ActionClientGetter` (currently only the Boxcutter and Helm configurators build one) and the `OrbStorageMigrator`, and prepend the migration step.

### Interaction with existing pieces

- The `OrbOperatorRevisionStatesGetter` already lists COS by `spec.group` and classifies installed via `completedAt` - so during phase 1 it reports the adopting revision as installed, and after handoff it reports the `Prevent` revision (highest revision wins). No getter changes needed.
- The externalizer already propagates owner references and labels onto the slices; reusing it for the migrator's revision requires the `ExternalizeCOS` generalization described in Components / decision 2.

## Resolved design decisions

Each of these was verified against `github.com/joelanford/orb-operator@v0.0.3` controller source:

1. **Adopting-revision cleanup: automatic, no manual step.** orb's COD controller `adoptOrphans` claims any COS in the group that has no controller owner, then `archiveSuperseded` archives non-latest owned revisions once the latest `IsAvailable`, and `pruneArchived` deletes them per `revisionHistoryLimit`. So the migrator creates the adopting revision with a **non-controller** CE ownerReference (leaving it adoptable), and orb handles takeover-then-archive-then-prune in the correct safe order. This also lets the Helm release be deleted at the adopting revision's `completedAt` without any cleanup-timing concern.
2. **Externalizer generalization: add `ExternalizeCOS`.** Refactor a shared phases-level core out of `ExternalizeCOD(cod)` and add `ExternalizeCOS(cos)`; `pack` already works on `[]PhaseApplyConfiguration` and COS embeds the same `Phases`.
3. **Reuse-vs-increment comparison: `equality.Semantic.DeepDerivative`** of the desired COS spec against the latest existing revision's spec (mirrors the applier's `alreadyApplied` gating), so the check is stable and does not spuriously increment.
4. **No `LabelTemplateHash` on the adopting revision.** Unset guarantees a mismatch with the COD's `Prevent` template hash, so the COD controller stamps the `Prevent` revision (rather than `ensureFieldOwnership`-ing the adopting revision back to `Prevent`).
5. **No finalizer interaction.** The orb content-manager-cache finalizer is a no-op today, and Helm-storage deletion is a reconcile-time action (not finalizer-driven), so there is nothing to coordinate.
6. **Dedicated orb step, shared interface untouched.** Keep Boxcutter's synchronous `StorageMigrator`/`MigrateStorage` as-is; the orb migrator's `Migrate` returns `(*ctrl.Result, error)` and is wrapped by an orb-specific step that can requeue during adoption.
