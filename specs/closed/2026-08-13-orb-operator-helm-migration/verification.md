# Verification

## Implementation Correctness

- [x] Deployed Helm release is the migration trigger; absence -> no-op, presence -> migration in progress
- [x] Most-recent `deployed` release is selected, with history fallback when the latest is not `deployed`
- [x] COS-from-Helm generator produces a COS with `None`, `spec.group = ext.Name`, `Active`, owner labels, bundle annotations, a **non-controller** CE ownerReference, and no `LabelTemplateHash` (the generator sets spec/labels/annotations; the migrator stamps the non-controller CE ownerReference, mirroring `BoxcutterStorageMigrator`)
- [x] The adopting COS places all objects in a single assertion-free phase (no per-GVK assertions, no kind-based ordering; chunked at 50 only when the object count requires it)
- [x] The migration step is ordered before resolution (`ResolveBundle`) - it is the first step after `ValidateClusterExtension`
- [x] Revision reuse-vs-increment uses `equality.Semantic.DeepDerivative` against the latest existing revision (comparing the externalized desired spec so large releases do not spuriously increment)
- [x] The adopting revision is externalized when large: phases use `objectRef`s and ClusterObjectSlices are created before the revision
- [x] Revision numbering respects immutability: equivalent desired spec reuses the latest revision; a differing desired spec creates the next revision number without modifying the existing one
- [x] The step returns a requeue `ctrl.Result` while the adopting revision's `completedAt` is nil (pipeline gated before `ApplyBundle`)
- [x] The Helm release storage (bookkeeping secrets only, not an uninstall) is deleted only after the adopting revision reaches `completedAt`
- [x] Release-history secrets are deleted oldest-to-newest, so a partial-delete failure leaves the newest deployed release present (no rewind)
- [x] After the gate lifts, the COD is created with `Prevent` (codgen default unchanged) and orb stamps the `Prevent` revision that takes over via sibling handoff
- [x] The migrator performs no manual adopting-revision cleanup (relies on orb's adopt/archive/prune); the non-controller CE ownerReference leaves the revision adoptable
- [x] Migration is idempotent/resumable across controller restarts
- [x] Helm `ActionClientGetter` and the migration step are wired into `orbOperatorReconcilerConfigurator` as the first step after `ValidateClusterExtension` (before resolution)
- [x] All unit tests pass
- [ ] e2e migration test passes - **deferred**: no migration e2e precedent exists in the repo (the analogous `BoxcutterStorageMigrator` is covered by unit tests only, and there is no orb e2e suite yet). Tracked as a follow-up.

## Project Conventions

- [x] Code follows Go style and passes `make lint`
- [x] No `//nolint` comments added
- [x] Reuses shared helpers (`splitManifestDocuments`, sanitization via `sanitizedUnstructured`, `mergeStringMaps`, the `orb` externalizer) rather than duplicating codgen logic
- [x] Uses the `labels.*` key constants for annotations/labels
- [x] Mirrors `BoxcutterStorageMigrator` structure where applicable (per specs/mission.md: simple, predictable, do not fight Kubernetes)
- [x] Uses orb-operator and Helm types from tech-stack (`github.com/joelanford/orb-operator`, `helm.sh/helm/v3`, helm-operator-plugins)
- [x] `make test-unit` passes; regeneration (`make generate manifests`) produces no unintended generated-code changes
