
# Granting Users Access to API Resources in OLM

When cluster extensions are managed via OLM, they often provide Custom Resource Definitions (CRDs) that expose new API resources. Typically, cluster administrators have full management access to these resources by default, whereas non-administrative users might lack sufficient permissions. Cluster administrators must create the needed permissions to create, view, or edit these Custom Resources for these users.

OLM v1 does **not** automatically configure or manage role-based access control (RBAC) for users to interact with the APIs provided by installed packages. Cluster administrators must manage RBAC to grant appropriate permissions to non-administrative users. This guide outlines the steps to manually configure RBAC, with a focus on creating ClusterRoles and binding them to specific users or groups.

---

## 1. Finding API Groups and Resources Provided by a ClusterExtension

To create appropriate RBAC policies, you need to know which API groups and resources are exposed by the installed cluster extension. You can inspect the installed CRDs and resources by running:

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

Administrators can define standard roles to control access to the API resources provided by installed cluster extensions. If the cluster extension does not provide default roles, you can create them yourself.

### Default Roles

- **View ClusterRole**: Grants read-only access to all custom resource objects of specified API resources across the cluster. This role is intended for users who need visibility into the resources without any permissions to modify them. It’s ideal for monitoring purposes and limited access viewing.
- **Edit ClusterRole**: Allows users to modify all custom resource objects within the cluster. This role enables users to create, update, and delete resources, making it suitable for team members who need to manage resources but should not control RBAC or manage permissions for others.
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

!!! note
    The `'*'` in verbs allows all actions on the specified resources.
    In each case, replace `<your-api-group>` and `<your-custom-resources>` with the actual API group and resource names provided by the installed cluster extension.

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
