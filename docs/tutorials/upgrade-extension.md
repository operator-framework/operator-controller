---
hide:
  - toc
---

# Upgrade an Extension

Existing extensions can be upgraded by updating the version field in the ClusterExtension resource.

For information on downgrading an extension, see [Downgrade an Extension](downgrade-extension.md).

## Prerequisites

* You have a ClusterExtension installed
* The target version is compatible with OLM v1 (see [OLM v1 limitations](../project/olmv1_limitations.md))
* Any changes to the CustomResourceDefinition in the new version meet compatibility requirements (see [CRD upgrade safety](../concepts/crd-upgrade-safety.md))
* The installer ServiceAccount's RBAC permissions are adequate for the target version (see [Minimal RBAC for Installer Service Account](../howto/derive-service-account.md))
* You are not attempting to upgrade between minor versions with a major version of zero (see [Upgrades within the major version zero](../concepts/upgrade-support.md#upgrades-within-the-major-version-zero))

For more detailed information see [Upgrade Support](../concepts/upgrade-support.md).

## Procedure

For this example, we will be using v0.2.0 of the ArgoCD operator. If you would like to follow along
with this tutorial, you can apply the following manifest to your cluster by, for example,
saving it to a local file and then running `kubectl apply -f FILENAME`:

??? info "ArgoCD v0.2.0 manifests"
    ```yaml
    ---
    apiVersion: v1
    kind: Namespace
    metadata:
      name: argocd
    ---
    apiVersion: v1
    kind: ServiceAccount
    metadata:
      name: argocd-installer
      namespace: argocd
    ---
    apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRoleBinding
    metadata:
      name: argocd-installer-binding
    roleRef:
      apiGroup: rbac.authorization.k8s.io
      kind: ClusterRole
      name: argocd-installer-clusterrole
    subjects:
    - kind: ServiceAccount
      name: argocd-installer
      namespace: argocd
    ---
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
      verbs: [create, list, watch]
    - apiGroups: [apiextensions.k8s.io]
      resources: [customresourcedefinitions]
      verbs: [get, update, patch, delete]
      resourceNames:
      - appprojects.argoproj.io
      - argocds.argoproj.io
      - applications.argoproj.io
      - argocdexports.argoproj.io
      - applicationsets.argoproj.io
    # Manage ArgoCD ClusterRoles and ClusterRoleBindings
    - apiGroups: [rbac.authorization.k8s.io]
      resources: [clusterroles]
      verbs: [create, list, watch]
    - apiGroups: [rbac.authorization.k8s.io]
      resources: [clusterroles]
      verbs: [get, update, patch, delete]
      resourceNames:
      - argocd-operator.v0-1dhiybrldl1gyksid1dk2dqjsc72psdybc7iyvse5gpx
      - argocd-operator-metrics-reader
      - argocd-operator.v0-22gmilmgp91wu25is5i2ec598hni8owq3l71bbkl7iz3
    - apiGroups: [rbac.authorization.k8s.io]
      resources: [clusterrolebindings]
      verbs: [create, list, watch]
    - apiGroups: [rbac.authorization.k8s.io]
      resources: [clusterrolebindings]
      verbs: [get, update, patch, delete]
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
    - apiGroups: ["coordination.k8s.io"]
      resources: ["leases"]
      verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
    ---
    apiVersion: rbac.authorization.k8s.io/v1
    kind: Role
    metadata:
      name: argocd-installer-role
      namespace: argocd
    rules:
    - apiGroups: [""]
      resources: [serviceaccounts]
      verbs: [create, list, watch]
    - apiGroups: [""]
      resources: [serviceaccounts]
      verbs: [get, update, patch, delete]
      resourceNames: [argocd-operator-controller-manager]
    - apiGroups: [""]
      resources: [configmaps]
      verbs: [create, list, watch]
    - apiGroups: [""]
      resources: [configmaps]
      verbs: [get, update, patch, delete]
      resourceNames: [argocd-operator-manager-config]
    - apiGroups: [""]
      resources: [services]
      verbs: [create, list, watch]
    - apiGroups: [""]
      resources: [services]
      verbs: [get, update, patch, delete]
      resourceNames: [argocd-operator-controller-manager-metrics-service]
    - apiGroups: [apps]
      resources: [deployments]
      verbs: [create, list, watch]
    - apiGroups: [apps]
      resources: [deployments]
      verbs: [get, update, patch, delete]
      resourceNames: [argocd-operator-controller-manager]
    ---
    apiVersion: rbac.authorization.k8s.io/v1
    kind: RoleBinding
    metadata:
      name: argocd-installer-binding
      namespace: argocd
    roleRef:
      apiGroup: rbac.authorization.k8s.io
      kind: Role
      name: argocd-installer-role
    subjects:
    - kind: ServiceAccount
      name: argocd-installer
      namespace: argocd
    ---
    apiVersion: olm.operatorframework.io/v1
    kind: ClusterExtension
    metadata:
      name: argocd
    spec:
      namespace: argocd
      serviceAccount:
        name: argocd-installer
      source:
        sourceType: Catalog
        catalog:
          packageName: argocd-operator
          version: 0.2.0
    ```

If we view the current state of our ClusterExtension we should see that we have installed version 0.2.0:

```terminal
kubectl get clusterextension argocd -o jsonpath-as-json="{.status.install}"
```

!!! success "Command output"
    ``` json
    [
        {
            "bundle": {
                "name": "argocd-operator.v0.2.0",
                "version": "0.2.0"
            }
        }
    ]
    ```

* To initiate our upgrade, let's update the version field in the ClusterExtension resource:

    ``` terminal title="Method 1: apply a new ClusterExtension manifest"
    kubectl apply -f - <<EOF
    apiVersion: olm.operatorframework.io/v1
    kind: ClusterExtension
    metadata:
      name: argocd
    spec:
      namespace: argocd
      serviceAccount:
        name: argocd-installer
      source:
        sourceType: Catalog
        catalog:
          packageName: argocd-operator
          version: 0.2.1 # Update to version 0.2.1
    EOF
    ```

    !!! success "Method 1 output"
        ``` text
        clusterextension.olm.operatorframework.io/argocd configured
        ```

    Alternatively, you can use `kubectl patch` to update the version field:

    ``` terminal title="Method 2: run patch command"
    kubectl patch clusterextension argocd --type='merge' -p '{"spec": {"source": {"catalog": {"version": "0.2.1"}}}}'
    ```

    !!! success "Method 2 output"
        ``` text
        clusterextension.olm.operatorframework.io/argocd patched
        ```

* We can now verify that the ClusterExtension is updated to the new version:

    ``` terminal title="Get the current ClusterExtension version"
    kubectl get clusterextension argocd -o jsonpath-as-json="{.status.install}"
    ```

    !!! success "Updated ClusterExtension version"
        ``` json
        [
            {
                "bundle": {
                    "name": "argocd-operator.v0.2.1",
                    "version": "0.2.1"
                }
            }
        ]
        ```

!!! note "Note on the `kubectl.kubernetes.io/last-applied-configuration` annotation"
    After your upgrade, the contents of the `kubectl.kubernetes.io/last-applied-configuration` annotation field will
    differ depending on your method of upgrade. If you apply a new ClusterExtension manifest as in the first method shown,
    the last applied configuration will show the new version since we replaced the existing manifest. If you use the patch
    method or `kubectl edit clusterextension`, then the last applied configuration will show the old version.