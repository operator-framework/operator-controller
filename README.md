# catalogd

Catalogd is a Kubernetes extension that unpacks [file-based catalog (FBC)](https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs) content for on-cluster clients. Currently, catalogd unpacks FBC content that is packaged and distributed as container images. The catalogd road map includes plans for unpacking other content sources, such as Git repositories and OCI artifacts. For more information, see the catalogd [issues](https://github.com/operator-framework/catalogd/issues/) page. 

Catalogd helps customers discover installable content by hosting catalog metadata for Kubernetes extensions, such as Operators and controllers. For more information on the Operator Lifecycle Manager (OLM) v1 suite of microservices, see the [documentation](https://github.com/operator-framework/operator-controller/docs/) for the Operator Controller.

## Quick start 

**NOTE:** Procedure steps marked with an astericks (`*`) are likely to change with future API updates.

1. To install catalogd, navigate to the [releases](https://github.com/operator-framework/catalogd/releases/) page, and follow the install instructions included in the release you want to install.

1. Create a `Catalog` object that points to the OperatorHub Community catalog by running the following command:

    ```sh
    $ kubectl apply -f - << EOF
    apiVersion: catalogd.operatorframework.io/v1alpha1
    kind: Catalog
    metadata:
      name: operatorhubio
    spec:
      source:
        type: image
        image:
          ref: quay.io/operatorhubio/catalog:latest
    EOF
    ```

1. Verify the `Catalog` object was created successfully by running the following command:

    ```sh
    $ kubectl describe catalog/operatorhubio
    ```
    
    *Example output*
    ```sh
    Name:         operatorhubio
    Namespace:    
    Labels:       <none>
    Annotations:  <none>
    API Version:  catalogd.operatorframework.io/v1alpha1
    Kind:         Catalog
    Metadata:
      Creation Timestamp:  2023-06-23T18:35:13Z
      Generation:          1
      Managed Fields:
        API Version:  catalogd.operatorframework.io/v1alpha1
        Fields Type:  FieldsV1
        fieldsV1:
          f:metadata:
            f:annotations:
              .:
              f:kubectl.kubernetes.io/last-applied-configuration:
          f:spec:
            .:
            f:source:
              .:
              f:image:
                .:
                f:ref:
              f:type:
        Manager:      kubectl-client-side-apply
        Operation:    Update
        Time:         2023-06-23T18:35:13Z
        API Version:  catalogd.operatorframework.io/v1alpha1
        Fields Type:  FieldsV1
        fieldsV1:
          f:status:
            .:
            f:conditions:
            f:phase:
        Manager:         manager
        Operation:       Update
        Subresource:     status
        Time:            2023-06-23T18:35:43Z
      Resource Version:  1397
      UID:               709cee9d-c669-46e1-97d0-e97dcce8f388
    Spec:
      Source:
        Image:
          Ref:  quay.io/operatorhubio/catalog:latest
        Type:   image
    Status:
      Conditions:
        Last Transition Time:  2023-06-23T18:35:13Z
        Message:               
        Reason:                Unpacking
        Status:                False
        Type:                  Unpacked
      Phase:                   Unpacking
    Events:                    <none>
    ```

1. Run the following command to get a list of packages: `*`

    ```sh
    $ kubectl get packages
    ```

    *Example output*
    ```sh
    NAME                                                     AGE
    operatorhubio-ack-acm-controller                         69s
    operatorhubio-ack-apigatewayv2-controller                69s
    operatorhubio-ack-applicationautoscaling-controller      69s
    operatorhubio-ack-cloudtrail-controller                  69s
    operatorhubio-ack-dynamodb-controller                    69s
    operatorhubio-ack-ec2-controller                         69s
    operatorhubio-ack-ecr-controller                         69s
    operatorhubio-ack-eks-controller                         69s
    operatorhubio-ack-elasticache-controller                 69s
    operatorhubio-ack-emrcontainers-controller               69s
    operatorhubio-ack-eventbridge-controller                 69s
    operatorhubio-ack-iam-controller                         69s
    operatorhubio-ack-kinesis-controller                     69s
    operatorhubio-ack-kms-controller                         69s
    operatorhubio-ack-lambda-controller                      69s
    operatorhubio-ack-memorydb-controller                    69s
    operatorhubio-ack-mq-controller                          69s
    ...
    ```
1. Run the following command to get a list of bundles: `*`

    ```sh
    $ kubectl get bundlemetadata
    ```
    
    *Example output*
    ```sh
    NAME                                                            AGE
    operatorhubio-ack-acm-controller.v0.0.1                         2m15s
    operatorhubio-ack-acm-controller.v0.0.2                         2m15s
    operatorhubio-ack-acm-controller.v0.0.4                         2m15s
    operatorhubio-ack-acm-controller.v0.0.5                         2m15s
    operatorhubio-ack-acm-controller.v0.0.6                         2m15s
    operatorhubio-ack-acm-controller.v0.0.7                         2m15s
    operatorhubio-ack-apigatewayv2-controller.v0.0.10               2m15s
    operatorhubio-ack-apigatewayv2-controller.v0.0.11               2m15s
    operatorhubio-ack-apigatewayv2-controller.v0.0.12               2m15s
    operatorhubio-ack-apigatewayv2-controller.v0.0.13               2m15s
    operatorhubio-ack-apigatewayv2-controller.v0.0.14               2m15s
    operatorhubio-ack-apigatewayv2-controller.v0.0.15               2m15s
    operatorhubio-ack-apigatewayv2-controller.v0.0.16               2m15s
    operatorhubio-ack-apigatewayv2-controller.v0.0.17               2m15s
    operatorhubio-ack-apigatewayv2-controller.v0.0.18               2m15s
    operatorhubio-ack-apigatewayv2-controller.v0.0.19               2m15s
    operatorhubio-ack-apigatewayv2-controller.v0.0.20               2m15s
    operatorhubio-ack-apigatewayv2-controller.v0.0.21               2m15s
    operatorhubio-ack-apigatewayv2-controller.v0.0.22               2m14s
    operatorhubio-ack-apigatewayv2-controller.v0.0.9                2m14s
    operatorhubio-ack-apigatewayv2-controller.v0.1.0                2m14s
    operatorhubio-ack-apigatewayv2-controller.v0.1.1                2m14s
    operatorhubio-ack-apigatewayv2-controller.v0.1.2                2m14s
    operatorhubio-ack-apigatewayv2-controller.v0.1.3                2m14s
    operatorhubio-ack-apigatewayv2-controller.v0.1.4                2m14s
    operatorhubio-ack-apigatewayv2-controller.v0.1.5                2m14s
    operatorhubio-ack-apigatewayv2-controller.v0.1.6                2m14s
    operatorhubio-ack-apigatewayv2-controller.v1.0.0                2m14s
    operatorhubio-ack-apigatewayv2-controller.v1.0.2                2m14s
    operatorhubio-ack-apigatewayv2-controller.v1.0.3                2m14s
    operatorhubio-ack-apigatewayv2-controller.v1.0.4                2m14s
    ...
    ```

## Contributing
Thanks for your interest in contributing to `catalogd`!

`catalogd` is in the very early stages of development and a more in depth contributing guide will come in the near future.

In the mean time, it is assumed you know how to make contributions to open source projects in general and this guide will only focus on how to manually test your changes (no automated testing yet).

If you have any questions, feel free to reach out to us on the Kubernetes Slack channel [#olm-dev](https://kubernetes.slack.com/archives/C0181L6JYQ2) or [create an issue](https://github.com/operator-framework/catalogd/issues/new)
### Testing Local Changes
**Prerequisites**
- [Install kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)

**Local (not on cluster)**
> **Note**: This will work *only* for the controller
- Create a cluster:
```sh
kind create cluster
```
- Install CRDs and run the controller locally:
```sh
kubectl apply -f config/crd/bases/ && make run
```

**On Cluster**
- Build the images locally:
```sh
make docker-build-controller && make docker-build-server
```
- Create a cluster:
```sh
kind create cluster
```
- Load the images onto the cluster:
```sh
kind load docker-image quay.io/operator-framework/catalogd-controller:latest && kind load docker-image quay.io/operator-framework/catalogd-server:latest
``` 
- Install cert-manager:
```sh
 make cert-manager
```
- Install the CRDs
```sh
kubectl apply -f config/crd/bases/
```
- Deploy the apiserver, etcd, and controller: 
```sh
kubectl apply -f config/
```
- Create the sample Catalog (this will trigger the reconciliation loop): 
```sh
kubectl apply -f config/samples/core_v1alpha1_catalog.yaml
```
