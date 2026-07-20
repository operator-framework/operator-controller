---
hide:
  - toc
---

# Upgrade an Extension

Existing extensions can be upgraded by updating the version field in the ClusterExtension resource.

For information on downgrading an extension, see [Downgrade an Extension](downgrade-extension.md).

## Prerequisites

* You have a ClusterExtension installed
* The target version is compatible with OLM v1 (see [OLM v1 limitations](../project/olmv1_limitations.md))
* Any changes to the CustomResourceDefinition in the new version meet compatibility requirements (see [CRD upgrade safety](../concepts/crd-upgrade-safety.md))
* You are not attempting to upgrade between minor versions with a major version of zero (see [Upgrades within the major version zero](../concepts/upgrade-support.md#upgrades-within-the-major-version-zero))

For more detailed information see [Upgrade Support](../concepts/upgrade-support.md).

## Procedure

For this example, we will be using v0.2.0 of the ArgoCD operator. If you would like to follow along
with this tutorial, you can apply the following manifest to your cluster by, for example,
saving it to a local file and then running `kubectl apply -f FILENAME`:

??? info "ArgoCD v0.2.0 manifests"
    ```yaml
    ---
    apiVersion: v1
    kind: Namespace
    metadata:
      name: argocd
    ---
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
          version: 0.2.0
    ```

If we view the current state of our ClusterExtension we should see that we have installed version 0.2.0:

```terminal
kubectl get clusterextension argocd -o jsonpath-as-json="{.status.install}"
```

!!! success "Command output"
    ``` json
    [
        {
            "bundle": {
                "name": "argocd-operator.v0.2.0",
                "version": "0.2.0"
            }
        }
    ]
    ```

* To initiate our upgrade, let's update the version field in the ClusterExtension resource:

    ``` terminal title="Method 1: apply a new ClusterExtension manifest"
    kubectl apply -f - <<EOF
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
          version: 0.2.1 # Update to version 0.2.1
    EOF
    ```

    !!! success "Method 1 output"
        ``` text
        clusterextension.olm.operatorframework.io/argocd configured
        ```

    Alternatively, you can use `kubectl patch` to update the version field:

    ``` terminal title="Method 2: run patch command"
    kubectl patch clusterextension argocd --type='merge' -p '{"spec": {"source": {"catalog": {"version": "0.2.1"}}}}'
    ```

    !!! success "Method 2 output"
        ``` text
        clusterextension.olm.operatorframework.io/argocd patched
        ```

* We can now verify that the ClusterExtension is updated to the new version:

    ``` terminal title="Get the current ClusterExtension version"
    kubectl get clusterextension argocd -o jsonpath-as-json="{.status.install}"
    ```

    !!! success "Updated ClusterExtension version"
        ``` json
        [
            {
                "bundle": {
                    "name": "argocd-operator.v0.2.1",
                    "version": "0.2.1"
                }
            }
        ]
        ```

!!! note "Note on the `kubectl.kubernetes.io/last-applied-configuration` annotation"
    After your upgrade, the contents of the `kubectl.kubernetes.io/last-applied-configuration` annotation field will
    differ depending on your method of upgrade. If you apply a new ClusterExtension manifest as in the first method shown,
    the last applied configuration will show the new version since we replaced the existing manifest. If you use the patch
    method or `kubectl edit clusterextension`, then the last applied configuration will show the old version.