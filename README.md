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

OLM v1 consists of four different components:
* operator-controller (this repository)
* [deppy](https://github.com/operator-framework/deppy)
* [rukpak](https://github.com/operator-framework/rukpak)
* [catalogd](https://github.com/operator-framework/catalogd)

For a more complete overview of OLM v1 and how it differs from OLM v0, see our [overview](./docs/olmv1_overview.md).

## Getting Started
You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

### Running on the cluster
1. Install Instances of Custom Resources:

```sh
kubectl apply -f config/samples/
```

2. Build and push your image to the location specified by `IMG`:
	
```sh
make docker-build docker-push IMG=<some-registry>/operator-controller:tag
```
	
3. Deploy the controller to the cluster with the image specified by `IMG`:

```sh
make deploy IMG=<some-registry>/operator-controller:tag
```

### Uninstall CRDs
To delete the CRDs from the cluster:

```sh
make uninstall
```

### Undeploy controller
To undeploy the controller from the cluster:

```sh
make undeploy
```

## Contributing

Refer to [CONTRIBUTING.md](./CONTRIBUTING.md) for more information.

### How it works
This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/).

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/)
which provide a reconcile function responsible for synchronizing resources until the desired state is reached on the cluster.

### Test It Out

Install the CRDs and the operator-controller into a new [KIND cluster](https://kind.sigs.k8s.io/):
```sh
make run
```
This will build a local container image of the operator-controller, create a new KIND cluster and then deploy onto that cluster.
This will also deploy the catalogd, rukpak and cert-manager dependencies.

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make help` for more information on all potential `make` targets.

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html).

## License

Copyright 2022-2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
