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
* Kubernetes CLI (`kubectl`) installed on your workstation

## Procedure

1. Create a catalog custom resource (CR):

    ``` yaml title="clustercatalog_cr.yaml"
    apiVersion: olm.operatorframework.io/v1
    kind: ClusterCatalog
    metadata:
      name: <catalog_name>
    spec:
      source:
        type: Image
        image:
          ref: <catalog_image>
          pollIntervalMinutes: <poll_interval_duration>
    ```

    `catalog_image`
    :   Specifies the image reference for the catalog you want to install, such as `quay.io/operatorhubio/catalog:latest`.

    `poll_interval_duration`
    :   Specifies the number of minutes for polling the remote registry for newer image digests.
        This field is optional. To disable polling, unset the field.

    ``` yaml title="Example `operatorhubio.yaml` CR"
    apiVersion: olm.operatorframework.io/v1
    kind: ClusterCatalog
    metadata:
      name: operatorhubio
    spec:
      source:
        type: Image
        image:
          ref: quay.io/operatorhubio/catalog:latest
          pollIntervalMinutes: 10
    ```

2. Apply the ClusterCatalog CR:

    ``` terminal
    kubectl apply -f operatorhubio.yaml
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
        NAME            LASTUNPACKED   SERVING   AGE
        operatorhubio   18s            True      27s
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
        API Version:  olm.operatorframework.io/v1
        Kind:         ClusterCatalog
        Metadata:
          Creation Timestamp:  2024-11-13T15:11:08Z
          Finalizers:
            olm.operatorframework.io/delete-server-cache
          Generation:        1
          Resource Version:  3069
          UID:               2c94ebf8-32ea-4a62-811a-c7098cd2d4db
        Spec:
          Availability Mode:  Available
          Priority:           0
          Source:
            Image:
              Poll Interval Minutes:  10
              Ref:                    quay.io/operatorhubio/catalog:latest
            Type:                     Image
        Status:
          Conditions:
            Last Transition Time:  2024-11-13T15:11:19Z
            Message:               Successfully unpacked and stored content from resolved source
            Observed Generation:   1
            Reason:                Succeeded
            Status:                True
            Type:                  Progressing
            Last Transition Time:  2024-11-13T15:11:19Z
            Message:               Serving desired content from resolved source
            Observed Generation:   1
            Reason:                Available
            Status:                True
            Type:                  Serving
          Last Unpacked:           2024-11-13T15:11:18Z
          Resolved Source:
            Image:
              Ref:  quay.io/operatorhubio/catalog@sha256:3cd8fde1dfd4269467451c4b2c77d4196b427004f2eb82686376f28265655c1c
            Type:   Image
          Urls:
            Base:  https://catalogd-service.olmv1-system.svc/catalogs/operatorhubio
        Events:    <none>
        ```
