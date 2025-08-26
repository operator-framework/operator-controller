[![unit-test](https://github.com/operator-framework/operator-controller/actions/workflows/unit-test.yaml/badge.svg)](https://github.com/operator-framework/operator-controller/actions/workflows/unit-test.yaml)
[![e2e](https://github.com/operator-framework/operator-controller/actions/workflows/e2e.yaml/badge.svg)](https://github.com/operator-framework/operator-controller/actions/workflows/e2e.yaml)
[![codecov](https://codecov.io/gh/operator-framework/operator-controller/graph/badge.svg?token=5f34zaWaN7)](https://codecov.io/gh/operator-framework/operator-controller)

# operator-controller
The operator-controller is the central component of Operator Lifecycle Manager (OLM) v1.
It extends Kubernetes with an API through which users can install extensions.

## Overview

OLM v1 is the follow-up to [OLM v0](https://github.com/operator-framework/operator-lifecycle-manager). Its purpose is to provide APIs, 
controllers, and tooling that support the packaging, distribution, and lifecycling of Kubernetes extensions. It aims to:

- align with Kubernetes designs and user assumptions
- provide secure, high-quality, and predictable user experiences centered around declarative GitOps concepts
- give cluster admins the minimal necessary controls to build their desired cluster architectures and to have ultimate control

OLM v1 consists of two different components:

* operator-controller
* catalogd

For a more complete overview of OLM v1 and how it differs from OLM v0, see our [overview](docs/project/olmv1_design_decisions.md).

## Documentation

The documentation currently lives at [website](https://operator-framework.github.io/operator-controller/). The source of the documentation exists in this repository, see [docs directory](docs/).

## Getting Started

To get started with OLM v1, please see our [Getting Started](https://operator-framework.github.io/operator-controller/getting-started/olmv1_getting_started/) documentation.

## ClusterCatalog

### Quickstart DEMO

[![asciicast](https://asciinema.org/a/682344.svg)](https://asciinema.org/a/682344)

### ClusterCatalog Quickstart Steps

Procedure steps marked with an asterisk (`*`) are likely to change with future API updates.

**NOTE:** The examples below use the `-k` flag in curl to skip validating the TLS certificates. This is for demonstration purposes only.

1. To get started with OLM v1, please see our [Getting Started](https://operator-framework.github.io/operator-controller/getting-started/olmv1_getting_started/) documentation.

1. Create a `ClusterCatalog` object that points to the OperatorHub Community catalog by running the following command:

    ```sh
    $ kubectl apply -f - << EOF
    apiVersion: olm.operatorframework.io/v1
    kind: ClusterCatalog
    metadata:
      name: operatorhubio
    spec:
      source:
        type: Image
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
    Labels:       olm.operatorframework.io/metadata.name=operatorhubio
    Annotations:  <none>
    API Version:  olm.operatorframework.io/v1
    Kind:         ClusterCatalog
    Metadata:
      Creation Timestamp:  2024-10-17T13:48:46Z
      Finalizers:
        olm.operatorframework.io/delete-server-cache
      Generation:        1
      Resource Version:  7908
      UID:               34eeaa91-9f8e-4254-9937-0ae9d25e92df
    Spec:
      Availability Mode:  Available
      Priority:  0
      Source:
        Image:
          Ref:            quay.io/operatorhubio/catalog:latest
        Type:             Image
    Status:
      Conditions:
        Last Transition Time:  2024-10-17T13:48:59Z
        Message:               Successfully unpacked and stored content from resolved source
        Observed Generation:   1
        Reason:                Succeeded
        Status:                False
        Type:                  Progressing
        Last Transition Time:  2024-10-17T13:48:59Z
        Message:               Serving desired content from resolved source
        Observed Generation:   1
        Reason:                Available
        Status:                True
        Type:                  Serving
      Last Unpacked:           2024-10-17T13:48:58Z
      Resolved Source:
        Image:
          Last Successful Poll Attempt:  2024-10-17T14:49:59Z
          Ref:                           quay.io/operatorhubio/catalog@sha256:82be554b15ff246d8cc428f8d2f4cf5857c02ce3225d95d92a769ea3095e1fc7
        Type:                            Image
      Urls:
        Base:  https://catalogd-service.olmv1-system.svc/catalogs/operatorhubio
    Events:    <none>
   ```

1. Port forward the `catalogd-service` service in the `olmv1-system` namespace:
    ```sh
    $ kubectl -n olmv1-system port-forward svc/catalogd-service 8080:443
    ```

1. Access the `v1/all` service endpoint and filter the results to a list of packages by running the following command:

    ```sh
    $ curl https://localhost:8080/catalogs/operatorhubio/api/v1/all | jq -s '.[] | select(.schema == "olm.package") | .name'
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
    $ curl https://localhost:8080/catalogs/operatorhubio/api/v1/all | jq -s '.[] | select(.schema == "olm.channel") | select(.package == "ack-acm-controller") | .name'
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
    $ curl https://localhost:8080/catalogs/operatorhubio/api/v1/all | jq -s '.[] | select(.schema == "olm.bundle") | select(.package == "ack-acm-controller") | .name'
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

## License

Copyright 2022-2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
