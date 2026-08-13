# Verification

## Implementation Correctness

- [ ] Deployed Helm release is the migration trigger; absence -> no-op, presence -> migration in progress
- [ ] Most-recent `deployed` release is selected, with history fallback when the latest is not `deployed`
- [ ] COS-from-Helm generator produces a COS with `None`, `spec.group = ext.Name`, `Active`, owner labels, bundle annotations, a **non-controller** CE ownerReference, and no `LabelTemplateHash`
- [ ] The adopting COS places all objects in a single assertion-free phase (no per-GVK assertions, no kind-based ordering; chunked at 50 only when the object count requires it)
- [ ] The migration step is ordered before resolution (`ResolveBundle`) - it is the first step after `ValidateClusterExtension`
- [ ] Revision reuse-vs-increment uses `equality.Semantic.DeepDerivative` against the latest existing revision
- [ ] The adopting revision is externalized when large: phases use `objectRef`s and ClusterObjectSlices are created before the revision
- [ ] Revision numbering respects immutability: equivalent desired spec reuses the latest revision; a differing desired spec creates the next revision number without modifying the existing one
- [ ] The step returns a requeue `ctrl.Result` while the adopting revision's `completedAt` is nil (pipeline gated before `ApplyBundle`)
- [ ] The Helm release storage (bookkeeping secrets only, not an uninstall) is deleted only after the adopting revision reaches `completedAt`
- [ ] Release-history secrets are deleted oldest-to-newest, so a partial-delete failure leaves the newest deployed release present (no rewind)
- [ ] After the gate lifts, the COD is created with `Prevent` (codgen default unchanged) and orb stamps the `Prevent` revision that takes over via sibling handoff
- [ ] The migrator performs no manual adopting-revision cleanup (relies on orb's adopt/archive/prune); the non-controller CE ownerReference leaves the revision adoptable
- [ ] Migration is idempotent/resumable across controller restarts
- [ ] Helm `ActionClientGetter` and the migration step are wired into `orbOperatorReconcilerConfigurator` as the first step after `ValidateClusterExtension` (before resolution)
- [ ] All unit tests pass; e2e migration test passes

## Project Conventions

- [ ] Code follows Go style and passes `make lint`
- [ ] No `//nolint` comments added
- [ ] Reuses shared helpers (`splitManifestDocuments`, sanitization, phase building) rather than duplicating codgen logic
- [ ] Uses the `labels.*` key constants for annotations/labels
- [ ] Mirrors `BoxcutterStorageMigrator` structure where applicable (per specs/mission.md: simple, predictable, do not fight Kubernetes)
- [ ] Uses orb-operator and Helm types from tech-stack (`github.com/joelanford/orb-operator`, `helm.sh/helm/v3`, helm-operator-plugins)
- [ ] `make test-unit` passes; `make verify` shows no unintended generated-code changes
