# ClusterObjectDeployment Progressing reasons: surfacing why a rollout is impeded

**Date:** 2026-10-27
**Status:** Design — approved for planning
**Branch:** `cod-progressing-reasons`

## Intent and why

The `ClusterObjectDeployment` (COD) `Progressing` condition today collapses every
in-flight rollout into a single reason: whenever the head active
`ClusterObjectSet` (COS) revision has not completed, the COD reports
`Progressing=True / NewClusterObjectSetProgressing` with a generic message. The
only non-progressing outcomes are `NewClusterObjectSetProgressed` (done),
`ProgressDeadlineExceeded`, and `InvalidSpecification` (the COD's own template
metadata is invalid).

This is opaque to a COD user. When a rollout is stuck, the user's real question
is **"is this rollout impeded in a way that requires me to act, and what do I
do?"** — not merely "is it done yet?". The head active COS already knows the
answer via its own `Ready` condition reason (`Pending`, `ObjectsNotReady`,
`PreconditionsNotMet`, `ObjectCollision`, `InvalidSpecification`,
`AllObjectsReady`, `Superseded`, `Archiving`, `Archived`). The COD flattens all
of that away.

This change gives the COD `Progressing` condition a small, COD-level vocabulary
that classifies the head active COS's `Ready` state into "rollout proceeding
normally" versus "rollout impeded, needs intervention," and carries the remedy
in the message.

## Goals

- Distinguish, on the COD `Progressing` condition, a normally-progressing
  rollout from one that is impeded and needs user intervention.
- For each impeded cause, surface a distinct, stable, machine-matchable reason
  and a human-readable message describing what to do.
- Follow Kubernetes condition conventions: `Progressing=False` means the rollout
  is stalled and will not advance without intervention; `Progressing=True` means
  it is progressing or is expected to (still retrying).
- Keep the user-facing documentation in lockstep: the `conditions` field
  docstring (the CRD schema description), the regenerated api-reference, and the
  concept docs must all describe the new reasons.
- Cover the new behavior with e2e tests, not just unit tests.

## Non-goals

- No change to the COD `Available` condition or its reasons.
- No change to the COS `Ready` condition, its reasons, or the COS controller.
- No 1:1 mirroring of COS reason strings onto the COD. The COD uses its own
  deployment-altitude vocabulary ("revision" = the head active COS).
- No change to how or when revisions are created, archived, or garbage
  collected.
- No change to the progress-deadline mechanism itself (only its precedence
  relative to the new hard-stall reasons).

## Anti-goals (what would make this a failure even if it technically works)

- A user still cannot tell a "just wait" rollout from a "go fix something"
  rollout at a glance.
- Reasons that churn on every reconcile (reason/message flapping) or that are
  too fine-grained to match in automation.
- Conflating the COD's own invalid template metadata with an invalid head
  revision — a user must be able to tell *which* spec is wrong.

## Decisions made (by the human partner)

1. **COD-level vocabulary**, not a faithful 1:1 re-export of the COS `Ready`
   reason string.
2. **Progressing True/False per cause follows Kubernetes convention:**
   - `InvalidSpecification` (head COS) → `Progressing=False` (hard stall)
   - `ObjectCollision` (head COS) → `Progressing=False` (hard stall)
   - `PreconditionsNotMet` (head COS) → `Progressing=True` (keeps retrying)
   - `Pending` / `ObjectsNotReady` (head COS) → `Progressing=True` (normal)
3. **Hard stalls short-circuit the deadline.** If the head revision is already
   stalled on an actionable cause (`ObjectCollision` / `InvalidSpecification`),
   report that — it is more useful than `ProgressDeadlineExceeded`. The deadline
   governs only the still-retrying states (`PreconditionsNotMet`,
   `ObjectsNotReady`, `Pending`).
4. **Use generic, COD-owned messages that name the head revision** — do *not*
   echo the COS `Ready` message. Each mapped reason gets a concise, stable
   COD-owned message that identifies the head `ClusterObjectSet` and points the
   user to inspect it for specifics (the Deployment→Pod inspection pattern: a
   Deployment does not echo its Pod's message). This keeps the COD condition
   message COD-owned and decoupled from COS message formatting; the authoritative,
   always-current detail lives on the revision. Reusing the COS message verbatim
   is a trivial future upgrade if inline remedies are later wanted.

### Note on `PreconditionsNotMet` (the accepted coarseness)

`PreconditionsNotMet` is **not** probe/assertion failure — probe failures live
under `ObjectsNotReady`. But grounding the design against the COS controller
showed `PreconditionsNotMet` is **coarser** than first assumed: the COS emits it,
and **always retries it on a flat timer**, for a mix of genuinely recoverable
causes (mutable referenced Secret, RBAC not yet granted, dependency CRD not yet
propagated) *and* permanent spec errors that only a spec change can fix (the same
object in two phases, a manifest carrying `ownerReferences`) — because the
boxcutter dry-run cannot tell them apart
(`clusterobjectset_controller.go:162,178,206,257,280`;
`cos_static_validation.feature`).

**Decision:** the COD keeps the simple mapping and **defers to the COS's own
classification** — `PreconditionsNotMet → Progressing=True /
RevisionPreconditionsNotMet`, with a generic COD-owned message naming the
revision (decision 4) that directs the user to inspect the revision for the
specific cause. Permanent cases that masquerade as `PreconditionsNotMet` therefore
show as "progressing/retrying" until they eventually trip `ProgressDeadlineExceeded`;
the COD does not try to out-guess the COS. Making this distinction crisp would
require teaching the COS/boxcutter layer to separate permanent from transient
preflight rejections — explicitly **out of scope** here, a possible follow-up.

### Note on `InvalidSpecification` reachability

`ClusterObjectSetReasonInvalidSpecification` is emitted at a single site
(`clusterobjectset_controller.go:186`, for a `configValidationError`), and CRD
admission validation blocks nearly every construction path, so a COD template
that passes admission can realistically reach it only via an edge case (e.g. a
malformed progression-probe label selector). The `RevisionInvalidSpecification`
reason and mapping are still implemented for completeness, but it is covered by a
**unit test only** — not a deterministic e2e. The hard-stall-beats-deadline
precedence is locked via `ObjectCollision`, which is reliably reachable.

## Proposed reason set

The head active COS is framed as "the revision" — the COD's natural altitude.

| Head active COS `Ready` reason | COD `Progressing` | COD reason | COD message |
|---|---|---|---|
| `Pending`, `ObjectsNotReady` | `True` | `NewClusterObjectSetProgressing` *(existing)* | existing generic "latest revision is progressing" |
| `PreconditionsNotMet` | `True` | `RevisionPreconditionsNotMet` *(new)* | generic, names the revision, points to inspect it |
| `ObjectCollision` | `False` | `RevisionObjectCollision` *(new)* | generic, names the revision, points to inspect it |
| `InvalidSpecification` | `False` | `RevisionInvalidSpecification` *(new)* | generic, names the revision, points to inspect it |
| `AllObjectsReady` | n/a | handled via `CompletedAt` → `NewClusterObjectSetProgressed` *(existing)* | rollout complete |

For the three mapped reasons the COD message is a concise COD-owned sentence that
names the head `ClusterObjectSet` and directs the user to inspect it (decision 4);
exact phrasing is the builder's call, following the repo's condition-message
style. The default/progressing path keeps its existing bespoke wording.

Unchanged existing reasons that remain:

- `NewClusterObjectSetProgressed` — head revision completed (`CompletedAt` set).
- `ProgressDeadlineExceeded` — a still-retrying rollout exceeded its deadline.
- `InvalidSpecification` — the COD's **own** template metadata is invalid (set
  before a revision is even created). Deliberately kept distinct from
  `RevisionInvalidSpecification` so the user can tell which spec is wrong.

Reasons stay a small, stable, machine-matchable set. The specific "what to do"
(including, where useful, the head COS's own `Ready` message) lives in the COD
`Progressing` message, not in the reason string.

## Control flow

`reconcileRollout` in
`internal/object-controller/controllers/clusterobjectdeployment_controller.go`
sets `Progressing` through `setProgressing(cod, status, reason, message)`. The
revised ordering (hard stalls short-circuit the deadline; the deadline governs
only still-retrying states):

1. COD template metadata invalid → `Progressing=False / InvalidSpecification`
   *(existing, unchanged — runs before a revision exists).*
2. Ensure the desired revision exists (create if template changed / none). On
   create error → `Progressing=True / NewClusterObjectSetProgressing`
   *(existing, unchanged).*
3. Head revision completed (`CompletedAt` set) →
   `Progressing=False / NewClusterObjectSetProgressed` *(existing, unchanged).*
4. **(new)** Read the head revision's `Ready` condition reason:
   - `InvalidSpecification` → `Progressing=False / RevisionInvalidSpecification`
   - `ObjectCollision` → `Progressing=False / RevisionObjectCollision`

   These short-circuit the deadline.
5. Deadline exceeded → `Progressing=False / ProgressDeadlineExceeded`
   *(existing — now reached only for still-retrying states).*
6. **(new)** Head revision `Ready` reason is `PreconditionsNotMet` →
   `Progressing=True / RevisionPreconditionsNotMet`.
7. Otherwise (`Pending`, `ObjectsNotReady`, missing/empty `Ready` condition, or
   any unrecognized reason) → `Progressing=True / NewClusterObjectSetProgressing`
   *(existing default).*

The head revision's `Ready` condition is read with
`apimeta.FindStatusCondition(latest.Status.Conditions, ocv1.ClusterObjectSetTypeReady)`.
A missing or non-matching reason falls through to the default progressing branch
(step 7), so partially-populated or future COS reasons never produce a false
hard stall.

## Builder's remaining latitude

- **Message phrasing.** The COD message is a generic, COD-owned sentence that
  names the head `ClusterObjectSet` and directs the user to inspect it
  (decision 4) — it must **not** echo the COS `Ready` message. Exact wording is
  the builder's call, following the repo's condition-message style. The reason
  strings and True/False mapping are fixed above.
- **Reconcile result / requeue for the new branches.** Match the existing
  behavior of the branch each new case sits near (hard-stall `False` branches
  return without a bespoke timed requeue and rely on the COS watch, as the
  completed/invalid branches do today; `PreconditionsNotMet` sits with the
  still-retrying states and keeps whatever requeue the deadline path already
  computes). Note the controller's `deadlineAwareRateLimiter` may still inject a
  deadline-based requeue on an active head revision even when a branch returns a
  zero `ctrl.Result{}`; this is harmless (idempotent re-report) since the COD also
  `Watches` owned COSes.
- **Test structure.** Extend the existing tabular tests in
  `clusterobjectdeployment_controller_test.go` and
  `clusterobjectdeployment_deadline_test.go` rather than adding standalone tests
  (per the per-PR consistency preference). Cover: each new reason; the
  hard-stall-beats-deadline precedence **via `ObjectCollision`**;
  `PreconditionsNotMet` staying `True` under the deadline and flipping to
  `ProgressDeadlineExceeded` past it; and the unrecognized/missing-reason
  fallthrough. **Also flip the existing `invalid specification` subtest** in
  `clusterobjectdeployment_deadline_test.go` (currently asserts
  `ProgressDeadlineExceeded` for a past-deadline COS with
  `Ready=False/InvalidSpecification`): under the new precedence it must now assert
  `RevisionInvalidSpecification`.
## Documentation to update

These are **required** parts of the change, not optional follow-ups.

- **`conditions` field docstring** in `api/v1/clusterobjectdeployment_types.go`
  (`ClusterObjectDeploymentStatus.Conditions`, currently lines ~142–157). It
  enumerates the `Progressing` reasons prose-style; extend it to describe
  `RevisionPreconditionsNotMet` (True), `RevisionObjectCollision` (False),
  `RevisionInvalidSpecification` (False), and that these reflect the head active
  revision's own `Ready` state. Keep the existing distinction between the COD's
  own `InvalidSpecification` (template metadata) and the revision's
  `RevisionInvalidSpecification`. This docstring **is** the CRD schema field
  description, so it must be regenerated (see Constraints).
- **Regenerated artifacts:** run `make manifests crd-ref-docs` so the standard
  and experimental CRD YAMLs and `docs/api-reference/olmv1-api-reference.md`
  pick up the revised description. Commit the regenerated files.
- **Concept docs:** review `docs/draft/concepts/clusterobjectsets.md` (and any
  COD-facing concept/how-to page) for a place that explains COD rollout status /
  `Progressing` reasons, and add the new impeded-rollout reasons with their
  "what to do" guidance. If no such section exists, add a short one — the whole
  point of this change is user-facing discoverability.

## e2e coverage (required)

Add e2e coverage for the new reasons, following the existing COD feature-file
patterns (`test/e2e/features/cod_rollout.feature`,
`cod_progression_timeout.feature`, `cod_template_metadata.feature`). The builder
may extend an existing feature or add a new `cod_progressing_reasons.feature`;
whichever reads more consistently with the suite.

Scenarios to cover, each asserting the COD `Progressing` condition's
`status` + `reason`:

- **`RevisionObjectCollision` (False):** pre-create an object that the head
  revision's phase will collide with (owned by another controller / already
  existing), roll out a COD whose template produces that object, assert
  `Progressing=False / RevisionObjectCollision`. (See the collision/adoption
  facts already established for COS e2e — two independent bare owners collide.)
- **`RevisionInvalidSpecification` (False):** **unit test only** — this COS state
  is not reliably reachable from an admission-passing COD template (see the
  reachability note above), so do **not** add a deterministic e2e for it.
- **`RevisionPreconditionsNotMet` (True):** drive the head revision into
  `PreconditionsNotMet` (e.g. a referenced-content precondition that is not yet
  satisfied, mirroring the COS `revision.feature` referenced-content scenario),
  assert `Progressing=True / RevisionPreconditionsNotMet`, and ideally that it
  recovers to progressing/complete once the precondition is satisfied.
- **Hard-stall-beats-deadline precedence:** an `ObjectCollision` stall reports
  `RevisionObjectCollision` rather than `ProgressDeadlineExceeded` even after the
  deadline would have elapsed. (A unit test must lock this too, and is the
  authoritative check; the e2e is belt-and-suspenders. `ObjectCollision` is used
  here because `InvalidSpecification` is not reliably reachable in e2e.)

Run the standard e2e as experimental-gated per the COD e2e workflow in AGENTS.md
and the e2e local validation notes. If any of these states is impractical to
reach deterministically in e2e, note it in the PR and rely on the unit test for
that case rather than a flaky scenario.

## Constraints

- The reason **constants** themselves are plain string consts in
  `api/v1/clusterobjectdeployment_types.go` and are not part of the CRD schema.
  **However**, the `conditions` field docstring that enumerates the reasons *is*
  the CRD schema field description, and this change updates it — so
  `make manifests crd-ref-docs` **is** required, and the regenerated CRD YAMLs
  and `docs/api-reference/olmv1-api-reference.md` must be committed. No DeepCopy
  change is expected (no new Go fields). Run `make lint` and `make test-unit`
  before opening the PR, and `make verify` to confirm generated artifacts are in
  sync.
- Experimental API surface (COD lives behind the experimental feature gate), so
  no compatibility guarantee is owed on these reason strings — but they should
  still be chosen to last.
- This is COD-controller-side only. No COS controller, migrator, or CRD changes.
