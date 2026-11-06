# Design: ClusterObjectDeployment / ClusterObjectSet referenced-secret ownership

**Date:** 2026-10-24
**Status:** Draft (pending approval)
**Branch:** `cod-cos-secret-ownership` (off `main`)

## Intent and why

`ClusterObjectSet` (COS) phases can reference serialized object manifests stored
in Secrets (`ObjectSourceRef{name, namespace, key}`), and OLM packs rendered
bundle content into immutable, content-addressed Secrets of type
`olm.operatorframework.io/object-data`. We need a clear, self-contained rule for
**who owns those Secrets and when they get deleted**, so that content Secrets do
not leak across upgrades, revision churn, or deletion — while keeping the
`ClusterObjectDeployment` (COD) and COS usable on their own, outside any
ClusterExtension (CE).

**Why now / the motivating problem:** on the `operator-controller-use-cod`
branch, ownership of packed Secrets was moved onto the **ClusterExtension**, which
is long-lived and never drops ownership. A code review found that this leaks
Secrets: once content changes across an upgrade, the old content's Secrets are
never referenced by a live revision again and are never deleted until the CE
itself is removed. We want an ownership model whose lifetime is scoped to the
things that actually consume the content (the COS revisions), not to the CE.

**Scope decision (set by the human):** this design covers **only the
object-controller side — COD and COS**. The CE / operator-controller side (who
*creates* the Secrets and how it bridges the pre-COS window) is deferred and
captured here only as a contract the creator must honor.

## Goals

- A referenced Secret's lifecycle **follows the COS(es) that reference it**: it is
  deleted once no live COS references it.
- No leaks across revision churn, COD deletion, or COS pruning.
- COD and COS are usable **standalone** (outside a CE); cleanup must not depend on
  any CE-side controller.
- Handle **adoption**: a COS created before the COD that later manages it (same
  `spec.group`), with a matching or a new template.
- Claim (adopt) **every referenced Secret**, regardless of type, so its deletion
  follows the COS. The `object-data`-only restriction is deferred to future work.

## Non-goals

- Who **creates** the packed Secrets, and closing the window before any COS
  exists. That is the creator's responsibility (CE / operator-controller),
  deferred. See [The creator contract](#the-creator-contract-deferred-ce-side).
- Changing the packing format, content-addressed naming, or immutability of the
  Secrets.
- Changing how the COS validates or reports on referenced content (the existing
  `Ready=False/PreconditionsNotMet` behavior stays as-is).
- CE-side ownership of Secrets (being removed; out of scope here).

## Anti-goals (what would make this a failure)

- Adopting or deleting a Secret the COS does **not** reference. (Deleting a Secret
  the COS *does* reference is expected — that is the GC semantics; narrowing *which*
  Secrets may be referenced is deferred to future work.)
- A design that only avoids leaks while a CE exists (i.e. not standalone-safe).
- Reintroducing a long-lived owner (CE or COD) that must actively *remove* its
  ownerRefs to avoid leaking — fragile bookkeeping we explicitly reject.
- Premature deletion of a Secret a live (or just-created / just-adopted) COS still
  needs.

## Core principle: own, not manage

The object-controller (COD/COS) **owns** the Secrets a COS references strictly as a
**garbage-collection anchor** — so a Secret's *deletion* follows the COS(es) that
reference it — and does **not manage** them in any other sense.

- **Own** = add a single, **non-controller**, additive, idempotent ownerRef. The
  COS becomes a thing whose disappearance reclaims the Secret. That one metadata
  write is the entire extent of what the object-controller does to a Secret.
- **Not manage** = it is **not the controller** of the Secret; it does **not
  watch** it, does **not mutate or repair** its content (they are immutable; it
  only reads them via `resolveObjectRef`), and takes **no responsibility for its
  existence or validity**. A missing/invalid Secret is *reported* by the COS
  (`Ready=False/PreconditionsNotMet`), never created or fixed by it.

**`Controller=false` is a direct consequence of this principle**, not an
incidental choice: the controller flag confers management/stewardship semantics
(single controller-of-record, `.Owns()` watch wiring, adoption/collision
arbitration) that we explicitly do not want — and that would forbid the
multi-owner model (shared content referenced by several COSes). We want only the
GC cascade, which works for non-controller owners.

Everything the object-controller deliberately omits — a sweeper, a finalizer, COD
Secret ownership, declarative ownerRef removal — would be *managing*. The
responsibility to **manage** a Secret (create it, bridge the pre-COS window,
steward its content) stays with its **creator** (see the creator contract).

## Key facts this design relies on

1. **Kubernetes GC deletes a dependent only when *all* its owners are gone.** As
   each owner is deleted, GC strips that ownerRef; when the last referenced owner
   is gone, the dependent is deleted. Multiple owners therefore means "deleted
   when the last one goes."
2. **Manually removing an ownerRef does *not* delete the dependent.** A dependent
   left with zero ownerRefs is a normal orphan and is never GC'd. Deletion only
   happens via owner *deletion* (cascade) or an explicit `Delete`. (This is why
   an owner cannot "release" a Secret by un-referencing it — it must either be
   deleted itself or the Secret must be explicitly deleted.)
3. **At most one `Controller=true` ownerRef** is allowed per object. A Secret may
   have several non-controller owners.
4. **OwnerRefs on an immutable Secret are allowed.** Immutability locks `data`,
   not metadata; `main` already patches ownerRefs onto immutable Secrets.
5. **Cross-scope ownerRefs are valid** for a namespaced dependent owned by a
   cluster-scoped owner (COS/COD are cluster-scoped; Secrets are namespaced).
6. **Secret metadata is outside both identity hashes.** The COD template hash is
   `DeepHashObject(spec.template)` — it covers the refs (name/namespace/key), not
   the Secret object. The content-addressed Secret name hashes `data` only. So
   ownerRef changes never churn a revision or rename a Secret. (Content changes
   *do* flow into identity, indirectly, via the content-addressed name in the
   ref.)

## The model: COS-only ownership

**Each COS owns the Secrets its phases reference, via a non-controller ownerRef,
regardless of type.** That is the entire object-controller mechanism. It is
sufficient because:

- **Revision churn:** a new template produces a new COS that owns the new
  Secrets; the old COS is archived and pruned by the COD, and GC cascades its
  Secrets. ✔
- **COD deletion:** the COD controller-owns its COSes, so deleting the COD
  cascades to the COSes, whose Secrets then cascade. ✔
- **Standalone COS:** owns its referenced Secrets and cascades them on its own
  deletion — no COD required. ✔
- **Shared content:** two revisions with identical content reference the same
  content-addressed Secret; each COS adds a non-controller ownerRef; the Secret
  survives until the last referencing COS is gone. ✔ (This is why ownerRefs must
  be non-controller.)

Everything that was ever referenced by a COS is reclaimed by COS ownership.
Everything that was *never* referenced by a COS is, by definition, outside the
object-controller's knowledge and is the creator's responsibility (below).

### What the COD does (nothing new for Secrets)

The COD does **not** own Secrets, does **not** run a sweeper, and needs **no
finalizer** for Secrets. Its existing responsibilities are unchanged: it owns its
COSes and prunes archived revisions, and pruning cascades Secret cleanup through
COS ownership.

The COD also does **not** gain a `PreconditionsNotMet` check for referenced
Secrets. A missing/invalid referenced Secret does not block COS *creation*; the
COS reports `Ready=False/PreconditionsNotMet` at resolve time and recovers when
the Secret appears. The COD keeps its existing `validateTemplateMetadata` check,
which guards a *different* failure mode (invalid template metadata makes the COS
`Create` itself fail at the API server). Optionally, the COD may propagate the
latest revision's `Ready` reason/message onto its own status so a stuck
precondition is legible — surfacing, not re-validating.

### Any referenced Secret is adopted and garbage-collected

The ref contract stays as it is today — a COS phase may reference a Secret of **any
type** (the existing immutability precondition still applies). This design does not
tighten that; see [Future work](#future-work).

What it adds is uniform GC semantics: **every referenced Secret is adopted (owned)
by the referencing COS and garbage-collected when that COS is deleted**, regardless
of type. The claim is *not* type-gated.

**Known consequence:** because any referenced Secret is adopted, referencing a
Secret hands its lifecycle to the COS — including a user-supplied Secret a bare COS
points at. That is accepted for now. The future `object-data`-only restriction
(below) is what will eventually prevent a COS from referencing (and thus adopting)
a Secret that was never meant to be OLM-managed. Until then, "referencing a Secret
means the COS owns and will delete it" is the documented contract.

## Future work

- **Restrict referenceable Secret types to `object-data`.** Today a COS phase may
  reference a Secret of any type, and this design adopts+GCs whatever is referenced.
  A later change should make a referenced Secret that is not
  `olm.operatorframework.io/object-data` a precondition failure
  (`Ready=False/PreconditionsNotMet`), so a COS can only reference (and therefore
  adopt/delete) Secrets that were meant to be OLM-managed content. This removes the
  "referencing a user's ordinary Secret deletes it" footgun by construction. It is a
  tightening of the (experimental) ref contract and is deferred so this change can
  land the GC semantics first.
- The CE-side creator contract (below) and the applier `Controller=true` →
  non-controller-bridge conversion.

## The creator contract (deferred, CE side)

> A referenced Secret's lifecycle transfers to a COS once that COS
> adds its ownerRef. **Until a COS owns it, the Secret's creator is responsible
> for it.** A controller that uses COD/COS to manage cluster content should add a
> **bridging ownerRef** to the Secret at creation time and **remove it once it
> observes that a COS has taken ownership.**

This closes the two windows the object-controller cannot see:

- **Creation window:** Secret created before any COS exists → the creator's
  bridge owns it, so it cascades if the creator is deleted.
- **Claim gap:** a COS exists and references the Secret but has not yet patched
  its ownerRef → the creator holds the bridge until it confirms the COS ownerRef
  is present, then drops it.

Because the bridge is **transient** (dropped once a COS owns the Secret), it does
not reintroduce the "long-lived owner never releases → leak across upgrades"
problem. The t=1/t=2 orphan (template referenced a Secret at t=1, the COS was
never created, template moved to t=2) is, by this contract, the creator's cleanup
— its bridge cascades on creator deletion, or the creator reclaims it directly.

This contract is documented here but **implemented later**, on the CE /
operator-controller side.

## Interaction with the existing applier ownerRef patching (main)

On `main` the operator-controller applier **already** owns referenced Secrets: at
COS creation it patches an ownerRef with `Controller=true`
(`patchSecretOwnerReferences` / `ensureSecretOwnerReferences` /
`patchOwnerRefsOnSecrets` in `internal/operator-controller/applier/boxcutter.go`).
Secret names are **per-revision** there (`<cosName>-<hash>`), so each Secret has
exactly one owner and `Controller=true` is correct and non-conflicting today.

**This PR does not modify the applier** (it is CE / operator-controller side,
explicitly deferred). The two mechanisms coexist cleanly:

- For an **operator-controller-created COS**, the applier patches the ownerRef at
  creation (before the COS controller first reconciles). The COS controller's
  claim dedups by owner UID, sees the ref already present, and is a no-op. The
  flag stays `Controller=true`, which is fine on `main` (single owner per
  per-revision Secret).
- For a **standalone COD/COS**, no applier runs, so the COS controller's claim is
  the sole and authoritative mechanism — adding a `Controller=false` ownerRef.

**Consequence (intended):** the non-controller / shared-content multi-owner model
only *fully* takes effect once the deferred CE-side rework converts the applier's
patching to the transient non-controller bridge (and stops setting
`Controller=true`). Until then the headline multi-owner behavior is exercised and
tested via the **standalone** path; the operator-controller path keeps its current
`main` behavior unchanged. This is deliberate, and matches the "CE later" scope.

**No orphan-window regression:** the operator-controller path keeps at-creation
applier ownership; the standalone path's pre-COS window is the creator's
responsibility by contract (for a bare COS, the creator is the user).

## Adoption

Adoption needs no special handling because ownership rides on the COS,
independent of creation order:

- **COS before COD, matching template:** the COS already owns its Secrets; the
  COD adopts the COS as its active revision; nothing to reconcile for Secrets.
- **COS before COD, new template same group:** the COD creates a new revision
  (which owns the new Secrets) and the pre-existing COS becomes an archived
  revision (still owning its Secrets until pruned, then cascaded).

There is no group-label discovery, no sweeper race, and no grace window to tune.

## What the object-controller implements

1. **COS controller — claim referenced Secrets.** For each referenced Secret in the
   COS's own `spec` phases, `get` the Secret and ensure a **non-controller** ownerRef
   pointing at the COS — **regardless of the Secret's type**. Make it idempotent and
   crash-recoverable (same intent as `main`'s `ensureSecretOwnerReferences`): a COS
   that failed to patch on a prior reconcile re-attempts on the next, deduping by
   owner UID.
   - **Discovery is ref-driven, not label-driven.** Do **not** reuse `main`'s
     label-list discovery (`List(InNamespace(SystemNamespace),
     MatchingLabels{RevisionNameKey: cos.Name})`): a standalone COS may reference
     Secrets in arbitrary namespaces that carry no revision-name label, so ownership
     must be driven off the COS's own refs. (Builder-check Q3.)
   - **Claim on existence, independent of content validity.** A referenced Secret
     that exists but fails a *content* precondition (mutable, malformed, changed) is
     still claimed, so it is still reclaimed when the COS is deleted. Wire the claim
     so it is **not** gated behind the content short-circuits — ownership tracks
     lifecycle, not content health; the preconditions are reported separately.
     (Builder-check Q4.)
   - **No type gate.** This design does not restrict which Secret types may be
     referenced (see Future work), so the claim owns every referenced Secret.
2. **COS controller — precondition reporting unchanged.** Keep the existing
   `Ready=False/PreconditionsNotMet` behavior for missing / mutable / changed /
   malformed referenced content.
3. **COD controller — no Secret logic.** Confirm pruning an archived COS cascades
   its Secrets (it does, via COS ownership). Keep `validateTemplateMetadata`.
   Optionally propagate the latest revision's `Ready` reason/message onto the COD
   condition.
4. **RBAC.** The object-controller needs Secret `get`/`list`/`update` in the
   namespaces it resolves refs from. It does **not** need Secret `delete`
   (cascade handles deletion).
5. **Docs.** Record the creator contract where COD/COS behavior is documented.

## Testing approach

- **Unit (COS claim):** ensures a non-controller ownerRef is added to each
  referenced Secret regardless of type; is idempotent; additive-only + UID-dedup
  (no-op when an ownerRef with the COS's UID already exists); re-adds after a
  simulated prior failure; claims on existence independent of content validity;
  skips a not-yet-created Secret; claims a referenced Secret once even when multiple
  objects reference it.
- **Unit (COD):** pruning an archived revision does not require any Secret-level
  action; `validateTemplateMetadata` unchanged.
- **envtest (ownerRef *write*, not cascade):** envtest runs only kube-apiserver
  + etcd — **not** kube-controller-manager — so the garbage collector does **not**
  run there and cascade deletion cannot be asserted in envtest. Use envtest only to
  validate the *write* against a real API server:
  - The claim patch is accepted on an **immutable** Secret (confirms metadata is
    patchable despite immutability) and is idempotent across a real Get/Patch
    roundtrip.
  - Coexistence: a Secret already carrying an applier-style `Controller=true`
    ownerRef is not double-ref'd by the claim (UID-dedup no-op).
- **GC cascade is an e2e concern** (see below) because only a real cluster runs the
  garbage collector.

Adoption needs no dedicated secret test: with COS-only ownership and **no sweeper**,
nothing deletes Secrets except k8s GC on owner deletion, so there is no
premature-deletion path for adoption to trigger. Ownership simply rides on the COS.

### E2E (standalone COD/COS)

These are the end-to-end, user-observable scenarios for this PR. They live in the
**experimental** suites (`@BoxcutterRuntime`) alongside the existing
`revision.feature` / `cos_*.feature` / `cod_*.feature`, which already create and
reference `object-data` Secrets. They exercise the **standalone** path — a COD/COS
driven directly, with no ClusterExtension/applier involved — which is where this
PR's new behavior is actually observable (the operator-controller path is
unchanged; see "Interaction with the existing applier").

1. **Standalone COS cleanup.** Create an `object-data` Secret and a COS that
   references it; the COS becomes Ready. Confirm the Secret carries a
   (non-controller) ownerRef to the COS. Delete the COS → the referenced
   `object-data` Secret is eventually removed (cascade).
2. **Standalone COD cleanup.** Create a COD whose template references
   `object-data` Secret content; it rolls out a revision (COS). Delete the COD →
   its COS revision(s) **and** their `object-data` Secrets are eventually removed.
3. **Type-agnostic adoption + GC.** Create a COS that references an **immutable,
   non-`object-data`** (e.g. Opaque) Secret holding a manifest; the COS becomes
   Ready and the Secret gains a non-controller ownerRef to the COS. Delete the COS →
   the Secret is eventually removed. This pins the "any referenced Secret is adopted
   and garbage-collected" contract (and documents that referencing a Secret hands
   its lifecycle to the COS, pending the future type restriction).

**Regression guard (existing, not new).** `uninstall.feature`'s "Removing
ClusterExtension cascades to grouped revisions and their content Secrets" covers
the CE → COS → Secret cascade. This PR is additive (applier untouched), so that
scenario must continue to pass; name it as a guard rather than modifying it.

**Step reuse:** the suites already have steps for applying a COS/COD, waiting on
Ready, creating `object-data` Secrets, and asserting a resource "is installed" /
"is eventually not found". Scenarios 1–3 should compose from those plus (if not
present) a step asserting a Secret's ownerRef points at a given COS and a step
asserting a Secret still exists after a deletion.

## What's left to the builder

- The precise call site in the COS controller for the claim step, and whether to
  reuse/relocate the existing `main` ownerRef helpers (`patchOwnerRefsOnSecrets`,
  `ensureSecretOwnerReferences`) from the CE-side applier into an
  object-controller-owned helper. (Today those live in
  `internal/operator-controller/applier/boxcutter.go` and set `Controller=true`;
  the object-controller version must set `Controller=false`.)
- Whether to resolve a ref's default namespace (empty → OLM system namespace)
  before claiming, matching how `resolveObjectRef` resolves it.
- Whether, and exactly how, to propagate the latest revision's `Ready`
  reason/message onto the COD condition (the optional legibility improvement).
- Exact RBAC marker changes and manifest regeneration.
