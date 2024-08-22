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
# Create ClusterCatalog
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

```bash
# Wait for the ClusterCatalog to be unpacked
kubectl wait --for=condition=Unpacked=True clustercatalog/operatorhubio --timeout=60s
```

### Install Cluster Extension

```bash
# Apply the sample ClusterExtension. Manifest already includes
# namespace and adequately privileged service account
kubectl apply -f config/samples/olm_v1alpha1_clusterextension.yaml
```

#### Upgrade/Downgrade

```bash
# Update the required version
kubectl patch clusterextension argocd --type='merge' -p '{"spec": {"version": "0.11.0"}}'
```

#### Uninstall

```bash
# Delete cluster extension and residing namespace
kubectl delete clusterextension/argocd && kubectl delete namespace argocd
```

```bash
# Delete cluster-scoped resources
kubectl delete --ignore-not-found=true -f config/samples/olm_v1alpha1_clusterextension.yaml 
```

### Advanced Usage

> [!WARNING]
> The scripts referenced in this section are best-effort and may not always work as
> intended. They are provided as a stopgap until we can offer production grade tooling
> for tasks such as: searching the catalog, discovering supported bundles, and determining
> the least-privilege set of permissions required by the installer service account to install
> the content.

#### Installation

An extension needs a namespace in which to be installed and a service account with sufficient
privileges to install the content. For instance:

```bash
# Create argocd namespace for the argocd-operator
kubectl create ns argocd
```

```bash
# Create installer service account
kubectl create serviceaccount -n argocd-system argocd-installer
```

> [!WARNING]
> We work around the absence of reliable tooling to determine the set of least privileges
> for the installer service account to be able to install a given bundle by giving
> the installer service account cluster admin privileges.
> This is not an option for production clusters due to the security implications.
> The OLM community is working hard to bridge this tooling gap.

```bash
# Give service account cluster admin privileges
# This works with KIND - consult documentation for instructions on how
# to grant admin privileges for your kubernetes distribution 
kubectl create clusterrolebinding "argocd-operator-installer-cluster-admin" \
    --clusterrole=cluster-admin \
    --serviceaccount="argocd-system:argocd-operator-installer"
```

```bash
# Apply ClusterExtension
cat <<EOF
apiVersion: olm.operatorframework.io/v1alpha1
kind: ClusterExtension
metadata:
  name: argocd
spec:
  installNamespace: argocd-system
  packageName: argocd-operator
  version: 0.6.0
  serviceAccount:
    name: argocd-operator-installer
EOF | kubectl apply -f -
```

```bash
# Wait for installation to finish successfully
kubectl wait --for=condition=Success=True clusterextension/argocd --timeout=60s
```

#### Finding Content

The catalog content can be downloaded locally as a json file and queried using tools like [jq](The catalog content can be downloaded locally as a json file and queried using tools like [jq](The catalog content can be downloaded locally as a json file and queried using tools like [jq](https://jqlang.github.io/jq/).
The _catalogd-catalogserver_ service in the _olmv1-system_ namespace provides an endpoint from which to
download the catalog. This endpoint can be found in the status (.status.contentURL). 

The [download-catalog.sh](hack/tools/catalogs/download-catalog) script automates this process:

```bash
# Download the catalog provided by the unpacked ClusterCatalog called operatorhuio
# The catalog will be downloaded to operatorhubio-catalog.json
./hack/tools/catalogs/download-catalog operatorhubio
```

OLM v1 currently supports the installation of bundles that:
- support the 'AllNamespaces' install mode
- do not have any package or gvk dependencies
- do not have webhooks

The [list-compatible-bundles.sh](hack/tools/catalogs/list-compatible-bundles) script attempts
to filter out unsupported bundles:

```bash
# Returns a JSON array of {packageName: "", versions: ["", ...]} objects
# This array can be further queried with jq
./hack/tools/catalogs/list-compatible-bundles < operatorhubio-catalog.json
# The -r option also allows you to define a regular expression for the package name
# for futher filtering
./hack/tools/catalogs/list-compatible-bundles -r 'argocd' < operatorhubio-catalog.json
```

#### Determining Service Account Privileges

The installer service account needs sufficient privileges to create the bundle's resources,
as well as to assign RBAC to those resources. This information can be derived from the 
bundle's ClusterServiceVersion manifest.

The [install-bundle.sh](hack/tools/catalogs/bundle-manifests) script generates all required
manifests for a bundle installation (including Namespace, and ClusterExtension resources), but can also
be used to determine the RBAC for the installer service account:

```bash
# Get RBAC for a ClusterExtension called 'argocd'
# using package 'argocd-operator' at version '0.6.0'
# in namespace 'argocd-system'
RBAC_ONLY=1 ./hack/tools/catalogs/bundle-manifests argocd argocd-operator 0.6.0 argocd-system < operatorhubio-catalog.json
# Or, let the script do all the heavy lifting (creation of Namespace, and ClusterExtension, as well as
# the ServiceAccount and all required RBAC
./hack/tools/catalogs/bundle-manifests argocd argocd-operator 0.6.0 argocd-system < operatorhubio-catalog.json | kubectl apply -f -
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
