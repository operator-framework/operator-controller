# Work Item: Grant operator-controller cluster-admin

**Parent:** [Single-Tenant Simplification](../single-tenant-simplification.md)
**Status:** Not started
**Depends on:** Nothing (first work item)

## Summary

Grant operator-controller's ServiceAccount `cluster-admin` privileges via its Helm-managed ClusterRoleBinding. This is a prerequisite for all other simplification work items: once operator-controller has cluster-admin, per-extension service account scoping becomes unnecessary.

## Current RBAC architecture

The Helm chart defines RBAC in `helm/olmv1/templates/rbac/clusterrole-operator-controller-manager-role.yml`. The ClusterRole is structured in two tiers, conditional on the `BoxcutterRuntime` feature gate:

### When BoxcutterRuntime is NOT enabled (line 1)

The entire ClusterRole is gated behind `{{- if and .Values.options.operatorController.enabled (not (has "BoxcutterRuntime" .Values.operatorConrollerFeatures)) }}`. It grants:

- `serviceaccounts/token` create and `serviceaccounts` get — for per-CE SA impersonation
- `customresourcedefinitions` get
- `clustercatalogs` get/list/watch
- `clusterextensions` get/list/patch/update/watch, plus finalizers and status updates
- `clusterrolebindings`, `clusterroles`, `rolebindings`, `roles` list/watch — for `PreflightPermissions` RBAC validation
- OpenShift SCC `use` (conditional on `.Values.options.openshift.enabled`)

### When BoxcutterRuntime IS enabled (lines 81-113)

Additional rules are appended within the same ClusterRole:

- `*/*` list/watch — broad permissions needed for the Boxcutter tracking cache
- `clusterextensionrevisions` full CRUD, status, and finalizers

### When BoxcutterRuntime is enabled but the outer guard is false

If BoxcutterRuntime is in `.Values.operatorConrollerFeatures`, the outer guard on line 1 is false, so the entire ClusterRole (including the Boxcutter-specific rules) is not rendered. This suggests the Boxcutter path uses a different RBAC mechanism or that there is a separate ClusterRole for that case.

## Proposed changes

### Helm RBAC

- Replace the conditionally-scoped ClusterRole with a `cluster-admin` ClusterRoleBinding (binding operator-controller's ServiceAccount to the built-in `cluster-admin` ClusterRole).
- Remove the conditional RBAC templating based on `BoxcutterRuntime`. With cluster-admin, the feature-gate-conditional rules are all subsumed.
- Remove the `serviceaccounts/token` create and `serviceaccounts` get rules (no longer needed since operator-controller uses its own identity).
- Remove the RBAC list/watch rules that existed for `PreflightPermissions` validation.
- The OpenShift SCC `use` permission is also subsumed by cluster-admin.

### Key files

- `helm/olmv1/templates/rbac/clusterrole-operator-controller-manager-role.yml` — Replace with cluster-admin ClusterRoleBinding
- Any other Helm RBAC templates that may need updating

## Notes

- There is a typo in the Helm template: `.Values.operatorConrollerFeatures` (missing "t" in "Controller"). This can be fixed as part of this work or separately.
- The cluster-admin grant is intentionally broad. The security boundary shifts from "what can operator-controller do" to "who can create ClusterExtension/ClusterCatalog resources." See the [security analysis](../single-tenant-simplification.md#security-analysis) in the design doc.
