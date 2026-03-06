# Work Item: Documentation updates

**Parent:** [Single-Tenant Simplification](../single-tenant-simplification.md)
**Status:** Not started
**Depends on:** All other work items

## Summary

Update documentation to reflect the single-tenant simplification changes. Archive obsolete guides, rewrite affected concept docs, and add explicit security guidance.

## Scope

### Archive / remove
- `docs/howto/derive-service-account.md` - No longer needed
- `docs/draft/howto/rbac-permissions-checking.md` - PreflightPermissions removed
- `docs/draft/howto/use-synthetic-permissions.md` - SyntheticPermissions removed
- `docs/draft/howto/single-ownnamespace-install.md` - SingleNamespace/OwnNamespace removed

### Rewrite
- `docs/concepts/permission-model.md` - Rewrite to describe the new model: operator-controller runs as cluster-admin, security boundary is who can create ClusterExtension/ClusterCatalog resources.
- Tutorials and getting-started guides - Remove ServiceAccount creation steps from installation workflows.

### New content
- Add a security considerations section to the main documentation:
  - ClusterExtension and ClusterCatalog are cluster-admin-only APIs.
  - Cluster-admins must not create RBAC granting non-admin users create/update/delete on these resources.
  - Rationale: creating a ClusterExtension is equivalent to cluster-admin.
- Add warnings to the API reference documentation for ClusterExtension and ClusterCatalog.

### Update
- `docs/project/olmv1_design_decisions.md` - Clarify that the security model relies on restricting who can create ClusterExtensions, not on restricting what OLM can do.
- `docs/project/olmv1_limitations.md` - Remove the note about SingleNamespace/OwnNamespace support.
