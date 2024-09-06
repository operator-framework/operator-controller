# Provided ServiceAccount for ClusterExtension Installation and Management

Adhering to OLM v1's "Secure by Default" tenet, OLM v1 does not have the permissions
necessary to install content. This follows the least privilege principle and reduces
the chance of a [confused deputy attack](https://en.wikipedia.org/wiki/Confused_deputy_problem).
Instead, users must explicitly specify a ServiceAccount that will be used to perform the
installation and management of a specific ClusterExtension. The ServiceAccount is specified
in the ClusterExtension manifest as follows:

```yaml
apiVersion: olm.operatorframework.io/v1alpha1
kind: ClusterExtension
metadata:
  name: argocd
spec:
  source:
    sourceType: Catalog
    catalog:
      packageName: argocd-operator
      version: 0.6.0
  install:
    namespace: argocd
    serviceAccount:
      name: argocd-installer
```

The ServiceAccount must be configured with the RBAC permissions required by the ClusterExtension.
If the permissions do not meet the minimum requirements, installation will fail. If no ServiceAccount
is provided in the ClusterExtension manifest, then the manifest will be rejected.

//TODO: Add link to documentation on determining least privileges required for the ServiceAccount