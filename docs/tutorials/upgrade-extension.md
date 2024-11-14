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

Suppose we have successfully created and installed v0.2.0 of the ArgoCD operator with the following `ClusterExtension`:

``` yaml title="Example CR"
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

* Update the version field in the ClusterExtension resource:

    ``` terminal
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

    !!! success
        ``` text title="Example output"
        clusterextension.olm.operatorframework.io/argocd configured
        ```

    Alternatively, you can use `kubectl patch` to update the version field:

    ``` terminal
    kubectl patch clusterextension argocd --type='merge' -p '{"spec": {"source": {"catalog": {"version": "0.2.1"}}}}'
    ```

    !!! success
        ``` text title="Example output"
        clusterextension.olm.operatorframework.io/argocd patched
        ```

* Verify that the Kubernetes extension is updated:

    ``` terminal
    kubectl get clusterextension argocd -o yaml
    ```

    ??? success
        ``` text title="Example output"
        apiVersion: olm.operatorframework.io/v1
        kind: ClusterExtension
        metadata:
          annotations:
            kubectl.kubernetes.io/last-applied-configuration: |
              {"apiVersion":"olm.operatorframework.io/v1","kind":"ClusterExtension","metadata":{"annotations":{},"name":"argocd"},"spec":{"namespace":"argocd","serviceAccount":{"name":"argocd-installer"},"source":{"catalog":{"packageName":"argocd-operator","version":"0.2.1"},"sourceType":"Catalog"}}}
          creationTimestamp: "2024-11-15T19:29:34Z"
          finalizers:
          - olm.operatorframework.io/cleanup-unpack-cache
          - olm.operatorframework.io/cleanup-contentmanager-cache
          generation: 2
          name: argocd
          resourceVersion: "7274"
          uid: 9af8e5f8-ae3d-4231-b15c-e63c62619db7
        spec:
          namespace: argocd
          serviceAccount:
            name: argocd-installer
          source:
            catalog:
              packageName: argocd-operator
              upgradeConstraintPolicy: CatalogProvided
              version: 0.2.1
            sourceType: Catalog
        status:
          conditions:
          - lastTransitionTime: "2024-11-15T19:29:34Z"
            message: ""
            observedGeneration: 2
            reason: Deprecated
            status: "False"
            type: Deprecated
          - lastTransitionTime: "2024-11-15T19:29:34Z"
            message: ""
            observedGeneration: 2
            reason: Deprecated
            status: "False"
            type: PackageDeprecated
          - lastTransitionTime: "2024-11-15T19:29:34Z"
            message: ""
            observedGeneration: 2
            reason: Deprecated
            status: "False"
            type: ChannelDeprecated
          - lastTransitionTime: "2024-11-15T19:29:34Z"
            message: ""
            observedGeneration: 2
            reason: Deprecated
            status: "False"
            type: BundleDeprecated
          - lastTransitionTime: "2024-11-15T19:29:37Z"
            message: Installed bundle quay.io/operatorhubio/argocd-operator@sha256:e1cfacacf891fb243ded2bcd449a4f5c76f3230bf96a4de32734a87303e087c8
              successfully
            observedGeneration: 2
            reason: Succeeded
            status: "True"
            type: Installed
          - lastTransitionTime: "2024-11-15T19:29:37Z"
            message: desired state reached
            observedGeneration: 2
            reason: Succeeded
            status: "True"
            type: Progressing
          install:
            bundle:
              name: argocd-operator.v0.2.1
              version: 0.2.1
        ```
