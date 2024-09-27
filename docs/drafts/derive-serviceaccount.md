# Derive minimal ServiceAccount required for ClusterExtension Installation and Management

OLM v1 does not have permission to install extensions on a cluster by default. In order to install a [supported bundle](../refs/supported-extensions.md), OLM must be provided a ServiceAccount configured with the appropriate permissions. For more information, see the [provided ServiceAccount](./provided-serviceaccount.md) documentation.

This document serves as a guide for how to derive the RBAC necessary to install a bundle.

### Required RBAC

The required permissions for the installation and management of a cluster extension can be determined by examining the contents of its bundle image.
This bundle image contains all the manifests that make up the extension (e.g. `CustomResourceDefinition`s, `Service`s, `Secret`s, `ConfigMap`s, `Deployment`s etc.)
as well as a [`ClusterServiceVersion`](https://olm.operatorframework.io/docs/concepts/crds/clusterserviceversion/) (CSV) that describes the extension and its service account's permission requirements.

The service account must have permissions to:
 - create and manage the extension's `CustomResourceDefinition`s
 - create and manage the resources packaged in the bundle
 - grant the extension controller's service account the permissions it requires for its operation
 - create and manage the extension controller's service account
 - create and manage the `Role`s, `RoleBinding`s, `ClusterRole`s, and `ClusterRoleBinding`s associated with the extension controller's service account
 - create and manage the extension controller's deployment

Additionally, for clusters that use the [OwnerReferencesPermissionEnforcement](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#ownerreferencespermissionenforcement) admission plug-in, the service account must also have permissions to:
 - update finalizers on the ClusterExtension to be able to set blockOwnerDeletion and ownerReferences

It is good security practice to follow the [principle of least privilege(https://en.wikipedia.org/wiki/Principle_of_least_privilege)], and scope permissions to specific resource names, wherever possible.
Keep in mind, that it is not possible to scope `create`, `list`, and `watch` permissions to specific resource names.

Depending on the scope, each permission will need to be added to either a `ClusterRole` or a `Role` and then bound to the service account with a `ClusterRoleBinding` or a `RoleBinding`.

### Example

The following example illustrates the process of deriving the minimal RBAC required to install the [ArgoCD Operator](https://operatorhub.io/operator/argocd-operator) v0.6.0 provided by [OperatorHub.io](https://operatorhub.io/).
The final permission set can be found in the [ClusterExtension sample manifest](../../config/samples/olm_v1alpha1_clusterextension.yaml) in the [samples](../../config/samples/olm_v1alpha1_clusterextension.yaml) directory.

The bundle image for the ArgoCD Operator v0.6.0 can be sourced from [quay.io/operatorhubio/argocd-operator:v0.6.0](https://quay.io/operatorhubio/argocd-operator:v0.6.0).

The bundle includes the following content:

* `ClusterServiceVersion`:
  - argocd-operator.v0.6.0.clusterserviceversion.yaml
* `CustomResourceDefinition`s:
  - argoproj.io_applicationsets.yaml
  - argoproj.io_applications.yaml
  - argoproj.io_appprojects.yaml
  - argoproj.io_argocdexports.yaml
  - argoproj.io_argocds.yaml
* Additional resources:
  - argocd-operator-controller-manager-service_v1_service.yaml
  - argocd-operator-controller-manager-metrics-service_v1_service.yaml
  - argocd-operator-manager-config_v1_configmap.yaml
  - argocd-operator-metrics-reader_rbac.authorization.k8s.io_v1_clusterrole.yaml

The `ClusterServiceVersion` defines a single `Deployment` in `spec.install.deployments` named `argocd-operator-controller-manager` with a `ServiceAccount` of the same name.
It declares the following cluster-scoped permissions in `spec.install.clusterPermissions`, and its namespace-scoped permissions in `spec.install.permissions`.

#### Derive permissions for the installer service account `ClusterRole`

##### Step 1. RBAC creation and management permissions

The installer service account must create and manage the `ClusterRole`s and `ClusterRoleBinding`s for the extension controller(s).
Therefore, it must have the following permissions:

```yaml
- apiGroups: [rbac.authorization.k8s.io]
  resources: [clusterroles]
  verbs: [create, list, watch]
- apiGroups: [rbac.authorization.k8s.io]
  resources: [clusterroles]
  verbs: [get, update, patch, delete]
  resourceNames: [<controller cluster role name>]
```

Note: The `resourceNames` field should be populated with the names of the `ClusterRole`s created by OLM v1. 
The names are generated and have the following format: `<packageName>.<hash>`. Since it is not a trivial task
to generate these names ahead of time, it is recommended to use a wildcard `*` in the `resourceNames` field for the installation.
Once the `ClusterRole`s are created, the cluster can be queried for the generated names and the `resourceNames` field can be updated accordingly.

The installer service account must create and manage the `ClusterRoleBinding`s for the extension controller(s).

```yaml
- apiGroups: [rbac.authorization.k8s.io]
  resources: [clusterrolebindings]
  verbs: [create, list, watch]
- apiGroups: [rbac.authorization.k8s.io]
  resources: [clusterrolebindings]
  verbs: [get, update, patch, delete]
  resourceNames: [<generated cluster role 1>, ..., <generated cluster role n>, <manifest cluster role binding name 1>, ..., <manifest cluster role binding name n>]
```

##### Step 2. `CustomResourceDefinition` permissions

The installer service account must be able to create and manage the `CustomResourceDefinition`s for the extension, as well 
as grant the extension controller's service account the permissions it needs to manage its CRDs.

```yaml
- apiGroups: [apiextensions.k8s.io]
  resources: [customresourcedefinitions]
  verbs: [create, list, watch]
- apiGroups: [apiextensions.k8s.io]
  resources: [customresourcedefinitions]
  verbs: [get, update, patch, delete]
  # Scoped to the CRDs in the bundle
  resourceNames: [applications.argoproj.io, appprojects.argoproj.io, argocds.argoproj.io, argocdexports.argoproj.io, applicationsets.argoproj.io]
```

##### Step 3. `OwnerReferencesPermissionEnforcement` permissions

For clusters that use `OwnerReferencesPermissionEnforcement`, the installer service account must be able to update finalizers on the ClusterExtension to be able to set blockOwnerDeletion and ownerReferences for clusters that use `OwnerReferencesPermissionEnforcement`.

```yaml
- apiGroups: [olm.operatorframework.io]
  resources: [clusterextensions/finalizers]
  verbs: [update]
  resourceNames: [argocd-operator.v0.6.0]
```

##### Step 4. `Deployment` permissions
The installer service account must be able to create and manage the `Deployment`s for the extension controller(s).

```yaml
- apiGroups: [apps]
  resources: [deployments, daemonsets, replicasets, statefulsets]
  verbs: [create, list, watch]
- apiGroups: [apps]
  resources: [deployments, daemonsets, replicasets, statefulsets]
  verbs: [get, update, patch, delete]
```

#### Step 5: Services permissions
The installer service account must be able to create and manage the resources listed under [`.spec.install.clusterPermissions`](./unpacked-argocd-bundle/argocd-operator.v0.6.0.clusterserviceversion.yaml#L917)

```yaml
- apiGroups: []
  resources: [pods, services, configmaps, secrets, ...]
  verbs: [create, list, watch]
- apiGroups: []
  resources: [pods, services, configmaps, secrets, ...]
  verbs: [get, update, patch, delete]
```

##### Step 6: RBAC creation and management permissions for namespaced-scoped resources

The installer service account must create and manage the `Role`s and `RoleBinding`s for the extension controller(s) to bind the controller's resources.
Therefore, it must have the following permissions:

```yaml
- apiGroups: [rbac.authorization.k8s.io]
  resources: [roles]
  verbs: [create, list, watch]
- apiGroups: [rbac.authorization.k8s.io]
  resources: [roles]
  verbs: [get, update, patch, delete]
  resourceNames: [<controller cluster role name>]
```
The installer service account must create and manage the `RoleBinding`s for the extension controller(s).

```yaml
- apiGroups: [rbac.authorization.k8s.io]
  resources: [rolebindings]
  verbs: [create, list, watch]
- apiGroups: [rbac.authorization.k8s.io]
  resources: [rolebindings]
  verbs: [get, update, patch, delete]
  resourceNames: [<generated role 1>, ..., <generated role n>, <manifest role binding name 1>, ..., <manifest role binding name n>]
```

##### Step 7. Permissions for scoped-resources.

The installer service account should be assign the controller's service account the permissions it needs to perform its operations i.e. the permissions to manage all resources listed under `.spec.install.permissions`. In order to grant the deployment service account permissions to manage the scoped resources, the installer service account must itself have permissions to manage and create the listed scoped resources.

```yml
rules:
- apiGroups: [""]
  resources: [configmaps]
  verbs: [create, delete, get, list, patch, update, watch ]
- apiGroups: [""]
  resources: [events]
  verbs: [create, delete, get, list, patch, update, watch]
```

##### Step 8: Installer `ServiceAccount` permissions
The installer service account needs permissions to create and manage the controller manager service accounts. We can specify the specific service account resource name of the cluster extension.

```yaml
- apiGroups: [""]
  resources: [serviceaccounts]
  verbs: [create, list, watch]
- apiGroups: [""]
  resources: [serviceaccounts]
  verbs: [get, update, patch, delete]
  resourceNames: [argocd-operator-controller-manager]
```

##### Step 9: Controller Manager `ServiceAccount` permissions

The controller manager deployment service account must be able to create and manage all resources listed under [`.spec.install.permissions`](./unpacked-argocd-bundle/argocd-operator.v0.6.0.clusterserviceversion.yaml#L1132) for the [ArgoCD extension](./unpacked-argocd-bundle/argocd-operator.v0.6.0.clusterserviceversion.yaml) namely `Configmap`s and `Events` etc.


The controller manager deployment service account will need permissions to create and manage the resources.
Therefore, it must have the following permissions:
```yml
rules:
- apiGroups: [""]
  resources: [configmaps]
  verbs: [create, delete, get, list, patch, update, watch ]
- apiGroups: [""]
  resources: [events]
  verbs: [create, delete, get, list, patch, update, watch]
```

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