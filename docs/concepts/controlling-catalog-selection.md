
# How A ClusterExtension Is Resolved From Various Catalogs

## Overview

Here you will find guidance on how catalog selection affects which bundle is actually resolved for a given package name. These features allow you to control which catalogs are used when resolving and installing operator bundles via `ClusterExtension`. You can:

- **Select specific catalogs by name or labels.**
- **Set priorities for catalogs to resolve ambiguities.**
- **Handle scenarios where multiple bundles match your criteria.**

## Usage Examples

### Selecting Catalogs by Name

To select a specific catalog by name, you can use the `matchLabels` field in your `ClusterExtension` resource.

#### Example

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: argocd
spec:
  namespace: argocd
  source:
    sourceType: Catalog
    catalog:
      packageName: argocd-operator
      selector:
        matchLabels:
          olm.operatorframework.io/metadata.name: operatorhubio
```

In this example, only the catalog named `operatorhubio` will be considered when resolving `argocd-operator`.

### Selecting Catalogs by Labels

If you have catalogs labeled with specific metadata, you can select them using `matchLabels` or `matchExpressions`.

#### Using `matchLabels`

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: argocd
spec:
  namespace: argocd
  source:
    sourceType: Catalog
    catalog:
      packageName: argocd-operator
      selector:
        matchLabels:
          example.com/support: "true"
```

This selects catalogs labeled with `example.com/support: "true"`.

#### Using `matchExpressions`

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: argocd
spec:
  namespace: argocd
  source:
    sourceType: Catalog
    catalog:
      packageName: argocd-operator
      selector:
        matchExpressions:
          - key: example.com/support
            operator: In
            values:
              - "gold"
              - "platinum"
```

This selects catalogs where the label `example.com/support` has the value `gold` or `platinum`.

### Excluding Catalogs

You can exclude catalogs by using the `NotIn` or `DoesNotExist` operators in `matchExpressions`.

#### Example: Exclude Specific Catalogs

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: argocd
spec:
  namespace: argocd
  source:
    sourceType: Catalog
    catalog:
      packageName: argocd-operator
      selector:
        matchExpressions:
          - key: olm.operatorframework.io/metadata.name
            operator: NotIn
            values:
              - unwanted-catalog
```

This excludes the catalog named `unwanted-catalog` from consideration.

#### Example: Exclude Catalogs with a Specific Label

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: argocd
spec:
  namespace: argocd
  source:
    sourceType: Catalog
    catalog:
      packageName: argocd-operator
      selector:
        matchExpressions:
          - key: example.com/support
            operator: DoesNotExist
```

This selects catalogs that do not have the `example.com/support` label.

### Setting Catalog Priority

When multiple available catalogs selected by a `ClusterExtension` provide the same package, you can set priorities to choose which catalog supplies it. Higher priority catalogs are considered first.

#### Defining Catalog Priority

In your `ClusterCatalog` resource, set the `priority` field:

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterCatalog
metadata:
  name: high-priority-catalog
spec:
  priority: 1000
  source:
    type: Image
    image:
      ref: quay.io/example/high-priority-content-management:latest
```

Catalogs have a default priority of `0`. The priority can be any 32-bit integer. Catalogs with higher priority values are considered first when selecting a catalog for a package.

#### How Priority Resolves Ambiguity

Catalogs are grouped by priority, from highest to lowest. For each group:

1. If no catalog in the group contains the requested package, consider the next lower-priority group.
2. If exactly one catalog contains the package, select that catalog. Only its bundles are filtered by the requested version, channels, and upgrade constraints. If none qualifies, resolution fails rather than switching to a lower-priority catalog.
3. If multiple catalogs in the group contain the package, resolution fails due to ambiguity, even if only one has a bundle matching the requested criteria.

Within the selected catalog, non-deprecated bundles are preferred over deprecated bundles, then the highest eligible version and release is chosen. Deprecation in another catalog does not affect catalog selection.

### Handling Ambiguity Errors

If multiple catalogs with the same priority contain the package, resolution fails. You can resolve this by:

- **Selecting only one catalog by name or label.**
- **Assigning different catalog priorities.**

Changing the requested version or channel does not resolve package-level catalog ambiguity. If the selected catalog has no matching bundle, adjust the version, channel, or upgrade constraints, or explicitly select another catalog.

## End to End Example

1. **Create or Update `ClusterCatalogs` with Appropriate Labels and Priority**

    ```yaml
    apiVersion: olm.operatorframework.io/v1
    kind: ClusterCatalog
    metadata:
      name: catalog-a
      labels:
        example.com/support: "true"
    spec:
      priority: 1000
      source:
        type: Image
        image:
          ref: quay.io/example/content-management-a:latest
    ```

    ```yaml
    apiVersion: olm.operatorframework.io/v1
    kind: ClusterCatalog
    metadata:
      name: catalog-b
      labels:
        example.com/support: "true"
    spec:
      priority: 500
      source:
        type: Image
        image:
          ref: quay.io/example/content-management-b:latest
    ```
    !!! note
        An `olm.operatorframework.io/metadata.name` label will be added automatically to ClusterCatalogs when applied


2. **Create a `ClusterExtension` with Catalog Selection**

    ```yaml
    apiVersion: olm.operatorframework.io/v1
    kind: ClusterExtension
    metadata:
      name: install-my-operator
    spec:
      namespace: my-operator-ns
      source:
        sourceType: Catalog
        catalog:
          packageName: my-operator
          selector:
            matchLabels:
              example.com/support: "true"
    ```

3. **Apply the Resources**

    ```shell
    kubectl apply -f content-management-a.yaml
    kubectl apply -f content-management-b.yaml
    kubectl apply -f install-my-operator.yaml
    ```

4. **Verify the Installation**

    Check the status of the `ClusterExtension`:

    ```shell
    kubectl get clusterextension install-my-operator -o yaml
    ```

    Both catalogs match the selector. If both contain `my-operator`, the higher-priority `catalog-a` is selected. Check `.status.install.bundle.name` and `.status.install.bundle.version` against the bundle content published in `catalog-a`. The `ClusterExtension` status does not directly report the name of the catalog used for resolution.

## Important Notes

- **Default Behavior**: Without a selector, every available catalog is considered. The package is resolved from the highest-priority catalog that contains it; if multiple catalogs at that priority contain it, resolution fails due to ambiguity.
- **Logical AND of Selectors**: When using both `matchLabels` and `matchExpressions`, catalogs must satisfy all criteria.
- **Deprecation Status**: Within the selected catalog, non-deprecated bundles are preferred over deprecated ones. Deprecation conditions reflect that catalog's package metadata even if no bundle matches the requested criteria.
- **Error Messages**: The system will update the `.status.conditions` of the `ClusterExtension` with meaningful messages if resolution fails due to ambiguity or no catalogs being selected.

## References

- [Minimal Controls for Selecting Catalogs to Resolve From](https://github.com/operator-framework/operator-controller/issues/1028)
- [RFC: Minimal Catalog Selection - Labels](https://docs.google.com/document/d/1v9iPRZHt1YXhwK9__7OITnEN430lryQzWaAaIpn---Y)
- [RFC: Minimal Catalog Selection - Priority](https://docs.google.com/document/d/1jGAOvE0yf_U2lpyhy2U0KPLGOaG-8-ML0lve1mR-o1Q)
- [General Concept - Working with Labels & Selectors](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/)
