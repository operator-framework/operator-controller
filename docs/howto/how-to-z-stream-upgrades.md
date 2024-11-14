# Z-Stream Automatic Updates

To restrict automatic updates to only z-stream patches and avoid breaking changes, use the `"~"` version range operator when setting the version for the desired package in Catalog source.

Example:

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: argocd
spec:
  namespace: argocd
  serviceAccount:
    name: argocd-installer
  source:
    sourceType: Catalog
    catalog:
      packageName: argocd-operator
      version: "~2.3" # Automatically upgrade patch releases for v2.3
```

For more information on SemVer version ranges see [version ranges](../concepts/version-ranges.md)
