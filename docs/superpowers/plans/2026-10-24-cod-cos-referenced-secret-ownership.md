# COD/COS referenced-secret ownership Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a `ClusterObjectSet` own (as a non-controller GC anchor) the `object-data` Secrets its phases reference, so those Secrets are reclaimed by Kubernetes garbage collection when the COS is deleted — without the object-controller otherwise managing them.

**Architecture:** The COS controller, early in its reconcile, ensures a single additive non-controller `ownerReference` (to itself) on each referenced Secret of type `olm.operatorframework.io/object-data`. It is idempotent and deduped by owner UID, so it coexists with the operator-controller applier's existing `Controller=true` patching (no-op where that ref is already present) and is the sole mechanism for standalone COD/COS. No COD secret logic, no sweeper, no finalizer.

**Tech Stack:** Go, controller-runtime, Kubernetes `ownerReferences` GC, Godog (e2e), envtest.

**Spec:** `docs/superpowers/specs/2026-10-24-cod-cos-referenced-secret-ownership-design.md`

## Global Constraints

- OwnerRefs added by the object-controller are **non-controller** (`Controller=false`), **additive-only**, deduped by owner **UID**. Never modify or remove an existing ownerRef. Do not set `BlockOwnerDeletion` (avoids needing `clusterobjectsets/finalizers` perms).
- **Claim (adopt) every referenced Secret, regardless of type**, so its deletion follows the COS. No type gate. (The `object-data`-only restriction is deferred to future work.)
- Claim on Secret **existence**, independent of **content** validity — the claim runs before the immutability/content precondition gates so a mutable/malformed referenced Secret is still owned.
- Secret namespace handling matches the existing code (`resolveObjectRef` / `verifyReferencedSecretsImmutable` use `ref.Namespace` verbatim).
- The COD gains **no** secret logic; keep `validateTemplateMetadata` unchanged.
- The operator-controller applier (`internal/operator-controller/applier/`) is **not modified** by this plan (CE side deferred).

## Review Focus

- **Empty-namespace ref:** the claim must `Get` the Secret at the same namespace the rest of the reconcile uses (`ref.Namespace` verbatim); a mismatch claims the wrong/no Secret. → Task 1 test.
- **Secret not yet created:** a referenced Secret absent at claim time must be skipped silently (no error, reconcile continues; `resolveObjectRef` reports it later). → Task 1 test.
- **Same Secret referenced by multiple phases/objects:** claim exactly once (dedup refs), one ownerRef. → Task 1 test.
- **Non-`object-data` (e.g. Opaque) Secret referenced:** adopted and GC'd just like any other (type-agnostic). → Task 1 unit test + Task 3 e2e.
- **Applier coexistence:** a Secret already carrying a `Controller=true` ownerRef with the COS's UID must not get a second ref (UID-dedup no-op). → Task 2 envtest.

---

### Task 1: COS claims referenced `object-data` Secrets

**Files:**
- Modify: `internal/object-controller/controllers/clusterobjectset_controller.go` (add `claimReferencedSecrets`, call it from `reconcile`, add RBAC marker)
- Test: `internal/object-controller/controllers/clusterobjectset_controller_internal_test.go` (package `controllers`, fake client — mirror `TestVerifyReferencedSecretsImmutable`)
- Modify (generated): run `make manifests` after adding the RBAC marker

**Interfaces:**
- Consumes: `ocv1.ClusterObjectSetKind`, `ocv1.GroupVersion`; the reconciler's `c.Client` and `*ocv1.ClusterObjectSet` (has `.UID`, `.Name`, `.Spec.Phases`).
- Produces: `func (c *ClusterObjectSetReconciler) claimReferencedSecrets(ctx context.Context, cos *ocv1.ClusterObjectSet) error` — called from `reconcile` after the deletion check and **before** `verifyReferencedSecretsImmutable`. Owns **every** referenced Secret regardless of type, independent of content validity. Returns a non-nil error only on a transient Get/Patch failure; nil when all existing refs are claimed and missing refs are skipped.

- [ ] **Step 1: Write the failing unit tests**

In the internal test file, add `Test_ClusterObjectSetReconciler_claimReferencedSecrets` (table or subtests), using `fake.NewClientBuilder().WithScheme(scheme).WithObjects(...).Build()` as in `TestVerifyReferencedSecretsImmutable`. Cases, each asserting the resulting Secret's `OwnerReferences` after calling `c.claimReferencedSecrets(ctx, cos)`:

```
- "object-data secret gets a non-controller ownerRef to the COS":
    secret.Type=SecretTypeObjectData; cos references it.
    want: exactly one ownerRef {APIVersion: ocv1.GroupVersion.String(), Kind: "ClusterObjectSet",
          Name: cos.Name, UID: cos.UID, Controller: ptr.To(false)}; err == nil.
- "idempotent: re-running adds no second ref":
    call claim twice; want still exactly one ownerRef with cos.UID.
- "already owned by this UID (e.g. applier Controller=true) is left untouched":
    secret pre-seeded with ownerRef {UID: cos.UID, Controller: ptr.To(true)};
    want: still exactly one ownerRef, Controller still true (no second ref, not downgraded); err == nil.
- "non-object-data (Opaque) secret is also claimed":
    secret.Type=corev1.SecretTypeOpaque; want: one non-controller ownerRef; err == nil.
- "missing secret is skipped":
    cos references a secret that does not exist; want: err == nil (no change, no error).
- "same secret referenced by two objects is claimed once":
    two refs to the same name/namespace; want: exactly one ownerRef.
- "empty-namespace ref is resolved at ref.Namespace verbatim":
    secret created in namespace "" is addressed by a ref with Namespace: ""; want: claimed.
```


- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -tags "!standard,containers_image_openpgp" -run 'Test_ClusterObjectSetReconciler_claimReferencedSecrets' ./internal/object-controller/controllers/`
Expected: FAIL (`claimReferencedSecrets` undefined).

- [ ] **Step 3: Implement `claimReferencedSecrets`**

Add the method to `clusterobjectset_controller.go`. Dedup refs exactly as `verifyReferencedSecretsImmutable` does (a `map[struct{name,namespace string}]struct{}`). For each unique ref: `c.Client.Get` the Secret; on `apierrors.IsNotFound` continue; on other error return a wrapped error; if `secret.Type != labels.SecretTypeObjectData` continue; if any existing `ownerReference.UID == cos.UID` continue; else build the non-controller ownerRef (fields above, no `BlockOwnerDeletion`) and apply it with a strategic merge patch (`patch := client.MergeFrom(secret.DeepCopy()); secret.OwnerReferences = append(secret.OwnerReferences, ownerRef); c.Client.Patch(ctx, secret, patch)`).

- [ ] **Step 4: Wire it into `reconcile`**

In `reconcile`, after the `!cos.DeletionTimestamp.IsZero()` early return and before `verifyReferencedSecretsImmutable`, add:
```go
if err := c.claimReferencedSecrets(ctx, cos); err != nil {
    return ctrl.Result{}, fmt.Errorf("claiming referenced Secrets: %w", err)
}
```

- [ ] **Step 5: Add the RBAC marker and regenerate**

Add `secrets` `update` to the object-controller's kubebuilder RBAC markers (the COS controller already has `get;list;watch` on `secrets`). Run `make manifests`.

- [ ] **Step 6: Run the unit tests + build**

Run: `go test -tags "!standard,containers_image_openpgp" -run 'claimReferencedSecrets' ./internal/object-controller/controllers/` → PASS
Run: `go build -tags containers_image_openpgp ./...` → succeeds

- [ ] **Step 7: Commit**

```bash
git add internal/object-controller/controllers/clusterobjectset_controller.go internal/object-controller/controllers/clusterobjectset_controller_internal_test.go
git add -A  # include regenerated RBAC manifests
git commit -s -m "✨ ClusterObjectSet owns its referenced object-data Secrets for GC"
```

---

### Task 2: envtest — claim write against a real API server

**Files:**
- Test: `internal/object-controller/controllers/clusterobjectset_controller_test.go` (package `controllers_test`, uses the suite `config` / `client.New(config, ...)` like `revision_engine_factory_test.go`)

**Interfaces:**
- Consumes: `claimReferencedSecrets` (Task 1), the envtest `config` from `suite_test.go`.
- Produces: nothing other tasks consume.

**Why envtest, not cascade:** envtest runs only kube-apiserver + etcd (no kube-controller-manager), so the garbage collector does not run here — cascade is a Task 3 (e2e) concern. envtest's value is validating the *write* against the real API server.

- [ ] **Step 1: Write the failing test**

Add `Test_ClusterObjectSetReconciler_claimReferencedSecrets_Envtest` building a real client from `config`. Create an **immutable** `object-data` Secret and a COS referencing it (unique names; `t.Cleanup` to delete). Call `claimReferencedSecrets` via a reconciler built with the real client. Assert:
- the patch succeeds against the real API server (no error) despite the Secret being immutable (metadata is patchable);
- re-get shows exactly one non-controller ownerRef with the COS UID;
- calling claim again is a no-op (still one ownerRef) — idempotent across a real Get/Patch roundtrip;
- coexistence: pre-seed the Secret with an ownerRef `{UID: cos.UID, Controller: ptr.To(true)}` (simulating the applier) → after claim, still exactly one ownerRef and it is unchanged.

- [ ] **Step 2: Run to verify it fails, then passes after no code change needed**

Run: `go test -tags "!standard,containers_image_openpgp" -run 'claimReferencedSecrets_Envtest' ./internal/object-controller/controllers/`
Expected: PASS once written (Task 1 provides the behavior). If it fails, the failure is a real API-server interaction gap (e.g. immutable-patch rejected) to fix in Task 1's method.

- [ ] **Step 3: Commit**

```bash
git add internal/object-controller/controllers/clusterobjectset_controller_test.go
git commit -s -m "🌱 envtest: COS claim patch is accepted and idempotent on immutable Secrets"
```

---

### Task 3: E2E — standalone COD/COS secret lifecycle

**Files:**
- Modify: an experimental `@BoxcutterRuntime` feature file — add to `test/e2e/features/revision.feature` (it already creates `object-data` Secrets and COSes) or a new `test/e2e/features/cos_secret_ownership.feature`.
- Modify (if steps are missing): `test/e2e/steps/steps.go` — add a step def asserting a Secret **has** an owner reference to ClusterObjectSet `<name>` (scenarios 1 and 3). Everything else composes from existing steps (apply, wait Ready, `is eventually not found`).

**Interfaces:**
- Consumes: the Task 1 behavior running in a real cluster; existing Godog steps (apply COS/COD, wait Ready, `resource "<kind>/<name>" is installed`, `resource "<kind>/<name>" is eventually not found`, apply a raw resource).
- Produces: nothing other tasks consume.

- [ ] **Step 1: Write the scenarios**

```gherkin
@BoxcutterRuntime
Scenario: Deleting a standalone ClusterObjectSet garbage-collects its object-data Secrets
  # Given an immutable object-data Secret holding a manifest, and a COS referencing it
  # Then the COS reports Ready, and the Secret has an owner reference to the COS
  # When the ClusterObjectSet is removed
  # Then resource "secret/<ref-secret>" is eventually not found

@BoxcutterRuntime
Scenario: Deleting a ClusterObjectDeployment garbage-collects its revisions' object-data Secrets
  # Given a COD whose template references object-data Secret content, rolled out to a revision
  # When the ClusterObjectDeployment is removed
  # Then its ClusterObjectSet revision(s) and their object-data Secrets are eventually not found

@BoxcutterRuntime
Scenario: A referenced Secret of any type is adopted and garbage-collected
  # Given an immutable Opaque (non-object-data) Secret holding a manifest, and a COS referencing it
  # Then the COS reports Ready and the Secret has an owner reference to the COS
  # When the ClusterObjectSet is removed
  # Then resource "secret/<secret>" is eventually not found
```

- [ ] **Step 2: Add any missing step definitions**

If "has an owner reference to ClusterObjectSet" is not already expressible, implement it in `test/e2e/steps/steps.go` following the existing step patterns (kubectl/get + assert). Reuse existing steps (apply, wait Ready, `is eventually not found`) everywhere else.

- [ ] **Step 3: Run the scenarios against an experimental cluster**

Run: `make experimental-e2e-setup` then `make "e2e/<feature>/Deleting a standalone ClusterObjectSet" KUBECONFIG=.kubeconfig/operator-controller-experimental-e2e.kubeconfig` (and the other two scenarios).
Expected: all three PASS. Tear down with `make experimental-e2e-teardown`.

- [ ] **Step 4: Commit**

```bash
git add test/e2e/features/ test/e2e/steps/
git commit -s -m "✨ e2e: standalone COD/COS object-data Secret lifecycle"
```

---

## Regression guard (no new work)

`test/e2e/features/uninstall.feature` — "Removing ClusterExtension cascades to grouped revisions and their content Secrets" must continue to pass unchanged (this plan is additive; the applier is untouched). Confirm it is green in the standard e2e run; do not modify it.

## Deferred / future work (out of scope)

- **Restrict referenceable Secret types to `object-data`** (reject others as `PreconditionsNotMet`). This design keeps the current "any type" behavior and adopts/GCs whatever is referenced; the restriction lands later. Tracked in the spec's "Future work".
- **CE side:** Secret creation, the transient bridging ownerRef (added at creation, dropped once a COS owns the Secret), and converting/removing the applier's `Controller=true` patching. Tracked in the spec's "The creator contract" and "Interaction with the existing applier" sections.
