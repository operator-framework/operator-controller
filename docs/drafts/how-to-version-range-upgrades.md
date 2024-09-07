## How-to: Version Range Automatic Updates

Set the version for the desired package in the Catalog source to a comparison string, like  `">=3.0, <3.6"`, to restrict the automatic updates to the version range. Any new version of the extension released in the catalog within this range will be automatically applied.

Example:

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
      version: ">=3.0, <3.6"  # Install versions from v3.0.0 up to, but not including, v3.6.0
  install:
    namespace: argocd
    serviceAccount:
      name: argocd-installer
```

For more information on SemVer version ranges see [version-rages](version-ranges.md)