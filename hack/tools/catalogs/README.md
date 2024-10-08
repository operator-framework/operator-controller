# Hack Catalog Tools

This directory contains scripts that automate some of the tasks related to catalog interaction and bundle installation.

---
> [!WARNING]
> These scripts are intended to help users navigate the catalog and produce installation RBAC until reliable tooling is available for OLM v1,
> and to document the process in code for contributors. These scripts are not officially supported.
> They are not meant to be used in production environments.
---

### Prerequisites

To execute the scripts, the following tools are required:

 * [jq](https://jqlang.github.io/jq/) to filter catalog data
 * [yq](https://mikefarah.gitbook.io/yq/) to parse YAML
 * [kubectl](https://kubernetes.io/docs/reference/kubectl/) to interact with the cluster running OLM v1
 * [wget](https://www.gnu.org/software/wget/) to download the catalog data
 * A container runtime, such as [podman](https://podman.io/) or [docker](https://www.docker.com/) to interact with bundle images.

#### Container Runtime

By default, the scripts use `podman` or `docker` as the container runtime. 
If you use another container runtime, set the `CONTAINER_RUNTIME` environment variable to the path of the container runtime binary.

### Tools

---
> [!NOTE]
> All examples assume that the current working directory is the `hack/tools/catalogs` directory.
---

#### download-catalog

Download a catalog from a served ClusterCatalog running on a cluster reachable by `kubectl`.

Example:

  ```terminal
  # Download the catalog from the operatorhubio ClusterCatalog
  ./download-catalog operatorhubio
  ```

The downloaded catalog is saved to <catalog-name>-catalog.json in the current directory.

#### list-compatible-bundles

List (potential) OLM v1 compatible bundles from the catalog.

Not all registry+v1 bundles made for OLM v0 are compatible with OLM v1. 
Compatible bundles must meet the following criteria:
 * Support for the 'AllNamespaces' install mode
 * No webhooks
 * No dependencies on other packages of GVKs
 * The operator does not make use of OLM v0's [`OperatorCondition`](https://olm.operatorframework.io/docs/concepts/crds/operatorcondition/) API

<!--- 
TODO: Update link to OLM v1 limitations doc when it is available.
-->
For more information, see [OLM v1 limitations](../../../docs/refs/olm-v1-limitations.md).

For some bundles, some of this criteria can only be determined by inspecting the contents bundle image. The script will return all bundles that are potentially compatible.

Examples:

  ``` terminal
  # List (potentially) OLM v1 compatible bundles from the operatorhubio catalog
  ./list-compatible-bundles < operatorhubio-catalog.json
  ```

  ``` terminal
  # List (potentially) OLM v1 compatible bundles that contain 'argco' in the package name
  # -r can be used with any regex supported by jq
  ./list-compatible-bundles -r 'argocd' < operatorhubio-catalog.json
  ```

#### find-bundle-image

Find the image for a bundle in the catalog.

Example:
  
  ``` terminal
  # Get the image for the argocd-operator v0.6.0 bundle from the operatorhubio catalog
  ./find-bundle-image argocd-operator 0.6.0 < operatorhubio-catalog.json
  ```

#### unpack-bundle

Unpack a bundle image to a directory.

Example:

  ``` terminal
  # Unpack the argocd-operator v0.6.0 bundle image to a temporary directory
  ./unpack-bundle quay.io/operatorhubio/argocd-operator@sha256:d538c45a813b38ef0e44f40d279dc2653f97ca901fb660da5d7fe499d51ad3b3
  ```

  ``` terminal
  # Unpack the argocd-operator v0.6.0 bundle image to a specific directory
  ./unpack-bundle quay.io/operatorhubio/argocd-operator@sha256:d538c45a813b38ef0e44f40d279dc2653f97ca901fb660da5d7fe499d51ad3b3 -o argocd-manifests
  ```

#### is-bundle-supported

Check if a bundle is supported by OLM v1 by inspecting the unpacked bundle manifests.

<!--- 
TODO: Update link to OLM v1 limitations doc when it is available.
-->
For more information on bundle support, see [OLM v1 limitations](../../../docs/refs/olm-v1-limitations.md).

Example:

  ``` terminal
  # Check if the argocd-operator v0.6.0 bundle from the operatorhubio catalog is supported by OLM v1
  ./is-bundle-supported argocd-manifests
  ```

  ``` terminal
  # Find bundle image, unpack, and verify support in one command
  ./find-bundle-image argocd-operator 0.6.0 < operatorhubio-catalog.json | ./unpack-bundle | ./is-bundle-supported
  ```

#### generate-manifests

Generate RBAC or installation manifests for a bundle. The generated manifests can be templates or fully rendered manifests.

The following options can be used to override resource naming defaults:
  -n <namespace> Namespace where the extension is installed
  -e <cluster-extension-name> - Name of the extension
  -cr <cluster-role-name> - Name of the cluster role
  -r <role-name> - Name of the role
  -s <service-account-name> - Name of the service account
  --template - Generate template manifests

Default resource name format:
  * Namespace: <cluster-extension-name>-system
  * Extension name: <package-name>
  * ClusterRole name: <service-account-name>-cluster-role
  * Role name: <service-account-name>-installer-role
  * ServiceAccount name: <package-name>-installer
  * ClusterRoleBinding name: <cluster-role-name>-binding
  * RoleBinding name: <role-name>-binding

Use `--template` to generate templated manifests that can be customized before applying to the cluster. 
Template manifests will contain the following template variables:

Template Variables:
* `${NAMESPACE}` - Namespace where the extension is installed
* `${EXTENSION_NAME}` - Name of the extension
* `${CLUSTER_ROLE_NAME}` - Name of the cluster role
* `${ROLE_NAME}` - Name of the role
* `${SERVICE_ACCOUNT_NAME}` - Name of the service account

Examples:

  ``` terminal
  # Generate installation manifests for the argocd-operator v0.6.0 bundle from the operatorhubio catalog
  ./generate-manifests install argocd-operator 0.6.0 < operatorhubio-catalog.json
  ```

  ``` terminal
  # Generate templated installation manifests for the argocd-operator v0.6.0 bundle from the operatorhubio catalog
  generate-manifests install argocd-operator 0.6.0 --template < operatorhubio-catalog.json
  ```

  ``` terminal
  # Generate RBAC manifests for the argocd-operator v0.6.0 bundle from the operatorhubio catalog
  generate-manifests rbac argocd-operator 0.6.0 < operatorhubio-catalog.json
  ```

  ``` terminal
  # Generate templated RBAC manifests for the argocd-operator v0.6.0 bundle from the operatorhubio catalog
  generate-manifests rbac argocd-operator 0.6.0 --template < operatorhubio-catalog.json
  ```
