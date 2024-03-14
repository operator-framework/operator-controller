# Installing an extension from a catalog

In Operator Lifecycle Manager (OLM) 1.0, Kubernetes extensions are scoped to the cluster.
After you add a catalog to your cluster, you can install an extension by creating a custom resource (CR) and applying it.

!!! important

    Currently, extensions that use webhooks or target a single or specified set of namespaces cannot be installed.
    Extensions must not include webhooks and must use the `AllNamespaces` install mode.


## Prerequisites

* The `jq` CLI tool is installed.
* You have added a catalog to your cluster.

## Procedure

1. Create a CR for the Kubernetes extension you want to install:

    ``` yaml title="Example CR"
    apiVersion: clusterextension.operatorframework.io/v1alpha1
    kind: ClusterExtension
    metadata:
      name: <extension_name>
    spec:
      packageName: <package_name>
      channel: <channel>
      version: "<version>"
    ```

     `extension_name`
     : Specifies a custom name for the Kubernetes extension you want to install, such as `my-argocd`.

     `package_name`
     : Specifies the name of the package you want to install, such as `argocd-operator`.

     `channel`
     : Optional: Specifies the extension's channel, such as `stable` or `candidate`.

     `version`
     : Optional: Specifies the version or version range you want installed, such as `1.3.1` or `"<2"`.
     If you use a comparison string to define a version range, the string must be surrounded by double quotes (`"`).

    !!! warning
        Currently, the following limitations affect the installation of extensions:

        * If mulitple catalogs are added to a cluster, you cannot specify a catalog when you install an extension.
        * OLM 1.0 requires that all of the extensions have unique bundle and package names for dependency resolution.

        As a result, if two catalogs have an extension with the same name, the installation might fail or lead to an unintended outcome.

2. Apply the CR the cluster:

    ``` terminal
    $ kubectl apply -f <cr_name>.yaml
    ```

    ??? success
        ``` text title="Example output"
        clusterextension.olm.operatorframework.io/argocd-operator created
        ```

### Verification

* Get information about your bundle deployment:

    ``` terminal
    $ kubectl get bundledeployment
    ```

    ??? success
        ``` text title="Example output"
        NAME              ACTIVE BUNDLE   INSTALL STATE   AGE
        argocd-operator                                   111s
        ```

* Describe the installed extension:

    ``` terminal
    $ kubectl describe clusterextensions
    ```

    ??? success
        ``` text title="Example output"
        Name:         argocd-operator
        Namespace:
        Labels:       <none>
        Annotations:  <none>
        API Version:  olm.operatorframework.io/v1alpha1
        Kind:         ClusterExtension
        Metadata:
          Creation Timestamp:  2024-03-14T19:42:43Z
          Generation:          2
          Resource Version:    371915
          UID:                 6f37c260-327f-4aa3-9ba1-fa1d9bc20621
        Spec:
          Package Name:               argocd-operator
          Upgrade Constraint Policy:  Enforce
        Status:
          Conditions:
            Last Transition Time:    2024-03-14T19:42:47Z
            Message:                 bundledeployment status is unknown
            Observed Generation:     2
            Reason:                  InstallationStatusUnknown
            Status:                  Unknown
            Type:                    Installed
            Last Transition Time:    2024-03-14T19:49:52Z
            Message:                 resolved to "quay.io/operatorhubio/argocd-operator@sha256:046a9764dadcbef0b9ce67e367393fb1c8e3b1d24e361341f33ac5fb93cf32a1"
            Observed Generation:     2
            Reason:                  Success
            Status:                  True
            Type:                    Resolved
          Resolved Bundle Resource:  quay.io/operatorhubio/argocd-operator@sha256:046a9764dadcbef0b9ce67e367393fb1c8e3b1d24e361341f33ac5fb93cf32a1
        Events:                      <none>
        ```
