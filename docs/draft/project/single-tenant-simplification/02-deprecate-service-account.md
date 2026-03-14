# Work Item: Deprecate and ignore spec.serviceAccount

**Parent:** [Single-Tenant Simplification](../single-tenant-simplification.md)
**Status:** Not started
**Depends on:** [01-cluster-admin](01-cluster-admin.md)

## Summary

Mark `ClusterExtension.spec.serviceAccount` as deprecated in the OpenAPI schema and make the controller ignore it. operator-controller uses its own ServiceAccount for all Kubernetes API interactions when managing ClusterExtension resources.

## Scope

- Mark `spec.serviceAccount` as deprecated in the CRD/OpenAPI schema.
- Modify the controller to stop reading `spec.serviceAccount` and instead use operator-controller's own identity for all API calls.
- The `RestConfigMapper` / `clientRestConfigMapper` plumbing in `cmd/operator-controller/main.go` that creates per-CE service account-scoped clients becomes unnecessary. The Helm `ActionConfigGetter` should use operator-controller's own config.
- The field remains in the API for backward compatibility but is ignored.

## Helm applier

- The `RestConfigMapper` / `clientRestConfigMapper` plumbing in `cmd/operator-controller/main.go` that creates per-CE service account-scoped clients becomes unnecessary. The Helm `ActionConfigGetter` should use operator-controller's own config.

## Boxcutter applier

The Boxcutter applier reads `spec.serviceAccount` and propagates it through the revision lifecycle:

- `applier/boxcutter.go:208-209` — `buildClusterExtensionRevision` writes `ServiceAccountNameKey` and `ServiceAccountNamespaceKey` annotations on `ClusterExtensionRevision` objects, sourced from `ext.Spec.ServiceAccount.Name` and `ext.Spec.Namespace`.
- `controllers/revision_engine_factory.go:82-98` — `getServiceAccount` reads those annotations back from the CER.
- `controllers/revision_engine_factory.go:101-122` — `createScopedClient` creates an anonymous REST config wrapped with `TokenInjectingRoundTripper` to impersonate the SA.
- `controllers/revision_engine_factory.go:57-80` — `CreateRevisionEngine` passes the scoped client into the boxcutter `machinery.NewObjectEngine` and `machinery.NewRevisionEngine`.

With single-tenant simplification:

- Stop writing SA annotations on CERs.
- `RevisionEngineFactory` uses operator-controller's own client directly instead of creating per-CER scoped clients. The factory struct fields `BaseConfig` and `TokenGetter` become unnecessary.
- Remove `getServiceAccount` and `createScopedClient` from the factory.
- Remove the `internal/operator-controller/authentication/` package (`TokenGetter` and `TokenInjectingRoundTripper`), which is only used for SA impersonation.
- Remove `ServiceAccountNameKey` / `ServiceAccountNamespaceKey` label constants.

Note: The Boxcutter `TrackingCache` (informers) is already shared across all CERs — no informer consolidation is needed. The per-CER isolation was only at the client level for SA scoping.

## Key files

- `api/v1/clusterextension_types.go` — Mark field deprecated
- `cmd/operator-controller/main.go:697-708` — `clientRestConfigMapper` and Helm `ActionConfigGetter` setup
- `internal/operator-controller/authentication/` — `TokenGetter` / `TokenInjectingRoundTripper` (remove entirely)
- `internal/operator-controller/applier/boxcutter.go:208-209` — SA annotations written on CERs
- `internal/operator-controller/controllers/revision_engine_factory.go` — Per-CER scoped client creation
- `internal/operator-controller/labels/labels.go:29-41` — `ServiceAccountNameKey` / `ServiceAccountNamespaceKey` constants

## Migration

- Existing ClusterExtensions that specify `spec.serviceAccount` continue to function; the field is simply ignored.
- Cluster-admins can clean up the ServiceAccount, ClusterRole, ClusterRoleBinding, Role, and RoleBinding resources they previously created at their convenience.

## Notes

- Full removal of `spec.serviceAccount` from the API happens in a future API version (Phase 2 in the design doc).
