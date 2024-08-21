# operator-controller
The operator-controller is the central component of Operator Lifecycle Manager (OLM) v1.
It extends Kubernetes with an API through which users can install extensions.

## Mission

OLM’s purpose is to provide APIs, controllers, and tooling that support the packaging, distribution, and lifecycling of Kubernetes extensions. It aims to:
- align with Kubernetes designs and user assumptions
- provide secure, high-quality, and predictable user experiences centered around declarative GitOps concepts
- give cluster admins the minimal necessary controls to build their desired cluster architectures and to have ultimate control

## Overview

OLM v1 is the follow-up to OLM v0, located [here](https://github.com/operator-framework/operator-lifecycle-manager).

OLM v1 consists of two different components:
* operator-controller (this repository)
* [catalogd](https://github.com/operator-framework/catalogd)

For a more complete overview of OLM v1 and how it differs from OLM v0, see our [overview](./docs/olmv1_overview.md).

## Getting Started
You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.

> [!NOTE]
> Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

### Installation

> [!CAUTION]  
> Operator-Controller depends on [cert-manager](https://cert-manager.io/). Running the following command
> may affect an existing installation of cert-manager and cause cluster instability.

The latest version of Operator Controller can be installed with the following command:

```bash
curl -L -s https://github.com/operator-framework/operator-controller/releases/latest/download/install.sh | bash -s
```

### Create a ClusterCatalog

The ClusterCatalog resource supports file-based catalog ([FBC](https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs)) images.
The following example uses the official [OperatorHub](https://operatorhub.io) catalog.

```bash
kubectl apply -f - <<EOF
apiVersion: catalogd.operatorframework.io/v1alpha1
kind: ClusterCatalog
metadata:
  name: operatorhubio
spec:
  source:
    type: image
    image:
      ref: quay.io/operatorhubio/catalog:latest
      pollInterval: 10m
EOF
```

Wait for the ClusterCatalog to be unpacked:

```bash
kubectl wait --for=condition=Unpacked=True clustercatalog/operatorhubio --timeout=300s
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
