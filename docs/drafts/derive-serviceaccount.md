# Derive minimal ServiceAccount required for ClusterExtension Installation and Management

OLM v1 does not have permission to install extensions on a cluster by default. In order to install a [supported bundle](../refs/supported-extensions.md), OLM must be provided a ServiceAccount configured with the appropriate permissions. For more information, see the [provided ServiceAccount](./provided-serviceaccount.md) documentation.

This document serves as a guide for how to derive the RBAC necessary to install a bundle.

### Required RBAC

You can determine the specifics of these permissions by referencing the bundle of the ClusterExtension you want to install. The service account must have the following permissions:

1) The service account should be bound to one ClusterRole, defining the permissions for cluster-scoped resources and cluster-wide namespace scoped resources.


* Create, list, watch verbs for all resources that are a part of the install (cannot be scoped to specific resource names).
  - Permissions to create and manage CustomResourceDefinitions
  - Permissions to create any other manifest objects
  - Rules to manage any other cluster-scoped resources
  - All the rules defined in the CSV under `.spec.install.clusterPermissions`.
  - Permissions for namespace-scoped resources.
  - These are specified with ClusterRole + ClusterRoleBinding, ClusterRole + RoleBinding, or Role + RoleBinding.

* Permissions to create any ClusterRole, ClusterRoleBinding, Role, RoleBinding resources required by the extension.
  - All the same rules defined in the ClusterRole and Role resources
  - Escalate and bind verbs for ClusterRole, ClusterRoleBinding, Role, RoleBinding resources
  - Permissions to create the necessary roles and rolebindings for the controller to be able to perform its job


2) The service account should be bound to a Role to define the permissions for the service account within the installation namespace.

* Get, update, patch, delete verbs for all resources that are a part of the install (can be scoped to specific resource names)
  - Permissions for non cluster-scoped resources
  - Permissions for namespace-scoped resources
   - All the rules defined in the CSV under `.spec.install.permissions`.
  - These are created using ClusterRole + ClusterRoleBinding, ClusterRole + RoleBinding, or Role + RoleBinding

* Update finalizers on the ClusterExtension to be able to set blockOwnerDeletion and ownerReferences.

* Permissions to create the controller deployment, this corresponds to the rules to manage the
  deployment defined in the ClusterServiceVersion.

### Example to derive RBAC

As an example, consider a ClusterExtension that has the following permissions defined in the `.spec.install.clusterPermissions` and `.spec.install.permissions` as part of its ClusterServiceVersion definition.

```yml
clusterPermissions:
  - rules:
    - apiGroups: [user.openshift.io]
      resources: [users, groups, identities]
      verbs: [get, list]
    - apiGroups: [rbac.authorization.k8s.io]
      resources: [clusterroles, clusterrolebindings]
      verbs: [create]
permissions:
  - rules:
    - apiGroups: [apps]
      resources: [deployments]
      verbs: [create, delete, get, list, patch, update, watch]
    - apiGroups: [""]
      resources: [pods, services]
      verbs: [create, delete, get, list, patch, update, watch]
```

Below is the procedure to create RBAC for the above extension iteratively.

1) Begin by defining a ClusterRole with your ClusterExtension name and including the above clusterPermissions . ClusterExtensions are cluster-scoped permissions and in order to update the ClusterExtension finalizer we should create the below ClusterRole.
2) In the ClusterServiceVersion above, OpenShift users related permissions are defined which are cluster-scoped resources and are added in ClusterRole below.
3) If your ClusterServiceVersion file defines additional clusterPermissions, add them to the ClusterRole as done below.

```yml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: <cluster-extension-name>
rules:
- apiGroups: [rbac.authorization.k8s.io]
  resources: [clusterroles, clusterrolebindings]
  verbs: [create]
- apiGroups: [user.openshift.io]
  resources: [users, groups, identities]
  verbs: [get,list]
```
4) The installer will need additional permissions to manage the CustomResourceDefinitions its owns. Add the below rules to enable the installer to manage CustomResourceDefinitions for your ClusterExtension.

```yaml
- apiGroups: [apiextensions.k8s.io]
  resources: [customresourcedefinitions]
  verbs: [create, list, watch]

- apiGroups: [apiextensions.k8s.io]
  resources: [customresourcedefinitions]
  verbs: [get, update, patch, delete]
  resourceNames: [<crd name 1>, ..., <crd name n>]
```

5) The installer will need permissions to set blockOwnerDeletion ownerReferences for your ClusterExtension which corresponds to the following rules in your ClusterRole.

```yaml
- apiGroups: [olm.operatorframework.io]
  resources: [clusterextensions/finalizers]
  verbs: [update]
  resourceNames: [<cluster-extension-name>]
```

6) The ClusterExtension installer will need permissions to create clusterroles for your ClusterExtension.

```yaml
- apiGroups: [rbac.authorization.k8s.io]
  resources: [clusterroles]
  verbs: [create, list, watch]
- apiGroups: [rbac.authorization.k8s.io]
  resources: [clusterroles]
  verbs: [get, update, patch, delete]
  resourceNames: [<generated cluster role 1>, ..., <generated cluster role n>, <manifest cluster role name 1>, ..., <manifest cluster role name n>]
```

7) The ClusterExtension installer will need permissions to manage ClusterRoleBindings for your ClusterExtension.

```yaml
- apiGroups: [rbac.authorization.k8s.io]
  resources: [clusterrolebindings]
  verbs: [create, list, watch]

- apiGroups: [rbac.authorization.k8s.io]
  resources: [clusterrolebindings]
  verbs: [get, update, patch, delete]
  resourceNames: [<generated cluster role 1>, ..., <generated cluster role n>, <manifest cluster role binding name 1>, ..., <manifest cluster role binding name n>]
```

8) The ClusterExtension installer will need permissions to manage ServiceAccounts for your ClusterExtension.

```yaml
- apiGroups: [apps]
  resources: [deployments]
  verbs: [create, list, watch]

- apiGroups: [apps]
  resources: [deployments]
  verbs: [get, update, patch, delete]
  resourceNames: [<cluster-extension>-controller-manager]
```

9) The ClusterExtension installer will need permissions to manage the deployment for your ClusterExtension.

```yaml
- apiGroups: [""]
  resources: [serviceaccounts]
  verbs: [create, list, watch]

- apiGroups: [""]
  resources: [serviceaccounts]
  verbs: [get, update, patch, delete]
  resourceNames: [argocd-operator-controller-manager]
```

10) Create an ClusterRoleBinding that associates your ClusterExtension ClusterRole to its ServiceAccount:

```yml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: <cluster-extension-name>
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: <cluster-extension-name>
subjects:
- kind: ServiceAccount
  name: <cluster-extension-sa-name>
  namespace: <cluster-extension-namespace>
```

11) As a next step define the Roles required by your the ClusterExtension.
12) The `.spec.install.permissions` section in your ClusterServiceVersion corresponds to the roles required by your ClusterExtension.
13) In our example, we have permissions defined for creating, listing and watching pods and services.

```yml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: <cluster-extension-name>
  namespace: <cluster-extension-name>
rules:
- apiGroups:
  - ""
  resources: [pods, services]
  verbs: [create, delete, get, list, patch, update, watch ]
- apiGroups: [apps]
  resources: [deployments]
  verbs: [create, delete, get, list, patch, update, watch]
```
14) The installer needs permissions to manage Roles and RoleBindings for your ClusterExtension.

```yml
- apiGroups: [""]
  resources: [serviceaccounts]
  verbs: [create, get, list, patch, update, watch ]
- apiGroups: [rbac.authorization.k8s.io]
  resources: [roles, rolebindings]
  verbs: [create]
```

15) Define the the RoleBinding that associates your ClusterExtension role to its ServiceAccount:

```yml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: <cluster-extension-name>
  namespace: <cluster-extension-namespace>
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: <cluster-extension-name>
subjects:
- kind: ServiceAccount
  name: <cluster-extension-sa-name>
```

You can reference the above example to derive the minimal RBAC for your ClusterExtension. The steps above can be detailed as below:

* You can get the bundle image, unpack the same and inspect the manifests to determine the required permissions.
* Check the ClusterServiceVersion for your bundle and create ClusterRoles defined in the `.spec.install.clusterPermissions` section.
*  Check the ClusterServiceVersion for your bundle and create ClusterRoles defined in the `.spec.install.permissions` section.
* Specify permissions to manage the Deployment and ServiceAccount for your ClusterExtension.
* Specify permissions to manage the ClusterRoles and Roles for your ClusterExtension.
* Specify permissions to manage the ClusterRoleBindings and RoleBindings for your ClusterExtension.
* By allowing the installer to get, list, watch and update ClusterRoles, you can update the RBAC to include generated ClusterRole names.
* The `kubectl` cli-tool creates cluster roles with a hash in their name. You can query the newly created ClusterRole names and reduce the installer RBAC scope to have the ClusterRoles needed, this can include generated roles.
* Create the above RBAC and then iterate over the ClusterExtension failures, if any, by examining conditions and statuses. 
* Read the failure conditions and update the RBAC to include the generated cluster role names (name will be in the failure condition).
* Read the failure conditions, update the installer RBAC and iterate until you are out of errors.

Note: There are [non production tools](../hack/tools/catalogs) that can be used to help with the process.

### Creation of ClusterRoleBinding using the cluster-admin ClusterRole in non-production environments

Below is an example ClusterRoleBinding using the cluster-admin ClusterRole for non-production environments.

```yaml
# Create ClusterRole
kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: my-cluster-extension-installer-role-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
- kind: ServiceAccount
  name: my-cluster-extension-service-account
  namespace: my-cluster-extension-namespace
EOF
```

Use the below on a Kind cluster to assign cluster-admin privileges to your cluster extension

```sh
kubectl create clusterrolebinding my-cluster-extension-installer-role-binding \
  --clusterrole=cluster-admin \
  --serviceaccount=my-cluster-extension-namespace:my-cluster-installer-service-account
```

### Example ClusterExtension with RBAC

Below is an example of the ArgoCD installer with the necessary RBAC to deploy the ArgoCD ClusterExtension.

??? example
    ```yaml
    apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRole
    metadata:
      name: argocd-installer-clusterrole
    rules:
    # Allow ClusterExtension to set blockOwnerDeletion ownerReferences
    - apiGroups: [olm.operatorframework.io]
      resources: [clusterextensions/finalizers]
      verbs: [update]
      resourceNames: [argocd]
    # Manage ArgoCD CRDs
    - apiGroups: [apiextensions.k8s.io]
      resources: [customresourcedefinitions]
      verbs: [create]
    - apiGroups: [apiextensions.k8s.io]
      resources: [customresourcedefinitions]
      verbs: [get, list, watch, update, patch, delete]
      resourceNames:
      - appprojects.argoproj.io
      - argocds.argoproj.io
      - applications.argoproj.io
      - argocdexports.argoproj.io
      - applicationsets.argoproj.io
    # Manage ArgoCD ClusterRoles and ClusterRoleBindings
    - apiGroups: [rbac.authorization.k8s.io]
      resources: [clusterroles]
      verbs: [create]
    - apiGroups: [rbac.authorization.k8s.io]
      resources: [clusterroles]
      verbs: [get, list, watch, update, patch, delete]
      resourceNames:
      - argocd-operator.v0-1dhiybrldl1gyksid1dk2dqjsc72psdybc7iyvse5gpx
      - argocd-operator-metrics-reader
      - argocd-operator.v0-22gmilmgp91wu25is5i2ec598hni8owq3l71bbkl7iz3
    - apiGroups: [rbac.authorization.k8s.io]
      resources: [clusterrolebindings]
      verbs: [create]
    - apiGroups: [rbac.authorization.k8s.io]
      resources: [clusterrolebindings]
      verbs: [get, list, watch, update, patch, delete]
      resourceNames:
      - argocd-operator.v0-1dhiybrldl1gyksid1dk2dqjsc72psdybc7iyvse5gpx
      - argocd-operator.v0-22gmilmgp91wu25is5i2ec598hni8owq3l71bbkl7iz3
    ---
    apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRoleBinding
    metadata:
      name: argocd-installer-rbac-binding
    roleRef:
      apiGroup: rbac.authorization.k8s.io
      kind: ClusterRole
      name: argocd-installer-rbac-clusterrole
    subjects:
    - kind: ServiceAccount
      name: argocd-installer
      namespace: argocd
    ---
    apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRole
    metadata:
      name: argocd-installer-rbac-clusterrole
    rules:
    # ArgoCD's operator requires the following permissions, which means the
    # installer also needs them in order to create ArgoCD's RBAC objects.
    - apiGroups: [""]
      resources: [configmaps]
      verbs: ['*']
    - apiGroups: [""]
      resources: [endpoints]
      verbs: ['*']
    - apiGroups: [""]
      resources: [events]
      verbs: ['*']
    - apiGroups: [""]
      resources: [namespaces]
      verbs: ['*']
    - apiGroups: [""]
      resources: [persistentvolumeclaims]
      verbs: ['*']
    - apiGroups: [""]
      resources: [pods]
      verbs: ['*', get]
    - apiGroups: [""]
      resources: [pods/log]
      verbs: [get]
    - apiGroups: [""]
      resources: [secrets]
      verbs: ['*']
    - apiGroups: [""]
      resources: [serviceaccounts]
      verbs: ['*']
    - apiGroups: [""]
      resources: [services]
      verbs: ['*']
    - apiGroups: [""]
      resources: [services/finalizers]
      verbs: ['*']
    - apiGroups: [apps]
      resources: [daemonsets]
      verbs: ['*']
    - apiGroups: [apps]
      resources: [deployments]
      verbs: ['*']
    - apiGroups: [apps]
      resources: [deployments/finalizers]
      resourceNames: [argocd-operator]
      verbs: [update]
    - apiGroups: [apps]
      resources: [replicasets]
      verbs: ['*']
    - apiGroups: [apps]
      resources: [statefulsets]
      verbs: ['*']
    - apiGroups: [apps.openshift.io]
      resources: [deploymentconfigs]
      verbs: ['*']
    - apiGroups: [argoproj.io]
      resources: [applications]
      verbs: ['*']
    - apiGroups: [argoproj.io]
      resources: [appprojects]
      verbs: ['*']
    - apiGroups: [argoproj.io]
      resources: [argocdexports]
      verbs: ['*']
    - apiGroups: [argoproj.io]
      resources: [argocdexports/finalizers]
      verbs: ['*']
    - apiGroups: [argoproj.io]
      resources: [argocdexports/status]
      verbs: ['*']
    - apiGroups: [argoproj.io]
      resources: [argocds]
      verbs: ['*']
    - apiGroups: [argoproj.io]
      resources: [argocds/finalizers]
      verbs: ['*']
    - apiGroups: [argoproj.io]
      resources: [argocds/status]
      verbs: ['*']
    - apiGroups: [authentication.k8s.io]
      resources: [tokenreviews]
      verbs: [create]
    - apiGroups: [authorization.k8s.io]
      resources: [subjectaccessreviews]
      verbs: [create]
    - apiGroups: [autoscaling]
      resources: [horizontalpodautoscalers]
      verbs: ['*']
    - apiGroups: [batch]
      resources: [cronjobs]
      verbs: ['*']
    - apiGroups: [batch]
      resources: [jobs]
      verbs: ['*']
    - apiGroups: [config.openshift.io]
      resources: [clusterversions]
      verbs: [get, list, watch]
    - apiGroups: [monitoring.coreos.com]
      resources: [prometheuses]
      verbs: ['*']
    - apiGroups: [monitoring.coreos.com]
      resources: [servicemonitors]
      verbs: ['*']
    - apiGroups: [networking.k8s.io]
      resources: [ingresses]
      verbs: ['*']
    - apiGroups: [oauth.openshift.io]
      resources: [oauthclients]
      verbs: [create, delete, get, list, patch, update, watch]
    - apiGroups: [rbac.authorization.k8s.io]
      resources: ['*']
      verbs: ['*']
    - apiGroups: [rbac.authorization.k8s.io]
      resources: [clusterrolebindings]
      verbs: ['*']
    - apiGroups: [rbac.authorization.k8s.io]
      resources: [clusterroles]
      verbs: ['*']
    - apiGroups: [route.openshift.io]
      resources: [routes]
      verbs: ['*']
    - apiGroups: [route.openshift.io]
      resources: [routes/custom-host]
      verbs: ['*']
    - apiGroups: [template.openshift.io]
      resources: [templateconfigs]
      verbs: ['*']
    - apiGroups: [template.openshift.io]
      resources: [templateinstances]
      verbs: ['*']
    - apiGroups: [template.openshift.io]
      resources: [templates]
      verbs: ['*']
    ---
    apiVersion: rbac.authorization.k8s.io/v1
    kind: Role
    metadata:
      name: argocd-installer-role
      namespace: argocd
    rules:
    - apiGroups: [""]
      resources: [serviceaccounts]
      verbs: [create]
    - apiGroups: [""]
      resources: [serviceaccounts]
      verbs: [get, list, watch, update, patch, delete]
      resourceNames: [argocd-operator-controller-manager]
    - apiGroups: [""]
      resources: [configmaps]
      verbs: [create]
    - apiGroups: [""]
      resources: [configmaps]
      verbs: [get, list, watch, update, patch, delete]
      resourceNames: [argocd-operator-manager-config]
    - apiGroups: [""]
      resources: [services]
      verbs: [create]
    - apiGroups: [""]
      resources: [services]
      verbs: [get, list, watch, update, patch, delete]
      resourceNames: [argocd-operator-controller-manager-metrics-service]
    - apiGroups: [apps]
      resources: [deployments]
      verbs: [create]
    - apiGroups: [apps]
      resources: [deployments]
      verbs: [get, list, watch, update, patch, delete]
      resourceNames: [argocd-operator-controller-manager]
    ```