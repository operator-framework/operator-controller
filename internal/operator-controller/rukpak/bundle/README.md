# Registry+v1 Bundle Configuration JSON Schema

This directory contains the JSON schema for registry+v1 bundle configuration validation.

## Overview

The `registryv1bundleconfig.json` schema is used to validate the bundle configuration in the ClusterExtension's inline configuration. This includes:

- `watchNamespace`: Controls which namespace(s) the operator watches for custom resources
- `deploymentConfig`: Customizes operator deployment (environment variables, resources, volumes, etc.)

The `deploymentConfig` portion is based on OLM v0's `SubscriptionConfig` struct but excludes the `selector` field which was never used in v0.

## Schema Generation

The schema in `registryv1bundleconfig.json` is a frozen snapshot that provides stability for validation. It is based on the `v1alpha1.SubscriptionConfig` type from `github.com/operator-framework/api/pkg/operators/v1alpha1/subscription_types.go`.

### Fields Included

- `nodeSelector`: Map of node selector labels
- `tolerations`: Array of pod tolerations
- `resources`: Container resource requirements (requests/limits)
- `envFrom`: Environment variables from ConfigMaps/Secrets
- `env`: Individual environment variables
- `volumes`: Pod volumes
- `volumeMounts`: Container volume mounts
- `affinity`: Pod affinity/anti-affinity rules
- `annotations`: Custom annotations for deployments/pods

### Fields Excluded

- `selector`: This field exists in v0's `SubscriptionConfig` but is never used by the v0 controller. It has been intentionally excluded from the v1 schema.

## Regenerating the Schema

To regenerate the schema when the `github.com/operator-framework/api` dependency is updated:

```bash
make update-registryv1-bundle-schema
```

This will regenerate the schema based on the current module-resolved version of `v1alpha1.SubscriptionConfig` from `github.com/operator-framework/api` (as determined via `go list -m`).

## Validation

The schema is used to validate user-provided bundle configuration (including `watchNamespace` and `deploymentConfig`) in ClusterExtension resources. The base schema is loaded and customized at runtime based on the operator's install modes to ensure proper validation of the `watchNamespace` field. Validation happens during:

1. **Admission**: When a ClusterExtension is created or updated
2. **Runtime**: When extracting configuration from the inline field

Validation errors provide clear, semantic feedback to users about what fields are invalid and why.
