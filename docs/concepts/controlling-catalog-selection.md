
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
  serviceAccount:
    name: argocd-installer
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
  serviceAccount:
    name: argocd-installer
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
  serviceAccount:
    name: argocd-installer
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
  serviceAccount:
    name: argocd-installer
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
  serviceAccount:
    name: argocd-installer
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

When multiple catalogs provide the same package, you can set priorities to resolve ambiguities. Higher priority catalogs are preferred.

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

Catalogs have a default priority of `0`. The priority can be any 32-bit integer. Catalogs with higher priority values are preferred during bundle resolution.

#### How Priority Resolves Ambiguity

When multiple bundles match your criteria:

1. **Bundles from catalogs with higher priority are selected.**
2. **If multiple bundles are from catalogs with the same highest priority, and there is still ambiguity, an error is generated.**
3. **Deprecated bundles are deprioritized.** If non-deprecated bundles are available, deprecated ones are ignored.

### Handling Ambiguity Errors

If the system cannot resolve to a single bundle due to ambiguity, it will generate an error. You can resolve this by:

- **Refining your catalog selection criteria.**
- **Adjusting catalog priorities.**
- **Ensuring that only one bundle matches your package name and version requirements.**

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
        example.com/support: "false"
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
      serviceAccount:
        name: my-operator-installer
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

    The status should indicate that the bundle was resolved from `catalog-a` due to the higher priority and matching label.

## Important Notes

- **Default Behavior**: If you do not specify any catalog selection criteria, the system may select any available catalog that provides the requested package, and the choice is undefined.
- **Logical AND of Selectors**: When using both `matchLabels` and `matchExpressions`, catalogs must satisfy all criteria.
- **Deprecation Status**: Non-deprecated bundles are preferred over deprecated ones during resolution.
- **Error Messages**: The system will update the `.status.conditions` of the `ClusterExtension` with meaningful messages if resolution fails due to ambiguity or no catalogs being selected.

## References

- [Minimal Controls for Selecting Catalogs to Resolve From](https://github.com/operator-framework/operator-controller/issues/1028)
- [RFC: Minimal Catalog Selection - Labels](https://docs.google.com/document/d/1v9iPRZHt1YXhwK9__7OITnEN430lryQzWaAaIpn---Y)
- [RFC: Minimal Catalog Selection - Priority](https://docs.google.com/document/d/1jGAOvE0yf_U2lpyhy2U0KPLGOaG-8-ML0lve1mR-o1Q)
- [General Concept - Working with Labels & Selectors](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/)
