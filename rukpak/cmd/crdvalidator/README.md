# crdvalidator

## Summary

A part of the core value proposition for RukPak is the safe upgrade of
a [CustomResourceDefinition](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/) (`CRD`)
. As a result, this repository comes equipped with a "CRD Validator Webhook" (`crdvalidator`), that will validate
all `CRD` upgrades created by RukPak. This protects your `BundleInstance` pivots from having potentially dangerous
effects such as data loss.

### How to use

`crdvalidator` will ensure that any `CRD` upgrade is safe as long as the CRD is associated with RukPak. If you would
like to disable validation for a `CRD` included in a specific `Bundle`, you will need to explicitly set
the `core.rukpak.io/safe-crd-upgrade-validation` annotation to `false` within that specific CRD's manifest.

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    core.rukpak.io/safe-crd-upgrade-validation: false # <--
```

### What is protected

There are three main cases `crdvalidator` is checking against with every specified `CRD` upgrade. These are where a
new `CRD`:

1. Removes a stored version and ensures that removing the stored version does not result in data loss.
2. Changes a version that old `CRD` had and it must validate existing CRs against the new schema.
3. Adds a version that old `CRD` does not have. In this case `crdvalidator` checks if the conversion strategy is:
    - None, then ensures that existing CRs validate with new schema.
    - Webhook, then allow update (assuming webhook handles conversion correctly)

#### Update the schema that invalidates existing CRs

Say that you have installed a `CRD` onto the cluster with a Kind of `Sample` that has a single property
of `examplearray`.

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.7.0
    core.rukpak.io/safe-upgrade-validation: enabled
  creationTimestamp: null
  name: sample.example.io
#
# ...
#
spec:
  versions:
    - name: v1alpha1
        schema:
        openAPIV3Schema:
          properties:
            exampleProperty:
              description: exampleProperty is a string property to demonstrate crd validation logic.
              type: string
```

With this installed onto the cluster, you go ahead and create a `CR` of a `Sample` onto the cluster.

```yaml
apiVersion: sample.example.io
kind: Sample
metadata:
  name: sampleResource
```

If you then update that `CRD`'s schema in such a way that the `CR` you just created was invalidated, like so:

```yaml
#
# ...
#
spec:
  versions:
    - name: v1alpha1
        schema:
        openAPIV3Schema:
          type: string
          required: # Updating the exampleProperty to be required
            - exampleProperty
          properties:
            exampleProperty:
              description: exampleProperty is an array of strings to demonstrate crd validation logic.
              type: string
```

`crdvalidator` will prevent the update from occurring with the following error. This is because the existing `CR` does
not have a value set for the required field of `exampleProperty`.

```text
admission webhook "webhook.crdvalidator.io" denied the request: failed to validate safety of UPDATE for CRD "sample.example.io" (NOTE: to disable this validation, set the "core.rukpak.io/safe-crd-upgrade-validation" annotation to "false"):
error validating existing CRs against new CRD's schema for "sample.example.io": existing custom object /sampleResource failed validation for new schema version v1alpha1: [].exampleProperty: Required value
```

## Running locally

`crdvalidator` is included with every default installation of RukPak. To run this locally, simply run the make target
with a kind cluster started.

```console
make run
```

By default, `crdvalidator`'s test suite runs alongside RukPak's, but you can also just run `crdvalidator`'s specific
tests.

```console
make e2e TEST="crdvalidator"
```

If you would like to install the `crdvalidator` onto your current cluster without RukPak, you'll need to
install `cert-mgr` and then `crdvalidator`

```console
make cert-mgr install-crdvalidator-webhook
```
