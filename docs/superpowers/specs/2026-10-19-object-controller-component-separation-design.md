# Design: Separate object-controller into its own downstream component

**Date:** 2026-10-19
**Status:** Draft design (pre-plan)
**Author:** Per G. da Silva (with Claude)

---

## 1. Intent and why

Downstream (OpenShift), the `object-controller` (the `ClusterObjectSet` /
boxcutter runtime manager) is currently a passenger: its binary is baked into
the `ose-olm-operator-controller` payload image, and its workload is rendered
into the `openshift-operator-controller` namespace as part of the
operator-controller Helm render. This was done to avoid breaking CI while the
component stabilized.

We want `object-controller` to be a first-class downstream component, on the
same footing as `catalogd` and `operator-controller`:

- Its own payload image (`ose-olm-object-controller`).
- Its own namespace (`openshift-object-controller`).
- Deployed only when the `BoxcutterRuntime` feature gate is enabled (TechPreview).

The hard constraint is **CI must stay green at every merge, in every repo.** It
is acceptable to pass through intermediate states where the binary still exists
in the operator-controller image, or where the component still installs into
`openshift-operator-controller`, as long as each individual merge is additive or
atomic and leaves CI green. A final cleanup PR after the cross-over is
explicitly in scope.

### Key reframing discovered during investigation

**Upstream (`operator-framework/operator-controller`, this repo) is already
fully split.** The object-controller already has its own `cmd/object-controller`
binary, its own `Dockerfile.object-controller`, its own `.goreleaser.yml`
entries, its own `internal/object-controller/` tree, and its own Helm templates
(gated by the `objectController.enabled` helper in `_helpers.tpl`). Upstream
deploys everything into the single `namespaces.olmv1.name` namespace, which is
correct and desirable for upstream.

Therefore **the bundling lives entirely in the downstream `openshift/` carry**
(`openshift/operator-framework-operator-controller`), not upstream. The bulk of
this work is in three places: the downstream fork, `openshift/ocp-build-data`,
and `openshift/cluster-olm-operator`. Upstream needs only two small, backward-
compatible Helm `_helpers.tpl` changes plus a smoke test and a docs update.

---

## 2. Goals / non-goals / anti-goals

### Goals
- `object-controller` ships as its own OpenShift payload image built from the
  downstream fork, mirroring how `catalogd` and `operator-controller` images are
  built.
- `object-controller` is deployed into `openshift-object-controller` and only
  when `BoxcutterRuntime` is enabled.
- cluster-olm-operator renders `object-controller` as a third, first-class
  component with its own image env var, `image-references` entry, manifest copy
  initContainer, namespace, and RBAC readiness waits.
- A clean final cleanup that removes the object-controller binary from the
  operator-controller image and removes the object-controller passenger block
  from the operator-controller Helm values.
- CI stays green at every individual merge in every repo.

### Non-goals
- **No scoped RBAC in this effort.** object-controller keeps its current
  binding to the built-in `cluster-admin` ClusterRole. A least-privilege
  ClusterRole is a separate follow-up.
- No change to upstream's single-namespace (`olmv1-system`) layout. Upstream
  continues to deploy all three controllers into one namespace.
- No change to the `BoxcutterRuntime` gate semantics or to when the feature is
  considered TechPreview.
- No new git repository. Like `catalogd` and `operator-controller`, the new
  component is a new *image* built from the single downstream fork
  `openshift/operator-framework-operator-controller`; it is not a new source
  repo.

### Anti-goals (would make this a failure even if it "works")
- Any merge that leaves a repo's CI red, requiring a follow-up "fixup" to go
  green.
- A window in which object-controller is deployed **twice** (once in
  `openshift-operator-controller` via the old passenger path and once in
  `openshift-object-controller` via the new path) — object collisions / double
  reconciliation. (See §6.5: on fresh installs this cannot happen; it would only
  arise on an upgrade across the change, which the feature-set semantics forbid.)
- A window in which cluster-olm-operator references the new operand image (env
  var, `image-references`, or manifest-copy initContainer) **before** that image
  is actually built and available in the CI payload — the operator pod would
  fail to start.
- Any destructive handoff of the `ClusterObjectSet` CRD that could cascade-delete
  live `ClusterObjectSet` CRs. (See §6.5: verified not to happen — apply-only.)

---

## 3. Current-state architecture (the full chain)

### Upstream (this repo) — already separated
- Binary: `cmd/object-controller/main.go` (own `main`, leader-election lock
  `object-controller-lock.olm.operatorframework.io`).
- Image: `Dockerfile.object-controller` → `object-controller:<tag>`; dedicated
  `.goreleaser.yml` build + docker-manifest entries; `make docker-build` builds
  it separately.
- Implementation: `internal/object-controller/`; API
  `api/v1/clusterobjectset_types.go`.
- Helm: Deployment, Service, PDB, ServiceMonitor, NetworkPolicy, cert, RBAC
  (ClusterRoleBinding to `cluster-admin`), and CRD — all gated by the
  `objectController.enabled` helper (`helm/olmv1/templates/_helpers.tpl:67-87`).
- Gate helper behavior:
  - `$boxcutterEnabled` = `operatorController.enabled` AND `BoxcutterRuntime` in
    `operatorController.features.enabled` AND NOT in `.disabled`.
  - `objectController.enabled` null → defaults to `$boxcutterEnabled`.
  - Explicit `true` is allowed ("Set true to deploy independently").
  - **Fails** if `objectController.enabled` is explicitly `false` while boxcutter
    is enabled (`_helpers.tpl:74-76`).
  - **Fails** unless `options.featureSet == experimental` (`_helpers.tpl:82-84`).
- Namespace: single `namespaces.olmv1.name` for all three controllers.

### Downstream fork (`operator-framework-operator-controller`, the `openshift/` carry)
- Uses the upstream `helm/olmv1` chart **verbatim** (COPY'd into each image, not
  vendored). Downstream customization is expressed only through external values
  files.
- `openshift/operator-controller.Dockerfile` copies **both** `/operator-controller`
  **and** `/object-controller` binaries into the one image (comment:
  "object-controller is baked into this image for now"). Also copies
  `openshift/operator-controller/cp-manifests`, `helm/olmv1`, `experimental.yaml`,
  and `operator-controller.yaml` (renamed to `openshift.yaml` in the image).
- `openshift/catalogd.Dockerfile` copies `catalogd` only, plus catalogd's
  `cp-manifests`, chart, and `catalogd.yaml`→`openshift.yaml`.
- `openshift/helm/operator-controller.yaml`: `namespaces.olmv1.name:
  openshift-operator-controller`, `operatorController.enabled: true`,
  `objectController` block present with **no `enabled` key** (null → follows the
  boxcutter gate) and `objectController.deployment.image: ${OPERATOR_CONTROLLER_IMAGE}`,
  `catalogd.enabled: false`, `openshift.enabled: true`, `featureSet: standard`.
- `openshift/helm/catalogd.yaml`: `namespaces.olmv1.name: openshift-catalogd`,
  `catalogd.enabled: true`, `operatorController.enabled: false`.
- `openshift/helm/experimental.yaml`: overlay; `featureSet: experimental`;
  **currently lists `BoxcutterRuntime` in the DISABLED list** — so with the
  shipped values object-controller does not render; cluster-olm-operator
  overrides `BoxcutterRuntime` on when the OpenShift gate is enabled.
- `cp-manifests` is a per-component shell script copying `/openshift/helm/*` into
  `${dest}/helm/<component>/`. `openshift/helm/generate-manifests.sh` produces
  per-component snapshot manifests for diff verification.
- **No** `object-controller.Dockerfile`, **no** `openshift/object-controller/`
  dir, **no** `openshift/helm/object-controller.yaml`.

### ocp-build-data (branches `openshift-4.20`, `openshift-4.21`)
- `images/ose-olm-operator-controller.yml` and `images/ose-olm-catalogd.yml`:
  both `for_payload: true`, built from the downstream fork via
  `openshift/<name>.Dockerfile`, `payload_name: olm-operator-controller` /
  `olm-catalogd`, distgit `rhaos-{MAJOR}.{MINOR}-rhel-9`.
- **No** `ose-olm-object-controller.yml`.

### cluster-olm-operator
- Renders operand charts in-process with the Helm SDK
  (`pkg/controller/helm.go`), from charts copied into `/operand-assets` by
  initContainers (`copy-catalogd-manifests`, `copy-operator-controller-manifests`
  in `manifests/0000_51_olm_06_deployment.yaml`) that run each operand image's
  `cp-manifests`.
- Renders only **two** components:
  `cb.BuildControllers("catalogd", "operator-controller")`
  (`cmd/cluster-olm-operator/main.go:384`). object-controller rides inside the
  operator-controller render.
- Image injection (`helm.go:78-91`): `options.catalogd.deployment.image` ←
  `CATALOGD_IMAGE`; `options.operatorController.deployment.image` ←
  `OPERATOR_CONTROLLER_IMAGE`; `options.objectController.deployment.image` ←
  **`OPERATOR_CONTROLLER_IMAGE`** (same env var — reuses the operator-controller
  image).
- Feature-gate mapping (`internal/featuregates/mapper.go`): OpenShift
  `FeatureGateNewOLMBoxCutterRuntime` → upstream `BoxcutterRuntime` on
  operator-controller; applied to helm values by
  `pkg/controller/featuregates_hook.go`.
- `manifests/image-references` lists 4 images (cluster-olm-operator,
  olm-catalogd, olm-operator-controller, kube-rbac-proxy). **No** object-controller.
- Namespaces: operator's own ns is `openshift-cluster-olm-operator`; operand
  namespaces come from operand chart values. The only hardcoded operand ns in Go
  is `operatorControllerNamespace = "openshift-operator-controller"`
  (`main.go:53`). object-controller shares it.
- Detection/readiness: `objectControllerRendered()` (`main.go:341-348`) checks
  for the object-controller SA in `openshift-operator-controller`;
  `waitForOLMv1ComponentsRBAC` then waits for its SA +
  `object-controller-cluster-admin-rolebinding`.
- HA overrides (`helm.go:102-127`) already cover
  `options.objectController.deployment.replicas` / `podDisruptionBudget.enabled`.

### Verified cluster-olm-operator runtime behavior (apply/prune)
Confirmed against the cluster-olm-operator source; this is the foundation of the
crossover-safety argument in §6.5.
- **Apply-only, never prune.** Each component gets its own
  library-go `StaticResourceController` built with the plain constructor (no
  `WithConditionalResources`), so the delete path is unreachable
  (`shouldDelete` is always false). Resources that disappear from a render are
  neither applied nor deleted — they **linger**.
- **Per-component, independent.** `BuildControllers("catalogd",
  "operator-controller")` builds one controller set per component; there is no
  combined aggregate set and no diff against prior cluster state.
- **The operand CRDs (`ClusterObjectSet`, `ClusterExtension`, `ClusterCatalog`)
  are rendered by the operand Helm charts** (copied from the operand images into
  `/operand-assets`) and applied apply-only via the per-component
  `StaticResourceController`. cluster-olm-operator **never deletes** a CRD it
  applied. (The only CRD baked into the cluster-olm-operator image itself is its
  own `operator.openshift.io` OLMConfig CRD, applied by the CVO.)
- **`relatedObjects` is informational only** — used for informer namespaces,
  ClusterOperator status/must-gather, and the object-controller readiness check.
  It is never used to prune. The only `Delete` calls in the whole operator are a
  hardcoded legacy-RBAC cleanup (`cleanupOldOperatorControllerRBAC`), never CRDs
  or workloads.
- **Re-render happens by process restart** (on feature-gate/topology change); the
  manifest set is fixed per process lifetime.

Consequence: moving a resource (including the `ClusterObjectSet` CRD) from one
render to another is **additive and non-destructive** — the new component applies
it; the old component ceasing to render it deletes nothing.

---

## 4. Target end state

- `ose-olm-object-controller` is a `for_payload: true` image built from
  `openshift/object-controller.Dockerfile` in the downstream fork, listed in
  cluster-olm-operator `image-references`.
- The downstream fork has `openshift/object-controller.Dockerfile` (copies only
  `/object-controller`), `openshift/object-controller/cp-manifests`, and
  `openshift/helm/object-controller.yaml` with `namespaces.olmv1.name:
  openshift-object-controller`.
- cluster-olm-operator renders three components:
  `BuildControllers("catalogd", "operator-controller", "object-controller")`.
  The object-controller render targets `openshift-object-controller`, takes its
  image from a new `OBJECT_CONTROLLER_IMAGE` env var, has a
  `copy-object-controller-manifests` initContainer, and its own namespace + RBAC
  readiness waits.
- object-controller is excluded from the operator-controller render.
- The operator-controller image no longer carries the object-controller binary;
  `operator-controller.yaml` no longer carries the `objectController` block.
- object-controller deploys only when `BoxcutterRuntime` (TechPreview) is on;
  default (non-TechPreview) clusters deploy nothing new.
- RBAC unchanged (still `cluster-admin`).

---

## 5. Constraints and decisions (locked)

1. **CI-safe at every merge.** Every merge is either strictly additive, or an
   atomic cross-over that updates producers and consumers together.
2. **RBAC: keep `cluster-admin`.** No scoped ClusterRole in this effort.
3. **ART/payload onboarding is an external prerequisite**, captured here as a
   checklist and sequenced first; we edit the `ocp-build-data` config and the
   cluster-olm-operator references, but the ART approval/distgit/CVP steps are
   driven out-of-band by the author.
4. **Downstream fork is the primary code repo for this work.** Upstream change
   is limited to two small, backward-compatible `_helpers.tpl` tweaks, one smoke
   render, and a docs update.
5. **No double-deploy window** and **no dangling-image-reference window** (see
   anti-goals).
6. **No installed base to migrate.** `BoxcutterRuntime` is TechPreview/CI-only
   with no upgrade guarantee (confirmed). It is reachable only via `*NoUpgrade`
   feature sets (`TechPreviewNoUpgrade` / `CustomNoUpgrade` /
   `DevPreviewNoUpgrade`), which **block cluster upgrades**. Therefore every
   cluster that receives the new layout is a **fresh install**, and no cluster
   carries the old object-controller layout across an upgrade.
7. **Derived constraint — land before GA.** The separation must land while
   `BoxcutterRuntime` is still pre-GA (not promoted to `Default`). Once it is on
   by default, a real installed base exists and a destructive migration would be
   required. Completing the split while it is TechPreview makes migration a
   non-issue forever (fresh GA installs get the new layout from day one).

---

## 6. The minimal upstream foundation (this repo)

These must be backward-compatible: existing standard/experimental renders (and
their checked-in `manifests/*.yaml`) must be byte-for-byte unchanged so upstream
CI stays green.

**U1 — Decouple object-controller enablement from operator-controller in
`helm/olmv1/templates/_helpers.tpl`.** Two problems block a clean separation:
- The standalone object-controller render (operatorController disabled) can never
  turn object-controller on, because `$boxcutterEnabled` requires
  `operatorController.enabled`.
- The operator-controller render cannot *exclude* object-controller when
  boxcutter is on, because setting `objectController.enabled: false` while
  boxcutter is enabled makes the helper fail (`_helpers.tpl:74-76`).

The fix must preserve current default behavior (null + operator-controller +
boxcutter → object-controller renders in the same namespace, as upstream does
today) while additionally allowing:
  - an explicit `objectController.enabled: false` to suppress object-controller
    even when boxcutter is on (relax/remove the fail-on-false guard), and
  - a standalone render where `objectController.enabled: true` (operatorController
    disabled) renders cleanly under `featureSet: experimental`.

The builder should choose the least-invasive helper refactor that satisfies both
and keeps all existing rendered manifests unchanged. (Candidate: keep the
null→`$boxcutterEnabled` default, drop the fail-on-false guard, and confirm the
explicit-true standalone path renders.)

**U2 — Standalone-render smoke test.** Add a render-only smoke check to
`make manifests` (alongside the existing `openshift.enabled=true` and tilt smoke
renders) that renders the chart with only objectController enabled,
`openshift.enabled=true`, `featureSet=experimental`, and a distinct
`namespaces.olmv1.name`, to lock the standalone render in CI. Verify the shared
ServiceAccount template, the `component.name.prefix` / `olmv1.label.name`
helpers, namespace creation, and ServiceMonitor all behave with only
object-controller present.

**U3 — Docs.** Update `docs/draft/concepts/clusterobjectsets.md` (and any
`api-reference` prose) to describe object-controller as a separate downstream
image/namespace rather than a passenger in the operator-controller image.

U1/U2/U3 are additive and backward-compatible → one upstream PR, CI stays green.
They flow downstream via the normal rebase/carry, so the updated chart is baked
into rebuilt operand images before the cross-over.

---

## 6.5 Crossover safety analysis (the decisive argument)

The §2 anti-goals (no double-deploy, no destructive CRD handoff) are satisfied by
the combination of three verified facts:

1. **Apply-only runtime (§3).** No render ever prunes. Moving the
   `ClusterObjectSet` CRD and the object-controller workload from the
   operator-controller render to the object-controller render is additive: the
   new component applies them; nothing deletes the old render's objects.
2. **Fresh installs only (constraint 6).** Because `BoxcutterRuntime` is only
   reachable on `*NoUpgrade` clusters, which cannot upgrade, there is never a
   running cluster that already has object-controller in
   `openshift-operator-controller` and then transitions to the new layout. On a
   fresh install of the post-crossover payload, the old-namespace object-controller
   objects are **never created in the first place** — so the "lingering old
   workload → double reconciliation" scenario cannot occur.
3. **Atomic consumer flip (Phase 3b).** The single cluster-olm-operator change
   both enables the object-controller render and excludes object-controller from
   the operator-controller render, so even within one process there is exactly
   one object-controller workload.

**CRD ownership after the split.** The object-controller component owns the
`ClusterObjectSet` CRD render (it is gated by `objectController.enabled`). The
operator-controller workload, when `BoxcutterRuntime` is on, *creates*
`ClusterObjectSet` CRs and therefore depends on that CRD existing. Because (a)
static-resource controllers start before the Deployment controllers
(CRD-apply precedes the workloads), and (b) the operator-controller workload
retries CR creation until the CRD is present, this is eventually consistent and
low-risk. If the builder wants to eliminate even the transient race, the CRD may
additionally be rendered by a stable component (apply-only makes
double-rendering a harmless idempotent re-apply) — but this is an optional
hardening, not a requirement.

**Residual (out-of-scope) risk.** The only scenario that would reintroduce the
double-deploy risk is an in-place upgrade of a cluster that had the old layout —
which the feature-set semantics forbid today. This is explicitly out of scope
per constraint 6, and is permanently avoided by landing the split before GA
(constraint 7). A *best-effort* cleanup of the old-namespace object-controller
objects (mirroring the existing `cleanupOldOperatorControllerRBAC` pattern) is
included as optional hygiene in Phase 4 for dev clusters that manually toggled
the gate — it is not required for correctness.

---

## 7. Phased, CI-safe migration plan

Phases are ordered by dependency. Each phase is green-on-merge.

### Phase 0 — ART / payload onboarding (external prerequisite, start first)
Longest lead time; begin immediately and in parallel.
- Create the distgit / delivery plumbing for a new payload image
  `ose-olm-object-controller` (`openshift4/ose-olm-object-controller-rhel9`,
  `payload_name: olm-object-controller`), mirroring
  `ose-olm-operator-controller.yml`.
- Secure ART approval to add a new payload image to the OCP release payload for
  the target release branch(es).

**Hard ordering dependencies (do not violate — these break the nightly):**
- **The `openshift/object-controller.Dockerfile` must already exist on the
  targeted downstream `release-X.Y` branch(es) before the ocp-build-data image
  config builds.** ART (Doozer) syncs source → distgit → builds; a config that
  references a missing Dockerfile fails that image's build, and because it is a
  payload member it can degrade/block the nightly payload assembly and page ART /
  the component owners. → Phase 1 is a hard prerequisite for Phase 2.
- **A `for_payload: true` image that no operator references via `image-references`
  is flagged by the release controller as an unreferenced payload image.** →
  `for_payload: true` must not be enabled until cluster-olm-operator's
  `image-references` entry (Phase 3a) is in place.
- **Therefore stage the ocp-build-data onboarding:** (1) ART onboarding *request*
  (distgit/delivery creation) can start now, in parallel, with no code
  dependency; (2) add the image config only after the Dockerfile lands, and
  introduce it **non-payload first** (`for_payload: false` / disabled) to shake
  out the build without touching the payload; (3) flip `for_payload: true` only
  once the build is green **and** the consumer reference (Phase 3a) exists.
The image build is onboarded on **two independent tracks**, both of which
depend on the Phase 1 Dockerfile. Only the ART onboarding *request* (distgit
creation) has no code dependency and should start immediately.

**Track A — CI build (`openshift/release`).** Lighter, self-serve; this is what
makes the image available in CI and spliced into the e2e payload that
cluster-olm-operator e2e boots from (gates Phase 3).
  - PR to `openshift/release` adding, under `images.items` in each
    `ci-operator/config/openshift/operator-framework-operator-controller/openshift-...-<branch>.yaml`:
    `{ dockerfile_path: openshift/object-controller.Dockerfile, to: olm-object-controller }`
    (plus the `from:`/`inputs:`/`build_args:` form in the `__okd-scos` variants).
    Run `make update` / `make jobs` to regenerate.
  - No `promotion` change needed: the existing block auto-promotes
    `olm-object-controller` to `ocp/<ver>`, and
    `releases.latest.integration.include_built_images: true` splices it into the
    e2e payload. Consumed via the payload, not via explicit `dependencies:`.
  - Approvals: DPTP + repo OWNERS.

**Track B — ART production onboarding (`ocp-build-data` + ART).** Heavyweight,
long lead.
  - **Start now (no code dependency):** file the ART onboarding request; ART
    creates distgit `ose-olm-object-controller-container`
    (`rhaos-{MAJOR}.{MINOR}-rhel-9`) and delivery repo
    `openshift4/ose-olm-object-controller-rhel9`.
  - ocp-build-data `images/ose-olm-object-controller.yml` (produced in Phase 2),
    `payload_name: olm-object-controller`, introduced non-payload/disabled first.
  - CVP / release-payload acceptance for the new payload member.
  - `for_payload: true` flipped last (Phase 2 step 3), coupled to the Phase 3a
    `image-references` entry.
  - Confirm the component appears in the CI release payload consumed by
    cluster-olm-operator e2e before the Phase 3b crossover.

**Naming:** use `olm-object-controller` consistently — CI `to:` name, ocp-build-data
`payload_name`, and the cluster-olm-operator `image-references` entry — matching
`olm-operator-controller` / `olm-catalogd`.

- **CI impact:** none on existing repos until the image is referenced.

### Phase 1 — Downstream fork: add object-controller build assets (additive)
Repo: `openshift/operator-framework-operator-controller`.
- Add `openshift/object-controller.Dockerfile` mirroring `catalogd.Dockerfile`:
  runtime stage copies only `/object-controller`, plus
  `openshift/object-controller/cp-manifests`, `helm/olmv1`,
  `openshift/helm/experimental.yaml`, and
  `openshift/helm/object-controller.yaml`→`openshift.yaml`.
- Add `openshift/object-controller/cp-manifests` (DEST
  `${1}/helm/object-controller`).
- Add `openshift/helm/object-controller.yaml`:
  - `namespaces.olmv1.name: openshift-object-controller`
  - `options.objectController.enabled` **left unset (null)** — see enablement
    resolution below.
  - `options.objectController.deployment.image: ${OBJECT_CONTROLLER_IMAGE}` and
    `.replicas: 1` (the deployment template requires replicas).
  - `options.operatorController.enabled: false`, `options.catalogd.enabled: false`
  - `options.openshift.enabled: true`, `featureSet: standard`
  - deployments.templateSpec mirrors the siblings, anti-affinity keyed on
    `control-plane: object-controller-controller-manager`.

  **Enablement resolution (RESOLVED — was an open item).** The helper
  (`_helpers.tpl:82-83`) **fails** a render when `objectController.enabled` is
  truthy but `featureSet != experimental`, and with `operatorController.enabled:
  false` the null-default can never turn it on. So `object-controller.yaml` must
  **not** hardcode `enabled: true` (that would crash the standard render that
  cluster-olm-operator does on non-TechPreview clusters). Instead it leaves
  `enabled` unset → the component renders **empty and non-failing** on standard
  (verified: only the namespace + default-deny NetworkPolicy), and
  cluster-olm-operator sets `options.objectController.enabled=true` for the
  object-controller render only when it is already rendering experimental +
  BoxcutterRuntime (Phase 3b).
- Update `openshift/helm/generate-manifests.sh` to emit
  `openshift/object-controller/manifests.yaml` (shipped values → namespace-only)
  and `manifests-experimental.yaml` rendered with
  `--values experimental.yaml --set options.objectController.enabled=true` so the
  separated render is reviewable and the standalone render is verified in the
  snapshot. Commit the snapshots.

  **Verified (helm template):** the forced-on experimental render produces the
  object-controller Deployment/Service/SA/ServiceMonitor/NetworkPolicy/RBAC and
  the `clusterobjectsets` CRD, all in `openshift-object-controller`, image
  `${OBJECT_CONTROLLER_IMAGE}`, with zero operator-controller/catalogd leakage.

  **Heads-up (pre-existing, out of scope):** regenerating the downstream
  snapshots shows the committed `openshift/{catalogd,operator-controller}/manifests*.yaml`
  on `main` are already stale vs the current chart (e.g. a missing
  `operator-controller-manager-role` ClusterRole). The Phase 1 PR must stay
  scoped to object-controller and not absorb that drift; it should be fixed in a
  separate regeneration PR.
- **Leave `operator-controller.Dockerfile` and `operator-controller.yaml`
  unchanged** (object-controller binary still baked in; passenger block still
  present). Purely additive.
- **CI impact:** fork CI builds an additional image / snapshot; nothing consumes
  it yet → green.

### Phase 2 — Build-config onboarding: CI + ART (additive)
Both artifacts depend on the Phase 1 Dockerfile.

**2-CI — `openshift/release` (Track A).** Add the `olm-object-controller` image
stanza (see Phase 0 Track A) to every relevant
`operator-framework-operator-controller-<branch>.yaml` (and `__okd-scos`
variant). Merge → postsubmit builds and promotes it to `ocp/<ver>`; e2e payloads
now include it. This is the gate for Phase 3 e2e consumption.

**2-ART — `openshift/ocp-build-data` (Track B).** Per target branch
(`openshift-4.21`, and `openshift-4.20` if backporting), add
`images/ose-olm-object-controller.yml` mirroring `ose-olm-operator-controller.yml`:
`content.source.dockerfile: openshift/object-controller.Dockerfile`, same git
source (the fork), `payload_name: olm-object-controller`,
`name: openshift4/ose-olm-object-controller-rhel9`, matching distgit/delivery.
- **Introduce non-payload/disabled first** (`for_payload: false`) to shake out
  the build without touching the nightly payload.
- **Flip `for_payload: true` only after** the build is green **and** the
  cluster-olm-operator `image-references` entry exists (Phase 3a) — otherwise the
  release controller flags an unreferenced payload image.
- **CI impact:** adds an image to the build; nothing references it yet; payload
  membership deferred to the coupled flip. Gated by Phase 0 ART onboarding.

### Phase 3 — cluster-olm-operator: add the third component (atomic crossover)
Repo: `openshift/cluster-olm-operator`. Split into an additive 3a and an atomic
crossover 3b.

**3a — Plumb the new image reference (only after the image is available in CI).**
- Add `OBJECT_CONTROLLER_IMAGE` env var to the operator Deployment
  (`manifests/0000_51_olm_06_deployment.yaml`), dev default
  `quay.io/openshift/origin-olm-object-controller:latest`.
- Add an `object-controller` entry to `manifests/image-references`.
- Add a `copy-object-controller-manifests` initContainer (runs the new operand
  image's `cp-manifests`).
- **Ordering gate:** 3a references the new operand image (initContainer +
  image-references). It must merge **only after** Phase 2 + Phase 0 make the
  image available in the CI payload, or the operator pod fails to start.
- 3a alone does not yet render or deploy object-controller as a separate
  component, so no behavior change for the operand → green.

**3b — Crossover: enable the new render, suppress the old passenger (atomic).**
- Add `"object-controller"` to `BuildControllers(...)`.
- In `helm.go`, set `options.objectController.deployment.image` ←
  `OBJECT_CONTROLLER_IMAGE` (for the object-controller render), and keep the
  operator-controller render from setting it.
- Drive object-controller enablement: in the object-controller render, map the
  `BoxcutterRuntime` gate → `options.objectController.enabled=true` only when the
  gate is on and featureSet is experimental; otherwise the component renders
  empty. (Per Phase 1, the shipped `object-controller.yaml` leaves `enabled`
  unset, so this runtime set is what actually turns the component on.)
- Avoid an idle namespace on GA/non-TechPreview clusters: the empty render still
  emits the `openshift-object-controller` namespace + a default-deny
  NetworkPolicy. Decide whether cluster-olm-operator should skip building/applying
  the object-controller component entirely when BoxcutterRuntime is off (cleaner),
  or accept the idle namespace. Prefer skipping.
- Suppress the old passenger: in the operator-controller render, set
  `options.objectController.enabled=false` (now permitted after upstream U1 is
  baked into the operand chart) so object-controller no longer renders into
  `openshift-operator-controller`.
- Add `openshift-object-controller` namespace handling + RBAC readiness waits;
  generalize or replace `objectControllerRendered()` /
  `waitForOLMv1ComponentsRBAC` to look in the new namespace. Confirm the
  operator's own ClusterRole already grants the object-controller role/binding
  management in the new namespace (it has `object-controller-*` grants today).
- Update e2e expectations to look for object-controller in
  `openshift-object-controller`.
- **Atomicity:** the move from `openshift-operator-controller` →
  `openshift-object-controller` happens in one merge — no double-deploy.
- **Prerequisite:** operand images must carry the U1 chart (so
  `objectController.enabled=false` is honored by the operator-controller render)
  — i.e. the fork has rebased past U1 and images are rebuilt.
- **CI impact:** TechPreview e2e now finds object-controller in its own
  namespace; updated in the same merge → green. Standard (non-TechPreview) e2e
  unaffected (object-controller renders empty).

### Phase 4 — Cleanup (the "clean up this repo" PR(s))
After 3b is live and verified on all target branches.
- **Downstream fork:**
  - `operator-controller.Dockerfile`: stop copying `/object-controller`.
  - `operator-controller.yaml`: remove the `objectController` block.
  - Regenerate `openshift/operator-controller/manifests*.yaml` snapshots.
- **cluster-olm-operator:** remove the old `OPERATOR_CONTROLLER_IMAGE`-reuse for
  object-controller and any now-dead detection branches for the old namespace.
- **cluster-olm-operator (optional hygiene):** a best-effort delete of any
  leftover object-controller Deployment/SA/RBAC in `openshift-operator-controller`,
  mirroring the existing hardcoded `cleanupOldOperatorControllerRBAC` pattern.
  Not required for correctness (fresh installs never create those objects per
  §6.5); useful only for dev clusters that manually toggled `BoxcutterRuntime`.
- **Upstream:** no functional cleanup required — upstream intentionally keeps the
  dual-mode chart (single-namespace deploy for local/upstream use). Optional:
  tighten docs.
- **CI impact:** each cleanup is a removal of now-unused behavior; green because
  nothing consumes the removed paths anymore.

---

## 8. Sequencing summary and CI-safety invariants

```
Phase 0 (ART, external) ─────────────────────────────┐ (image available in CI)
                                                      v
U1/U2/U3 (upstream, additive) → rebase to fork → operand images rebuilt with U1 chart
                                                      │
Phase 1 (fork: new image assets, additive) ──────────┤
                                                      │
Phase 2 (ocp-build-data: image config, additive) ────┤
                                                      v
Phase 3a (c-olm-o: image ref plumbing)  [needs image available]
                                                      v
Phase 3b (c-olm-o: atomic crossover)    [needs U1 chart in operand images]
                                                      v
Phase 4 (cleanup: fork + c-olm-o)       [needs 3b live everywhere]
```

Invariants checked at every merge:
- **I1 — no dangling image ref:** no repo references `OBJECT_CONTROLLER_IMAGE`,
  the object-controller `image-references` entry, or the
  `copy-object-controller-manifests` initContainer until the image is built and
  available (Phase 0+2 before Phase 3a).
- **I2 — no double-deploy:** the old passenger render is suppressed in the exact
  same merge that enables the new render (Phase 3b).
- **I3 — upstream renders unchanged:** U1/U2/U3 keep every checked-in
  `manifests/*.yaml` byte-identical.
- **I4 — default clusters unaffected:** object-controller only renders under
  experimental + BoxcutterRuntime throughout; standard renders stay empty.

---

## 9. Open items left to the builder

- **U1 helper refactor shape:** choose the least-invasive `_helpers.tpl` change
  satisfying both the standalone-render and the force-exclude requirements while
  keeping existing manifests unchanged. Validate with `make manifests` + the new
  U2 smoke render.
- **Enablement mechanism (Phase 1 vs 3b):** decide whether the object-controller
  component's enablement is driven purely by the shipped
  `object-controller.yaml` (`enabled: true`, relying on experimental overlay) or
  by cluster-olm-operator setting `objectController.enabled` at render time based
  on the `BoxcutterRuntime` gate. Prefer whichever keeps the standard render
  empty without a chart error and avoids divergence between the two mechanisms.
- **Target release branches:** confirm whether the separation lands on
  `openshift-4.21` only or is also backported to `openshift-4.20` (affects
  ocp-build-data branches and fork release branches).
- **kube-rbac-proxy / metrics:** confirm object-controller metrics serving in
  the new namespace matches operator-controller (cert-manager vs
  openshift-service-ca, ServiceMonitor, cluster-monitoring namespace label).
- **ClusterOperator relatedObjects / status:** confirm the new namespace and
  object-controller objects are recorded as related objects and that
  `incompatible_operator_controller.go`'s ClusterObjectSet handling is unaffected
  by the namespace move.
- **RBAC readiness wait generalization:** confirm SCC `privileged` access and
  RBAC propagation waits work for the new namespace.
- **CRD render-ownership hardening (optional):** decide whether to additionally
  render the `ClusterObjectSet` CRD from a stable component to remove the transient
  start-order race (§6.5), or accept the eventually-consistent default.
- **GA timing (tracking, not a build decision):** ensure the split lands before
  `BoxcutterRuntime` is promoted to `Default` (constraint 7). If GA timing slips
  ahead of the split, the migration assumptions in §6.5 must be revisited.

---

## 10. Repos and key files touched

- **Upstream — `operator-framework/operator-controller` (this repo):**
  `helm/olmv1/templates/_helpers.tpl`, `Makefile` (manifests smoke target),
  `docs/draft/concepts/clusterobjectsets.md`.
- **Downstream fork — `openshift/operator-framework-operator-controller`:**
  new `openshift/object-controller.Dockerfile`,
  `openshift/object-controller/cp-manifests`,
  `openshift/helm/object-controller.yaml`,
  `openshift/helm/generate-manifests.sh`,
  new `openshift/object-controller/manifests*.yaml`; later
  `openshift/operator-controller.Dockerfile` + `openshift/helm/operator-controller.yaml`.
- **`openshift/ocp-build-data`:** new `images/ose-olm-object-controller.yml` per
  target branch.
- **`openshift/cluster-olm-operator`:**
  `manifests/0000_51_olm_06_deployment.yaml`, `manifests/image-references`,
  `pkg/controller/helm.go`, `pkg/controller/builder.go`,
  `cmd/cluster-olm-operator/main.go`, `internal/featuregates/mapper.go` /
  `pkg/controller/featuregates_hook.go`, e2e.
```
