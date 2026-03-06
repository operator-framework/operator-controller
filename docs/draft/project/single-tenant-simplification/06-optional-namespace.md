# Work Item: Make spec.namespace optional with automatic namespace management

**Parent:** [Single-Tenant Simplification](../single-tenant-simplification.md)
**Status:** Not started
**Depends on:** [02-deprecate-service-account](02-deprecate-service-account.md)

## Summary

Change `ClusterExtension.spec.namespace` from required to optional. When not specified, operator-controller determines the installation namespace automatically using CSV annotations or a fallback convention. The installation namespace becomes a managed object of the ClusterExtension.

## Namespace determination precedence

operator-controller determines the installation namespace using the following precedence (highest to lowest):

1. **`ClusterExtension.spec.namespace`** — If specified by the user, this is the namespace name used.
2. **`operatorframework.io/suggested-namespace` CSV annotation** — If the CSV provides a suggested namespace name, it is used.
3. **`operatorframework.io/suggested-namespace-template` CSV annotation** — If the CSV provides a namespace template (a full JSON Namespace object), its `metadata.name` is used.
4. **`<packageName>-system` fallback** — If none of the above are present, operator-controller generates a namespace name from the package name. If `<packageName>-system` exceeds the maximum namespace name length, `<packageName>` is used instead.

## Namespace body/template

- If the CSV contains the `operatorframework.io/suggested-namespace-template` annotation, its value (a full JSON Namespace object) is used as the template for creating the namespace. This allows bundle authors to specify labels, annotations, and other namespace metadata.
- If `spec.namespace` or `suggested-namespace` specifies a different name than what appears in the template, the template body is still used but with the name overridden.
- If no template annotation is present, operator-controller creates a plain namespace with the determined name.

### Behavior when both annotations are present

When a CSV defines both `operatorframework.io/suggested-namespace` and `operatorframework.io/suggested-namespace-template`:

- The **name** comes from `suggested-namespace` (unless overridden by `spec.namespace`).
- The **body** (labels, annotations, other metadata) comes from `suggested-namespace-template`.
- If the template's `metadata.name` differs from the name determined by `suggested-namespace`, the template's name is overridden.

In other words, `suggested-namespace` controls naming and `suggested-namespace-template` controls the namespace shape. When both are present, the name from `suggested-namespace` takes precedence over any name embedded in the template.

## Namespace lifecycle

- The installation namespace is a **managed object** of the ClusterExtension. It follows the same ownership rules as all other managed objects:
  - It is created by operator-controller if it does not exist.
  - A pre-existing namespace results in a conflict error, consistent with the [single-owner objects](../../concepts/single-owner-objects.md) design.
  - It is deleted when the ClusterExtension is deleted (along with all other managed objects).
- The immutability constraint on `spec.namespace` is retained — once set (explicitly or by auto-determination), the namespace cannot be changed.

## Migration

- Existing ClusterExtensions that specify `spec.namespace` continue to function identically.
- For existing installations where the namespace was manually created before the ClusterExtension, operator-controller should adopt the namespace during the migration period (one-time reconciliation to add ownership metadata). The pre-existence error applies only to new installations going forward.

## Scope

### API changes

- Mark `spec.namespace` as optional in `api/v1/clusterextension_types.go` (remove the `required` validation or make the field a pointer).
- Update CRD/OpenAPI schema generation.

### Controller changes

- Add namespace determination logic implementing the precedence rules above.
- Add namespace creation/management as a reconciliation step (likely a new `ReconcileStepFunc` in `internal/operator-controller/controllers/clusterextension_reconcile_steps.go`).
- Read `operatorframework.io/suggested-namespace` and `operatorframework.io/suggested-namespace-template` from the resolved CSV.
- Handle the migration path: adopt pre-existing namespaces for existing installations.

### Key files

- `api/v1/clusterextension_types.go` — Make `Namespace` optional
- `internal/operator-controller/controllers/clusterextension_reconcile_steps.go` — New namespace management step
- `internal/operator-controller/controllers/clusterextension_controller.go` — Integration
- CSV annotation reading (location TBD based on where bundle metadata is accessed)
