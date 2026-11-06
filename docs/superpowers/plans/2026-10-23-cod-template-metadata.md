# ClusterObjectDeployment Template Metadata Propagation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the ClusterObjectDeployment template a generic `metadata` (labels + annotations) that the COD controller propagates verbatim onto every ClusterObjectSet revision it creates.

**Architecture:** Add `spec.template.metadata` to the COD API (orb-operator shape), then extend `buildClusterObjectSet` to clone those labels/annotations onto each COS, stamping the controller-managed `template-hash` label last so it always wins. `TemplateHash` already hashes the whole template (JSON-based `DeepHashObject`), so metadata folds into revision identity automatically; `omitzero` keeps metadata-less CODs unchanged.

**Tech Stack:** Go, controller-runtime, kubebuilder validation markers, godog (e2e).

**Spec:** `docs/superpowers/specs/2026-10-23-cod-template-metadata-propagation-design.md`

## Global Constraints

- API changes follow OpenShift + Kubernetes conventions (AGENTS.md): value types, `omitzero` for struct JSON tags / `omitempty` for maps, `+optional`, doc comments use lowercase JSON field names.
- After API type changes run `make generate manifests crd-ref-docs`; validate with `make lint-api-diff`. Commit generated files.
- `olm.operatorframework.io/*` prefixes stay in the OLM domain; `template-hash` label stays controller-managed.
- Scope is additive and OLM-agnostic: no bundle/OLM concepts in the COD controller. Do NOT touch `bundleAnnotationKeys` or the operator-controller applier (they live on `operator-controller-use-cod`, not this branch).
- `MaxProperties=32` on both maps; annotation values ≤ 256 KiB (262144 bytes).
- Must build on macOS and Linux.

## Review Focus

- **Caller supplies a `template-hash` label in `template.metadata.labels`** — the controller must override it with the computed hash, never let the caller's value win (covered: Task 2 override test).
- **Nil / empty `template.metadata`** — created COS has only the `template-hash` label and no annotations; existing metadata-less CODs keep the same template hash (no re-rollout) (covered: Task 2 empty-metadata + hash-stability tests).
- **Metadata-only template change** — must produce a different template hash so a new revision is cut (covered: Task 2 hash-differs test).
- **More than 32 labels or annotations** — apiserver rejects via CRD `MaxProperties` (covered: Task 1 validation test).
- **Annotation value larger than 256 KiB** — apiserver rejects via CRD CEL rule (covered: Task 1 validation test).

---

### Task 1: COD API — `template.metadata` field + CRD validation

**Files:**
- Modify: `api/v1/clusterobjectdeployment_types.go` (add field to `ClusterObjectDeploymentTemplate`, add `ClusterObjectDeploymentTemplateMetadata` type)
- Modify (generated, via make): `api/v1/zz_generated.deepcopy.go`, `applyconfigurations/api/v1/*`, `helm/olmv1/base/object-controller/crd/**/*clusterobjectdeployments.yaml`, `manifests/*.yaml`, `docs/api-reference/olmv1-api-reference.md`
- Test: `api/v1/validation_test.go`

**Interfaces:**
- Produces: `ClusterObjectDeploymentTemplate.Metadata ClusterObjectDeploymentTemplateMetadata` (json `metadata`, omitzero); `ClusterObjectDeploymentTemplateMetadata{ Labels map[string]string; Annotations map[string]string }` (json `labels`/`annotations`, omitempty).

- [ ] **Step 1: Add the type and field**

In `api/v1/clusterobjectdeployment_types.go`, add to `ClusterObjectDeploymentTemplate`:
```go
// metadata contains labels and annotations propagated verbatim to each
// ClusterObjectSet created from this template. Changes to metadata change
// template identity and therefore create a new revision.
// +optional
Metadata ClusterObjectDeploymentTemplateMetadata `json:"metadata,omitzero"`
```
and add the new type:
```go
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

- [ ] **Step 2: Regenerate**

Run: `make generate manifests crd-ref-docs`
Expected: `zz_generated.deepcopy.go`, applyconfigurations, CRDs, manifests, and API ref docs update with the new field; no errors.

- [ ] **Step 3: Validate API conventions**

Run: `make lint-api-diff`
Expected: PASS (no kube-api-linter violations on the new type/field).

- [ ] **Step 4: Write failing CRD validation tests**

In `api/v1/validation_test.go` `TestValidate`, add cases (following the existing `ClusterObjectDeploymentSpec` case shape at the `defaultDeploymentSpec` / `expectError` pattern):
- valid: `Template.Metadata` with a couple of labels and a small annotation → no error.
- invalid: `Template.Metadata.Labels` with 33 entries → error.
- invalid: `Template.Metadata.Annotations` with one value > 262144 bytes → error (match on the CEL message substring `256 KiB`).

- [ ] **Step 5: Run validation tests**

Run: `go test -mod=mod ./api/v1/... -run TestValidate -count=1`
Expected: PASS (requires envtest, as the existing suite does).

- [ ] **Step 6: Commit**

```bash
git add api/v1 applyconfigurations helm manifests docs/api-reference
git commit -s -m "✨ add template.metadata to ClusterObjectDeployment"
```

---

### Task 2: COD controller — propagate `template.metadata` to each revision

**Files:**
- Modify: `internal/object-controller/controllers/clusterobjectdeployment_controller.go` (`buildClusterObjectSet`)
- Test: `internal/object-controller/controllers/clusterobjectdeployment_controller_test.go`

**Interfaces:**
- Consumes: `ClusterObjectDeploymentTemplate.Metadata` (Task 1); existing `labels.TemplateHashKey`, `TemplateHash`, test helpers `codFixture()`, `newCODClient(t, objs...)`, `reconcileCOD(t, c)`, `getCOS(t, c, name)`, const `clusterObjectDeploymentName` (= `"test-cod-1"`; first revision COS name is `test-cod-1-1`).

- [ ] **Step 1: Write the failing tests**

Add to `clusterobjectdeployment_controller_test.go`:

`Test_ClusterObjectDeploymentReconciler_PropagatesTemplateMetadata` — build `codFixture()`, set
`cod.Spec.Template.Metadata = ocv1.ClusterObjectDeploymentTemplateMetadata{Labels: map[string]string{"team":"platform"}, Annotations: map[string]string{"note":"hello"}}`;
`newCODClient(t, cod)`, `reconcileCOD`, `getCOS(t, c, "test-cod-1-1")`; assert the COS has label `team=platform`, annotation `note=hello`, and a non-empty `labels.TemplateHashKey`.

`Test_ClusterObjectDeploymentReconciler_TemplateHashLabelOverridesCaller` — set
`cod.Spec.Template.Metadata.Labels = map[string]string{labels.TemplateHashKey: "caller-wins-NOT"}`; reconcile; assert the created COS's `labels.TemplateHashKey` equals `TemplateHash(cod.Spec.Template)`, not the caller value.

`Test_TemplateHash_MetadataParticipates` — two `ClusterObjectDeploymentTemplate` values identical except `Metadata.Labels`; assert `TemplateHash(a) != TemplateHash(b)`, and that a template with zero-value `Metadata` hashes equal to the same template built before metadata existed is covered implicitly by `omitzero` — assert `TemplateHash(tmplNoMeta) == TemplateHash(tmplEmptyMeta)` where `tmplEmptyMeta` sets `Metadata: ClusterObjectDeploymentTemplateMetadata{}`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -mod=mod ./internal/object-controller/controllers/... -run 'PropagatesTemplateMetadata|TemplateHashLabelOverridesCaller|TemplateHash_MetadataParticipates' -count=1`
Expected: FAIL (COS lacks propagated labels/annotations; override not applied).

- [ ] **Step 3: Implement propagation in `buildClusterObjectSet`**

Clone `cod.Spec.Template.Metadata.Labels` into the COS labels (init empty map if nil), then set `labels[labels.TemplateHashKey] = hash` last; set COS `Annotations` to `maps.Clone(cod.Spec.Template.Metadata.Annotations)`. Leave `Spec` and the name unchanged.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -mod=mod ./internal/object-controller/controllers/... -run 'PropagatesTemplateMetadata|TemplateHashLabelOverridesCaller|TemplateHash_MetadataParticipates' -count=1`
Expected: PASS

- [ ] **Step 5: Run the full package + fmt**

Run: `gofmt -l internal/object-controller/controllers/ && go test -mod=mod ./internal/object-controller/... -count=1`
Expected: no files listed by gofmt; all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/object-controller/controllers
git commit -s -m "✨ propagate ClusterObjectDeployment template metadata to revisions"
```

---

### Task 3: Experimental e2e — metadata reaches the COS through a real apiserver

**Files:**
- Create: `test/e2e/features/cod_template_metadata.feature`

**Interfaces:**
- Consumes: existing steps `ClusterObjectSet "<name>" has label "<key>" with value "<value>"` (`ClusterObjectSetHasLabelWithValue`) and `ClusterObjectSet "<name>" contains annotation "<key>" with value` (`ClusterObjectSetHasAnnotationWithValue`); existing COD-creation steps used by `cod_rollout.feature` (model the Given/When on that file).

- [ ] **Step 1: Write the scenario**

Create `test/e2e/features/cod_template_metadata.feature` with a scenario that applies a ClusterObjectDeployment whose `spec.template.metadata` sets a label (`team: platform`) and an annotation (`note: hello`), waits for the first revision COS to exist, then asserts (reusing the steps above) that the COS carries `team=platform`, the annotation `note=hello`, and has the `olm.operatorframework.io/template-hash` label. Follow the Background/Given/When/Then structure and COD manifest style already in `cod_rollout.feature`.

- [ ] **Step 2: Run the scenario against the experimental cluster**

Run (per AGENTS.md iterative e2e flow):
```bash
make experimental-e2e-setup
make "e2e/cod_template_metadata" KUBECONFIG=.kubeconfig/operator-controller-experimental-e2e.kubeconfig
```
Expected: scenario passes (the CRD accepts `template.metadata`; the COS carries the propagated label+annotation).

- [ ] **Step 3: Commit**

```bash
git add test/e2e/features/cod_template_metadata.feature
git commit -s -m "✨ add e2e for ClusterObjectDeployment template metadata propagation"
```

---

## Notes for the executor

- Every commit message ends with the DCO sign-off (`-s`) and the Claude attribution line per the session's attribution reminder.
- If `make lint-api-diff` flags the CEL rule or markers, adjust marker syntax (builder's call) but keep the 32-property and 256-KiB semantics.
- Do not add owner-kind/owner-name labels — ownership is via owner reference + `spec.group` (unchanged).
