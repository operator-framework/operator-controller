#### OLMv1 Permission Model

OLM v1's permission model is built on a simple principle: **operator-controller runs as cluster-admin**, and the security boundary is defined by who can create and modify `ClusterExtension` and `ClusterCatalog` resources.

#### How It Works

1. **operator-controller** runs with `cluster-admin` privileges, giving it full authority to install and manage extension lifecycle resources (CRDs, Deployments, RBAC, etc.) on behalf of `ClusterExtension` objects.
2. **Security is enforced via RBAC on the OLM APIs themselves.** Only cluster administrators should have `create`, `update`, or `delete` permissions on `ClusterExtension` and `ClusterCatalog` resources.
3. **Creating a `ClusterExtension` is equivalent to having cluster-admin privileges**, because it instructs operator-controller to install arbitrary workloads and RBAC. Cluster administrators must not grant non-admin users write access to these APIs.

#### Security Considerations

!!! warning
    `ClusterExtension` and `ClusterCatalog` are **cluster-admin-only APIs**. Granting non-admin users
    `create`, `update`, or `delete` access on these resources is equivalent to granting them cluster-admin
    privileges.

- Cluster admins must audit RBAC to ensure only trusted users can manage `ClusterExtension` and `ClusterCatalog` resources.
- The rationale for this model is explained in the [Design Decisions](../project/olmv1_design_decisions.md) document.

#### Extension Resources

operator-controller installs and manages all resources declared in an extension's bundle, including any ServiceAccounts, RBAC, Deployments, and other Kubernetes objects. These resources are distinct from operator-controller's own service account and are scoped to whatever the bundle declares.
