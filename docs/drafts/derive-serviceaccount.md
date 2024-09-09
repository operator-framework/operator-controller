# Derive minimal ServiceAccount required for ClusterExtension Installation and Management

OLM v1 security stance (secure by default)

Adhering to OLM v1's "Secure by Default" tenet, OLM v1 does not have the permissions necessary to install content. This follows the least privilege principle and reduces the chance of a [confused deputy attack](https://en.wikipedia.org/wiki/Confused_deputy_problem). Instead, a ServiceAccount must be provided by users to install and manage content.

Explain installing a CE requires a Service Account

OLMv1 does not provide cluster admin privileges by default for installing cluster extensions. It depends on the cluster extension developer to specify the exact permissions required for the management of any specific bundle. A ServiceAccount needs to be explicitly specified for installing and upgrading operators else will face errors when deploying your cluster extension.

The ServiceAccount is specified in the ClusterExtension manifest as follows:

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

The cluster extension installer will need RBAC in order to be able to assign the controller the RBAC it requires.
In order to derive the minimal RBAC for the installer service account, you must specify the following permissions:
* ClusterRole with all the roles specified in the bundle ClusterServiceVersion.
* ClusterExtension finalizer
* Allow ClusterExtenstion to set blockOwnerDeletion ownerReferences
* create the controller deployment
* create the ClusterResourceDefnitions
* create the other manifest objects
* create the necessary Cluster/Roles for the controller to be able to perform its job.
* get, list, watch, update, patch, delete the specific resources that get created
* update finalizers on the ClusterExtension to be able to set blockOwnerDeletion and ownerReferences


The following ClusterRole rules are needed:
# Allow ClusterExtension to set blockOwnerDeletion ownerReferences
- apiGroups: [olm.operatorframework.io]
  resources: [clusterextensions/finalizers]
  verbs: [update]
  resourceNames: [<cluster-extension-name>]
# Manage CRDs
- apiGroups: [apiextensions.k8s.io]
  resources: [customresourcedefinitions]
  verbs: [create, list, watch]
- apiGroups: [apiextensions.k8s.io]
  resources: [customresourcedefinitions]
  verbs: [get, update, patch, delete]
  resourceNames: [<crd name 1>, ..., <crd name n>]
# Manage ClusterRoles
- apiGroups: [rbac.authorization.k8s.io]
  resources: [clusterroles]
  verbs: [create, list, watch]
- apiGroups: [rbac.authorization.k8s.io]
  resources: [clusterroles]
  verbs: [get, update, patch, delete]
  resourceNames: [<generated cluster role 1>, ..., <generated cluster role n>, <manifest cluster role name 1>, ..., <manifest cluster role name n>]
# Manage ClusterRoleBindings
- apiGroups: [rbac.authorization.k8s.io]
  resources: [clusterrolebindings]
  verbs: [create, list, watch]
- apiGroups: [rbac.authorization.k8s.io]
  resources: [clusterrolebindings]
  verbs: [get, update, patch, delete]
  resourceNames: [<generated cluster role 1>, ..., <generated cluster role n>, <manifest cluster role binding name 1>, ..., <manifest cluster role binding name n>]
* Rules defined in the CSV under `.spec.install.clusterPermissions` and `.spec.install.permissions`
* Rules to manage any other cluster-scoped resources shipped with the bundle
* Rules to manage any other namespace-scoped resources
* Rules to manage the deployment defined in the ClusterServiceVersion
* Rules to manage the service account used for the deployment
- apiGroups: [apps]
  resources: [deployments]
  verbs: [create, list, watch]

- apiGroups: [apps]
  resources: [deployments]
  verbs: [get, update, patch, delete]
  resourceNames: [argocd-operator-controller-manager]

- apiGroups: [""]
  resources: [serviceaccounts]
  verbs: [create, list, watch]

- apiGroups: [""]
  resources: [serviceaccounts]
  verbs: [get, update, patch, delete]
  resourceNames: [argocd-operator-controller-manager]
```

Below is an example of the argocd installer with the necessary RBAC to deploy the ArgoCD ClusterExtension:

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

# Creation of ClusterRoleBinding using the cluster-admin ClusterRole in non-production environments

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
kubectl create clusterrolebinding my-cluster-extension-installer-role-binding \
  --clusterrole=cluster-admin \
  --serviceaccount=my-cluster-extension-namespace:my-cluster-installer-service-account
```


If no ServiceAccount is provided in the ClusterExtension manifest, then the manifest will be rejected.
Installation will fail if the ServiceAccount does not have the necessary permissions to install a bundle.
