# Pin Version and Disable Automatic Updates

To disable automatic updates, and pin the version of an extension, set `version` in the Catalog source to a specific version (e.g. 1.2.3).

Example:

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: argocd
spec:
  source:
    sourceType: Catalog
    catalog:
        packageName: argocd-operator
        version: 0.6.0 # Pin argocd-operator to v0.6.0
  install:
    namespace: argocd
    serviceAccount:
      name: argocd-installer
```

For more information on SemVer version ranges see [version ranges](../concepts/version-ranges.md)
