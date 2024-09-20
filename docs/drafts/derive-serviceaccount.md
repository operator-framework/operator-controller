# Derive minimal ServiceAccount required for ClusterExtension Installation and Management

OLM v1 does not have permission to install extensions on a cluster by default. In order to install a [supported bundle](../refs/supported-extensions.md), OLM must be provided a ServiceAccount configured with the appropriate permissions. For more information, see the [provided ServiceAccount](./provided-serviceaccount.md) documentation.

This document serves as a guide for how to derive the RBAC necessary to install a bundle.

### Required RBAC

You can determine the specifics of these permissions by referencing the bundle of the ClusterExtension you want to install. The service account must have the following permissions:

* Create, list, watch verbs for all resources that are a part of the install (cannot be scoped to specific resource names).
  - Permissions to create and manage CustomResourceDefinitions
  - Permissions to create any other manifest objects
  - Rules to manage any other cluster-scoped resources
  - All the rules defined in the CSV under `.spec.install.clusterPermissions` and `.spec.install.permissions`
  - Permissions for namespace-scoped resources.
  - These are specified with ClusterRole + ClusterRoleBinding, ClusterRole + RoleBinding, or Role + RoleBinding.

* Get, update, patch, delete verbs for all resources that are a part of the install (can be scoped to specific resource names)
  - Permissions for cluster-scoped resources
  - Permissions for namespace-scoped resources
  - These are created using ClusterRole + ClusterRoleBinding, ClusterRole + RoleBinding, or Role + RoleBinding

* Permissions to create any ClusterRole, ClusterRoleBinding, Role, RoleBinding resources required by the extension.
  - All the same rules defined in the ClusterRole and Role resources
  - Escalate and bind verbs for ClusterRole, ClusterRoleBinding, Role, RoleBinding resources
  - Permissions to create the necessary roles and rolebindings for the controller to be able to perform its job

* Update finalizers on the ClusterExtension to be able to set blockOwnerDeletion and ownerReferences

* Permissions to create the controller deployment, this corresponds to the rules to manage the
  deployment defined in the ClusterServiceVersion

### Derive minimal RBAC

As an example, consider a cluster extension that needs to query OpenShift users and groups as part of its controller logic and specifies the below cluster permissions in its ClusterServiceVersion:

```yml
clusterPermissions:
        - rules:
            - apiGroups:
                - user.openshift.io
              resources:
                - users
                - groups
                - identities
              verbs:
                - get
                - list
```

In addition to cluster permissions, it specifies these additional permissions to manage itself:

```yml
permissions:
        - rules:
            - apiGroups:
                - apps
              resourceNames:
                - <cluster-extension-name>
              resources:
                - deployments/finalizers
              verbs:
                - update
            - apiGroups:
                - apps
              resources:
                - deployments
              verbs:
                - create
                - delete
                - get
                - list
                - patch
                - update
                - watch
```

Below is the translation of the above cluster permissions into ClusterRole and ClusterRoleBinding for your cluster extension:

```yml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: <cluster-extension-name>
rules:
- apiGroups:
  - olm.operatorframework.io
  resourceNames:
  - <cluster-extension-name>
  resources:
  - clusterextensions/finalizers
  verbs:
  - update
- apiGroups:
  - rbac.authorization.k8s.io
  resources:
  - clusterroles
  - clusterrolebindings
  verbs:
  - create
- apiGroups:
  - user.openshift.io
  resources:
  - users
  - groups
  - identities
  verbs:
  - get
  - list
```

Below is the ClusterRoleBinding that associates your cluster extension to its service account:

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

Below is the translation of the above cluster permissions into Role and RoleBinding for your cluster extension:

```yml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: <cluster-extension-name>
  namespace: <cluster-extension-name>
rules:
- apiGroups:
  - ""
  resources:
  - pods
  - services
  - services/finalizers
  - endpoints
  - persistentvolumeclaims
  - events
  - configmaps
  - secrets
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - apps
  resources:
  - deployments
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - apps
  resourceNames:
  - <cluster-extension-name>
  resources:
  - deployments/finalizers
  verbs:
  - update
- apiGroups:
  - ""
  resources:
  - pods
  verbs:
  - get
- apiGroups:
  - apps
  resources:
  - deployments
  verbs:
  - get
- apiGroups:
  - ""
  resources:
  - serviceaccounts
  verbs:
  - create
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - rbac.authorization.k8s.io
  resources:
  - roles
  - rolebindings
  verbs:
  - create
```
Below is the RoleBinding that associates your cluster extension to its service account:

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

You can use the above example as a starting point to derive the minimal RBAC for your cluster extension.
You should start by specifying permissions to manage the Deployment, ServiceAccount and Roles required for your cluster extension. Subsequently, you can iterate as below.

* Create all the initial RBAC and then iterate over the ClusterExtension failures, examining conditions and updating the RBAC to include the generated cluster role names (name will be in the failure condition).
* After reading the failure condition, update the installer RBAC and iterate until you are out of errors.
* You can get the bundle image, unpack the same and inspect the manifests to determine the required permissions.
* The `oc` cli-tool creates cluster roles with a hash in their name. You can query the newly created ClusterRole names and reduce the installer RBAC scope to have the ClusterRoles needed, this can include generated roles.
* You can achieve this by allowing the installer to get, list, watch and update any cluster roles.

Note: Production tools to help you manage RBAC are also available with OLM v1 release.

### Sample snippets of ClusterRole rule definitions

ClusterExtension to set blockOwnerDeletion ownerReferences

```yaml
- apiGroups: [olm.operatorframework.io]
  resources: [clusterextensions/finalizers]
  verbs: [update]
  resourceNames: [<cluster-extension-name>]
```

CRUD for Custom Resource Definitions

```yaml
- apiGroups: [apiextensions.k8s.io]
  resources: [customresourcedefinitions]
  verbs: [create, list, watch]

- apiGroups: [apiextensions.k8s.io]
  resources: [customresourcedefinitions]
  verbs: [get, update, patch, delete]
  resourceNames: [<crd name 1>, ..., <crd name n>]
```

CRUD for Cluster Roles

```yaml
- apiGroups: [rbac.authorization.k8s.io]
  resources: [clusterroles]
  verbs: [create, list, watch]

- apiGroups: [rbac.authorization.k8s.io]
  resources: [clusterroles]
  verbs: [get, update, patch, delete]
  resourceNames: [<generated cluster role 1>, ..., <generated cluster role n>, <manifest cluster role name 1>, ..., <manifest cluster role name n>]
```

CRUD for Cluster Role Bindings

```yaml
- apiGroups: [rbac.authorization.k8s.io]
  resources: [clusterrolebindings]
  verbs: [create, list, watch]

- apiGroups: [rbac.authorization.k8s.io]
  resources: [clusterrolebindings]
  verbs: [get, update, patch, delete]
  resourceNames: [<generated cluster role 1>, ..., <generated cluster role n>, <manifest cluster role binding name 1>, ..., <manifest cluster role binding name n>]
```

CRUD for managing deployments

```yaml
- apiGroups: [apps]
  resources: [deployments]
  verbs: [create, list, watch]

- apiGroups: [apps]
  resources: [deployments]
  verbs: [get, update, patch, delete]
  resourceNames: [argocd-operator-controller-manager]
```

CRUD for service accounts used by the deployment
 
```yaml
- apiGroups: [""]
  resources: [serviceaccounts]
  verbs: [create, list, watch]

- apiGroups: [""]
  resources: [serviceaccounts]
  verbs: [get, update, patch, delete]
  resourceNames: [argocd-operator-controller-manager]
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