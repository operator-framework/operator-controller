# Deleting an extension

You can uninstall a Kubernetes extension and its associated custom resource definitions (CRD) by deleting the extension's custom resource (CR).

## Prerequisites

* You have an extension installed.

## Procedure

* Delete the extension's CR:

    ``` terminal
    $ kubectl delete clusterextensions <extension_name>
    ```

    `extension_name`
    : Specifies the name defined in the `metadata.name` field of the extension's CR.

    ``` text title="Example output"
    clusterextension.olm.operatorframework.io "argocd-operator" deleted
    ```

### Verification

1. Verify that the Kubernetes extension is deleted:

    ``` terminal
    $ kubectl get clusterextension.olm.operatorframework.io
    ```

    ``` text title="Example output"
    No resources found
    ```

2. Verify that the extension's system namespace is deleted:

    ``` terminal
    $ kubectl get ns <extension_name>-system
    ```

    ``` text title="Example output"
    Error from server (NotFound): namespaces "argo-operator-system" not found
    ```
