# operator-controller writes a ClusterObjectDeployment (PR 1) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On the `BoxcutterRuntime` path, make the operator-controller create and maintain one `ClusterObjectDeployment` (COD) per `ClusterExtension` instead of creating `ClusterObjectSet` (COS) revisions directly; the COD controller owns revision creation/rollout/retention.

**Architecture:** The `Boxcutter` applier renders objects, runs preflights, packs objects into content-addressed `ClusterExtension`-owned Secrets, and server-side-applies a single COD whose template carries the ref'd phases plus bundle-metadata annotations. The COD controller creates the COS revisions (copying those annotations onto each COS) and adopts any pre-existing COS in the group. ClusterExtension **status derivation is unchanged in this PR** — it still reads COS revisions by group, which now exist because the COD created them.

**Tech Stack:** Go, controller-runtime, server-side apply (applyconfigurations), gomock, envtest, Godog e2e.

**Spec:** `docs/superpowers/specs/2026-10-22-operator-controller-use-cod-design.md`

## Global Constraints

- All changes are confined to the `BoxcutterRuntime` feature-gated path. The Helm path (gate off, standard manifest) and its applier/status are untouched.
- No standard-manifest e2e may be modified; all must pass unchanged. New/changed e2es are `@BoxcutterRuntime`-tagged and run on the experimental manifest.
- API domain for all generated labels/annotations: `olm.operatorframework.io` (reuse keys in `internal/shared/labels/labels.go`).
- Secrets are owned by the `ClusterExtension`, set at creation (no two-phase owner-ref patch).
- COD name == `ext.Name` == COS `spec.group`.
- After changing `//+kubebuilder:rbac` markers or Helm charts, run `make manifests` and commit the regenerated files.
- Build tag for object-controller code: `!standard`. Build tag for image handling: `containers_image_openpgp`.

## Review Focus

- **contentFS == nil (catalog unavailable).** `Apply` must keep the install healthy using the already-applied COD without generating a new template; today the COS path has an explicit nil branch. Owned by Task 5.
- **Pre-existing COS adoption without re-roll.** When the new applier first writes a COD for an extension that already has COS revisions, a steady-state extension must not trigger a spurious new revision (template hash must match). Owned by Task 6.
- **Secret name collisions across extensions.** Dropping the revision-name prefix must not let two extensions (or two bundles) with identical object content share a Secret in a way that breaks GC/ownership; names remain unique per owner. Owned by Task 1.
- **COD SSA ownership churn.** Re-applying an unchanged COD via SSA must be a no-op (no generation bump, no new revision). Owned by Task 5.
- **Bundle annotations missing on COS.** If the COD does not propagate bundle annotations to the COS, the unchanged CE status reader reports an empty installed bundle. Owned by Task 2.

---

## File structure

- `internal/operator-controller/applier/secretpacker.go` — content-addressed Secret naming; owner-independent of revision (Task 1).
- `internal/object-controller/controllers/clusterobjectdeployment_controller.go` — propagate bundle-metadata annotations COD→COS (Task 2).
- `internal/operator-controller/applier/clusterobjectdeployment.go` *(new)* — build a COD applyconfiguration from rendered phases + annotations + CE owner (Task 3, 4).
- `internal/operator-controller/applier/boxcutter.go` — rewrite `Boxcutter.Apply` to write a COD; delete direct-COS mechanics (Task 5).
- `cmd/operator-controller/main.go` — RBAC marker + wiring (Task 7).
- Tests alongside each, plus `test/e2e/features/cod_extension_rollout.feature` (Task 8).

---

### Task 1: Content-addressed, revision-independent Secret packing

**Files:**
- Modify: `internal/operator-controller/applier/secretpacker.go`
- Test: `internal/operator-controller/applier/secretpacker_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `SecretPacker{ OwnerName, SystemNamespace }` (field `RevisionName` removed). `Pack(phases []ocv1.ClusterObjectSetPhase) (*PackResult, error)` unchanged in signature. Secret names are `fmt.Sprintf("%s-%x", p.OwnerName, <sha256-of-sorted-data>[:8])`. Secret labels carry `labels.OwnerNameKey` (no `RevisionNameKey`).

- [ ] **Step 1: Write the failing test** — in `secretpacker_test.go`, `TestSecretPacker_NameIsRevisionIndependent`: pack the same phases with two `SecretPacker{OwnerName:"ext"}` values and assert the produced `Secrets[0].Name` are equal and begin with `"ext-"`; assert `Secrets[0].Labels` has `labels.OwnerNameKey=="ext"` and no `labels.RevisionNameKey`.
- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags containers_image_openpgp -run TestSecretPacker_NameIsRevisionIndependent ./internal/operator-controller/applier/`
Expected: FAIL (field `RevisionName` still referenced / name has revision prefix).

- [ ] **Step 3: Implement** — remove `RevisionName` from `SecretPacker`; change `secretNameFromData` to prefix with `p.OwnerName`; drop `RevisionNameKey` from `newSecret` labels. Leave data-key hashing and gzip logic unchanged.
- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags containers_image_openpgp -run TestSecretPacker ./internal/operator-controller/applier/`
Expected: PASS.

- [ ] **Step 5: Commit** — `git commit -m "✨ make packed Secret names revision-independent"`

---

### Task 2: COD propagates bundle-metadata annotations onto each COS

**Files:**
- Modify: `internal/object-controller/controllers/clusterobjectdeployment_controller.go` (`buildClusterObjectSet`)
- Test: `internal/object-controller/controllers/clusterobjectdeployment_controller_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `buildClusterObjectSet` copies the COD's bundle-metadata annotations (the keys in `internal/shared/labels/labels.go`: `PackageNameKey`, `BundleNameKey`, `BundleVersionKey`, `BundleReleaseKey`, `BundleReferenceKey`) from `cod.Annotations` onto the created COS's `Annotations`.

- [ ] **Step 1: Write the failing test** — `Test_ClusterObjectDeploymentReconciler_PropagatesBundleAnnotations`: create a COD (via `codFixture()`) with `cod.Annotations = {BundleNameKey:"b", BundleVersionKey:"1.0.0+2", BundleReferenceKey:"reg/x@sha256:...", PackageNameKey:"p"}`; reconcile; `getCOS(..., "test-cod-1-1")` and assert those four annotation keys/values are present on the COS; assert a non-bundle annotation on the COD is **not** copied.
- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags containers_image_openpgp -run Test_ClusterObjectDeploymentReconciler_PropagatesBundleAnnotations ./internal/object-controller/controllers/`
Expected: FAIL (COS has no annotations).

- [ ] **Step 3: Implement** — in `buildClusterObjectSet`, set `ObjectMeta.Annotations` to the subset of `cod.Annotations` whose keys are in the known bundle-metadata set (define a package-level `bundleAnnotationKeys` slice referencing the `labels` constants).
- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags containers_image_openpgp ./internal/object-controller/controllers/`
Expected: PASS (existing COD tests still pass).

- [ ] **Step 5: Commit** — `git commit -m "✨ propagate bundle annotations from ClusterObjectDeployment to its revisions"`

---

### Task 3: Build a COD applyconfiguration from rendered content

**Files:**
- Create: `internal/operator-controller/applier/clusterobjectdeployment.go`
- Test: `internal/operator-controller/applier/clusterobjectdeployment_test.go`

**Interfaces:**
- Consumes: `PhaseSort` (existing, `applier/phase.go`), `defaultProgressionProbes` (existing, `boxcutter.go`), the bundle-annotation keys (`internal/shared/labels`).
- Produces: `buildClusterObjectDeployment(objects []ocv1ac.ClusterObjectSetObjectApplyConfiguration, ext *ocv1.ClusterExtension, annotations map[string]string, collisionProtection ocv1.CollisionProtection) *ocv1ac.ClusterObjectDeploymentApplyConfiguration`. Sets `metadata.name = ext.Name`, `metadata.annotations = annotations`, `metadata.labels = {OwnerKindKey: ClusterExtensionKind, OwnerNameKey: ext.Name}`, and `spec.template.spec` with `WithPhases(PhaseSort(objects)...)`, `WithProgressionProbes(defaultProgressionProbes...)`, `WithCollisionProtection(collisionProtection)`, and `WithProgressDeadlineMinutes(ext.Spec.ProgressDeadlineMinutes)` when `> 0`.

> Depends on the generated `ocv1ac.ClusterObjectDeployment*` applyconfigurations already present in `applyconfigurations/api/v1/`.

- [ ] **Step 1: Write the failing test** — `TestBuildClusterObjectDeployment`: given one object and `ext{Name:"ext", Spec:{ProgressDeadlineMinutes:15}}`, assert the returned config has `*Name=="ext"`, one phase, `*Spec.Template.Spec.CollisionProtection==Prevent`, `*Spec.Template.Spec.ProgressDeadlineMinutes==15`, the owner labels set, and the passed annotations present.
- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags containers_image_openpgp -run TestBuildClusterObjectDeployment ./internal/operator-controller/applier/`
Expected: FAIL (function undefined).

- [ ] **Step 3: Implement `buildClusterObjectDeployment(...)`** in the new file, mirroring the structure of the existing `SimpleRevisionGenerator.buildClusterObjectSet`.
- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags containers_image_openpgp -run TestBuildClusterObjectDeployment ./internal/operator-controller/applier/`
Expected: PASS.

- [ ] **Step 5: Commit** — `git commit -m "✨ add ClusterObjectDeployment applyconfiguration builder"`

---

### Task 4: Render → phases → packed Secrets → COD (generator seam)

**Files:**
- Modify: `internal/operator-controller/applier/clusterobjectdeployment.go`
- Test: `internal/operator-controller/applier/clusterobjectdeployment_test.go`

**Interfaces:**
- Consumes: `ManifestProvider.Get`, `getBundleAnnotations`, `SecretPacker.Pack`, `buildClusterObjectDeployment` (Task 3).
- Produces: a method on a generator type that, given `contentFS`, `ext`, `objectLabels`, `revisionAnnotations`, returns `(*ocv1ac.ClusterObjectDeploymentApplyConfiguration, []corev1.Secret, error)` — the COD config with ref'd phases plus the content-addressed Secrets to create. Bundle-metadata annotations (package/bundle name/version/release/reference) are placed on the **COD** metadata. Reuse the existing object-rendering/annotation logic from `SimpleRevisionGenerator.GenerateRevision` (extract shared helpers rather than duplicating).

- [ ] **Step 1: Write the failing test** — `TestGenerateClusterObjectDeployment_PacksAndRefs`: with a fake `ManifestProvider` returning two objects, assert the returned COD config's phases contain only `Ref`s (no inline `Object`), the returned Secrets are non-empty and named `"<ext>-..."`, and the COD metadata carries the bundle annotations from the bundle properties.
- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags containers_image_openpgp -run TestGenerateClusterObjectDeployment ./internal/operator-controller/applier/`
Expected: FAIL (method undefined).

- [ ] **Step 3: Implement** the generator method: render objects, build phases, `Pack` them, replace inline objects with refs (reuse `replaceInlineWithRefs`/`extractPhasesForPacking` from `boxcutter.go`), build the COD via Task 3, and return COD + Secrets.
- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags containers_image_openpgp -run TestGenerateClusterObjectDeployment ./internal/operator-controller/applier/`
Expected: PASS.

- [ ] **Step 5: Commit** — `git commit -m "✨ generate a packed ClusterObjectDeployment from bundle content"`

---

### Task 5: Rewrite `Boxcutter.Apply` to write a COD

**Files:**
- Modify: `internal/operator-controller/applier/boxcutter.go`
- Test: `internal/operator-controller/applier/boxcutter_test.go`

**Interfaces:**
- Consumes: Task 4 generator; `Preflight.Install`/`Upgrade`; CE owner reference.
- Produces: `Boxcutter.Apply(ctx, contentFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (bool, string, error)` — **signature unchanged**. New behavior:
  1. If `contentFS == nil`: if a COD for `ext.Name` exists, return `(true, "", nil)` (maintain current state); else return an error "catalog content unavailable and no deployment installed".
  2. Else: generate COD + Secrets (Task 4); set the COD owner reference to `ext`; pick preflight mode (`Install` when `ext.Status.Install == nil`, else `Upgrade`) and run enabled preflights on the rendered objects; create the Secrets (owner ref = `ext`, `ForceOwnership`) **before** applying the COD; SSA-apply the COD (`FieldOwner`, `ForceOwnership`); return `(true, "", nil)`.
- Deletes: `createExternalizedRevision`, `garbageCollectOldRevisions`, `latestRevisionNumber`, the COS SSA-diff state machine, `patchSecretOwnerReferences`/`ensureSecretOwnerReferences`, and the `ClusterObjectSetRetentionLimit`/revision-naming logic.

- [ ] **Step 1: Write the failing test** — `TestBoxcutterApply_WritesClusterObjectDeployment`: fake client; call `Apply` with a stub content FS and `ext{Name:"ext"}`; assert exactly one COD named `"ext"` exists with owner ref to the CE, its template phases use refs, and the referenced Secrets exist in the system namespace owned by the CE; assert **no** COS is created directly by the applier.
- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags containers_image_openpgp -run TestBoxcutterApply_WritesClusterObjectDeployment ./internal/operator-controller/applier/`
Expected: FAIL (Apply still creates COS).

- [ ] **Step 3: Implement** the rewrite per the Interfaces block. Keep `Preflight` invocation and `shouldSkipPreflight` semantics; replace `state` selection with the `ext.Status.Install` check.
- [ ] **Step 4: Write the failing test for the nil-content and unchanged paths** — `TestBoxcutterApply_NilContentUsesExistingCOD` (COD exists → `(true,...)`, none exists → error) and `TestBoxcutterApply_UnchangedIsNoop` (applying identical content twice does not bump the COD `generation`).
- [ ] **Step 5: Run tests to verify they fail, implement, and verify they pass**

Run: `go test -tags containers_image_openpgp ./internal/operator-controller/applier/`
Expected: PASS (all applier tests).

- [ ] **Step 6: Commit** — `git commit -m "✨ apply a ClusterObjectDeployment instead of managing ClusterObjectSets"`

---

### Task 6: Adoption parity — no spurious revision for a steady-state extension

**Files:**
- Test: `internal/operator-controller/applier/boxcutter_test.go` (or an envtest in `internal/object-controller/controllers/`)

**Interfaces:**
- Consumes: Task 5 `Apply`; the COD controller's `templateHash` behavior.
- Produces: no new production code if Tasks 1–5 already yield hash parity; otherwise a fix so the COD template operator-controller writes hashes identically to an already-adopted revision.

- [ ] **Step 1: Write the failing test** — `TestApply_AdoptsExistingRevisionsWithoutReroll`: seed a COS in group `"ext"` whose `TemplateHashKey` equals the hash of the template the applier would produce for the same content; run `Apply`; reconcile the COD controller; assert **no** new COS revision (`ext-2`) is created and the existing revision is adopted (controller owner ref → COD).
- [ ] **Step 2: Run test to verify it fails or passes**

Run: `go test -tags containers_image_openpgp -run TestApply_AdoptsExistingRevisionsWithoutReroll ./internal/operator-controller/applier/`
Expected: FAIL if hashes diverge (then fix in Step 3); PASS means parity already holds — record that and proceed.

- [ ] **Step 3: If failing, implement** the minimal alignment so operator-controller's template and the COD controller's `templateHash` agree for identical content (e.g. ensure identical field population/ordering).
- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags containers_image_openpgp -run TestApply_AdoptsExistingRevisionsWithoutReroll ./internal/operator-controller/applier/`
Expected: PASS.

- [ ] **Step 5: Commit** — `git commit -m "✨ adopt pre-existing ClusterObjectSet revisions without re-rollout"`

---

### Task 7: RBAC, wiring, and manifests

**Files:**
- Modify: `cmd/operator-controller/main.go` (RBAC marker near the CE reconciler; applier wiring already satisfied by unchanged `Apply` signature)
- Modify: generated manifests via `make manifests`

**Interfaces:**
- Consumes: Task 5 applier.
- Produces: operator-controller ClusterRole gains `clusterobjectdeployments` verbs `create;get;list;watch;update;patch;delete` and retains Secret permissions. COS create/patch verbs may be narrowed to `get;list;watch;delete` (operator-controller no longer creates COS). Keep COS read for the unchanged status path.

- [ ] **Step 1: Add the `//+kubebuilder:rbac` marker** for `clusterobjectdeployments` (and adjust the COS marker) on the CE reconciler.
- [ ] **Step 2: Regenerate** — `make manifests generate` ; confirm the operator-controller ClusterRole in `manifests/experimental.yaml` includes `clusterobjectdeployments`.
- [ ] **Step 3: Build** — `go build -tags containers_image_openpgp ./...`; `make lint`.
Expected: builds; `0 issues`.
- [ ] **Step 4: Commit** — `git commit -m "✨ grant operator-controller RBAC for ClusterObjectDeployments"`

---

### Task 8: Experimental e2e — install and adoption via COD

**Files:**
- Create: `test/e2e/features/cod_extension_rollout.feature` (`@BoxcutterRuntime`)
- Possibly add steps in `test/e2e/steps/steps.go` only if an assertion is missing.

**Interfaces:**
- Consumes: existing e2e steps (`ClusterExtension ... Installed`, `ClusterObjectSet "<name>-1" ... Ready`, `ClusterObjectDeployment "<name>" reports ...`).

- [ ] **Step 1: Write the feature** — Scenario: apply a catalog + `ClusterExtension`; assert a `ClusterObjectDeployment` named after the extension exists, a `ClusterObjectSet "<ext>-1"` becomes `Ready`, and the `ClusterExtension` reports `Installed=True` with the expected bundle in `.status.install`.
- [ ] **Step 2: Run against the experimental cluster**

Run: `make e2e/cod_extension_rollout KUBECONFIG=$PWD/.kubeconfig/operator-controller-experimental-e2e.kubeconfig` (rebuild+load operator-controller and object-controller images first).
Expected: scenario passes.

- [ ] **Step 3: Confirm standard e2es are untouched** — `git status` shows no changes under standard-only features; run a representative standard scenario to confirm no regression.
- [ ] **Step 4: Commit** — `git commit -m "✨ add e2e for ClusterExtension rollout via ClusterObjectDeployment"`

---

## Roadmap (subsequent PRs — not in this plan)

- **PR 2:** CE status from the COD — new gated COD-reading path (Available mirror, layered Progressing, generation-gated Installed from COD annotations); remove `BoxcutterRevisionStatesGetter`/COS reading. Design §4/§5.
- **PR 3:** Helm→COD migration + `None→Prevent` collision flip (label-gated, rollout-gated). Design §6.
- **PR 4:** Secret GC — prune content-addressed Secrets no longer referenced by any live revision. Design §2 open item.

## Self-review notes

- Spec coverage (PR 1 slice): applier→COD (T3–T5), secrets content-addressed+CE-owned (T1, T5), COD→COS annotation propagation keeping status working (T2), adoption (T6), RBAC/manifests (T7), experimental e2e + standard-unchanged gate (T8). Status refactor, migration, collision flip, secret GC are explicitly deferred to PRs 2–4.
- Review Focus items each mapped to an owning task's test (nil content/unchanged→T5, adoption→T6, secret naming→T1, COS annotations→T2).
- Interfaces: `Apply` signature preserved (no main.go pipeline change beyond RBAC); `SecretPacker.RevisionName` removed consistently across T1/T5.
