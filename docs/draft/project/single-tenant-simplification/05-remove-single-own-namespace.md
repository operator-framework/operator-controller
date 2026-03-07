# Work Item: Remove SingleNamespace/OwnNamespace install mode support

**Parent:** [Single-Tenant Simplification](../single-tenant-simplification.md)
**Status:** Not started
**Depends on:** Nothing (independent)

## Summary

Remove the `SingleOwnNamespaceInstallSupport` feature gate (currently GA, default on) and all associated code. Operators are always installed in AllNamespaces mode. The `watchNamespace` configuration option is removed from the bundle config schema.

## Scope

### Remove feature gate

- Remove the `SingleOwnNamespaceInstallSupport` feature gate definition from `internal/operator-controller/features/features.go:35-39`.
- Remove all references to `IsSingleOwnNamespaceEnabled` / `SingleOwnNamespaceInstallSupport` throughout the codebase.

### Remove watchNamespace from the config schema

- Remove the `watchNamespace` property from the bundle config JSON schema (`internal/operator-controller/rukpak/bundle/registryv1bundleconfig.json`).
- Remove `GetWatchNamespace()` from `internal/operator-controller/config/config.go:88-106`.
- Remove the conditional block in `internal/operator-controller/applier/provider.go:73-79` that extracts `watchNamespace` and passes it to the renderer via `render.WithTargetNamespaces()`.
- Remove related tests in `internal/operator-controller/config/config_test.go` and `internal/operator-controller/applier/provider_test.go`.

With `watchNamespace` removed from the schema, any ClusterExtension that specifies `spec.config.inline.watchNamespace` will fail schema validation automatically — no explicit validation error needs to be added.

### Update install mode handling

- If a CSV only declares support for `SingleNamespace` and/or `OwnNamespace` (and not `AllNamespaces`), OLM v1 installs it in AllNamespaces mode anyway. OLM v1 takes the position that watching all namespaces is always correct for a cluster-scoped controller installation.
- Remove the `render.WithTargetNamespaces()` option and related rendering logic that generates namespace-scoped configurations.

## Key files

- `internal/operator-controller/features/features.go` — Feature gate definition
- `internal/operator-controller/config/config.go` — `GetWatchNamespace()` method
- `internal/operator-controller/applier/provider.go` — Conditional `IsSingleOwnNamespaceEnabled` block
- `internal/operator-controller/rukpak/bundle/registryv1bundleconfig.json` — Bundle config schema
- `internal/operator-controller/rukpak/bundle/registryv1.go` — Bundle config schema handling
- `hack/tools/schema-generator/main.go` — Schema generator
- `docs/draft/howto/single-ownnamespace-install.md` — To be archived/removed (see [09-documentation](09-documentation.md))
