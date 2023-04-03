# operator-controller
The operator-controller is the central component of Operator Lifecycle Manager (OLM) v1. It extends Kubernetes with an API through which users can install Operators.

## Description
OLM v1 is the follow-up to [OLM v0](https://github.com/operator-framework/operator-lifecycle-manager). The vision for OLM V1 is discussed in detail in the [OLM V1 Product Requirements Document](https://docs.google.com/document/d/1-vsZ2dAODNfoHb7Nf0fbYeKDF7DUqEzS9HqgeMCvbDs/edit). 

The Operator Controller project consists of four different components, including this one, which are as follows:
* operator-controller
* [deppy](https://github.com/operator-framework/deppy)
* [rukpak](https://github.com/operator-framework/rukpak)
* [catalogd](https://github.com/operator-framework/catalogd)

Ongoing feature development can always be tracked at the [OLM V1 GitHub Project](https://github.com/orgs/operator-framework/projects/8) by looking at `Milestone #` tabs. 

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
Undeploy the controller to the cluster:

```sh
make undeploy
```

## Contributing
If you are interested in getting involved, please see the [CONTRIBUTING.md](TO/Be/Added) for more information.

### How it works
This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/) 
which provides a reconcile function responsible for synchronizing resources until the desired state is reached on the cluster 

### Test It Out
1. Install the CRDs into the cluster:

```sh
make install
```

2. Run your controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):

```sh
make run
```

**NOTE:** You can also run this in one step by running: `make install run`

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

