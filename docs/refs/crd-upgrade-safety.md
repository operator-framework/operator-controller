# Custom Resource Definition Upgrade Safety

When you update a Custom Resource Definition (CRD), OLM runs a CRD Upgrade Safety preflight
check to ensure backwards compatibility with previous versions of that CRD. The CRD update
must pass the validation checks before the change is allowed to progress on a cluster.

## Prohibited CRD Upgrade Changes

The following changes to an existing CRD will be caught by the CRD Upgrade
Safety preflight check and prevent the upgrade:

- The scope changes from Cluster to Namespace or from Namespace to Cluster
- An existing stored version of the CRD is removed
- A new required field is added to an existing version of the CRD
- An existing field is removed from an existing version of the CRD
- An existing field type is changed in an existing version of the CRD
- A new default value is added to a field that did not previously have a default value
- The default value of a field is changed
- An existing default value of a field is removed
- New enum restrictions are added to an existing field which did not previously have enum restrictions
- Existing enum values from an existing field are removed
- The minimum value of an existing field is increased in an existing version
- The maximum value of an existing field is decreased in an existing version
- Minimum or maximum field constraints are added to a field that did not previously have constraints

!!! note
    The rules for changes to minimum and maximum values apply to `minimum`, `minLength`,
    `minProperties`, `minItems`, `maximum`, `maxLength`, `maxProperties`, and `maxItems` constraints.

If the CRD Upgrade Safety preflight check encounters one of the disallowed upgrade changes,
it will log an error for each disallowed change detected in the CRD upgrade.

!!! tip
    In cases where a change to the CRD does not fall into one of the disallowed change categories
    but is also unable to be properly detected as allowed, the CRD Upgrade Safety preflight check
    will prevent the upgrade and log an error for an "unknown change."

If you identify any preflight checks that should be implemented to prevent issues during CRD upgrades, please [create a new issue](https://github.com/operator-framework/operator-controller/issues).


## Allowed CRD Upgrade Changes

The following changes to an existing CRD are safe for backwards compatibility and will
not cause the CRD Upgrade Safety preflight check to halt the upgrade:

- Adding new enum values to the list of allowed enum values in a field
- An existing required field is changed to optional in an existing version
- The minimum value of an existing field is decreased in an existing version
- The maximum value of an existing field is increased in an existing version
- A new version of the CRD is added with no modifications to existing versions


## Disabling CRD Upgrade Safety

The CRD Upgrade Safety preflight check can be entirely disabled by adding the
`preflight.crdUpgradeSafety.disabled` field with a value of "true" to the ClusterExtension of the CRD.

```yaml
apiVersion: olm.operatorframework.io/v1alpha1
kind: ClusterExtension
metadata:
    name: clusterextension-sample
spec:
    source:
      sourceType: Catalog
      catalog:
        packageName: argocd-operator
        version: 0.6.0
    install:
      namespace: default
      serviceAccount:
        name: argocd-installer
      preflight:
        crdUpgradeSafety:
          disabled: true
```

You cannot disable individual field validators. If you disable the CRD Upgrade Safety preflight check, all field validators are disabled.

!!! warning
    Disabling the CRD Upgrade Safety preflight check could break backwards compatibility with stored
    versions of the CRD and cause other unintended consequences on the cluster.


## Examples of Unsafe CRD Changes

Take the following CRD as our starting version:

```yaml
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.13.0
  name: example.test.example.com
spec:
  group: test.example.com
  names:
    kind: Sample
    listKind: SampleList
    plural: samples
    singular: sample
  scope: Namespaced
  versions:
  - name: v1alpha1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            type: string
          kind:
            type: string
          metadata:
            type: object
          spec:
            type: object
          status:
            type: object
          pollInterval:
            type: string
        type: object
    served: true
    storage: true
    subresources:
      status: {}
```

The following examples will demonstrate specific changes to sections of the example CRD
that would be caught by the CRD Upgrade Safety preflight check.

### Changing Scope

In this example, `scope` has been changed from `Namespaced` to `Cluster`.

??? example
    ```yaml
    spec:
      group: test.example.com
      names:
        kind: Sample
        listKind: SampleList
        plural: samples
        singular: sample
      scope: Cluster
      versions:
      - name: v1alpha1
    ```

??? failure "Error output"
    ```
    validating upgrade for CRD "test.example.com" failed: CustomResourceDefinition test.example.com failed upgrade safety validation. "NoScopeChange" validation failed: scope changed from "Namespaced" to "Cluster"
    ```

### Removing a stored version

In this example, the existing stored version, `v1alpha1`, has been removed:

??? example
    ```yaml
      versions:
      - name: v1alpha2
        schema:
          openAPIV3Schema:
            properties:
              apiVersion:
                type: string
              kind:
                type: string
              metadata:
                type: object
              spec:
                type: object
              status:
                type: object
              pollInterval:
                type: string
            type: object
    ```

??? failure "Error output"
    ```
    validating upgrade for CRD "test.example.com" failed: CustomResourceDefinition test.example.com failed upgrade safety validation. "NoStoredVersionRemoved" validation failed: stored version "v1alpha1" removed
    ```

### Removing an existing field

In this example, the `pollInterval` field has been removed from `v1alpha1`:

??? example
    ```yaml
      versions:
      - name: v1alpha1
        schema:
          openAPIV3Schema:
            properties:
              apiVersion:
                type: string
              kind:
                type: string
              metadata:
                type: object
              spec:
                type: object
              status:
                type: object
            type: object
    ```

??? failure "Error output"
    ```
    validating upgrade for CRD "test.example.com" failed: CustomResourceDefinition test.example.com failed upgrade safety validation. "NoExistingFieldRemoved" validation failed: crd/test.example.com version/v1alpha1 field/^.spec.pollInterval may not be removed
    ```

### Adding a required field

In this example, `pollInterval` has been changed to a required field:

??? example
    ```yaml
      versions:
      - name: v1alpha2
        schema:
          openAPIV3Schema:
            properties:
              apiVersion:
                type: string
              kind:
                type: string
              metadata:
                type: object
              spec:
                type: object
              status:
                type: object
              pollInterval:
                type: string
            type: object
            required:
            - pollInterval
    ```

??? failure "Error output"
    ```
    validating upgrade for CRD "test.example.com" failed: CustomResourceDefinition test.example.com failed upgrade safety validation. "ChangeValidator" validation failed: version "v1alpha1", field "^": new required fields added: [pollInterval]
    ```
