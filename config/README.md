# OPERATOR-CONTROLLER CONFIGURATION

## Samples

Select examples from `config/samples` according to the APIs and controllers installed
on your cluster. Run the commands below from the repository root. Do not apply the
whole directory: it also contains an experimental ClusterObjectDeployment sample.

### Standard extension installation

Install operator-controller and catalogd before applying these samples:

| Sample | Contents | Prerequisites |
| --- | --- | --- |
| `catalogd_operatorcatalog.yaml` | The `operatorhubio` ClusterCatalog | ClusterCatalog CRD, running catalogd, and access to the catalog image registry |
| `olm_v1_clusterextension.yaml` | The `argocd` namespace and ClusterExtension | ClusterExtension CRD, running operator-controller, and a serving catalog containing the requested Argo CD bundle |

```sh
kubectl apply -f config/samples/catalogd_operatorcatalog.yaml
kubectl wait --for=condition=Serving clustercatalog/operatorhubio --timeout=180s
kubectl apply -f config/samples/olm_v1_clusterextension.yaml
```

The ClusterExtension sample pins a specific version. Check that it is still
available in the chosen catalog and adjust the sample if needed.
`kubectl apply -k config/samples` selects only the ClusterExtension sample through
`kustomization.yaml`; it does not install a catalog or the experimental COD sample.

### Experimental standalone deployment

`olm_v1_clusterobjectdeployment.yaml` creates the `cod-example` namespace and a
ClusterObjectDeployment whose template contains a ConfigMap. It requires the
experimental COD and COS CRDs and a running object-controller. It does not require
ClusterExtension or ClusterCatalog APIs or controllers.

Follow [Manage standalone ClusterObjectDeployments](../docs/draft/tutorials/manage-clusterobjectdeployments.md)
to build and install object-controller, apply the sample, and inspect revisions.
Use a test cluster and a fresh `cod-example` namespace/group; cleanup deletes the
sample namespace and its contents. Do not apply the sample to a standard
installation, which does not provide the COD/COS APIs.

### Cleanup

Delete the parent ClusterExtension or COD first so its controllers and Kubernetes
garbage collection can remove the owned resources. Keep the controllers running
until teardown completes. Remove a sample's namespace only after cleanup and only
if it contains no unrelated resources. See the
[extension uninstall guide](../docs/tutorials/uninstall-extension.md) and
[COD tutorial cleanup](../docs/draft/tutorials/manage-clusterobjectdeployments.md#clean-up)
for the respective workflows.
