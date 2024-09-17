# Upgrading an Extension

Existing extensions can be upgraded by updating the version field in the ClusterExtension resource.

For information on downgrading an extension, see [Downgrade an Extension](../downgrading-an-extension.md).

## Prerequisites

* You have an extension installed
* The target version is compatible with OLM v1 (see [OLM v1 limitations](../refs/olmv1-limitations.md))
* CRD compatibility between the versions being upgraded or downgraded (see [CRD upgrade safety](../../refs/crd-upgrade-safety.md))
* The installer service account's RBAC permissions are adequate for the target version (see [Minimal RBAC for Installer Service Account](create-installer-service-account.md))

For more detailed information see [Upgrade Support](../upgrade-support.md).

## Procedure

Suppose we have successfully created and installed v0.5.0 of the ArgoCD operator with the following `ClusterExtension`:

``` yaml title="Example CR"
apiVersion: olm.operatorframework.io/v1alpha1
kind: ClusterExtension
metadata:
  name: argocd
spec:
  source:
    sourceType: Catalog
    catalog:
      packageName: argocd-operator
      version: 0.5.0
  install:
    namespace: argocd
    serviceAccount:
      name: argocd-installer
```

* Update the version field in the ClusterExtension resource:

    ``` terminal
      kubectl apply -f - <<EOF
      apiVersion: olm.operatorframework.io/v1alpha1
      kind: ClusterExtension
        metadata:
          name: argocd
        spec:
          source:
            sourceType: Catalog
            catalog:
              packageName: argocd-operator
              version: 0.6.0 # Update to version 0.6.0
          install:
            namespace: argocd
            serviceAccount:
              name: argocd-installer
      EOF
    ```

    ??? success
    ``` text title="Example output"
    clusterextension.olm.operatorframework.io/argocd-operator configured
    ```

    Alternatively, you can use `kubectl patch` to update the version field:

    ``` terminal
    kubectl patch clusterextension <extension_name> --type='merge' -p '{"spec": {"source": {"catalog": {"version": "<target_version>"}}}}'
    ```

    `extension_name`
    : Specifies the name defined in the `metadata.name` field of the extension's CR.
    
    `target_version`
    : Specifies the version to upgrade or downgrade to.

    ??? success
      ``` text title="Example output"
      clusterextension.olm.operatorframework.io/argocd-operator patched
      ```

### Verification

* Verify that the Kubernetes extension is deleted:

    ``` terminal
    kubectl get clusterextension.olm.operatorframework.io/<extension_name>
    ```

    ??? success
      ``` text title="Example output"
      Name:         argocd
      Namespace:    
      Labels:       olm.operatorframework.io/owner-kind=ClusterExtension
                    olm.operatorframework.io/owner-name=argocd
      Annotations:  <none>
      API Version:  olm.operatorframework.io/v1alpha1
      Kind:         ClusterExtension
      Metadata:
        Creation Timestamp:  2024-09-06T13:38:38Z
        Finalizers:
          olm.operatorframework.io/cleanup-unpack-cache
          olm.operatorframework.io/cleanup-contentmanager-cache
        Generation:        5
        Resource Version:  21167
        UID:               5abdf57d-aedc-45d4-ba0d-a86e785fd34a
      Spec:
        Install:
          Namespace:  argocd
          Service Account:
            Name:  argocd-installer
        Source:
          Catalog:
            Package Name:  argocd-operator
          Selector:
          Upgrade Constraint Policy:  Enforce
          Version:                    0.6.0
        Source Type:                  Catalog
      Status:
        Conditions:
          Last Transition Time:  2024-09-06T13:38:38Z
          Message:               
          Observed Generation:   5
          Reason:                Deprecated
          Status:                False
          Type:                  Deprecated
          Last Transition Time:  2024-09-06T13:38:38Z
          Message:               
          Observed Generation:   5
          Reason:                Deprecated
          Status:                False
          Type:                  PackageDeprecated
          Last Transition Time:  2024-09-06T13:38:38Z
          Message:               
          Observed Generation:   5
          Reason:                Deprecated
          Status:                False
          Type:                  ChannelDeprecated
          Last Transition Time:  2024-09-06T13:38:38Z
          Message:               
          Observed Generation:   5
          Reason:                Deprecated
          Status:                False
          Type:                  BundleDeprecated
          Last Transition Time:  2024-09-06T13:40:14Z
          Message:               resolved to "quay.io/operatorhubio/argocd-operator@sha256:d538c45a813b38ef0e44f40d279dc2653f97ca901fb660da5d7fe499d51ad3b3"
          Observed Generation:   5
          Reason:                Success
          Status:                True
          Type:                  Resolved
          Last Transition Time:  2024-09-06T13:38:38Z
          Message:               unpack successful:
          Observed Generation:   5
          Reason:                UnpackSuccess
          Status:                True
          Type:                  Unpacked
          Last Transition Time:  2024-09-06T13:40:31Z
          Message:               Installed bundle quay.io/operatorhubio/argocd-operator@sha256:d538c45a813b38ef0e44f40d279dc2653f97ca901fb660da5d7fe499d51ad3b3 successfully
          Observed Generation:   5
          Reason:                Success
          Status:                True
          Type:                  Installed
      Install:
        Bundle:
          Name:     argocd-operator.v0.6.0
          Version:  0.6.0
      Resolution:
        Bundle:
          Name:     argocd-operator.v0.6.0
          Version:  0.6.0
      ```
