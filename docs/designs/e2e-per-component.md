# E2E Per-Component Test Suites

**Date:** 2026-10-15
**Status:** Draft (design)
**Topic:** Reorganize the e2e test suite so each API/component (catalogd, operator-controller, object-controller) is independently testable with its own features, while still supporting a single run across all of them.

---

## Problem & Intent

The project has grown three distinct APIs and controllers:

- **catalogd** — `ClusterCatalog`
- **operator-controller** — `ClusterExtension`
- **object-controller** — `ClusterObjectSet` (and soon `ClusterObjectDeployment`)

Today all e2e tests live in a single Godog (Gherkin/BDD) suite at `test/e2e`:

- One entrypoint, `test/e2e/features_test.go`, globs every `*.feature` under `test/e2e/features/` and runs them together.
- Features are organized **by concern** (`install`, `update`, `status`, `metrics`, `namespace`, …), not by component. These cut across all three APIs.
- Step definitions live in a single shared package `test/e2e/steps` (~5,000 lines total; `steps.go` alone is ~2,800), consumed by both the e2e suite **and** `test/upgrade-e2e`.
- Selection is driven by `--godog.tags` and feature paths from the Makefile. Existing tags: `@Serial`, `@demo`, `@BoxcutterRuntime`, `@TLSProfile`.

**Driver:** Each API/controller should be treated as its own thing — independently testable, with its own set of features — while keeping a way to run everything together. This favors **physical separation** (per-component test packages, features, and suite entrypoints) over logical tag-only separation: it keeps each component's tests self-contained today, and if the controllers are ever split into separate projects, moving a component's tests out is close to a `git mv`.

### Goals

- Each component has its own `features/` directory and its own suite entrypoint, runnable in isolation.
- A single command still runs the entire suite across all components.
- A future repo split is mechanical: move a component directory out; the shared library becomes a vendored/published dependency.
- No change to test behavior during the reorganization — moves are behavior-preserving.

### Non-Goals

- **True dependency isolation / mocking.** These are e2e tests against a real cluster. "Independently testable" means *a component's suite can be run on its own and contains the scenarios it owns* — not that operator-controller is tested without a real catalogd. The install path legitimately depends on catalogd serving a catalog; that remains an integration reality.
- Rewriting or changing the semantics of any existing scenario.
- Introducing new test frameworks. We stay on Godog.

---

## Chosen Approach: Directory-per-component + shared library (Approach A)

### Target layout

```
test/e2e/
  shared/                       # extractable library: everything reusable
    world/hooks/helpers         # world state, hooks, "OLM is available", registry + catalog helpers
    runner                      # TestMain boilerplate: options, --e2e.scenario filter, summary wiring
    steps/                      # (post-PR3) truly-shared step definitions
    support-bundle.yaml         # shared diagnostics (moved from test/e2e/)
  operator-controller/
    features/                   # install, update, uninstall, status, namespace, recover, vap-access-control
    suite_test.go               # entrypoint: shared runner + shared steps + OC-specific steps
    steps/                      # (post-PR3) ClusterExtension-specific steps
  catalogd/
    features/                   # ha, proxy, tls
    suite_test.go
    steps/                      # (post-PR3) catalog-specific steps
  object-controller/
    features/                   # revision, user-managed-fields
    suite_test.go
    steps/                      # (post-PR3) COS/COD-specific steps
  platform/                     # genuinely cross-component features
    features/                   # metrics (OC + catalogd), demos
    suite_test.go
```

Each `*/suite_test.go` is a thin Go test package that calls the shared runner, pointing it at its own `features/` directory and registering the shared step set plus any component-specific steps.

### How "run one" and "run all" work

- **Run all:** Go's `...` wildcard (`go test ./test/e2e/...`) discovers and runs every component subpackage automatically — no central list to maintain. The canonical run-all stays the `make test-e2e` target, which already carries the established invocation: `-count=1` (test caching off, so every run actually contacts the cluster) and the two-pass tag policy — `~@Serial && ~@demo` concurrently, then `@Serial && ~@demo` serially, with `@demo` run only by `update-demos.yaml`.
- **Run one component:** `go test ./test/e2e/catalogd/...` (etc.), carrying the same `-count=1` and `--godog.tags` flags as the run-all invocation.
- Existing tag-based selection (`@Serial` serial vs. concurrent, `~@demo` exclusion) is preserved **within** each suite via `--godog.tags`.

### Why this over the alternatives

- **Tag-only on a single suite (rejected):** smallest change, but leaves everything in one package/dir — it does *not* create the clean boundary a repo split needs, and features are not physically owned. Weakest match to the driver.
- **Hybrid single parameterized runner (rejected):** physically separates features/steps but keeps one runner selected by flag. The runner becomes a shared choke point that accretes `if component == …` conditionals the moment components need different suite-level behavior, and the conversion to per-entrypoint has to happen at split time anyway. We pay the cost later without the clean boundary now.

The expensive work — partitioning features and (eventually) factoring the step library — is identical across these options. Approach A is the only one that makes the repo split mechanical and avoids the shared-runner-conditionals risk, so we do it directly.

---

## Cross-Cutting Feature Ownership

Some features are not cleanly owned by a single component. Assignment is by the **primary user-facing API under test**, with a dedicated `platform/` bucket for genuinely cross-component features.

| Feature | Owner | Notes |
|---|---|---|
| `install`, `update`, `uninstall`, `status`, `namespace`, `recover`, `vap-access-control` | operator-controller | Exercise `ClusterExtension`; catalogd is an assumed dependency, not the subject. |
| `revision`, `user-managed-fields` | object-controller | Already `@BoxcutterRuntime`. |
| `ha`, `proxy`, `tls` | catalogd | `tls.feature` comments confirm all scenarios test catalogd only. All `@Serial`. |
| `metrics` | **platform** | Single scenario outline tests both `operator-controller` and `catalogd` via an Examples table. Splitting would duplicate the scenario; keep as one cross-component feature. |
| `demos` | **platform** | `@demo`, cross-cutting, already excluded from normal runs via `~@demo`. Governed by `update-demos.yaml`. |

**Decision:** Introduce a `platform/` suite for cross-component features rather than forcing `metrics`/`demos` into a single owner. The "run all" target (`go test ./test/e2e/...`) covers it automatically. This is the one design choice most worth confirming on review; the alternative (split `metrics` per component, drop `platform/`) is viable but duplicates the metrics scenario.

---

## The Shared Library Boundary

`test/e2e/shared` is the seam a repo split will cut along, so it must hold everything reusable and nothing component-specific:

- **World/state + hooks** (`hooks.go`): scenario setup/teardown, namespace lifecycle.
- **Common Given steps:** "OLM is available", "an image registry is available", "a catalog … with packages", generic apply/assert helpers. These are prerequisites every component needs.
- **Runner boilerplate:** the Godog `Options`, the `--e2e.scenario` prefix filter (`findScenarioFirstLineNumberByPrefix`), and summary wiring (`test/internal/summary`) — currently inlined in `features_test.go:36-134`. Lifted into a shared helper so each `suite_test.go` is a few lines. **Caveat:** `test/internal/summary` is a Go `internal` package, importable only within this repo's tree; the shared runner can import it today, but a repo split would make that import illegal. Either move the summary code under `shared/` as part of the extraction, or keep the summary callback in each repo's `suite_test.go` so the shared runner carries no `internal` dependency.
- **Diagnostics:** `support-bundle.yaml`.

`test/upgrade-e2e` already imports `test/e2e/steps`; after the move it becomes another consumer of `test/e2e/shared/steps`. This is expected and reinforces the shared-library design.

---

## PR Sequence

The ordering is deliberate: **PR2 is safe precisely because PR3 has not happened yet.** Through PR1–PR2 the step definitions remain one monolithic package, so every component suite can register the *entire* shared step set. A feature moved into `catalogd/features/` still resolves all its steps. Godog `Strict: true` fails only on *undefined/pending* steps, never on *registered-but-unused* ones, so over-registering is free. The step split (PR3) therefore blocks nothing.

### PR1 — Reroot layout + establish `shared` (mechanical, behavior-preserving)

- `git mv test/e2e/features/` → `test/e2e/operator-controller/features/` (all features stay together for now).
- `git mv test/e2e/steps/` → `test/e2e/shared/steps/`.
- Lift runner boilerplate (`TestMain`, `--e2e.scenario` filter, summary wiring) from `features_test.go` into `test/e2e/shared` so later suites reuse it.
- Create the operator-controller `suite_test.go` calling the shared runner.
- Move `support-bundle.yaml` into `shared/`.
- **Update all references in the same PR:**
  - `test/upgrade-e2e/upgrade_test.go` import path for the steps package.
  - Makefile targets referencing `./test/e2e/features_test.go` (lines ~431, 435, 446, 479, 740) and the `e2e/%` scenario path.
  - `.github/workflows/update-demos.yaml` paths (`features/demos.feature`, `steps/demo_steps.go`, `steps/asciicast_*.go`) and the `support-bundle.yaml` path in `e2e.yaml:72`.
- **Constraint:** no behavior change. The suite still runs the same scenarios; only locations and import paths move.
- ⚠️ **CI workflow edits require maintainer sign-off** (per AGENTS.md "never modify `.github/workflows/*` without explicit permission"). Flag this explicitly in the PR; the demos/support-bundle path updates are unavoidable consequences of the move.

### PR2 — Sort features into component suites (may split per component)

- Create `catalogd/`, `object-controller/`, and `platform/` suites, each with a thin `suite_test.go` that calls the shared runner and registers the full shared step set.
- `git mv` features to their owners per the ownership table. Moving `demos.feature` into `platform/features/` shifts a path tracked by `.github/workflows/update-demos.yaml`; update that workflow in the same PR (maintainer sign-off, as in PR1).
- The operator-controller suite shrinks as features leave; `go test ./test/e2e/...` now runs four suites.
- Can be landed as separate PRs per component (e.g. object-controller first, then catalogd, then platform).
- **Still no step refactor** — every suite registers everything, so all steps resolve.

### PR3 — Split the step library per component (judgment-heavy, incremental)

- Partition `shared/steps` into truly-shared vs. component-specific. Rough starting map:
  - **Shared:** `hooks.go`, world setup, "OLM is available"/registry/catalog Given steps, generic apply/assert (the shared portion of `steps.go`).
  - **operator-controller:** `ClusterExtension`-specific steps from `steps.go`.
  - **catalogd:** `ha_steps.go`, `proxy_steps.go`, `tls_steps.go`.
  - **platform/demos:** `demo_steps.go`, `asciicast_hooks.go`, `asciicast_recorder.go`.
  - **upgrade-e2e:** `upgrade_steps.go` (consider relocating to the upgrade suite).
- Moving the demo step files (`demo_steps.go`, `asciicast_*.go`) again shifts paths tracked by `.github/workflows/update-demos.yaml`; update that workflow in the same PR.
- Each suite registers shared + its own steps.
- Can land one component at a time; nothing depends on it completing. This is where a future repo split becomes trivial.

### Sequencing summary

| PR | Scope | Risk | Splittable |
|---|---|---|---|
| PR1 | Reroot + `shared`, update refs | Low (mechanical) | No |
| PR2 | Sort features into suites | Low (moves; over-registered steps) | Up to 3–4 PRs |
| PR3 | Split step library | Medium (judgment) | Up to 3–4 PRs, incremental |

---

## Testing & Verification

- **Each PR:** `make test-e2e` (and `make test-experimental-e2e` for object-controller / `@BoxcutterRuntime` features) must stay green. Because PR1–PR2 are behavior-preserving moves, the same scenario set runs with the same pass/fail.
- **Run-all:** verify `go test ./test/e2e/...` discovers and runs every component suite.
- **Run-one:** verify `go test ./test/e2e/<component>/...` runs only that component's features.
- **Tag behavior:** confirm `@Serial` scenarios still run serially and `~@demo` exclusion still applies within each suite.
- **upgrade-e2e:** confirm it still builds and runs after the steps package moves.
- `make verify` / `make lint` for import and formatting correctness.

---

## Open Decisions (for review)

1. **`platform/` bucket vs. splitting `metrics`/`demos` per component.** Recommended: keep `platform/`. Alternative: duplicate the metrics scenario outline per component and drop `platform/`.
2. **Per-component Makefile targets.** Whether to add `test-e2e-<component>` convenience targets, or rely on `go test ./test/e2e/<component>/...` directly. (Leaning: add thin targets mirroring the existing `test-e2e` env setup, since cluster/manifest env wiring is non-trivial.)
3. **`upgrade_steps.go` home.** Relocate into the upgrade suite during PR3, or keep in `shared/steps`.
