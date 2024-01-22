# Install Modes and WatchNamespaces in OMLv1

Operator Lifecycle Manager (OLM) operates with cluster-admin privileges, enabling it to grant necessary permissions to the Extensions it deploys. For extensions packaged as [`RegistryV1`][registryv1] bundles, it's the responsibility of the authors to specify supported `InstallModes` in the ClusterServiceVersion ([CSV][csv]). InstallModes define the operational scope of the extension within the Kubernetes cluster, particularly in terms of namespace availability. The four recognized InstallModes are as follows:

1. OwnNamespace: This mode allows the extension to monitor and respond to events within its own deployment namespace.
1. SingleNamespace: In this mode, the extension is set up to observe events in a single, specific namespace other than the one it is deployed in.
1. MultiNamespace: This enables the extension to function across multiple specified namespaces.
1. AllNamespaces: Under this mode, the extension is equipped to monitor events across all namespaces within the cluster.

When creating a cluster extension, users have the option to define a list of `watchNamespaces`. This list determines the specific namespaces within which they intend the operator to operate. The configuration of `watchNamespaces` must align with the InstallModes supported by the extension as specified by the bundle author. The supported configurations in the order of preference are as follows:


| Length of `watchNamespaces` specified through ClusterExtension | Allowed values                                    | Supported InstallMode in CSV | Description                                                     |
|------------------------------|-------------------------------------------------------|----------------------|-----------------------------------------------------------------|
| **0 (Empty/Unset)**          | -                                                  | AllNamespaces        | Extension monitors all namespaces.      |
|                              | -                                                  | OwnNamespace         | Supported when `AllNamespaces` is false. Extension only active in its deployment namespace.    |
| **1 (Single Entry)**         | `""` (Empty String)                          | AllNamespaces        | Extension monitors all namespaces.                     |
|                              | Entry equals Install Namespace                        | OwnNamespace         | Extension watches only its install namespace.                   |
|                              | Entry is a specific namespace (not the Install Namespace) | SingleNamespace      | Extension monitors a single, specified namespace in the spec.               |
| **>1 (Multiple Entries)**    | Entries are specific, multiple namespaces             | MultiNamespace       | Extension monitors each of the specified multiple namespaces in the spec.


[registryv1]: https://olm.operatorframework.io/docs/tasks/creating-operator-manifests/#writing-your-operator-manifests
[csv]: https://olm.operatorframework.io/docs/concepts/crds/clusterserviceversion/