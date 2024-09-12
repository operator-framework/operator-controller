
# OLM Ownership Enforcement for `ClusterExtensions`

In OLM, **a custom resource (or object) can only be owned by a single `ClusterExtension` at a time**. This ensures that resources within a Kubernetes cluster are managed consistently and prevents conflicts between multiple `ClusterExtensions` attempting to control the same resource.

## Key Concept: Single Ownership

The core principle enforced by OLM is that each resource can only have one `ClusterExtension` as its owner. This prevents overlapping or conflicting management by multiple `ClusterExtensions`, ensuring that each resource is uniquely associated with only one operator bundle.

## Implications of Single Ownership

### 1. Operator Bundles That Provide a CRD Can Only Be Installed Once

Operator bundles provide `CustomResourceDefinitions` (CRDs), which are part of a `ClusterExtension`. This means a bundle can only be installed once in a cluster. Attempting to install another bundle that provides the same CRDs will result in a failure, as each custom resource can have only one `ClusterExtension` as its owner.


### 2. `ClusterExtensions` Cannot Share Objects
OLM's single-owner policy means that **`ClusterExtensions` cannot share ownership of any resources**. If one `ClusterExtension` manages a specific resource (e.g., a `Deployment`, `CustomResourceDefinition`, or `Service`), another `ClusterExtension` cannot claim ownership of the same resource. Any attempt to do so will be blocked by the system.

## Error Messages

When a conflict occurs due to multiple `ClusterExtensions` attempting to manage the same resource, `operator-controller` will return a clear error message, indicating the ownership conflict.

- **Example Error**:
  ```plaintext
  CustomResourceDefinition 'logfilemetricexporters.logging.kubernetes.io' already exists in namespace 'kubernetes-logging' and cannot be managed by operator-controller
  ```

This error message signals that the resource is already being managed by another `ClusterExtension` and cannot be reassigned or "shared."

## What This Means for You

- **Uniqueness of Operator Bundles**: Ensure that operator bundles providing the same CRDs are not installed more than once. This can prevent potential installation failures due to ownership conflicts.
- **Avoid Resource Sharing**: If you need different `ClusterExtensions` to interact with similar resources, ensure they are managing separate resources. `ClusterExtensions` cannot jointly manage the same resource due to the single-owner enforcement.
