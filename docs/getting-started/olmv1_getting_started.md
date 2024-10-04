### Installation

The following script will install OLMv1 on a Kubernetes cluster. If you don't have one, you can deploy a Kubernetes cluster with [KIND](https://sigs.k8s.io/kind).

> [!CAUTION]  
> Operator-Controller depends on [cert-manager](https://cert-manager.io/). Running the following command
> may affect an existing installation of cert-manager and cause cluster instability.

The latest version of Operator Controller can be installed with the following command:

```bash
curl -L -s https://github.com/operator-framework/operator-controller/releases/latest/download/install.sh | bash -s
```

### Getting Started with OLM v1

This quickstart procedure will guide you through the following processes:

* Deploying a catalog
* Installing, upgrading, or downgrading an extension
* Deleting catalogs and extensions

### Create a Catalog

OLM v1 is designed to source content from an on-cluster catalog in the file-based catalog ([FBC](https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs)) format.
These catalogs are deployed and configured through the `ClusterCatalog` resource. More information on adding catalogs
can be found [here](../tutorials/add-catalog.md).

The following example uses the official [OperatorHub](https://operatorhub.io) catalog that contains many different
extensions to choose from. Note that this catalog contains packages designed to work with OLM v0, and that not all packages
will work with OLM v1. More information on catalog exploration and content compatibility can be found [here](../howto/catalog-queries.md).

To create the catalog, run the following command:

```bash
# Create ClusterCatalog
kubectl apply -f - <<EOF
apiVersion: catalogd.operatorframework.io/v1alpha1
kind: ClusterCatalog
metadata:
  name: operatorhubio
spec:
  source:
    type: Image
    image:
      ref: quay.io/operatorhubio/catalog:latest
      pollInterval: 10m
EOF
```

Once the catalog is being served, its content will be available for installation.

```bash
# Wait for the ClusterCatalog to be ready
kubectl wait --for=condition=Serving=True clustercatalog/operatorhubio --timeout=60s
```

### Install a Cluster Extension

For simplicity, the following example manifest includes all necessary resources to install the ArgoCD operator.
The manifest includes installation namespace, installer service account and associated minimal set of RBAC permissions
needed for installation, and the ClusterExtension resource, which specifies the name and version of the extension to install.
More information on installing extensions can be found [here](../tutorials/install-extension.md).

```bash
# Apply the sample ClusterExtension. Manifest already includes
# namespace and adequately privileged service account
kubectl apply -f https://raw.githubusercontent.com/operator-framework/operator-controller/main/config/samples/olm_v1alpha1_clusterextension.yaml
```

### Upgrade the Cluster Extension

To upgrade the installed extension, update the version field in the ClusterExtension resource. Note that
there must be CRD compatibility between the versions being upgraded, and the target version must be
compatible with OLM v1. More information on CRD upgrade safety can be found [here](../concepts/crd-upgrade-safety.md),
compatible with OLM v1. More information on CRD upgrade safety can be found [here](../concepts/crd-upgrade-safety.md),
and on the extension upgrade process [here](../tutorials/upgrade-extension.md).

```bash
# Update to v0.11.0
kubectl patch clusterextension argocd --type='merge' -p '{"spec": {"source": {"content-management": {"version": "0.11.0"}}}}'

```

For information on the downgrade process, see [here](../tutorials/downgrade-extension.md).

### Uninstall the Cluster Extension

To uninstall an extension, delete the ClusterExtension resource. This will trigger the uninstallation process, which will
remove all resources created by the extension. More information on uninstalling extensions can be found [here](../tutorials/uninstall-extension.md).

```bash
# Delete cluster extension and residing namespace
kubectl delete clusterextension/argocd
```

### Cleanup

Extension installation requires the creation of a namespace, an installer service account, and its RBAC. Once the
extension is uninstalled, these resources can be cleaned up.

```bash
# Delete namespace, and by extension, the installer service account, Role, and RoleBinding
kubectl delete namespace argocd
```

```bash
# Delete installer service account cluster roles
kubectl delete clusterrole argocd-installer-clusterrole && kubectl delete clusterrole argocd-rbac-clusterrole
```

```bash
# Delete installer service account cluster role bindings
kuebctl delete clusterrolebinding argocd-installer-binding && kubectl delete clusterrolebinding argocd-rbac-binding
```
