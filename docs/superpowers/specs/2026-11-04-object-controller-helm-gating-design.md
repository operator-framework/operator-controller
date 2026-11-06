# Design: Decouple object-controller Helm rendering from featureSet/Boxcutter

Date: 2026-11-04
Size: small change

## Intent / Why

Today `helm template` **fails** when object-controller would be rendered in the
`standard` profile. The failure comes from the `objectController.enabled` helper
in `helm/olmv1/templates/_helpers.tpl`, which conflates two unrelated concerns:

1. **Generation** — whether object-controller manifests are emitted.
2. **Runtime** — whether operator-controller *uses* object-controller
   (the `BoxcutterRuntime` feature gate).

Because of this conflation, enabling `BoxcutterRuntime` implicitly turns on
object-controller generation, and a non-experimental `featureSet` then triggers
`fail "objectController requires options.featureSet=experimental"`, aborting the
whole render instead of simply producing no object-controller manifests.

We want: standard renders **nothing** for object-controller (no error),
experimental renders it, and `BoxcutterRuntime` is *only* a runtime flag.

## Goals

- Standard profile renders no object-controller resources, with no error.
- Experimental profile renders object-controller resources as today.
- `BoxcutterRuntime` becomes purely a `--feature-gates` arg on operator-controller;
  it no longer drives object-controller generation.
- object-controller remains an independent, explicit toggle (standalone deploy).
- The shared/common resources (namespace, default deny-all NetworkPolicy) render
  iff at least one component is enabled — never orphaned, never missing.

## Non-goals

- No change to `featureSet` semantics (CRD schema selection + `feature-set` label).
- No new values files (standard = chart defaults, experimental = overlay).
- No change to operator-controller / catalogd gating.

## Constraints

- object-controller CRDs only exist under `base/object-controller/crd/experimental/`
  and the CRD templates hardcode that path (no `featureSet` branch), so
  `enabled: true` renders the experimental CRDs regardless of `featureSet`.
  `.Files.Get` returns empty for missing files, so no render error is possible.
- Shell/Helm behavior must stay cross-platform (no new tooling).

## Approaches considered

- **A. Key generation off `featureSet == experimental`.** Zero new config, but
  keeps object-controller welded to the profile and removes the independent toggle.
- **B. Explicit `objectController.enabled` knob, defaulted per overlay (chosen).**
  `enabled: false` default, `enabled: true` in the experimental overlay. Keeps a
  standalone toggle and cleanly separates generation from the Boxcutter runtime flag.
- Rejected a dedicated `commonResources.enabled` variable for the shared resources
  in favour of gating them on "at least one component enabled" — same outcome with
  no new surface area.

## The change

### 1. `objectController.enabled` helper (`helm/olmv1/templates/_helpers.tpl`)

Simplify to a plain boolean gate:

- Remove the `null -> $boxcutterEnabled` default.
- Remove the `$boxcutterEnabled && enabled==false -> fail` guard.
- Remove the `featureSet != experimental -> fail` guard.
- Also drop the now-dead BoxcutterRuntime both-lists conflict `fail` from this helper
  (it is about runtime gate config, not object-controller generation).

Result: returns `true` when `options.objectController.enabled` is true, empty otherwise.

### 2. Values

- `helm/olmv1/values.yaml`: set `options.objectController.enabled: false`
  (replacing the `null` default and its Boxcutter-follows comment).
- `helm/experimental.yaml`: add `options.objectController.enabled: true`.

### 3. Common resources gate

Guard both `helm/olmv1/templates/namespace.yml` and
`helm/olmv1/templates/networkpolicy/networkpolicy-olmv1-system-default-deny-all-traffic.yml`
with three plain boolean reads (symmetric; the helper is now a dumb passthrough):

```
{{- if or .Values.options.catalogd.enabled .Values.options.operatorController.enabled .Values.options.objectController.enabled -}}
...
{{- end -}}
```

So they render whenever any component is on, and are omitted when nothing is enabled
(e.g. a standalone object-controller render with `enabled: false`).

## Net effect

- `make manifests` -> `standard.yaml`: no object-controller resources, no error.
- `make manifests` -> `experimental.yaml`: object-controller rendered (overlay).
- Namespace + deny-all render iff >=1 component enabled.
- `BoxcutterRuntime` only toggles `--feature-gates=BoxcutterRuntime=true/false`.

## Verification

- `make manifests` succeeds; diff `manifests/standard.yaml` (object-controller gone)
  and `manifests/experimental.yaml` (unchanged content for object-controller).
- `helm template olmv1 helm/olmv1` (standard) succeeds with no object-controller docs.
- `helm template olmv1 helm/olmv1 --values helm/experimental.yaml` renders them.
- Standalone: operator-controller + catalogd disabled, objectController.enabled=false
  -> no namespace / deny-all emitted.

## Left to the builder

- Exact comment wording in `values.yaml`.
- Whether `make manifests` surfaces any unrelated regenerated diffs to commit.
