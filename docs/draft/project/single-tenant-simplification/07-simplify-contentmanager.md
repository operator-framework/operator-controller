# Work Item: Simplify contentmanager to a single set of informers

**Parent:** [Single-Tenant Simplification](../single-tenant-simplification.md)
**Status:** Not started
**Depends on:** [02-deprecate-service-account](02-deprecate-service-account.md)

## Summary

The contentmanager currently creates a separate `DynamicSharedInformerFactory`, dynamic client, and set of informers for each ClusterExtension. This per-CE isolation exists for two reasons:

1. **Per-CE service account credentials** - Each CE's informers use a REST config scoped to that CE's service account (via `RestConfigMapper`).
2. **Per-CE label selector** - Each factory filters with `OwnerKind=ClusterExtension, OwnerName=<ce-name>`.

With single-tenant simplification, reason (1) disappears entirely: operator-controller uses its own identity for everything. This means a single dynamic client and a single set of informers can serve all ClusterExtensions, using a broader label selector that matches all operator-controller-managed objects (e.g., `OwnerKind=ClusterExtension` without filtering by name).

## Current architecture

- `contentmanager.go:40` - `caches map[string]cmcache.Cache` maps CE name to a per-CE cache
- `contentmanager.go:99` - `i.rcm(ctx, ce, i.baseCfg)` creates a per-CE REST config via the service account mapper
- `contentmanager.go:104` - Creates a per-CE `dynamic.Client` from that config
- `contentmanager.go:109-122` - Each factory uses label selector `OwnerKind=ClusterExtension, OwnerName=<ce-name>`
- `contentmanager.go:119-123` - A new `DynamicSharedInformerFactory` is created per CE (and per-GVK within a CE)
- `cache/cache.go:51-57` - Each cache tracks `map[GVK]CloserSyncingSource` per CE

## Proposed simplification

- Remove the `RestConfigMapper` and per-CE client creation.
- Use a single dynamic client with operator-controller's own credentials.
- Use a single `DynamicSharedInformerFactory` (or a single set of informers) with a label selector matching all managed objects: `OwnerKind=ClusterExtension`.
- The `Manager` interface simplifies: no longer needs `Get(ctx, *ClusterExtension)` keyed by CE. Instead, a single cache watches all managed GVKs.
- Event routing to the correct ClusterExtension reconciliation is already handled by the `EnqueueRequestForOwner` handler in the sourcerer (`sourcerer.go:42`), which reads owner references from the objects.

## Key files

- `internal/operator-controller/contentmanager/contentmanager.go` - Manager with per-CE cache map
- `internal/operator-controller/contentmanager/cache/cache.go` - Per-CE cache with GVK-to-source map
- `internal/operator-controller/contentmanager/sourcerer.go` - Creates sources with per-CE label selectors and schemes
- `internal/operator-controller/contentmanager/source/dynamicsource.go` - Dynamic informer source
- `internal/operator-controller/applier/helm.go:69` - Helm applier's `Manager` field
- `cmd/operator-controller/main.go:727` - ContentManager creation with `clientRestConfigMapper`

## Notes

- This work item only applies if the Helm applier path is still in use. If the Boxcutter runtime fully replaces Helm before this work begins, the contentmanager can simply be deleted instead of simplified (Boxcutter does not use contentmanager at all; see `main.go:588-593`).
