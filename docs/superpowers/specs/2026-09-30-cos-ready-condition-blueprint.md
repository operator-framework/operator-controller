# ClusterObjectSet `Ready` condition — reasons & messages blueprint

**Date:** 2026-09-30
**Status:** Design blueprint (implementation deferred)
**Branch context:** `replace-available-with-ready`

## Purpose

Blueprint the target status-condition vocabulary for the `ClusterObjectSet`
(COS) health condition, revisited from a UX perspective. Two changes frame this
work:

1. The condition is being **renamed `Available` → `Ready`** (see the
   `ClusterObjectDeployment` layering decision — COS gets `Ready`, the future
   `ClusterObjectDeployment` gets `Available` + `Progressing`).
2. The reasons/messages are being **revisited for clarity, actionability, and
   strict semantic boundaries** so COS reads correctly when used standalone
   (outside ClusterExtension) and when its condition is propagated onto the
   ClusterExtension.

## Design decisions made during brainstorming

- **Layering:** rollout/progress reasons (`RollingOut`, `ProgressDeadlineExceeded`)
  stay on COS **for now**, but ultimately belong on `ClusterObjectDeployment`.
  `ProgressDeadlineExceeded` in particular is transitional.
- **Verbiage:** drop "revision" and bundle `<version>` from messages. COS
  self-describes as **"the object set"** (matches the kind, standalone-safe,
  reads correctly when propagated onto ClusterExtension). Per-object messages
  say **"object(s)"**. Bundle version is a CE concept and must not appear.
- **Split the overloaded `Reconciling`** (currently ~11 distinct causes) into
  specific, actionable reasons; keep `Reconciling` only for truly transient
  internal errors.
- **Split `Blocked`** (currently 2 causes) into two reasons with distinct
  remediations: `MutableReferencedSecret` (a referenced Secret is not immutable —
  a precondition/policy check) and `DigestMismatch` (current phase content
  digests no longer match those recorded in `status.observedPhases` — drift after
  creation, e.g. a referenced Secret was modified or recreated with different
  content).
- **Add `Archiving`** for teardown-in-progress (distinct from generic transient
  errors), leading to the terminal `Archived`.
- **Message style:** capitalized, complete sentence, terminal period; avoid raw
  Go `err.Error()` jargon except in the transient `Reconciling` catch-all.

## Target blueprint

### `Ready = True`

| Reason | Trigger | Message |
| --- | --- | --- |
| `AllObjectsReady` | Rollout complete — `rres.IsComplete()`: all phases done and every object passes its progress probe. Also stamps `status.completedAt` (once). | `All objects are ready and pass their probes.` |

`AllObjectsReady` was chosen over `ProbesSucceeded` (hardcodes the probe
mechanism), `Succeeded` (implies terminal completion — that is now tracked by
`status.completedAt`; `Ready` is a live health signal), and bare `Ready`
(tautological). It is general, informative, and future-proof if readiness grows
beyond a single progress probe.

### `Ready = False`

| Reason | Trigger | Message (template) | Notes |
| --- | --- | --- | --- |
| `ObjectsNotReady` | Not complete and deadline not exceeded: ≥1 object is not yet ready (progress probe pending/failing, or objects still being applied). Absorbs the former `RollingOut` + `ProbeFailure` — on the readiness axis these are one state seen at two instants (a pending probe already counts as not-`True`, `clusterobjectset_controller.go:266`). | `<N> object(s) are not yet ready:\n<per-object detail>` (or `Objects are not yet ready.` before any probe reports) | readiness-axis reason, symmetric with `AllObjectsReady`; per-object detail preserved |
| `ProgressDeadlineExceeded` | `isDeadlineExceeded` (time since start > `spec.progressDeadlineMinutes`) on any non-blocked path; wraps the would-be message as "Last status". | `The object set did not become ready within <N> minute(s). Last status: <msg>` | ⚑ transitional → moves to `ClusterObjectDeployment.Progressing` (the only remaining reason destined for COD) |
| `ValidationFailed` | A boxcutter *classified* preflight validation error — either the static revision check (`rres.GetValidationError()`) or the per-phase dry-run check (`pres.GetValidationError()`). See "Validation failure taxonomy" below for the exact causes. | `Object set manifests are invalid: <detail>` / `Phase <N> is invalid: <detail>` | split out of `Reconciling` (preflight + phase validation). Only *classified* validation errors land here; an unclassified dry-run failure (transient/network, or an APIStatus reason boxcutter does not treat as validation) returns a plain error → `Reconciling`. |
| `ObjectCollision` | ≥1 object reports `machinery.ActionCollision`. This is structurally a **foreign** owner: boxcutter only emits `ActionCollision` for unknown/foreign controllers (sibling-revision transitions become `ActionProgressed`, never a collision). No revision comparison needed on our side. | `Object(s) in phase <N> are managed by another object set:\n<list>` | split out of `Reconciling` |
| `MutableReferencedSecret` | ≥1 referenced Secret is not immutable — `verifyReferencedSecretsImmutable` fails (a referenced Secret has `immutable != true`). A precondition/policy check, **not** drift detection. **Precedence: over the deadline.** | `One or more referenced Secrets are not immutable. Referenced Secrets must set immutable: true; make them immutable or reference immutable Secrets.` | split out of `Blocked` |
| `DigestMismatch` | Current phase content digests differ from those recorded in `status.observedPhases` (`verifyObservedPhases` fails). Catches any post-creation content change — including a referenced Secret modified, or deleted and recreated with different content. **Precedence: over the deadline.** | `The resolved content for phase(s) <names> no longer matches the digest recorded in status.observedPhases. A referenced object source may have been deleted and recreated with different content. Create a new object set to apply the changes.` | split out of `Blocked` |
| `Archiving` | `spec.lifecycleState=Archived` and teardown in progress (teardown error, or owned resources not yet removed). | `The object set is being archived; removing owned objects.` | new — teardown in progress |
| `Reconciling` | Transient internal error: phase build, engine creation, watch establishment, revision reconcile, or cache free. | `Encountered a transient error, retrying: <err>` | now only transient/internal errors |
| `Archived` | `spec.lifecycleState=Archived` and teardown complete. | `The object set is archived.` | terminal |

**Precedence (when multiple triggers hold):** `MutableReferencedSecret` /
`DigestMismatch` (blocked) → `ProgressDeadlineExceeded` →
`ObjectsNotReady`. Blocked wins because it is the most actionable; the deadline
wins over per-object detail so the summarized "Last status" is reported.
(Precedence is a guideline to finalize during implementation.)

### `Ready = Unknown`

| Trigger | Message |
| --- | --- |
| Before the first reconcile — the condition is absent (consumers treat a missing condition as `Unknown`). | *(none — condition not yet set)* |

## Mapping from current state

Current (`Available`) reasons and where they go:

| Current reason | Current message | Target |
| --- | --- | --- |
| `ProbesSucceeded` | `Objects are available and pass all probes.` | `AllObjectsReady` (generalized; reworded to "ready") |
| `RollingOut` | `Revision <version> is rolling out.` | merged → `ObjectsNotReady` |
| `ProbeFailure` | `Object <Kind>.<gv> <ns>/<name>: <msgs>` (joined) | merged → `ObjectsNotReady` (add count summary; per-object detail kept) |
| `ProgressDeadlineExceeded` | `Revision has not rolled out for <N> minute(s). Last status: <msg>` | `ProgressDeadlineExceeded` (reworded; transitional → COD) |
| `Reconciling` | raw `err.Error()` across ~11 sites | split → `ValidationFailed`, `ObjectCollision`, `Archiving`, and a slimmed `Reconciling` |
| `Blocked` | raw `err.Error()` (2 causes) | split → `MutableReferencedSecret`, `DigestMismatch` |
| `Archived` | `revision is archived` | `Archived` (reworded/capitalized) |

## Validation failure taxonomy (`ValidationFailed`)

Confirmed against boxcutter v0.14.0 (`validation/` package). Two validators feed
the two error surfaces the COS controller reads. `ValidationFailed` covers both.

**A. Revision-level, static preflight** — `rres.GetValidationError()`
(`*RevisionValidationError`), from `RevisionValidator.Validate` →
`staticValidateMultiplePhases`. Runs once, up front, **no API-server contact**:

1. Static object-metadata violations (`validation/metadata.go`
   `validateObjectMetadata`): `apiVersion` empty, `kind` empty, or any of
   `metadata.{uid,generation,generateName,finalizers,ownerReferences,resourceVersion}`
   non-empty (manifests must not carry these).
2. Invalid phase name (`validatePhaseName`) — not a DNS1035 label.
3. Duplicate object across/within phases (`checkForObjectDuplicates`) — same
   GVK + namespace + name in more than one phase (`PhaseObjectDuplicationError`).

**B. Phase-level, dry-run preflight** — `pres.GetValidationError()`
(`*PhaseValidationError`), from `PhaseValidator.Validate` (`machinery/phases.go:198`),
run per phase right before that phase applies. Does everything in A for its
objects **plus** live checks:

4. Namespace-scope violations (`validateNamespace`, **only** on the namespaced
   validator, `allowNamespaceEscalation=false`): `MustBeNamespaceScopedResourceError`
   (cluster-scoped resource in a namespaced context), `MustBeInNamespaceError`
   (object namespace ≠ owner namespace), `meta.NoMatchError` (GVK not in cluster).
5. API-server dry-run rejection (`validateDryRun` → `DryRunValidationError`):
   SSA dry-run (falling back to create-dry-run on NotFound) rejected with an
   APIStatus reason of `Unauthorized`, `Forbidden`, `AlreadyExists`, `Conflict`,
   `Invalid`, `BadRequest`, `MethodNotAllowed`, `RequestEntityTooLarge`,
   `UnsupportedMediaType`, `NotAcceptable`, or `NotFound`; plus the empty-reason
   case whose message contains `"failed to create typed patch object"`.

**Boundary:** only *classified* errors become a validation error. In the phase
validator, a dry-run failure that is **not** an `ObjectValidationError` (transient
network error, or an APIStatus reason outside the list above) is `errors.Join`-ed
and returned as a **plain error** — so it flows through the reconcile-error path
(→ `Reconciling`), not `ValidationFailed`. This keeps `ValidationFailed` meaning
"the manifests are structurally/semantically bad or the apiserver rejects them,"
while transient infra hiccups stay in `Reconciling`.

## Implementation notes (when this is picked up)

Primary file: `internal/object-controller/controllers/clusterobjectset_controller.go`.

- Rename the condition type constant `ClusterObjectSetTypeAvailable = "Available"`
  → `Ready` in `api/v1/clusterobjectset_types.go:29`; update the doc block
  (~497-509) and the `printcolumn` (~563). COS is experimental-only (no
  standard-channel CRD), so no compat gate blocks the rename.
- Remove the dead `ClusterObjectSetReasonRetrying = "Retrying"` constant.
- Retire the `Blocked` reason; add the new reason constants.
- Drop `revVersion := cos.GetAnnotations()[labels.BundleVersionKey]` from the
  message paths (~line 226) — bundle version is a CE concept.
- Merge the `RollingOut` (~228, ~295) and `ProbeFailure` (~293) paths into a
  single `ObjectsNotReady` reason; keep the per-object detail as the message,
  falling back to a generic line before any probe reports. Drop the bare
  in-transition `RollingOut` (~228) as a distinct reason.
- Split `setRetryingConditions` call sites by cause:
  - preflight validation (~201) + phase validation (~208) → `ValidationFailed`
  - object collisions (~221) → `ObjectCollision` (foreign owner only — boxcutter
    emits `ActionCollision` solely for unknown/foreign controllers).
  - archive teardown incomplete (~324) → `Archiving`
  - phase build (~143), engine create (~157), watch (~183), reconcile (~193),
    cache-free (~171, ~307), archive teardown error (~320) → `Reconciling`
- Split the `markAsUnavailable(..., Blocked, ...)` sites: referenced-Secret
  immutability check `verifyReferencedSecretsImmutable` (~137) →
  `MutableReferencedSecret`; resolved-phase digest drift `verifyObservedPhases`
  (~151) → `DigestMismatch`.
- Update readers/tests: `internal/operator-controller/controllers/common_controller.go`
  (reads the condition to roll up CE status) and the `_test.go` files for both
  controllers. (NB: `internal/operator-controller/applier/boxcutter.go` asserts a
  managed *Deployment's* `appsv1.DeploymentAvailable` condition — unrelated to the
  COS condition; do not touch it.)
- **Keep the ClusterExtension `Available` condition — decouple it from the COS rename.**
  `common_controller.go` currently copies the COS condition onto the CE *verbatim,
  including `.Type`* (`setAvailableFromRevisionStates`, `setActiveRevisionsFromRevisionStates`),
  so a naive rename would silently flip the CE's condition (top-level and
  `status.activeRevisions[].conditions[]`) to `Ready`. The CE contract (documented at
  `clusterextension_types.go:522`) must keep exposing `Available`. So: the reader
  *looks up* the COS `Ready` condition but *re-emits* onto the CE as a CE-owned
  `TypeAvailable = "Available"` constant (new, in `common_types.go`). This also keeps
  the CE contract stable across the future COS→COD transition (COD will own `Available`).
  The CE `Progressing`/`Installed` **reasons** are preserved; the CE `Progressing`
  **message** is allowed to change (it flows through from the reworded COS message via
  `progressingFromAvailable`) — this is intended.
- Regenerate: `make manifests crd-ref-docs` (experimental CRD YAML,
  `manifests/experimental*.yaml`, `docs/api-reference/olmv1-api-reference.md`).

## Open / follow-on

- **`Superseded` reason — DEFERRED (not in this plan; to be added later).** A
  distinct reason for objects adopted by a newer sibling revision is out of scope
  for this iteration. The boxcutter research below is retained so it can be picked
  up when `Superseded` is added. Until then, a partial supersession surfaces as
  `ObjectsNotReady` (phase `InTransition() == true`).
  Engine support is already **CONFIRMED (boxcutter v0.14.0)** — boxcutter
  distinguishes a newer-sibling adoption from a foreign owner, so no
  revision-comparison logic is needed on our side:
  - OLM wires siblings via `boxcutter.WithSiblingOwners(siblingObjs)`
    (`clusterobjectset_controller.go:491`, from `listSiblingRevisions`).
  - `machinery/objects.go:365-419`: a newer sibling owning an object →
    `newObjectResultProgressed` → `ActionProgressed`; an unknown/foreign
    controller → `newObjectResultConflict` → `ActionCollision`. `ActionProgressed`
    has a single producer (that path).
  - When added, detect `Superseded` via per-object `Action() == machinery.ActionProgressed`,
    or phase-level `HasProgressed()` (all objects moved) / `InTransition()`
    (some moved) in `machinery/phases.go`.
  - Note: a *partial* supersession sets phase `InTransition() == true`, which
    drives the old revision to the in-progress state (`ObjectsNotReady` after the
    merge) — so when `Superseded` is added it must be evaluated before `ObjectsNotReady`.
- When `ClusterObjectDeployment` lands: move `ProgressDeadlineExceeded` to
  `COD.Progressing` (the only remaining COS reason destined for COD, now that
  `RollingOut` is absorbed into the readiness reason `ObjectsNotReady`), and
  rewire ClusterExtension to observe COD (`Available`/`Progressing`) rather than
  reading COS directly in `common_controller.go`.
