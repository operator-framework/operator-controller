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
    apiVersion: olm.operatorframework.io/v1alpha1
    kind: ClusterExtension
    metadata:
      name: <extension_name>
    spec:
      packageName: <package_name>
      channel: <channel>
      version: "<version>"
    ```

     `extension_name`
     : Specifies a custom name for the Kubernetes extension you want to install, such as `my-camel-k`.

     `package_name`
     : Specifies the name of the package you want to install, such as `camel-k`.

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
        For example, the first extension that matches might install successfully and finish without searching for a match in the second catalog.

2. Apply the CR to the cluster:

    ``` terminal
    kubectl apply -f <cr_name>.yaml
    ```

    ??? success
        ``` text title="Example output"
        clusterextension.olm.operatorframework.io/camel-k created
        ```

### Verification

* Describe the installed extension:

    ``` terminal
    kubectl describe clusterextensions
    ```

    ??? success
        ``` text title="Example output"
        Name:         my-camel-k
        Namespace:
        Labels:       <none>
        Annotations:  <none>
        API Version:  olm.operatorframework.io/v1alpha1
        Kind:         ClusterExtension
        Metadata:
          Creation Timestamp:  2024-03-15T15:03:47Z
          Generation:          1
          Resource Version:    7691
          UID:                 d756879f-217d-4ebe-85b1-8427bbb2f1df
        Spec:
          Package Name:               camel-k
          Upgrade Constraint Policy:  Enforce
        Status:
          Conditions:
            Last Transition Time:     2024-03-15T15:03:50Z
            Message:                  resolved to "quay.io/operatorhubio/camel-k@sha256:d2b74c43ec8f9294450c9dcf2057be328d0998bb924ad036db489af79d1b39c3"
            Observed Generation:      1
            Reason:                   Success
            Status:                   True
            Type:                     Resolved
            Last Transition Time:     2024-03-15T15:04:13Z
            Message:                  installed from "quay.io/operatorhubio/camel-k@sha256:d2b74c43ec8f9294450c9dcf2057be328d0998bb924ad036db489af79d1b39c3"
            Observed Generation:      1
            Reason:                   Success
            Status:                   True
            Type:                     Installed
          Installed Bundle Resource:  quay.io/operatorhubio/camel-k@sha256:d2b74c43ec8f9294450c9dcf2057be328d0998bb924ad036db489af79d1b39c3
          Resolved Bundle Resource:   quay.io/operatorhubio/camel-k@sha256:d2b74c43ec8f9294450c9dcf2057be328d0998bb924ad036db489af79d1b39c3
        Events:                       <none>
        ```
