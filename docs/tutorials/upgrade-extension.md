---
hide:
  - toc
---

# Upgrade an Extension

Existing extensions can be upgraded by updating the version field in the ClusterExtension resource.

For information on downgrading an extension, see [Downgrade an Extension](downgrade-extension.md).

## Prerequisites

* You have an extension installed
* The target version is compatible with OLM v1 (see [OLM v1 limitations](../project/olmv1_limitations.md))
* CRD compatibility between the versions being upgraded or downgraded (see [CRD upgrade safety](../concepts/crd-upgrade-safety.md))
* The installer service account's RBAC permissions are adequate for the target version (see [Minimal RBAC for Installer Service Account](../howto/derive-service-account.md))

For more detailed information see [Upgrade Support](../concepts/upgrade-support.md).

## Procedure

Suppose we have successfully created and installed v0.5.0 of the ArgoCD operator with the following `ClusterExtension`:

``` yaml title="Example CR"
apiVersion: olm.operatorframework.io/v1alpha1
kind: ClusterExtension
metadata:
  name: argocd
spec:
  source:
    sourceType: Catalog
    catalog:
      packageName: argocd-operator
      version: 0.5.0
  install:
    namespace: argocd
    serviceAccount:
      name: argocd-installer
```

* Update the version field in the ClusterExtension resource:

    ``` terminal
      kubectl apply -f - <<EOF
      apiVersion: olm.operatorframework.io/v1alpha1
      kind: ClusterExtension
        metadata:
          name: argocd
        spec:
          source:
            sourceType: Catalog
            catalog:
              packageName: argocd-operator
              version: 0.6.0 # Update to version 0.6.0
          install:
            namespace: argocd
            serviceAccount:
              name: argocd-installer
      EOF
    ```

    ??? success
    ``` text title="Example output"
    clusterextension.olm.operatorframework.io/argocd-operator configured
    ```

    Alternatively, you can use `kubectl patch` to update the version field:

    ``` terminal
    kubectl patch clusterextension <extension_name> --type='merge' -p '{"spec": {"source": {"catalog": {"version": "<target_version>"}}}}'
    ```

    `extension_name`
    : Specifies the name defined in the `metadata.name` field of the extension's CR.
    
    `target_version`
    : Specifies the version to upgrade or downgrade to.

    ??? success
      ``` text title="Example output"
      clusterextension.olm.operatorframework.io/argocd-operator patched
      ```

### Verification

* Verify that the Kubernetes extension is deleted:

    ``` terminal
    kubectl get clusterextension.olm.operatorframework.io/<extension_name>
    ```

    ??? success
      ``` text title="Example output"
        apiVersion: olm.operatorframework.io/v1alpha1
        kind: ClusterExtension
        metadata:
        annotations:
            kubectl.kubernetes.io/last-applied-configuration: |
            {"apiVersion":"olm.operatorframework.io/v1alpha1","kind":"ClusterExtension","metadata":{"annotations":{},"name":"argocd"},"spec":{"install":{"namespace":"argocd","serviceAccount":{"name":"argocd-installer"}},"source":{"catalog":{"packageName":"argocd-operator","version":"0.6.0"},"sourceType":"Catalog"}}}
        creationTimestamp: "2024-10-03T16:02:40Z"
        finalizers:
        - olm.operatorframework.io/cleanup-unpack-cache
        - olm.operatorframework.io/cleanup-contentmanager-cache
        generation: 2
        name: argocd
        resourceVersion: "1174"
        uid: 0fcaf3f5-d142-4c7e-8d88-c88a549f7764
        spec:
        install:
            namespace: argocd
            serviceAccount:
            name: argocd-installer
        source:
            catalog:
            packageName: argocd-operator
            selector: {}
            upgradeConstraintPolicy: CatalogProvided
            version: 0.6.0
            sourceType: Catalog
        status:
        conditions:
        - lastTransitionTime: "2024-10-03T16:02:41Z"
            message: ""
            observedGeneration: 2
            reason: Deprecated
            status: "False"
            type: Deprecated
        - lastTransitionTime: "2024-10-03T16:02:41Z"
            message: ""
            observedGeneration: 2
            reason: Deprecated
            status: "False"
            type: PackageDeprecated
        - lastTransitionTime: "2024-10-03T16:02:41Z"
            message: ""
            observedGeneration: 2
            reason: Deprecated
            status: "False"
            type: ChannelDeprecated
        - lastTransitionTime: "2024-10-03T16:02:41Z"
            message: ""
            observedGeneration: 2
            reason: Deprecated
            status: "False"
            type: BundleDeprecated
        - lastTransitionTime: "2024-10-03T16:02:43Z"
            message: Installed bundle quay.io/operatorhubio/argocd-operator@sha256:d538c45a813b38ef0e44f40d279dc2653f97ca901fb660da5d7fe499d51ad3b3
            successfully
            observedGeneration: 2
            reason: Succeeded
            status: "True"
            type: Installed
        - lastTransitionTime: "2024-10-03T16:02:43Z"
            message: desired state reached
            observedGeneration: 2
            reason: Succeeded
            status: "False"
            type: Progressing
        install:
            bundle:
            name: argocd-operator.v0.6.0
            version: 0.6.0
      ```
