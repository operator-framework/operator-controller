# Design: Single-Tenant Simplification

**Status:** Draft
**Date:** 2026-03-05

## Summary

This design document proposes a set of changes to OLM v1 that re-affirm its single-tenant, cluster-admin-only operational model. Over time, several multi-tenancy concepts have crept into OLM v1's API surface and implementation despite the [explicit decision](../../project/olmv1_design_decisions.md) to not support multi-tenancy. This proposal removes those vestiges, simplifies the user experience, and strengthens the security posture of OLM v1.

The three themes of this proposal are:

1. **Re-affirm that multi-tenancy is not supported.** OLM v1 APIs are cluster-admin-only APIs.
2. **Remove multi-tenancy artifacts from the API and implementation.** Deprecate the service account field, remove SingleNamespace/OwnNamespace install mode support, and automate namespace management.
3. **Clarify security expectations in documentation.** Cluster-admins must not delegate ClusterExtension or ClusterCatalog creation to non-cluster-admin users.

## Motivation

### Service account complexity

The current `ClusterExtension.spec.serviceAccount` field requires cluster-admins to derive, create, and maintain a purpose-built ServiceAccount with precisely scoped RBAC for every extension they install. This process is documented in the [derive-service-account guide](../../howto/derive-service-account.md), which itself acknowledges the complexity:

> We understand that manually determining the minimum RBAC required for installation/upgrade of a `ClusterExtension` is quite complex and protracted.

This design exists because OLM v1 was originally designed to not run as cluster-admin itself. The intent was to prevent OLM from becoming a privilege escalation vector — a real problem in OLM v0 where any user who could create a Subscription effectively had cluster-admin access.

However, the ServiceAccount-per-extension model has proven to be a poor fit for a system that only cluster-admins should interact with:

- **It solves the wrong problem.** The privilege escalation risk in OLM v0 existed because non-cluster-admins could trigger installations. If only cluster-admins can create ClusterExtensions (which is already a requirement), the risk is eliminated regardless of which service account performs the installation.
- **It creates an enormous usability burden.** Cluster-admins must read bundle contents, understand OLM's internal object naming schemes, and iteratively grant permissions. This is the single largest source of friction in OLM v1 adoption.
- **It provides a false sense of scoped security.** The ServiceAccount field may lead cluster-admins to believe they can safely delegate ClusterExtension creation to non-admin users, expecting that the delegated user is constrained to only using ServiceAccounts with a subset of their own permissions. In reality, a ClusterExtension writer can reference any ServiceAccount in any namespace on the cluster. Even if the writer has minimal privileges and cannot create a privileged ServiceAccount on their own, they can trivially reference an existing privileged ServiceAccount — one created by another team for a different extension, or a default SA with elevated permissions — to achieve cluster-admin directly or to trampoline into further escalation. The ServiceAccount field was designed to constrain OLM's power, but it actually hands that power to whoever can write a ClusterExtension. Meanwhile, most extensions require near-cluster-admin level permissions anyway (CRDs, cluster-scoped RBAC, webhooks, etc.), and the recommended workaround for testing is to bind `cluster-admin`.

### Watch namespace configuration

The `SingleOwnNamespaceInstallSupport` feature gate (currently GA, default enabled) allows operators to be installed in SingleNamespace or OwnNamespace mode. This feature exists solely for backwards compatibility with OLM v0 bundles, but contradicts the [core design decision](../../project/olmv1_design_decisions.md#watched-namespaces-cannot-be-configured-in-a-first-class-api) that OLM v1 will not configure watched namespaces:

> Kubernetes APIs are global. Kubernetes is designed with the assumption that a controller WILL reconcile an object no matter where it is in the cluster.

Supporting SingleNamespace and OwnNamespace modes creates:
- Operational confusion when CRDs (which are cluster-scoped) conflict between installations.
- A false expectation that multiple installations of the same operator are supported.
- Unnecessary complexity in the rendering and validation pipeline.

### Namespace management

The current model requires the installation namespace to pre-exist and contain the specified ServiceAccount. This creates unnecessary manual steps and prevents OLM from leveraging CSV-provided namespace metadata (suggested-namespace annotations) that bundle authors have already defined.

## Benefits

Beyond simplifying the current user experience, these changes unlock future capabilities that were previously impossible or impractical due to multi-tenancy constraints.

### Simplified user experience

- Cluster-admins no longer need to derive, create, and maintain per-extension ServiceAccounts with precisely scoped RBAC. Installing an extension becomes: create a ClusterExtension resource and (optionally) specify a namespace.
- Fewer feature gates and less code to maintain, reducing the surface area for bugs and confusion.
- A clearer, more auditable security boundary: who can create ClusterExtension resources, rather than what each ServiceAccount is allowed to do.
- Alignment with how OLM v1 is already deployed in practice (most users bind cluster-admin to the installer ServiceAccount anyway).

### Dependency discovery and reporting

With all operators guaranteed to watch all namespaces, dependency relationships between operators can be cleanly discovered, assessed, and reported. In the current model, an operator installed in SingleNamespace mode may or may not satisfy a dependency depending on which namespace it watches — a fact OLM cannot reliably determine. With AllNamespaces as the only mode, if an API's CRD exists on the cluster and a controller is installed for it, the dependency is satisfied. Period.

### Improved resolver diagnostics

In OLM v0, the dependency resolver had to be careful not to leak information in Subscription status messages about operators installed in other namespaces, because that could violate tenant isolation expectations. With multi-tenancy explicitly off the table, a future dependency resolver can provide rich, detailed diagnostic messages when resolution fails — including which installed operators were considered, why they didn't satisfy constraints, and what the user can do to fix the situation — without worrying about cross-namespace information leaks.

### Cluster-state-aware configuration templating

In OLM v0, the configuration engine could not plumb arbitrary resource contents into templates because reading resources from other namespaces could leak data across tenant boundaries. With a single-tenant model and cluster-admin permissions, a future configuration templating engine can safely query arbitrary cluster state — infrastructure node counts, available storage classes, cluster version, installed CRDs — and use that information to generate context-aware default configurations for extensions.

## Proposal

### 1. Deprecate and ignore `ClusterExtension.spec.serviceAccount`

**API change:**
- Mark `spec.serviceAccount` as deprecated in the OpenAPI schema. It remains in the API for backwards compatibility but is ignored by the controller.
- The field should eventually be removed in a future API version.

**Behavior change:**
- operator-controller's own ServiceAccount is granted `cluster-admin` via a ClusterRoleBinding, deployed as part of the operator-controller installation manifests.
- operator-controller uses its own ServiceAccount for all Kubernetes API interactions when managing ClusterExtension resources (creating, updating, deleting managed objects).
- The `PreflightPermissions` feature gate and the preflight permissions checking logic become unnecessary and should be removed.
- The `SyntheticPermissions` feature gate and synthetic user permission model become unnecessary and should be removed.

**Migration:**
- Existing ClusterExtensions that specify `spec.serviceAccount` continue to function — the field is simply ignored.
- The ServiceAccount, ClusterRole, ClusterRoleBinding, Role, and RoleBinding resources that were created for the installer ServiceAccount can be cleaned up by the cluster-admin at their convenience. OLM will not delete them.

**Documentation impact:**
- The [derive-service-account guide](../../howto/derive-service-account.md) should be archived/removed.
- The [permission model concept doc](../../concepts/permission-model.md) should be rewritten.
- The [preflight permissions check guide](../howto/rbac-permissions-checking.md) should be archived/removed.
- The [synthetic permissions guide](../howto/use-synthetic-permissions.md) should be archived/removed.
- Tutorials and getting-started guides should be simplified to remove ServiceAccount creation steps.

### 2. Remove SingleNamespace and OwnNamespace install mode support

**Behavior change:**
- Remove the `SingleOwnNamespaceInstallSupport` feature gate.
- operator-controller stamps out ALL registry+v1 bundle installations such that they are configured to watch all namespaces, regardless of the `installModes` declared in the CSV.
- The `spec.config.inline.watchNamespace` configuration option is no longer accepted and should cause a validation error.
- If a CSV only declares support for `SingleNamespace` and/or `OwnNamespace` (and not `AllNamespaces`), OLM v1 installs it in AllNamespaces mode anyway. OLM v1 takes the position that watching all namespaces is always correct for a cluster-scoped controller installation.

**Rationale:**
- This aligns with the [design decision](../../project/olmv1_design_decisions.md#watched-namespaces-cannot-be-configured-in-a-first-class-api) that OLM v1 will not configure watched namespaces.
- Operators that genuinely cannot function when watching all namespaces are rare and would need to be updated by their authors.
- The `installModes` concept is an OLM v0 artifact that will not exist in future bundle format versions.

**Documentation impact:**
- The [SingleNamespace/OwnNamespace install guide](../howto/single-ownnamespace-install.md) should be archived/removed.
- The [limitations doc](../../project/olmv1_limitations.md) should be updated to remove the note about SingleNamespace/OwnNamespace support.

### 3. Change `ClusterExtension.spec.namespace` to optional with automatic namespace management

**API change:**
- `spec.namespace` becomes optional (currently required).
- The immutability constraint on `spec.namespace` is retained.

**Behavior change — namespace determination:**

operator-controller determines the installation namespace using the following precedence (highest to lowest):

1. **`ClusterExtension.spec.namespace`** — If specified by the user, this is the namespace name used.
2. **`operatorframework.io/suggested-namespace` CSV annotation** — If the CSV provides a suggested namespace name, it is used.
3. **`<packageName>-system` fallback** — If neither of the above is present, operator-controller generates a namespace name from the package name.

**Behavior change — namespace body/template:**

- If the CSV contains the `operatorframework.io/suggested-namespace-template` annotation, its value (a full JSON Namespace object) is used as the template for creating the namespace. This allows bundle authors to specify labels, annotations, and other namespace metadata.
- If `spec.namespace` specifies a different name than what appears in the template, the template body is still used but with the name overridden to match `spec.namespace`.
- If no template annotation is present, operator-controller creates a plain namespace with the determined name.

**Behavior change — namespace lifecycle:**

- The installation namespace is a **managed object** of the ClusterExtension. It follows the same ownership rules as all other managed objects:
  - It is created by operator-controller if it does not exist.
  - A pre-existing namespace results in a conflict error, consistent with the [single-owner objects](../../concepts/single-owner-objects.md) design. This ensures that managed resources are not accidentally adopted or clobbered.
  - It is deleted when the ClusterExtension is deleted (along with all other managed objects).

**Migration:**
- Existing ClusterExtensions that specify `spec.namespace` continue to function identically.
- For existing installations where the namespace was manually created before the ClusterExtension, operator-controller should adopt the namespace during the migration period (one-time reconciliation to add ownership metadata). The pre-existence error applies only to new installations going forward.

### 4. Restrict API access: ClusterExtension and ClusterCatalog are cluster-admin-only

**No API or behavior changes.** This is a documentation and guidance change.

**Key points to document:**

- `ClusterExtension` and `ClusterCatalog` are **cluster-admin-only APIs**. Cluster-admins MUST NOT create RBAC that grants non-cluster-admin users the ability to create, update, or delete these resources.
- **Rationale:** ClusterExtension enables the creation and manipulation of any Kubernetes object on the cluster (CRDs, RBAC, Deployments, webhooks, etc.). Granting a non-cluster-admin user access to create ClusterExtensions is equivalent to granting them cluster-admin, making it a privilege escalation vector.
- **ClusterCatalog** has a similar risk profile: catalogs determine what content is available for installation, and a malicious catalog could provide bundles containing arbitrary cluster-scoped resources.
- This is not a new restriction — it has always been the intent — but it must be stated explicitly and prominently rather than implied.

**Documentation impact:**
- Add a security considerations section to the main documentation.
- Update the [design decisions doc](../../project/olmv1_design_decisions.md#make-olm-secure-by-default) to clarify that the security model relies on restricting who can create ClusterExtensions, not on restricting what OLM can do once a ClusterExtension is created.
- Add warnings to the API reference documentation for ClusterExtension and ClusterCatalog.

## Impact on existing features and feature gates

| Feature / Feature Gate | Current State | Proposed State |
|---|---|---|
| `spec.serviceAccount` | Required field | Deprecated, ignored |
| `PreflightPermissions` | Alpha (default off) | Remove |
| `SyntheticPermissions` | Alpha (default off) | Remove |
| `SingleOwnNamespaceInstallSupport` | GA (default on) | Remove |
| `spec.namespace` | Required field | Optional field |
| `spec.config.inline.watchNamespace` | Accepted config | Validation error |
| operator-controller ClusterRoleBinding | Not cluster-admin | cluster-admin |

## Security analysis

### Current model

- operator-controller runs without cluster-admin.
- Each ClusterExtension specifies a ServiceAccount scoped to the minimum permissions needed.
- In practice, most ServiceAccounts are bound to cluster-admin because deriving minimal RBAC is impractical.
- The security boundary is: "What can this ServiceAccount do?"

### Proposed model

- operator-controller runs as cluster-admin.
- The security boundary shifts to: "Who can create ClusterExtension and ClusterCatalog resources?"
- This is a simpler, more auditable, and more correct security boundary:
  - It is binary: either you can create these resources or you cannot.
  - It aligns with how Kubernetes RBAC is designed to work.
  - It does not rely on cluster-admins correctly deriving complex RBAC for every extension.
  - It matches the reality of how OLM v1 is already deployed in most environments.

### Risk: compromise of operator-controller

With cluster-admin, a compromised operator-controller pod is a more severe risk. Mitigations:

- operator-controller already has significant privileges in practice (most deployments use cluster-admin ServiceAccounts).
- operator-controller is a trusted cluster component, deployed by the cluster provider/administrator.
- Standard Kubernetes security practices apply: network policies, pod security, image provenance, etc.
- This is the same trust model as kube-controller-manager, kube-scheduler, and other core control plane components.

## Rollout

This proposal involves breaking changes and should be rolled out in two phases:

1. **Phase 1 — Behavior change:** operator-controller is granted cluster-admin and begins using its own ServiceAccount for all API interactions. `spec.serviceAccount` is marked deprecated and ignored. `spec.namespace` becomes optional with automatic namespace management. SingleNamespace/OwnNamespace support is removed. The `PreflightPermissions`, `SyntheticPermissions`, and `SingleOwnNamespaceInstallSupport` feature gates and their associated code are removed. Documentation is updated to reflect all changes.

    No existing ClusterExtension installations will break — operator-controller's cluster-admin ServiceAccount is a strict superset of any permissions that a user-configured ServiceAccount would have had. The only impact is on security-conscious users who relied on the limited permissions of the configured ServiceAccount to constrain what OLM could do on behalf of a given ClusterExtension. For those users, the security boundary shifts from per-extension ServiceAccount RBAC to controlling who can create ClusterExtension resources.

2. **Phase 2 — API cleanup:** Remove `spec.serviceAccount` from the API in a future API version.

## Alternatives considered

### Keep ServiceAccount but automate its creation

An alternative would be to keep the ServiceAccount model but have operator-controller or a CLI tool automatically create and maintain the ServiceAccount with the correct RBAC.

**Rejected because:** This adds complexity to solve a problem that doesn't need to exist. If only cluster-admins create ClusterExtensions, the ServiceAccount is just an unnecessary indirection. Auto-generating it means operator-controller needs the permissions to create RBAC anyway, which is effectively cluster-admin.

### Allow AllNamespaces + SingleNamespace as a user choice

An alternative would be to continue allowing the user to choose between AllNamespaces and SingleNamespace modes.

**Rejected because:** This contradicts the core design principle that APIs are global in Kubernetes. It creates a false sense of isolation and introduces complexity for a use case (multi-tenancy) that OLM v1 explicitly does not support.

### Make ClusterExtension namespace-scoped to enable delegation

An alternative would be to create a namespace-scoped `Extension` API that could be safely delegated to non-cluster-admins.

**Rejected because:** The content installed by an extension (CRDs, cluster RBAC, webhooks) is inherently cluster-scoped. A namespace-scoped API would either need to be severely restricted in what it can install (making it useless for most operators) or would still be a privilege escalation vector.

## Work items

| # | Work item | Depends on |
|---|-----------|-----------|
| [01](single-tenant-simplification/01-cluster-admin.md) | Grant operator-controller cluster-admin | — |
| [02](single-tenant-simplification/02-deprecate-service-account.md) | Deprecate and ignore spec.serviceAccount | 01 |
| [03](single-tenant-simplification/03-remove-preflight-permissions.md) | Remove PreflightPermissions feature gate and code | — |
| [04](single-tenant-simplification/04-remove-synthetic-permissions.md) | Remove SyntheticPermissions feature gate and code | — |
| [05](single-tenant-simplification/05-remove-single-own-namespace.md) | Remove SingleNamespace/OwnNamespace install mode support | — |
| [06](single-tenant-simplification/06-optional-namespace.md) | Make spec.namespace optional with automatic namespace management | 02 |
| [07](single-tenant-simplification/07-simplify-contentmanager.md) | Simplify contentmanager to a single set of informers | 02 |
| [09](single-tenant-simplification/09-documentation.md) | Documentation updates | all |
