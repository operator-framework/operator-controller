---
hide:
  - toc
---

# Install an Extension from a Catalog

In Operator Lifecycle Manager (OLM) 1.0, Kubernetes extensions are scoped to the cluster.
After you add a catalog to your cluster, you can install an extension by creating a custom resource (CR) and applying it.

## Prerequisites

* A catalog that is being served
* The name, and optionally version, or channel, of the [supported extension](../project/olmv1_limitations.md) to be installed
* An existing namespace in which to install the extension

### ServiceAccount for ClusterExtension Installation and Management

Adhering to OLM v1's "Secure by Default" tenet, OLM v1 does not have the permissions
necessary to install content. This follows the least privilege principle and reduces
the chance of a [confused deputy attack](https://en.wikipedia.org/wiki/Confused_deputy_problem).
Instead, users must explicitly specify a ServiceAccount that will be used to perform the
installation and management of a specific ClusterExtension.

The ServiceAccount must be configured with the RBAC permissions required by the ClusterExtension.
If the permissions do not meet the minimum requirements, installation will fail. If no ServiceAccount
is provided in the ClusterExtension manifest, then the manifest will be rejected.

For information on determining the ServiceAccount's permission, please see [Derive minimal ServiceAccount required for ClusterExtension Installation and Management](../howto/derive-service-account.md).


## Procedure

1. Create a CR for the Kubernetes extension you want to install. You can also specify arbitrary configuration values under `spec.config` (per [RFC: Registry+v1 Configuration Support](../../RFC_Config_registry+v1_bundle_config.md)):

    ```yaml title="Example CR"
    apiVersion: olm.operatorframework.io/v1
    kind: ClusterExtension
    metadata:
      name: <extension_name>
    spec:
      namespace: <namespace_name>
      serviceAccount:
        name: <serviceAccount_name>
      source:
        sourceType: Catalog
        catalog:
          packageName: <package_name>
          channels: [<channel1>,<channel2>]
          version: "<version>"
      config:
        version: "v2.0.0-demo"
        name:    "demo-configmap"
    ```

     `extension_name`
     : Specifies a custom name for the Kubernetes extension you want to install, such as `my-camel-k`.

     `package_name`
     : Specifies the name of the package you want to install, such as `camel-k`.

     `channels`
     : Optional: Specifies a set of the extension's channels from which to select, such as `stable` or `fast`.

     `version`
     : Optional: Specifies the version or version range you want installed, such as `1.3.1` or `"<2"`.
     If you use a comparison string to define a version range, the string must be surrounded by double quotes (`"`).

    `namespace_name`
    : Specifies a name for the namespace in which the bundle of content for the package referenced
    in the packageName field will be applied.

    `serviceAccount_name`
    : serviceAccount name is a required reference to a ServiceAccount that exists
    in the `namespace_name`. The provided ServiceAccount is used to install and
    manage the content for the package specified in the packageName field.

    !!! warning
        Currently, the following limitations affect the installation of extensions:

        * If multiple catalogs are added to a cluster, you cannot specify a catalog when you install an extension.
        * OLM 1.0 requires that all of the extensions have unique bundle and package names for dependency resolution.

        As a result, if two catalogs have an extension with the same name, the installation might fail or lead to an unintended outcome.
        For example, the first extension that matches might install successfully and finish without searching for a match in the second catalog.

2. Apply the CR to the cluster:

    ``` terminal
    kubectl apply -f <cr_name>.yaml
    ```

    !!! success
        ``` text title="Example output"
        clusterextension.olm.operatorframework.io/argocd created
        ```

### Verification

* Describe the installed extension:

    ``` terminal
    kubectl describe clusterextensions
    ```

    ??? success
        ``` text title="Example output"
        Name:         argocd
        Namespace:
        Labels:       <none>
        Annotations:  <none>
        API Version:  olm.operatorframework.io/v1
        Kind:         ClusterExtension
        Metadata:
          Creation Timestamp:  2024-11-11T13:41:23Z
          Finalizers:
            olm.operatorframework.io/cleanup-unpack-cache
            olm.operatorframework.io/cleanup-contentmanager-cache
          Generation:        1
          Resource Version:  5426
          UID:               bde55f03-abe2-48af-8c09-28d32df878ad
        Spec:
          Namespace:  argocd
          Service Account:
            Name:  argocd-installer
          Source:
            Catalog:
              Package Name:               argocd-operator
              Upgrade Constraint Policy:  CatalogProvided
              Version:                    0.6.0
            Source Type:                  Catalog
        Status:
          Conditions:
            Last Transition Time:  2024-11-11T13:41:23Z
            Message:
            Observed Generation:   1
            Reason:                Deprecated
            Status:                False
            Type:                  Deprecated
            Last Transition Time:  2024-11-11T13:41:23Z
            Message:
            Observed Generation:   1
            Reason:                Deprecated
            Status:                False
            Type:                  PackageDeprecated
            Last Transition Time:  2024-11-11T13:41:23Z
            Message:
            Observed Generation:   1
            Reason:                Deprecated
            Status:                False
            Type:                  ChannelDeprecated
            Last Transition Time:  2024-11-11T13:41:23Z
            Message:
            Observed Generation:   1
            Reason:                Deprecated
            Status:                False
            Type:                  BundleDeprecated
            Last Transition Time:  2024-11-11T13:41:31Z
            Message:               Installed bundle quay.io/operatorhubio/argocd-operator@sha256:d538c45a813b38ef0e44f40d279dc2653f97ca901fb660da5d7fe499d51ad3b3 successfully
            Observed Generation:   1
            Reason:                Succeeded
            Status:                True
            Type:                  Installed
            Last Transition Time:  2024-11-11T13:41:32Z
            Message:               desired state reached
            Observed Generation:   1
            Reason:                Succeeded
            Status:                True
            Type:                  Progressing
          Install:
            Bundle:
              Name:     argocd-operator.v0.6.0
              Version:  0.6.0
        Events:         <none>
        ```
