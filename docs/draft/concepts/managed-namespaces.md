# Managed Namespaces

## What is a managed namespace?

> **Note:** Managed namespaces (omitting `spec.namespace`) are available only in the
> experimental feature set, which enables the `BoxcutterRuntime` feature gate. In the
> standard feature set, `spec.namespace` is required.

For registry+v1 bundles, when you create a ClusterExtension without specifying `spec.namespace`, operator-controller automatically creates and manages a namespace for the operator. The namespace name comes from the bundle's metadata or defaults to `<packageName>-system`.

When you specify `spec.namespace`, the namespace must already exist on the cluster and operator-controller installs into it without managing its lifecycle.

The mode is locked at creation time: you cannot switch between managed and user-provided after the ClusterExtension is created.

Managed mode requires the `BoxcutterRuntime` feature gate. Without it, omitting `spec.namespace` results in a terminal error, so you must set `spec.namespace` to an existing namespace instead.

> **Note:** The behavior described in this document applies to the registry+v1 bundle format. Other bundle formats are likely to handle namespace management differently — for example, by including namespace objects directly in their manifests. This points toward namespace configuration being bundle-format-specific rather than a top-level ClusterExtension concern.

## Namespace resolution

For registry+v1 bundles in managed mode, the namespace name is resolved from CSV annotations in this order:

1. `operatorframework.io/suggested-namespace-template`: the `metadata.name` field from the JSON template
2. `operatorframework.io/suggested-namespace`: a plain string with the preferred name
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
