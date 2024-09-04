# catalogd

Catalogd is a Kubernetes extension that unpacks [file-based catalog (FBC)](https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs) content for on-cluster clients. Currently, catalogd unpacks FBC content that is packaged and distributed as container images. The catalogd road map includes plans for unpacking other content sources, such as Git repositories and OCI artifacts. For more information, see the catalogd [issues](https://github.com/operator-framework/catalogd/issues/) page. 

Catalogd helps customers discover installable content by hosting catalog metadata for Kubernetes extensions, such as Operators and controllers. For more information on the Operator Lifecycle Manager (OLM) v1 suite of microservices, see the [documentation](https://github.com/operator-framework/operator-controller/tree/main/docs) for the Operator Controller.

## Quickstart DEMO
[![asciicast](https://asciinema.org/a/624043.svg)](https://asciinema.org/a/624043)

## Quickstart Steps
Procedure steps marked with an asterisk (`*`) are likely to change with future API updates.

**NOTE:** The examples below use the `-k` flag in curl to skip validating the TLS certificates. This is for demonstration purposes only.

1. To install catalogd, navigate to the [releases](https://github.com/operator-framework/catalogd/releases/) page, and follow the install instructions included in the release you want to install.

1. Create a `ClusterCatalog` object that points to the OperatorHub Community catalog by running the following command:

    ```sh
    $ kubectl apply -f - << EOF
    apiVersion: olm.operatorframework.io/v1alpha1
    kind: ClusterCatalog
    metadata:
      name: operatorhubio
    spec:
      source:
        type: image
        image:
          ref: quay.io/operatorhubio/catalog:latest
    EOF
    ```

1. Verify the `ClusterCatalog` object was created successfully by running the following command:

    ```sh
    $ kubectl describe clustercatalog/operatorhubio
    ```
    
    *Example output*
    ```sh
    Name:         operatorhubio
    Namespace:    
    Labels:       <none>
    Annotations:  <none>
    API Version:  olm.operatorframework.io/v1alpha1
    Kind:         ClusterCatalog
    Metadata:
      Creation Timestamp:  2023-06-23T18:35:13Z
      Generation:          1
      Managed Fields:
        API Version:  olm.operatorframework.io/v1alpha1
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
        API Version:  olm.operatorframework.io/v1alpha1
        Fields Type:  FieldsV1
        fieldsV1:
          f:status:
            .:
            f:conditions:
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
    Events:                    <none>
    ```

1. Port forward the `catalogd-catalogserver` service in the `olmv1-system` namespace:
    ```sh
    $ kubectl -n olmv1-system port-forward svc/catalogd-catalogserver 8080:443
    ```

1. Run the following command to get a list of packages:

    ```sh
    $ curl -k https://localhost:8080/catalogs/operatorhubio/all.json | jq -s '.[] | select(.schema == "olm.package") | .name'
    ```

    *Example output*
    ```sh
      % Total    % Received % Xferd  Average Speed   Time    Time     Time  Current
                                    Dload  Upload   Total   Spent    Left  Speed
    100  110M  100  110M    0     0   112M      0 --:--:-- --:--:-- --:--:--  112M
    "ack-acm-controller"
    "ack-apigatewayv2-controller"
    "ack-applicationautoscaling-controller"
    "ack-cloudtrail-controller"
    "ack-cloudwatch-controller"
    "ack-dynamodb-controller"
    "ack-ec2-controller"
    "ack-ecr-controller"
    "ack-eks-controller"
    "ack-elasticache-controller"
    "ack-emrcontainers-controller"
    "ack-eventbridge-controller"
    "ack-iam-controller"
    "ack-kinesis-controller"
    ...
    ```
1. Run the following command to get a list of channels for the `ack-acm-controller` package:

    ```sh
    $ curl -k https://localhost:8080/catalogs/operatorhubio/all.json | jq -s '.[] | select(.schema == "olm.channel") | select(.package == "ack-acm-controller") | .name'
    ```

    *Example output*
    ```sh
      % Total    % Received % Xferd  Average Speed   Time    Time     Time  Current
                                    Dload  Upload   Total   Spent    Left  Speed
    100  110M  100  110M    0     0   115M      0 --:--:-- --:--:-- --:--:--  116M
    "alpha"
    ```

1. Run the following command to get a list of bundles belonging to the `ack-acm-controller` package:

    ```sh
    $ curl -k https://localhost:8080/catalogs/operatorhubio/all.json | jq -s '.[] | select(.schema == "olm.bundle") | select(.package == "ack-acm-controller") | .name'
    ```
    
    *Example output*
    ```sh
      % Total    % Received % Xferd  Average Speed   Time    Time     Time  Current
                                    Dload  Upload   Total   Spent    Left  Speed
    100  110M  100  110M    0     0   122M      0 --:--:-- --:--:-- --:--:--  122M
    "ack-acm-controller.v0.0.1"
    "ack-acm-controller.v0.0.2"
    "ack-acm-controller.v0.0.4"
    "ack-acm-controller.v0.0.5"
    "ack-acm-controller.v0.0.6"
    "ack-acm-controller.v0.0.7"
    ```

## Contributing
Thanks for your interest in contributing to `catalogd`!

`catalogd` is in the very early stages of development and a more in depth contributing guide will come in the near future.

In the mean time, it is assumed you know how to make contributions to open source projects in general and this guide will only focus on how to manually test your changes (no automated testing yet).

If you have any questions, feel free to reach out to us on the Kubernetes Slack channel [#olm-dev](https://kubernetes.slack.com/archives/C0181L6JYQ2) or [create an issue](https://github.com/operator-framework/catalogd/issues/new)
### Testing Local Changes
**Prerequisites**
- [Install kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)

**Test it out**

```sh
make run
```

This will build a local container image for the catalogd controller, create a new KIND cluster and then deploy onto that cluster.
