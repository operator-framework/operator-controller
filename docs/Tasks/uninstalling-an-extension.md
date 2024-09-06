# Deleting an extension

You can uninstall a Kubernetes extension and its associated custom resource definitions (CRD) by deleting the extension's custom resource (CR).

## Prerequisites

* You have an extension installed.

## Procedure

* Delete the extension's CR:

    ``` terminal
    kubectl delete clusterextensions <extension_name>
    ```

    `extension_name`
    : Specifies the name defined in the `metadata.name` field of the extension's CR.

    ``` text title="Example output"
    clusterextension.olm.operatorframework.io "argocd-operator" deleted
    ```

### Verification

* Verify that the Kubernetes extension is deleted:

    ``` terminal
    kubectl get clusterextension.olm.operatorframework.io
    ```

    ``` text title="Example output"
    No resources found
    ```
  
### Cleanup

* Remove the extension namespace, and installer service account cluster-scoped RBAC resources (if applicable).
