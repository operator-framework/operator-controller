---
hide:
  - toc
---

# Add a Catalog of Extensions to a Cluster

Extension authors can publish their products in catalogs.
ClusterCatalogs are curated collections of Kubernetes extensions, such as Operators.
Cluster administrators can add these ClusterCatalogs to their cluster.
Cluster administrators can enable polling to get over-the-air updates to ClusterCatalogs when extension authors publish changes such as bug fixes and new features.

For example, the [Kubernetes community Operators catalog](https://github.com/k8s-operatorhub/community-operators) is a catalog of curated extensions that is developed by the Kubernetes community.
You can see the available extensions at [Operatorhub.io](https://operatorhub.io).
This catalog is distributed as an image [quay.io/operatorhubio/catalog](https://quay.io/repository/operatorhubio/catalog?tag=latest&tab=tags) that can be installed on clusters.

## Prerequisites

* Access to a Kubernetes cluster, for example `kind`, using an account with `cluster-admin` permissions
* [Operator Controller installed](https://github.com/operator-framework/operator-controller/releases) on the cluster
* [Catalogd installed](https://github.com/operator-framework/catalogd/releases/) on the cluster
* Kubernetes CLI (`kubectl`) installed on your workstation

## Procedure

1. Create a catalog custom resource (CR):

    ``` yaml title="clustercatalog_cr.yaml"
    apiVersion: olm.operatorframework.io/v1alpha1
    kind: ClusterCatalog
    metadata:
      name: operatorhubio
    spec:
      source:
        type: Image
        image:
          ref: <catalog_image>
          pollInterval: <poll_interval_duration>
    ```

    `catalog_name`
    :   Specifies the image reference for the catalog you want to install, such as `quay.io/operatorhubio/catalog:latest`.

    `poll_interval_duration`
    :   Specifies the interval for polling the remote registry for newer image digests.
            The default value is `24h`.
            Valid units include seconds (`s`), minutes (`m`), and hours (`h`).
            To disable polling, set a zero value, such as `0s`.

    ``` yaml title="Example `operatorhubio.yaml` CR"
    apiVersion: olm.operatorframework.io/v1alpha1
    kind: ClusterCatalog
    metadata:
      name: operatorhub
    spec:
      source:
        type: Image
        image:
          ref: quay.io/operatorhubio/catalog:latest
          pollInterval: 10m
    ```

2. Apply the ClusterCatalog CR:

    ``` terminal
    kubectl apply -f <clustercatalog_cr>.yaml
    ```

    ``` text title="Example output"
    clustercatalog.olm.operatorframework.io/operatorhubio created
    ```

### Verification

* Run the following commands to verify the status of your catalog:

    * Check if your catalog is available on the cluster:

        ``` terminal
        kubectl get clustercatalog
        ```

        ``` terminal title="Example output"
        NAME           LASTUNPACKED   AGE
        operatorhubio  9m31s          9m55s
        ```

    * Check the status of your catalog:

        ``` terminal
        kubectl describe clustercatalog
        ```

        ``` terminal title="Example output"
        Name:         operatorhubio
        Namespace:
        Labels:       olm.operatorframework.io/metadata.name=operatorhubio
        Annotations:  <none>
        API Version:  olm.operatorframework.io/v1alpha1
        Kind:         ClusterCatalog
        Metadata:
        Creation Timestamp:  2024-10-02T19:51:24Z
        Finalizers:
            olm.operatorframework.io/delete-server-cache
        Generation:        1
        Resource Version:  33321
        UID:               52894532-0646-41a5-8285-c7f48bba49e4
        Spec:
          Priority:  0
          Source:
            Image:
              Poll Interval:  10m0s
              Ref:            quay.io/operatorhubio/catalog:latest
            Type:             Image
        Status:
          Conditions:
            Last Transition Time:  2024-03-12T19:35:34Z
            Message:               Successfully unpacked and stored content from resolved source
            Observed Generation:   2
            Reason:                Succeeded
            Status:                False
            Type:                  Progressing
            Last Transition Time:  2024-03-12T19:35:34Z
            Message:               Serving desired content from resolved source
            Observed Generation:   2
            Reason:                Available
            Status:                True
            Type:                  Serving
          Content URL:             https://catalogd-server.olmv1-system.svc/catalogs/operatorhubio/all.json
          Last Unpacked:           2024-03-12T19:35:34Z
          Resolved Source:
            Image:
              Last Successful Poll Attempt:  2024-03-12T19:35:26Z
              Ref:                           quay.io/operatorhubio/catalog@sha256:dee29aaed76fd1c72b654b9bc8bebc4b48b34fd8d41ece880524dc0c3c1c55ec
            Type:                            Image
        Events:                              <none>
        ```
