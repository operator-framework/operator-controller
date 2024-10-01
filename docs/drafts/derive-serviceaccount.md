# Derive minimal ServiceAccount required for ClusterExtension Installation and Management

OLMv1 does not provide cluster admin privileges by default for installing cluster extensions. This means that the installation process will require a service account with sufficient privileges to install the bundle. It depends on the cluster extension developer to specify the exact permissions required for the management of any specific bundle. A Service Account needs to be explicitly specified for installing and upgrading operators else will face errors when deploying your cluster extension.

The Service Account is specified in the ClusterExtension manifest as shown below:

```yaml
apiVersion: olm.operatorframework.io/v1alpha1
kind: ClusterExtension
metadata:
  name: argocd
spec:
  source:
    sourceType: Catalog
    catalog:
      packageName: argocd-operator
      version: 0.6.0
  install:
    namespace: argocd
    serviceAccount:
      name: argocd-installer
```

The initial stable version (v1.0.0) only supports FBC catalogs containing registry+v1 bundles. OLMv1 will not support all OLMv0 content. OLMv1 will only support bundles that meet the following criteria:
* AllNamespaces install mode is enabled
* No dependencies on other packages or GVKs
* No webhooks
* Does not make use of the OperatorConditions API

### Required RBAC

The cluster extension installer should have the prerequisite permissions in order to be able to assign the controller the RBAC it requires. In order to derive the minimal RBAC for the installer service account, you must specify the following permissions:

* ClusterRole with all the roles specified in the bundle ClusterServiceVersion. This includes all the
  rules defined in the CSV under `.spec.install.clusterPermissions` and `.spec.install.permissions`
* ClusterRole to create and manage CustomResourceDefinitions
* Update finalizers on the ClusterExtension to be able to set blockOwnerDeletion and ownerReferences
* Permissions to create the controller deployment, this corresponds to the rules to manage the
  deployment defined in the ClusterServiceVersion
* Permissions to create the other manifest objects, rules to manage any other cluster-scoped resources
  shipped with the bundle
* Rules to manage any other namespace-scoped resources 
* Permissions to create the necessary roles and rolebindings for the controller to be able to perform its job
* Get, list, watch, update, patch, delete the specific resources that get created

If no ServiceAccount is provided in the ClusterExtension manifest then the manifest will be rejected.
The installation will fail if the ServiceAccount does not have the necessary permissions to install a bundle.


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

### Manual process for minimal RBAC creation

There are no production tools available to help you understand the precise RBAC required for your cluster extensions. However, if you want to figure this manually you can try the below:

* Create all the intial rbac and then iterate over the ClusterExtention failures, examining conditions and updating the RBAC to include the generated cluster role names (name will be in the failure condition).
Install the ClusterExtension, read the failure condition, update installer RBAC and iterate until you are out of errors
* You can get the bundle image, unpacking the same and inspect the manifests to figure out the required permissions.
* The `oc` cli-tool creates cluster roles with a hash in their name, query the newly created ClusterRole names, then reduce the installer RBAC scope to just the ClusterRoles needed (inc. the generated ones). You can achieve this by allowing the installer to get, list, watch and update any cluster roles.



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
This is an example of the argocd installer with the necessary RBAC to deploy the ArgoCD ClusterExtension.

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