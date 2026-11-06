# Design: ClusterObjectDeployment template metadata propagation

**Date:** 2026-10-23
**Status:** Draft (pending approval)
**Branch (to be created):** `cod-template-metadata` (off `main`)

## Intent and why

We want a generic, OLM-agnostic way for a `ClusterObjectDeployment` (COD) to
carry labels and annotations down to every `ClusterObjectSet` (COS) revision it
creates — the Kubernetes `Deployment.spec.template.metadata` → Pod pattern,
matching orb-operator's `ClusterObjectDeploymentTemplateMetadata`.

**Why now / the motivating problem:** on the `operator-controller-use-cod`
branch, the COD controller hardcodes a list of OLM-specific annotation keys
(`bundleAnnotationKeys`: `package-name`, `bundle-*`, `olm.properties`) and copies
them from the COD's top-level `ObjectMeta` onto each COS, so the ClusterExtension
status reader (which reads bundle metadata off each COS) keeps working. That
hardcoded list is an OLM concept leaking into a controller that is meant to be
generic.

**Scope decision (set by the human):** build the *generic capability* on `main`
as a self-contained change. Leave `operator-controller-use-cod` untouched. That
branch will later adopt this capability — replacing `bundleAnnotationKeys` with
populating `template.metadata` — as follow-up work on that branch, not here.

This keeps the main change pure infrastructure with **zero OLM knowledge**, which
is exactly the boundary we want.

## Goals

- Add a generic `spec.template.metadata` (labels + annotations) to the COD API.
- Propagate those labels/annotations verbatim onto each COS revision the COD
  controller creates, with the controller-managed `template-hash` label taking
  precedence.
- Keep the change additive and OLM-agnostic: no knowledge of bundles anywhere in
  the COD controller.
- Match orb-operator semantics so the implementations stay convergent.

## Non-goals

- Removing `bundleAnnotationKeys` or changing the operator-controller applier —
  those live on `operator-controller-use-cod` and are **follow-ups on that
  branch**, not part of this PR.
- Changing where/how the CE reads revision state.
- Adding per-revision history to COD status.
- Any change to COS behavior, phases, or rollout logic.

## Anti-goals

- Do **not** introduce any OLM concept into the COD controller.
- Do **not** let callers override controller-managed revision machinery (the
  `template-hash` label) via `template.metadata`.

## Constraints

- `api/v1/*_types.go` changes follow OpenShift + Kubernetes API conventions
  (AGENTS.md): value types with `omitzero`/`omitempty`, `+optional`, doc comments
  using lowercase JSON field names. Run `make generate manifests crd-ref-docs`
  and `make lint-api-diff`.
- The COD API is experimental; incompatible changes are allowed.
- `olm.operatorframework.io/*` prefixes stay in the OLM domain (AGENTS.md). The
  `template-hash` label keeps that prefix — it is COD machinery, not a bundle
  concept — and remains controller-managed.
- Must build/pass on macOS and Linux.

## Design

### 1. API change (`api/v1/clusterobjectdeployment_types.go`)

Add a `Metadata` field to `ClusterObjectDeploymentTemplate` and a new type:

```go
// ClusterObjectDeploymentTemplate describes the caller-controlled part of a revision.
type ClusterObjectDeploymentTemplate struct {
    // metadata contains labels and annotations propagated verbatim to each
    // ClusterObjectSet created from this template. Changes to metadata change
    // template identity and therefore create a new revision.
    // +optional
    Metadata ClusterObjectDeploymentTemplateMetadata `json:"metadata,omitzero"`

    // spec contains the desired revision content. Group, revision, lifecycle state,
    // and ownership are assigned by the controller and cannot be set here.
    // +required
    Spec ClusterObjectDeploymentTemplateSpec `json:"spec,omitzero"`
}

// ClusterObjectDeploymentTemplateMetadata contains labels and annotations
// propagated to each ClusterObjectSet revision created from the template.
type ClusterObjectDeploymentTemplateMetadata struct {
    // labels is a set of key/value pairs propagated to each revision's metadata.
    // +optional
    // +kubebuilder:validation:MaxProperties=32
    Labels map[string]string `json:"labels,omitempty"`

    // annotations is a set of key/value pairs propagated to each revision's
    // metadata. Each value must be 256 KiB or less.
    // +optional
    // +kubebuilder:validation:MaxProperties=32
    // +kubebuilder:validation:XValidation:rule="self.all(k, size(self[k]) <= 262144)",message="annotation values must be 256 KiB or less"
    Annotations map[string]string `json:"annotations,omitempty"`
}
```

Regeneration: `make generate manifests crd-ref-docs` updates
`zz_generated.deepcopy.go`, applyconfigurations under `applyconfigurations/api/v1/`,
the standard/experimental CRDs, the manifests, and the API reference docs.
Validate with `make lint-api-diff`.

`TemplateHash` already hashes the whole `ClusterObjectDeploymentTemplate` via
`hashutil.DeepHashObject`, which is **JSON-based** (`json.Encoder` → sha224).
Adding `Metadata` folds it into template identity automatically: a change to
metadata (once set) produces a different hash and cuts a new revision. No hashing
code changes. Because the field is `omitzero`, a COD that sets **no** metadata
serializes exactly as before, so existing CODs keep the same hash (see
Compatibility).

### 2. COD controller (`internal/object-controller/controllers/clusterobjectdeployment_controller.go`)

`buildClusterObjectSet` on `main` today sets only the `template-hash` label and no
annotations. Extend it to propagate `cod.Spec.Template.Metadata`:

- Labels: clone `Metadata.Labels`, then set `labels.TemplateHashKey = hash`
  **last** so the controller-managed hash label always wins over a caller-supplied
  value of the same key.
- Annotations: clone `Metadata.Annotations` verbatim (nil → no annotations).

Sketch:

```go
revLabels := maps.Clone(cod.Spec.Template.Metadata.Labels)
if revLabels == nil {
    revLabels = map[string]string{}
}
revLabels[labels.TemplateHashKey] = hash

return &ocv1.ClusterObjectSet{
    ObjectMeta: metav1.ObjectMeta{
        Name:        fmt.Sprintf("%s-%d", cod.Name, revision),
        Labels:      revLabels,
        Annotations: maps.Clone(cod.Spec.Template.Metadata.Annotations),
    },
    Spec: ...unchanged...,
}
```

### Propagation / merge semantics (decided)

- Controller-managed keys win. Only `template-hash` is controller-managed; it is
  applied after cloning `Metadata.Labels`, overwriting any caller value.
- No validation rejecting caller use of reserved keys — the controller overwrites.
  Keeps the controller simple and generic.
- Nil maps tolerated.

## Compatibility

- Experimental API; no migration guarantees.
- `metadata` is `omitzero` and `DeepHashObject` is JSON-based, so a COD that sets
  no metadata hashes identically to before this change. **No re-rollout churn** for
  existing CODs on `main`.
- The upgrade re-rollout churn discussed during design (existing CODs getting one
  content-identical new revision because annotations move *into* the hashed
  template) only arises when `template.metadata` is actually populated — i.e. on
  `operator-controller-use-cod` when operator-controller starts writing bundle
  annotations there. That is handled on that branch, out of scope here.

## Testing

- COD controller unit tests (follow existing table-test conventions):
  - `template.metadata.labels`/`annotations` are propagated verbatim onto created
    COSes.
  - a caller-supplied `template-hash` label is overridden by the controller.
  - a metadata-only template change produces a different hash and cuts a new
    revision; empty metadata leaves the hash unchanged (no churn).
- `api/v1/validation_test.go`: add cases for MaxProperties / the annotation-value
  size rule if warranted.
- Full `./internal/object-controller/...` suite; `make verify`, `make lint`,
  `make test-unit`.
- **e2e (experimental suite — COD is experimental):** a focused scenario that
  creates a COD with `spec.template.metadata` labels + annotations and asserts the
  created COS revision carries them, plus that the controller-managed
  `template-hash` label is present (not clobbered). This exercises what unit tests
  cannot: the CRD schema accepting the new field through a real apiserver
  (including the MaxProperties / 256 KiB validation) and propagation end-to-end.
  Reuse the existing steps `ClusterObjectSet "<name>" has label "<key>" with
  value "<value>"` (`ClusterObjectSetHasLabelWithValue`) and `ClusterObjectSet
  "<name>" contains annotation "<key>" with value`
  (`ClusterObjectSetHasAnnotationWithValue`) — no new step defs required. Natural
  home: `test/e2e/features/cod_rollout.feature` or a small dedicated
  `cod_template_metadata.feature`. Run via `make test-experimental-e2e`.

## Decisions made (my calls, flagged for review)

- **Scope is additive and main-based.** Removal of `bundleAnnotationKeys` and the
  operator-controller applier change are explicitly deferred to the
  `operator-controller-use-cod` branch.
- **Metadata participates in the hash** (metadata-only change → new revision),
  matching orb and K8s; `omitzero` means existing metadata-less CODs are
  unaffected.
- **Field shape** mirrors orb: `Metadata{Labels, Annotations}`, no `name`.
- **`MaxProperties=32`** on both maps and the **256 KiB per-annotation-value**
  CEL bound, both matching orb.
- **`template-hash` stays controller-managed and overrides caller values.**

## Follow-up (on `operator-controller-use-cod`, not this PR)

Once this capability is on `main` and the branch rebases onto it:

1. Delete `bundleAnnotationKeys` and the top-level annotation copy from the COD
   controller.
2. Change the operator-controller applier
   (`internal/operator-controller/applier/clusterobjectdeployment.go`) to write
   the bundle annotations into `spec.template.metadata.annotations` instead of the
   COD top-level `ObjectMeta`.
3. Accept (or explicitly handle) the one-time content-identical re-rollout per
   installed ClusterExtension that results from the annotations moving into the
   hashed template.

## What's left to the builder

- Exact kubebuilder marker tuning to satisfy `make lint-api-diff` (incl. the CEL
  rule syntax).
- `maps.Clone` vs. an existing helper.
- Test-table structure, following existing COD controller test conventions.
