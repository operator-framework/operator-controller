# Channel-Based Automatic Upgrades

A "channel" is a package author defined stream of updates for an extension. A set of channels can be set in the Catalog source to restrict automatic updates to the set of versions defined in those channels.

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
      # Automatically upgrade to the latest version found in the preview and dev-preview channels
      channels: [dev-preview, preview]
```

Note that the `version` field also supports [version pinning](./how-to-pin-version.md) and [version ranges](./how-to-version-range-upgrades.md) to further restrict the set of possible upgradable operator versions.

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
      channels: [stable] # Automatically upgrade to the latest version found in ‘stable’
      version: "!=1.3.2" # Don’t allow version 1.3.2
```

For more information on SemVer version ranges see [version ranges](../concepts/version-ranges.md)
