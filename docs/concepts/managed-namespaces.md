# Managed Namespaces

## What is a managed namespace?

When you create a ClusterExtension without specifying `spec.namespace`, operator-controller automatically creates and manages a namespace for the operator. The namespace name comes from the bundle's metadata or defaults to `<packageName>-system`.

When you specify `spec.namespace`, the namespace must already exist on the cluster and operator-controller installs into it without managing its lifecycle.

The mode is locked at creation time: you cannot switch between managed and user-provided after the ClusterExtension is created.

## Namespace resolution

In managed mode, the namespace name is resolved from bundle CSV annotations in this order:

1. `operatorframework.io/suggested-namespace-template`: the `metadata.name` field from the JSON template
2. `operators.operatorframework.io/suggested-namespace`: a plain string with the preferred name
3. `<packageName>-system`: convention fallback

## What belongs in a managed namespace

- The operator's own workloads (deployments, services, configmaps)
- The operator's RBAC resources (service accounts, roles, role bindings)
- CRDs and webhooks installed by the operator

## What does NOT belong in a managed namespace

- User application workloads
- Shared services used by multiple operators
- Persistent data that should survive operator uninstallation

## Deletion behavior

Deleting a ClusterExtension with a managed namespace **deletes the entire namespace and everything in it.** If you have created resources in the managed namespace that are not part of the operator, they will be lost.

If you need the namespace to persist beyond the operator's lifecycle, use `spec.namespace` to point at an existing namespace you manage yourself.

## PSA labels

If the bundle declares PSA requirements via `operatorframework.io/suggested-namespace-template`, those labels are applied to the managed namespace automatically. This ensures the namespace has the correct Pod Security Admission level for the operator's workloads without manual configuration.

## Drift protection

Managed namespaces are reconciled by the ClusterObjectSet controller. If someone manually modifies or removes labels that the controller owns (e.g., PSA labels from the template), they are automatically restored.

Labels or annotations added by other actors that don't conflict with controller-owned fields are preserved.
