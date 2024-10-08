---
hide:
  - toc
---

## OLM v0 Extension Support

Currently, OLM v1 supports installing cluster extensions that meet the following criteria:

* The extension must support installation via the `AllNamespaces` install mode.
* The extension must not use webhooks.
* The extension must not declare dependencies using any of the following file-based catalog properties:

    * `olm.gvk.required`
    * `olm.package.required`
    * `olm.constraint`

When you install an extension, OLM v1 validates that the bundle you want to install meets these constraints. If you try to install an extension that does not meet these constraints, an error message is printed in the cluster extension's conditions.

!!! important

    OLM v1 does not support the `OperatorConditions` API introduced in legacy OLM.

    Currently, there is no testing to validate against this constraint. If an extension uses the `OperatorConditions` API, the extension does not install correctly. Most extensions that rely on this API fail at start time, but some might fail during reconcilation. 
    
