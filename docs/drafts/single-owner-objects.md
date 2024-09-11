
# OLM Ownership Enforcement for `ClusterExtensions`

In OLM, **a given object can only be owned by a single `ClusterExtension` at a time**. This ensures that resources within a Kubernetes cluster are managed consistently and prevents conflicts between multiple `ClusterExtensions` attempting to control the same object.

## Key Concept: Single Ownership

The core principle enforced by OLM is that each resource (object) can only have one `ClusterExtension` as its owner. This prevents overlapping or conflicting management by multiple `ClusterExtensions`, ensuring that each resource is uniquely associated with only one operator bundle.

## Implications of Single Ownership

### 1. Operator Bundles That Provide a CRD Can Only Be Installed Once
If an operator bundle provides a `CustomResourceDefinition` (CRD), it can only be installed once in the cluster. Attempting to install another `ClusterExtension` that provides the same CRD will result in a failure, as OLM prevents multiple ownership of the CRD resource.

### 2. `ClusterExtensions` Cannot Share Objects
OLM's single-owner policy means that **`ClusterExtensions` cannot share ownership of any objects**. If one `ClusterExtension` manages a specific resource (e.g., a custom resource or a service), another `ClusterExtension` cannot claim ownership of the same object. Any attempt to do so will be blocked by the system.

## Error Messages

When a conflict occurs due to multiple `ClusterExtensions` attempting to manage the same resource, `operator-controller` will return a clear error message, indicating the ownership conflict.

- **Example Error**:
  ```plaintext
  CustomResourceDefinition 'logfilemetricexporters.logging.openshift.io' already exists in namespace 'openshift-logging' and cannot be managed by operator-controller
  ```

This error message signals that the resource is already being managed by another `ClusterExtension` and cannot be reassigned or "shared."

## What This Means for You

- **Uniqueness of Operator Bundles**: Ensure that operator bundles providing the same CRDs are not installed more than once. This can prevent potential installation failures due to ownership conflicts.
- **Avoid Resource Sharing**: If you need different `ClusterExtensions` to interact with similar resources, ensure they are managing separate objects. `ClusterExtensions` cannot jointly manage the same resource due to the single-owner enforcement.
