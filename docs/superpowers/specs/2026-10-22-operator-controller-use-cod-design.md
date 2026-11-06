# Design: operator-controller delegates revision management to ClusterObjectDeployment

- **Status:** Draft (for review)
- **Date:** 2026-10-22
- **Branch:** `operator-controller-use-cod` (built on `main` + #3050 `progressDeadlineMinutes` + #3052 `revisionHistoryLimit`)

## Intent & why

Today the operator-controller's `ClusterExtension` controller manages the entire
`ClusterObjectSet` (COS) **revision lifecycle** itself: it computes the desired
revision, diffs it, creates each COS, packs objects into Secrets, patches owner
references, and garbage-collects old revisions. Meanwhile a separate
`ClusterObjectDeployment` (COD) controller already exists in the object-controller
binary that is purpose-built to manage COS revisions for a deployment template.

The two code paths overlap and duplicate revision mechanics (both independently
build and create COS objects today). The goal of this refactor is an
**architectural separation of concerns**: make the COD the single owner of COS
revision lifecycle, and shrink the operator-controller to *rendering desired
content and writing one COD per `ClusterExtension`*. This is the direction the
existing `TODO(COD)` markers in `internal/operator-controller/applier/boxcutter.go`
and `internal/object-controller/controllers/clusterobjectset_controller.go` point
toward.

## Goals

- operator-controller creates and maintains exactly **one COD per `ClusterExtension`**
  (COD name == `ext.Name` == COS `spec.group`), instead of creating COS revisions
  directly.
- The COD controller is the single component that **creates, archives, retains,
  and rolls out** COS revisions.
- `ClusterExtension` status (`Installed`, `Available`, `Progressing`,
  `.status.install`) is derived from the **COD** (single source of truth for
  rollout state), plus operator-controller's own pre-apply error reporting.
- Existing installations continue to work across the upgrade:
  - Helm-based installs migrate to a **COD** (Helm → COD), which then produces
    revision 1.
  - Pre-existing COS revisions (created by the current operator-controller) are
    **adopted** by the new COD without a disruptive re-rollout.

## Non-goals

- Changing the COS API or the COS controller's reconcile semantics (archival,
  probes, collision protection, progress deadline) beyond what is explicitly
  called out here.
- Moving **archival of predecessor revisions** from the COS controller to the COD
  controller. That remains a separate `TODO(COD)` and is out of scope; the COS
  controller continues to archive its lower-numbered siblings on completion.
- Enriching the COD `Progressing` condition with the finer COS-level rollout
  reasons (`Retrying`/`Blocked`). We explicitly **accept coarser rollout reasons**
  (see Decisions).
- Changing deprecation status handling (it is derived from the catalog, not from
  revisions, and is unaffected).

## Anti-goals (what would make this a failure)

- Any window in which `ClusterExtension.status.install.bundle` reports a bundle
  that has **not actually rolled out** (this corrupts upgrade-path resolution).
- An upgrade that **re-rolls or disrupts** a healthy, already-installed workload
  because the new COD does not recognize existing revisions.
- Secret objects leaking (never garbage collected) or being deleted while still
  referenced by a live revision.

## Constraints

- **Experimental-only; standard behaviour is frozen.** All changes are confined to
  the `BoxcutterRuntime` feature-gated code path. The gate is enabled only in the
  **experimental** manifest (`manifests/experimental.yaml`), not the **standard** one
  (`cmd/operator-controller/main.go` selects `applier.Boxcutter` +
  `BoxcutterRevisionStatesGetter` + `BoxcutterStorageMigrator` when the gate is on,
  and the Helm applier + `HelmRevisionStatesGetter` when it is off). COS/COD are
  experimental APIs.
- **No standard-manifest e2e may change.** e2e scenarios that run against the
  standard manifest set must not be modified and must pass **unchanged**. Only
  experimental e2es (and `@BoxcutterRuntime`-tagged features) may change behaviour or
  assertions. Behaviour changes are permitted **only** on the experimental API path.
- **The Helm/standard status path is untouched.** The `HelmRevisionStatesGetter`
  path and the observable CE conditions/reasons it produces must remain byte-for-byte
  equivalent. The new COD-based status derivation is a **separate, gated** path — it
  must not be implemented by mutating the shared `common_controller.go` helpers in a
  way that changes the Helm path's output. (Consequence: the "accept coarser
  Progressing reasons" and "wedged rollout never shows `Failed`" decisions apply
  **only** to the experimental path; the standard path keeps its current reasons.)

## Background: current state (verified against the tree)

### operator-controller applier (`internal/operator-controller/applier/boxcutter.go`)
- `Boxcutter.Apply` lists existing COS by group (`revisions.ListForGroup`, group =
  `ext.Name`), generates the desired COS, SSA-diffs refs-vs-refs to classify
  install/upgrade/unchanged, runs **preflights**, creates the COS with objects
  **externalized to Secrets**, patches Secret owner refs → COS UID, and
  **garbage-collects** archived revisions beyond a limit.
- `SimpleRevisionGenerator` builds the COS (`buildClusterObjectSet`), sets
  collision protection (`Prevent` normal, `None` for Helm-migrated), progression
  probes, and progress-deadline passthrough.
- `BoxcutterStorageMigrator.Migrate` creates revision 1 directly from a deployed
  Helm release, marks it `CompletedAt`, labelled `MigratedFromHelmKey`.

### Secret packing (`internal/operator-controller/applier/secretpacker.go`)
- Already essentially **content-addressable**: Secret *data keys* are content
  hashes (`contentHash`); Secret *name* is `<RevisionName>-<hash-of-data>`
  (`secretNameFromData`). The only revision coupling is the `RevisionName` prefix
  and the `RevisionNameKey` label. Secrets are immutable, typed
  `labels.SecretTypeObjectData`, in the system namespace.

### COD controller (`internal/object-controller/controllers/clusterobjectdeployment_controller.go`)
- `For(COD).Owns(COS)` plus a `Watches` mapping COS `spec.group` → COD name, which
  **adopts not-yet-owned (orphan) COS in the group**.
- Creates the next revision when the latest active revision's `TemplateHashKey`
  label != `templateHash(spec.template)`; copies `Phases`, `ProgressDeadlineMinutes`,
  `ProgressionProbes`, `CollisionProtection` verbatim into the COS.
- `setAvailable`: `True/Available` if any active (non-archived, non-deleting)
  revision is COS `Ready`; else `False/Unavailable` or `False/NoActiveRevision`.
- `Progressing`: `True/NewClusterObjectSetProgressing` while rolling,
  `False/NewClusterObjectSetProgressed` on completion (`latest.Status.CompletedAt`),
  `False/ProgressDeadlineExceeded` on deadline breach. `setCondition` stamps
  `ObservedGeneration = cod.Generation`.
- Retention/GC landed in #3052 (`garbageCollectArchivedRevisions`), keyed by
  `spec.revisionHistoryLimit`.

### CE status derivation (`internal/operator-controller/controllers/`)
- `BoxcutterRevisionStatesGetter.GetRevisionStates` lists COS by group, skips
  archived, classifies a revision **Installed** when `Status.CompletedAt != 0`
  (its bundle metadata → `.status.install.bundle`) and **RollingOut** otherwise.
- Bundle metadata is read from COS **annotations**: `package-name`, `bundle-name`,
  `bundle-version`, `bundle-release`, `bundle-reference` (`internal/shared/labels/labels.go`).
- `common_controller.go` maps: `Available` from the installed revision's COS `Ready`
  (re-emitted `ProbesSucceeded`); `Installed` True/`Succeeded` when a completed
  revision exists, else False/`Absent`|`Failed`; `Progressing` from the latest
  rolling revision's COS `Ready.Reason` → `RollingOut`/`Retrying`/`Blocked`/
  `ProgressDeadlineExceeded`/`Succeeded`, **and** from operator-controller's own
  pre-apply errors (`setStatusProgressing`: `Retrying`/`Blocked`/`InvalidConfiguration`).

## Target architecture

```
ClusterExtension (operator-controller)
  ├─ resolve bundle (catalog)            ── unchanged
  ├─ unpack content                      ── unchanged
  ├─ render manifests                    ── unchanged
  ├─ run preflights (CRD safety)         ── STAYS in operator-controller
  ├─ pack objects into Secrets           ── STAYS in operator-controller (content-addressed, COD-owned)
  └─ SSA write ONE ClusterObjectDeployment  ── REPLACES direct COS creation
        (template = ref'd phases, + bundle-metadata annotations)
            │
            ▼
ClusterObjectDeployment (object-controller)   ── single owner of revision lifecycle
  ├─ create next COS revision (verbatim template)
  ├─ retention / GC (revisionHistoryLimit)     ── #3052
  ├─ progress deadline                         ── #3050
  └─ status: Available, Progressing (+ ObservedGeneration)
            │
            ▼
ClusterObjectSet (object-controller)          ── unchanged; archives its own predecessors
```

## Detailed design

### 1. operator-controller writes a COD instead of COS revisions

`Boxcutter.Apply` is refactored to:
1. Render the desired objects (unchanged `ManifestProvider` / `SimpleRevisionGenerator`
   logic that produces phases).
2. Run preflights (unchanged).
3. Pack objects into content-addressed Secrets (see §2).
4. Build a **COD apply configuration** with:
   - `spec.template.spec` = the ref'd phases + `progressionProbes` +
     `collisionProtection` + `progressDeadlineMinutes` (the fields that used to go
     on the COS now go on the template; COD copies them to each COS).
   - `metadata.annotations` = bundle-metadata annotations (see §4/§5).
   - owner reference → the `ClusterExtension`.
5. Server-side apply the COD (field owner unchanged). The COD controller does the
   rest (revision creation, archival via COS, retention, rollout).

The direct COS mechanics removed from operator-controller: `createExternalizedRevision`,
`garbageCollectOldRevisions`, `latestRevisionNumber`, revision SSA-diff/state
machine, Secret owner-ref patching to the COS. Secret *packing* stays.

`collisionProtection` today is `Prevent` for normal installs and `None` for
Helm-migrated. On the COD template it is a single value per COD; see §5 for how the
Helm-migration `None` case is handled.

### 2. Secret packing & ownership

- **Keying:** drop the per-revision `RevisionName` prefix from the Secret name;
  prefix with the extension/COD name instead (e.g. `<ext.Name>-<content-hash>`).
  Data keys remain content hashes. Secrets become **fully content-addressed** and
  revision-number-independent, so operator-controller can create them without
  knowing the revision number the COD will assign.
- **Ownership:** operator-controller sets the Secret owner reference to the
  **`ClusterExtension`**. The CE object is already in hand during reconcile, so no
  COD/COS UID lookup or two-phase owner-ref patch is needed, and it is consistent
  with the COD itself being CE-owned. Secrets cascade-delete when the
  `ClusterExtension` is deleted.
- **References:** the COD template carries `ObjectSourceRef`s pointing at these
  Secrets; the COD controller copies them verbatim into each COS (`buildClusterObjectSet`
  already copies `Phases` as-is), and the COS controller resolves them unchanged.
- **Secret GC (builder decision, see Open Items):** because Secrets are CE-owned
  (not COS-owned), deleting an archived COS during retention does **not** cascade to
  its Secrets, and old-content Secrets accumulate as the template changes over the
  CE's lifetime. A pruning pass must delete Secrets in the group that are no longer
  referenced by **any** live COS revision (active or retained-archived). Content
  addressing makes this safe: a Secret shared by multiple revisions is retained
  while any revision references it. Recommended home: the COD controller (it already
  owns revision GC and can enumerate every revision's refs; it can delete the
  Secrets regardless of their owner). This must run **after** retention GC and must
  never delete a Secret referenced by a not-yet-GC'd archived revision.
- **Creation ordering:** operator-controller creates the Secrets (owner ref = CE,
  set directly — the CE exists) **before** applying the COD. Because the COD
  controller then asynchronously creates a COS referencing those Secrets, there is a
  brief window where a COS may reference a Secret that is already present (Secrets
  created first) — the ordering (Secrets → COD) avoids a dangling ref, and the COS
  controller's normal resolve-with-retry covers any transient lag. CE-ownership
  removes the old two-phase owner-ref patch entirely (no COD/COS UID needed first).
- **Deletion ordering (risk):** with CE-ownership, Secret deletion is driven by the
  CE's deletion rather than each COS's. The design must ensure referenced Secrets
  outlive any COS teardown that still needs to resolve them (e.g. via the CE
  finalizer ordering, so the COD/COS teardown completes before the CE — and thus its
  Secrets — is garbage collected).

### 3. Preflights stay in operator-controller

CRD-safety `Install`/`Upgrade` preflights run in `Boxcutter.Apply` before the COD is
written, exactly as today. A preflight failure is surfaced on the CE `Progressing`
condition via the existing pre-apply error path (§4) and the COD is not updated.

**Install-vs-Upgrade selection (builder call):** today the choice between
`preflight.Install` and `preflight.Upgrade` is produced by the COS SSA-diff state
machine (`boxcutter.go` `StateNeedsInstall`/`StateNeedsUpgrade`), which §1 removes.
Replacement signal: treat it as an **Install** when the extension has no prior
completed revision, else an **Upgrade** — derivable without reading COS from either
(a) the existence of the COD / whether it has ever reported
`Progressing=False/NewClusterObjectSetProgressed`, or (b) the CE's own current
`.status.install` (populated == already installed). Recommended: key off the CE's
`.status.install` presence, falling back to a COD `Get` when status is empty, so no
COS read is reintroduced. This is the one piece of "current state" operator-controller
still inspects before writing the COD.

### 4. ClusterExtension status (COD as single source of truth)

CE status is derived on the **success path** from the COD, and on the **error path**
from operator-controller's own reconcile errors (unchanged). Concretely:

- **Available** — mirror `COD.Available` onto `CE.Available`. COD reports `True`
  while *any* active revision is Ready, which preserves availability across
  upgrades. Reason mapping: **builder decision** — either adopt COD's reasons
  (`Available`/`Unavailable`/`NoActiveRevision`) or re-map to CE's existing
  `ProbesSucceeded` for API compatibility (recommended: re-map `True` →
  `ProbesSucceeded` to minimize client-visible change; pass through the non-ready
  states).
- **Progressing** — **layered**:
  - If operator-controller hits a **pre-apply error** (resolution, unpack,
    preflight), set `Progressing` from that error as today
    (`Retrying`/`Blocked`/`InvalidConfiguration`). The COD is not written.
  - Otherwise (a valid COD was applied), derive `Progressing` from
    `COD.Progressing`. We **accept the coarser reasons**: COD's
    `NewClusterObjectSetProgressing` / `NewClusterObjectSetProgressed` /
    `ProgressDeadlineExceeded`. The finer COS-level `Retrying`/`Blocked` rollout
    reasons are intentionally **not** preserved on the success path.
  - **Accepted status regression (revisit in a future PR):** because COD does not
    surface the COS `Ready` reasons (`ObjectCollision`/`PreconditionsNotMet`/
    `InvalidSpecification`), a rollout wedged on one of those shows
    `Progressing=True/RollingOut` and `Installed=False/Absent` — **never `Failed`** —
    until the progress deadline eventually flips `Progressing` to
    `False/ProgressDeadlineExceeded`. This is accepted for now (consistent with the
    coarser-reasons decision) and will be revisited (likely by enriching COD) in a
    follow-up.
- **Installed + `.status.install`** — update **only** when the COD reports the
  current rollout complete:
  - Condition: `COD.Progressing.Status == False` and
    `COD.Progressing.Reason == NewClusterObjectSetProgressed` **and**
    `COD.Progressing.ObservedGeneration == COD.metadata.generation` (the
    **generation gate**, see §5). When satisfied, set `Installed=True/Succeeded`
    and populate `.status.install.bundle` from the COD's bundle-metadata
    annotations.
  - When the gate is not satisfied, **hold** the previous `.status.install` (latch
    behaviour across upgrades). Only clear it / set `Installed=False` when there has
    never been a completed revision — detectable via `COD.Available ==
    False/NoActiveRevision` together with no prior install.

On the `BoxcutterRuntime` path only, `BoxcutterRevisionStatesGetter` and the
COS-reading status derivation (`boxcutter_reconcile_steps.go`) are replaced by a
COD-reading equivalent, and the CE controller **no longer lists or reads COS
objects**. The `HelmRevisionStatesGetter` path (standard manifest) and any shared
`common_controller.go` helpers it relies on are **left untouched**; the COD-based
derivation must be added as a separate, gated path rather than by changing the Helm
path's observable output.

### 5. Bundle-metadata annotations on the COD + generation gate

- operator-controller sets **annotations** on the COD reflecting the **desired**
  bundle: `package-name`, `bundle-name`, `bundle-version`, `bundle-release`,
  `bundle-reference` (reusing the existing keys in `internal/shared/labels/labels.go`).
  It writes these in the **same apply** as `spec.template`.
  - **Annotations, not labels:** several of these values are not label-safe —
    `bundle-reference` is an image ref (`registry/foo@sha256:...`, contains
    `/`/`:`/`@`, exceeds 63 chars) and `bundle-version` may carry build metadata
    (`1.0.0+2`, contains `+`). They are COS annotations today for exactly this
    reason. Annotations, like labels, are `metadata` and do **not** bump
    `generation`, so the gate below is unaffected.
- **The two-writer race:** the annotations are written by operator-controller;
  `Progressing` is written by the COD controller. Between operator-controller
  applying a new template+annotations and the COD controller reconciling,
  `COD.Progressing` can still read `False/NewClusterObjectSetProgressed` from the
  *previous* rollout while the annotations already describe the *new* bundle. Copying
  annotations → `.status.install` in that window would report the new bundle as
  installed before it rolled out.
- **The gate:** CE trusts the annotations only when
  `COD.Progressing.ObservedGeneration == COD.metadata.generation`. A bundle change
  always changes rendered content (object annotations carry the bundle version),
  hence changes `spec.template`, hence bumps `metadata.generation`. The COD
  controller stamps `ObservedGeneration` on the `Progressing` condition only after
  it has reconciled the new generation. So in the race window the condition still
  carries the *old* generation and CE correctly waits.
  - The builder-check verified the gate holds against the current COD controller:
    any generation bump not matched by a template-hash change still routes through
    the create-revision path and sets `Progressing=True` before stamping the new
    `ObservedGeneration`, so the gate cannot falsely pass.
- **Helm-migration `collisionProtection`:** see §6 for the `None → Prevent` flip.
  The COD template value starts `None` (to adopt Helm-owned objects) and is flipped
  to `Prevent` by operator-controller after the first successful rollout.

### 6. Migration & adoption

- **Helm → COD:** `BoxcutterStorageMigrator` is adapted to create a **COD** (not a
  COS) from a deployed Helm release. The COD's template starts with
  `collisionProtection: None` to adopt the objects the Helm release already owns, and
  the migrated COD is marked with a **pending-flip label** (see below). The COD
  controller then creates revision 1. The "mark revision 1 as completed immediately"
  behaviour that exists today (so the extension is treated as installed during the
  upgrade window) must be reproduced: because status is now COD-derived, the migrator
  (or COD) must ensure the migrated revision records `CompletedAt` and the COD
  surfaces `Progressing=False/Progressed` + bundle annotations, so CE reports the
  prior bundle as installed without re-rolling.
- **`collisionProtection: None → Prevent` flip.** The per-revision behaviour today —
  migrated rev 1 is `None`, every revision after is `Prevent` — cannot be expressed
  directly because the COD template is single-valued. It is reproduced as an explicit
  flip driven by operator-controller:
  1. The migrator creates the COD with `collisionProtection: None` and a
     pending-flip label (e.g. `olm.operatorframework.io/migrated-collision-flip: "true"`).
  2. While the label is present **and** the COD has not yet reported a successful
     rollout for the current generation (generation gate, §5),
     operator-controller keeps the desired template at `None` (does not flip,
     avoiding a premature rev 2 before rev 1 is healthy).
  3. Once the COD reports `Progressing=False/NewClusterObjectSetProgressed` for the
     current generation, operator-controller sets the desired template
     `collisionProtection: Prevent` and removes the pending-flip label. This is a
     template change, so the COD creates a `Prevent` rev 2 that hands over rev 1's
     objects.
  - **This reproduces proven behaviour.** The current architecture already performs
    the `None → Prevent` transition across successive same-group revisions (migrated
    rev 1 `None` via `GenerateRevisionFromHelmRelease`, then `Prevent` via
    `GenerateRevision`); the hand-over works because collision protection guards
    against *foreign* owners, not a predecessor revision in the same group. The only
    new machinery is the label-driven, rollout-gated flip of the single template
    value. Fresh (non-migrated) installs use `Prevent` from the start and never carry
    the label.
- **Existing COS adoption:** when the new operator-controller writes a COD named
  `ext.Name`, the COD controller's group watch adopts the pre-existing COS revisions
  in that group (sets the controller owner ref). If the latest existing revision's
  `TemplateHashKey` matches the COD template hash, **no new revision is created** and
  the workload is not disrupted; otherwise a normal rollout of the newly-desired
  template occurs (the same as any upgrade). The migrator must stamp the
  `TemplateHashKey` consistently so a steady-state extension does not trigger a
  spurious revision on first adoption.

### 7. Retention / archival (already handled / unchanged)

- Retention/GC: COD (`revisionHistoryLimit`, #3052).
- Archival of predecessors: COS controller, unchanged (out of scope to move).

## What is removed from operator-controller

- Direct COS creation and the install/upgrade/unchanged state machine in
  `Boxcutter.Apply`.
- `garbageCollectOldRevisions`, `latestRevisionNumber`, `createExternalizedRevision`.
- COS-based status reading (`BoxcutterRevisionStatesGetter`, the COS `Ready`→CE
  mapping helpers), replaced by COD-based reading.
- Secret owner-ref patching to the COS (Secrets now CE-owned, set directly at
  creation — no two-phase patch).

Kept: manifest rendering, preflights, Secret packing.

## Feature gating & rollout

COD is an experimental API; this work lands behind the experimental manifest. The
standard (non-experimental) path is unaffected until COD graduates. The upgrade from
a prior experimental operator-controller to this one must exercise the adoption path
(§6) in `test/upgrade-e2e`.

### Runtime dependencies & RBAC (builder work)

- **New cross-binary status dependency.** The COD controller lives in the separate
  `object-controller` binary; CE/applier/migrator live in `operator-controller`.
  After this change, CE status (`Installed`/`Available`/`Progressing`) depends on the
  object-controller binary being **running** (today only workload apply depends on
  the COS controller). The experimental manifest must guarantee object-controller is
  deployed wherever operator-controller runs; if the object-controller is down, CE
  status will not progress. (Note: per project direction the object-controller is
  being split into its own image/namespace — the manifest/RBAC work here must track
  that split.)
- **RBAC.** operator-controller currently has RBAC for `clusterobjectsets` but not
  `clusterobjectdeployments`. It must gain `create;get;list;watch;update;patch`
  (and `delete` for cleanup) on `clusterobjectdeployments`, plus continued Secret
  permissions. COS create/patch RBAC on operator-controller can be dropped once it no
  longer creates COS.
- **Upgrade ordering.** The §6 adoption path must be validated across a cross-binary
  upgrade (old operator-controller that created COS directly → new one that writes a
  COD) in `test/upgrade-e2e`.

## Testing strategy

- **Unit:** applier writes the expected COD (template, labels, owner ref, refs);
  CE status derivation from COD conditions + generation gate (installed only on
  `Progressed` + matching `ObservedGeneration`; held otherwise); secret keying is
  revision-independent and content-addressed.
- **envtest:** COD apply + adoption of pre-existing COS by group; no spurious
  revision when template hash matches.
- **e2e (experimental only):** install → status reports installed bundle; upgrade →
  `.status.install` holds the old bundle until the new rollout completes, then flips;
  Helm → COD migration; availability stays `True` across an upgrade; secret GC leaves
  no orphans after retention; the `None → Prevent` collision flip after migration.
  New/changed scenarios are `@BoxcutterRuntime`-tagged and run on the experimental
  manifest.
- **Standard e2es are not modified and must pass unchanged** (the Helm path is
  untouched). This is a hard acceptance gate for the work.

## Open items / builder decisions

1. **Secret GC home & algorithm** (§2) — recommended: a pruning pass in the COD
   controller that deletes group Secrets unreferenced by any live revision, run
   after retention GC. Must be proven safe against archived-but-retained revisions.
2. **Available reason mapping** (§4) — recommended: re-map COD `True` →
   `ProbesSucceeded` for client compatibility; otherwise adopt COD reasons.
3. **Migrated-revision "installed" signalling** (§6) — the exact mechanism by which
   a Helm→COD migrated install reports installed without re-rolling (reproducing
   today's immediate-`CompletedAt` behaviour through the COD).
4. **`TemplateHashKey` parity on adoption** (§6) — ensure the hash operator-controller
   would compute for a steady-state extension matches the existing revision's label,
   so adoption does not trigger a spurious rollout.
```
