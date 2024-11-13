# Derive minimal ServiceAccount required for ClusterExtension Installation and Management

OLM v1 does not have permission to install extensions on a cluster by default. In order to install a [supported bundle](../project/olmv1_limitations.md),
OLM must be provided a ServiceAccount configured with the appropriate permissions.

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

It is good security practice to follow the [principle of least privilege](https://en.wikipedia.org/wiki/Principle_of_least_privilege), and scope permissions to specific resource names, wherever possible.
Keep in mind, that it is not possible to scope `create`, `list`, and `watch` permissions to specific resource names.

Depending on the scope, each permission will need to be added to either a `ClusterRole` or a `Role` and then bound to the service account with a `ClusterRoleBinding` or a `RoleBinding`.

### Example

The following example illustrates the process of deriving the minimal RBAC required to install the [ArgoCD Operator](https://operatorhub.io/operator/argocd-operator) [v0.6.0](https://operatorhub.io/operator/argocd-operator/alpha/argocd-operator.v0.6.0) provided by [OperatorHub.io](https://operatorhub.io/).
The final permission set can be found in the [ClusterExtension sample manifest](https://github.com/operator-framework/operator-controller/blob/main/config/samples/olm_v1_clusterextension.yaml) in the [samples](https://github.com/operator-framework/operator-controller/blob/main/config/samples/olm_v1_clusterextension.yaml) directory.

The bundle includes the following manifests, which can be found [here](https://github.com/argoproj-labs/argocd-operator/tree/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0):

* `ClusterServiceVersion`:
  - [argocd-operator.v0.6.0.clusterserviceversion.yaml](https://github.com/argoproj-labs/argocd-operator/blob/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0/argocd-operator.v0.6.0.clusterserviceversion.yaml)
* `CustomResourceDefinition`s:
  - [argoproj.io_applicationsets.yaml](https://github.com/argoproj-labs/argocd-operator/blob/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0/argoproj.io_applicationsets.yaml)
  - [argoproj.io_applications.yaml](https://github.com/argoproj-labs/argocd-operator/blob/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0/argoproj.io_applications.yaml)
  - [argoproj.io_appprojects.yaml](https://github.com/argoproj-labs/argocd-operator/blob/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0/argoproj.io_appprojects.yaml)
  - [argoproj.io_argocdexports.yaml](https://github.com/argoproj-labs/argocd-operator/blob/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0/argoproj.io_argocdexports.yaml)
  - [argoproj.io_argocds.yaml](https://github.com/argoproj-labs/argocd-operator/blob/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0/argoproj.io_argocds.yaml)
* Additional resources:
  - [argocd-operator-controller-manager-metrics-service_v1_service.yaml](https://github.com/argoproj-labs/argocd-operator/blob/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0/argocd-operator-controller-manager-metrics-service_v1_service.yaml)
  - [argocd-operator-manager-config_v1_configmap.yaml](https://github.com/argoproj-labs/argocd-operator/blob/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0/argocd-operator-manager-config_v1_configmap.yaml)
  - [argocd-operator-metrics-reader_rbac.authorization.k8s.io_v1_clusterrole.yaml](https://github.com/argoproj-labs/argocd-operator/blob/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0/argocd-operator-metrics-reader_rbac.authorization.k8s.io_v1_clusterrole.yaml)

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
  resourceNames: [<controller cluster role name 1>, ...]
- apiGroups: [rbac.authorization.k8s.io]
  resources: [clusterrolebindings]
  verbs: [create, list, watch]
- apiGroups: [rbac.authorization.k8s.io]
  resources: [clusterrolebindings]
  verbs: [get, update, patch, delete]
  resourceNames: [<controller cluster rolebinding name 1>, ...]
```

Note: The `resourceNames` field should be populated with the names of the `ClusterRole`s and `ClusterRoleBinding`s created by OLM v1.
These names are generated with the following format: `<packageName>.<hash>`. Since it is not a trivial task
to generate these names ahead of time, it is recommended to use a wildcard `*` in the `resourceNames` field for the installation.
Then, update the `resourceNames` fields by inspecting the cluster for the generated resource names. For instance, for `ClusterRole`s:

```terminal
kubectl get clusterroles | grep argocd
```

Example output:

```terminal
argocd-installer-clusterrole                                           2024-09-30T08:02:09Z
argocd-installer-rbac-clusterrole                                      2024-09-30T08:02:09Z
argocd-operator-metrics-reader                                         2024-09-30T08:02:12Z
# The following are the generated ClusterRoles
argocd-operator.v0-1dhiybrldl1gyksid1dk2dqjsc72psdybc7iyvse5gpx        2024-09-30T08:02:12Z
argocd-operator.v0-22gmilmgp91wu25is5i2ec598hni8owq3l71bbkl7iz3        2024-09-30T08:02:12Z
```

The same can be done for `ClusterRoleBindings`.

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
This is only a requirement for clusters that use the [OwnerReferencesPermissionEnforcement](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#ownerreferencespermissionenforcement) admission plug-in.

```yaml
- apiGroups: [olm.operatorframework.io]
  resources: [clusterextensions/finalizers]
  verbs: [update]
  # Scoped to the name of the ClusterExtension
  resourceNames: [argocd-operator.v0.6.0]
```

##### Step 4. Bundled cluster-scoped resource permissions

Permissions must be added for the creation and management of any cluster-scoped resources included in the bundle.
In this example, the ArgoCD bundle contains a `ClusterRole` called `argocd-operator-metrics-reader`. Given that
`ClusterRole` permissions have already been created in [Step 1](#step-1-rbac-creation-and-management-permissions), it
is sufficient to add the `argocd-operator-metrics-reader`resource name to the `resourceName` list of the pre-existing rule:

```yaml
- apiGroups: [rbac.authorization.k8s.io]
  resources: [clusterroles]
  verbs: [get, update, patch, delete]
  resourceNames: [<controller cluster role name 1>, ..., argocd-operator-metrics-reader]
```

##### Step 5. Operator permissions declared in the ClusterServiceVersion

Include all permissions defined in the `.spec.install.permissions` ([reference](https://github.com/argoproj-labs/argocd-operator/blob/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0/argocd-operator.v0.6.0.clusterserviceversion.yaml#L1091)) and `.spec.install.clusterPermissions` ([reference](https://github.com/argoproj-labs/argocd-operator/blob/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0/argocd-operator.v0.6.0.clusterserviceversion.yaml#L872)) stanzas in the bundle's `ClusterServiceVersion`.
These permissions are required by the extension controller, and therefore the installer service account must be able to grant them.

Note: there may be overlap between the rules defined in each stanza. Overlapping rules needn't be added twice.

```yaml
# from .spec.install.clusterPermissions
- apiGroups: [""]
  resources: ["configmaps", "endpoints", "events", "namespaces", "persistentvolumeclaims", "pods", "secrets", "serviceaccounts", "services", "services/finalizers"]
  verbs: ["*"]
- apiGroups: [""]
  resources: ["pods", "pods/log"]
  verbs: ["get"]
- apiGroups: ["apps"]
  resources: ["daemonsets", "deployments", "replicasets", "statefulsets"]
  verbs: ["*"]
- apiGroups: ["apps"]
  resourceNames: ["argocd-operator"]
  resources: ["deployments/finalizers"]
  verbs: ["update"]
- apiGroups: ["apps.openshift.io"]
  resources: ["deploymentconfigs"]
  verbs: ["*"]
- apiGroups: ["argoproj.io"]
  resources: ["applications", "appprojects"]
  verbs: ["*"]
- apiGroups: ["argoproj.io"]
  resources: ["argocdexports", "argocdexports/finalizers", "argocdexports/status"]
  verbs: ["*"]
- apiGroups: ["argoproj.io"]
  resources: ["argocds", "argocds/finalizers", "argocds/status"]
  verbs: ["*"]
- apiGroups: ["autoscaling"]
  resources: ["horizontalpodautoscalers"]
  verbs: ["*"]
- apiGroups: ["batch"]
  resources: ["cronjobs", "jobs"]
  verbs: ["*"]
- apiGroups: ["config.openshift.io"]
  resources: ["clusterversions"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["monitoring.coreos.com"]
  resources: ["prometheuses", "servicemonitors"]
  verbs: ["*"]
- apiGroups: ["networking.k8s.io"]
  resources: ["ingresses"]
  verbs: ["*"]
- apiGroups: ["oauth.openshift.io"]
  resources: ["oauthclients"]
  verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
- apiGroups: ["rbac.authorization.k8s.io"]
  resources: ["*"]
  verbs: ["*"]
- apiGroups: ["rbac.authorization.k8s.io"]
  resources: ["clusterrolebindings", "clusterroles"]
  verbs: ["*"]
- apiGroups: ["route.openshift.io"]
  resources: ["routes", "routes/custom-host"]
  verbs: ["*"]
- apiGroups: ["template.openshift.io"]
  resources: ["templateconfigs", "templateinstances", "templates"]
  verbs: ["*"]
- apiGroups: ["authentication.k8s.io"]
  resources: ["tokenreviews"]
  verbs: ["create"]
- apiGroups: ["authorization.k8s.io"]
  resources: ["subjectaccessreviews"]
  verbs: ["create"]

# copied from .spec.install.permissions
- apiGroups: ["coordination.k8s.io"]
  resources: ["leases"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
# overlapping permissions:
# - apiGroups: [""]
#  resources: ["configmaps"]
#  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
# - apiGroups: [""]
#  resources: ["events"]
#  verbs: ["create", "patch"]
```

#### Derive permissions for the installer service account `Role`

The following steps detail how to define the namespace-scoped permissions needed by the installer service account's `Role`.
The installer service account must create and manage the `RoleBinding`s for the extension controller(s).

##### Step 1. `Deployment` permissions

The installer service account must be able to create and manage the `Deployment`s for the extension controller(s).
The `Deployment` name(s) can be found in the `ClusterServiceVersion` resource packed in the bundle under `.spec.install.deployments` ([reference](https://github.com/argoproj-labs/argocd-operator/blob/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0/argocd-operator.v0.6.0.clusterserviceversion.yaml#L1029)).
This example's `ClusterServiceVersion` can be found [here](https://github.com/argoproj-labs/argocd-operator/blob/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0/argocd-operator.v0.6.0.clusterserviceversion.yaml).

```yaml
- apiGroups: [apps]
  resources: [deployments]
  verbs: [create]
- apiGroups: [apps]
  resources: [deployments]
  verbs: [get, list, watch, update, patch, delete]
  # scoped to the extension controller deployment name
  resourceNames: [argocd-operator-controller-manager]
```

##### Step 2: `ServiceAccount` permissions

The installer service account must be able to create and manage the `ServiceAccount`(s) for the extension controller(s).
The `ServiceAccount` name(s) can be found in deployment template in the `ClusterServiceVersion` resource packed in the bundle under `.spec.install.deployments`.
This example's `ClusterServiceVersion` can be found [here](https://github.com/argoproj-labs/argocd-operator/blob/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0/argocd-operator.v0.6.0.clusterserviceversion.yaml).

```yaml
- apiGroups: [""]
  resources: [serviceaccounts]
  verbs: [create, list, watch]
- apiGroups: [""]
  resources: [serviceaccounts]
  verbs: [get, update, patch, delete]
  # scoped to the extension controller's deployment service account
  resourceNames: [argocd-operator-controller-manager]
```

##### Step 3. Bundled namespace-scoped resource permissions

The installer service account must also create and manage other namespace-scoped resources included in the bundle.
In this example, the bundle also includes two additional namespace-scoped resources:
 * the [argocd-operator-controller-manager-metrics-service](https://github.com/argoproj-labs/argocd-operator/blob/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0/argocd-operator-controller-manager-metrics-service_v1_service.yaml) `Service`, and
 * the [argocd-operator-manager-config](https://github.com/argoproj-labs/argocd-operator/blob/da6b8a7e68f71920de9545152714b9066990fc4b/deploy/olm-catalog/argocd-operator/0.6.0/argocd-operator-manager-config_v1_configmap.yaml) `ConfigMap`

Therefore, the following permissions must be given to the installer service account:

```yaml
- apiGroups: [""]
  resources: [services]
  verbs: [create]
- apiGroups: [""]
  resources: [services]
  verbs: [get, list, watch, update, patch, delete]
  # scoped to the service name
  resourceNames: [argocd-operator-controller-manager-metrics-service]
- apiGroups: [""]
  resources: [configmaps]
  verbs: [create]
- apiGroups: [""]
  resources: [configmaps]
  verbs: [get, list, watch, update, patch, delete]
  # scoped to the configmap name
  resourceNames: [argocd-operator-manager-config]
```

#### Putting it all together

Once the installer service account required cluster-scoped and namespace-scoped permissions have been collected:
1. Create the installation namespace
2. Create the installer `ServiceAccount`
3. Create the installer `ClusterRole`
4. Create the `ClusterRoleBinding` between the installer service account and its cluster role
5. Create the installer `Role`
6. Create the `RoleBinding` between the installer service account and its role
7. Create the `ClusterExtension`

A manifest with the full set of resources can be found [here](https://github.com/operator-framework/operator-controller/blob/main/config/samples/olm_v1_clusterextension.yaml).

### Alternatives

We understand that manually determining the minimum RBAC required for installation/upgrade of a `ClusterExtension` quite complex and protracted.
In the near future, OLM v1 will provide tools and automation in order to simplify this process while maintaining our security posture.
For users wishing to test out OLM v1 in a non-production settings, we offer the following alternatives:

#### Give the installer service account admin privileges

The `cluster-admin` `ClusterRole` can be bound to the installer service account giving it full permissions to the cluster.
While this obviates the need to determine the minimal RBAC required for installation, it is also dangerous. It is highly recommended
that this alternative only be used in test clusters. Never in production.

Below is an example ClusterRoleBinding using the cluster-admin ClusterRole:

```terminal
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

#### hack/tools/catalog

In the spirit of making this process more tenable until the proper tools are in place, the scripts
in [hack/tools/catalogs](https://github.com/operator-framework/operator-controller/blob/main/hack/tools/catalogs) were created to help the user navigate and search catalogs as well
as to generate the minimal RBAC requirements. These tools are offered as is, with no guarantees on their correctness,
support, or maintenance. For more information, see [Hack Catalog Tools](https://github.com/operator-framework/operator-controller/blob/main/hack/tools/catalogs/README.md).
