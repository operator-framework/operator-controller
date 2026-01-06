---
hide:
  - toc
---

## Content Support

Currently, OLM v1 only supports installing operators packaged in [OLM v0 bundles](https://olm.operatorframework.io/docs/tasks/creating-operator-bundle/)
, also known as `registry+v1` bundles. Additionally, the bundled operator, or cluster extension:

* **must** support installation via the `AllNamespaces` install mode.
    * Note that `AllNamespaces` is the recommended install mode. OLM v1 supports `SingleNamespace` and `OwnNamespace` modes for `registry+v1` bundles for backwards compatibility with OLM v0, but these are not recommended install modes as there is a [hard limitation](https://operator-framework.github.io/operator-controller/project/olmv1_design_decisions/#do-not-fight-kubernetes) of only one instance of a given CRD in the cluster.
* **must not** declare dependencies using any of the following file-based catalog properties:
    * `olm.gvk.required`
    * `olm.package.required`
    * `olm.constraint`

OLM v1 verifies these criteria at install time and will surface violations in the `ClusterExtensions`'s `.status.conditions`.

!!! important

    OLM v1 does not support the `OperatorConditions` API introduced in legacy OLM.

    Currently, there is no testing to validate against this constraint. If an extension uses the `OperatorConditions` API, the extension does not install correctly. Most extensions that rely on this API fail at start time, but some might fail during reconcilation.
