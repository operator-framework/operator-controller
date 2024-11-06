
# Granting Users Access to API Resources in OLM

When operators or cluster extensions are managed via OLM, they often provide Custom Resource Definitions (CRDs) that expose new API resources. Typically, cluster administrators hold full management access to these resources by default, whereas non-administrative users may lack sufficient permissions. Such users often need designated permissions to create, view, or edit these Custom Resources.

OLM does **not** automatically configure or manage RBAC for users to interact with the APIs provided by installed packages. It is recommended that cluster administrators manage RBAC (Role-Based Access Control) to grant appropriate permissions to non-administrative users. This guide outlines the steps to manually configure RBAC, with a focus on creating ClusterRoles and binding them to specific users or groups.

---

## 1. Finding API Groups and Resources Provided by a ClusterExtension

To create appropriate RBAC policies, you need to know which API groups and resources are exposed by the installed operator. You can inspect the installed CRDs and resources by running:

```bash
kubectl get crds
```

This will list all available CRDs, and you can inspect individual CRDs for their API groups:

```bash
kubectl get crd <crd-name> -o yaml
```

A user can use label selectors to find CRDs owned by a specific cluster extension:

```bash
kubectl get crds -l 'olm.operatorframework.io/owner-kind=ClusterExtension,olm.operatorframework.io/owner-name=<clusterExtensionName>'
```

---

## 2. Creating Default ClusterRoles for API/CRD Access

Administrators can define standard roles to control access to the API resources provided by installed operators. If the operator does not provide default roles, you can create them yourself.

### Default Roles

- **View ClusterRole**: Grants read-only access to all custom resource objects of specified API resources across the cluster.
- **Edit ClusterRole**: Allows modifying all custom resource objects within the cluster.
- **Admin ClusterRole**: Provides full permissions (create, update, delete) over all custom resource objects for the specified API resources across the cluster.

### Example: Defining a Custom "View" ClusterRole

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: custom-resource-view
rules:
- apiGroups:
  - <your-api-group>
  resources:
  - <your-custom-resources>
  verbs:
  - get
  - list
  - watch
```

### Example: Defining a Custom "Edit" ClusterRole

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: custom-resource-edit
rules:
- apiGroups:
  - <your-api-group>
  resources:
  - <your-custom-resources>
  verbs:
  - get
  - list
  - watch
  - create
  - update
  - patch
  - delete 
```

### Example: Defining a Custom "Admin" ClusterRole

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: custom-resource-admin
rules:
- apiGroups:
  - <your-api-group>
  resources:
  - <your-custom-resources>
  verbs:
  - '*'
```

In each case, replace `<your-api-group>` and `<your-custom-resources>` with the actual API group and resource names provided by the installed operator.

---

## 3. Granting User Access to API Resources

Once the roles are created, you can bind them to specific users or groups to grant them the necessary permissions. There are two main ways to do this:

### Option 1: Binding Default ClusterRoles to Users

- **ClusterRoleBinding**: Use this to grant access across all namespaces.
- **RoleBinding**: Use this to grant access within a specific namespace.

#### Example: ClusterRoleBinding

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: custom-resource-view-binding
subjects:
- kind: User
  name: <username> # Or use Group for group-based binding
roleRef:
  kind: ClusterRole
  name: custom-resource-view
  apiGroup: rbac.authorization.k8s.io
```

This binding grants `<username>` read-only access to the custom resource across all namespaces.

#### Example: RoleBinding

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: custom-resource-edit-binding
  namespace: <namespace>
subjects:
- kind: User
  name: <username>
roleRef:
  kind: Role
  name: custom-resource-edit
  apiGroup: rbac.authorization.k8s.io
```

This RoleBinding restricts permissions to a specific namespace.

### Option 2: Extending Default Kubernetes Roles

To automatically extend existing Kubernetes roles (e.g., the default `view`, `edit`, and `admin` roles), you can add **aggregation labels** to **ClusterRoles**. This allows users who already have `view`, `edit`, or `admin` roles to interact with the custom resource without needing additional RoleBindings.

#### Example: Adding Aggregation Labels to a ClusterRole

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: custom-resource-aggregated-view
  labels:
    rbac.authorization.k8s.io/aggregate-to-view: "true"
rules:
  - apiGroups:
      - <your-api-group>
    resources:
      - <your-custom-resource>
    verbs:
      - get
      - list
      - watch
```

You can create similar ClusterRoles for `edit` and `admin` with appropriate verbs (such as `create`, `update`, `delete` for `edit` and `admin`). By using aggregation labels, the permissions for the custom resources are added to the default roles.

> **Source**: [Kubernetes RBAC Aggregation](https://kubernetes.io/docs/reference/access-authn-authz/rbac/#default-roles-and-role-bindings)

---

## Notes

- OLM does not handle RBAC for users interacting with CRDs, so it's up to cluster administrators to configure these settings.
- It is not recommended for operator bundles to include RBAC policies granting access to the operator's APIs because cluster administrators should maintain control over the permissions in their clusters.

---
