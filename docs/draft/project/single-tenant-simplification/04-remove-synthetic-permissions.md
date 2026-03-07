# Work Item: Remove SyntheticPermissions feature gate and code

**Parent:** [Single-Tenant Simplification](../single-tenant-simplification.md)
**Status:** Not started
**Depends on:** Nothing (independent)

## Summary

Remove the `SyntheticPermissions` feature gate (currently Alpha, default off) and all associated synthetic user permission model code.

## Scope

- Remove the `SyntheticPermissions` feature gate definition.
- Remove the synthetic user REST config mapper and related code.
- Remove related tests.

## Key files

- `internal/operator-controller/features/` - Feature gate definitions
- `cmd/operator-controller/main.go:698-699` - Conditional wrapping with `SyntheticUserRestConfigMapper`
- Any code implementing the synthetic permissions model
