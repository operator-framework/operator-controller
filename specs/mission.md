# Mission

## Overview

operator-controller is the central component of Operator Lifecycle Manager (OLM) v1. It provides APIs, controllers, and tooling for packaging, distributing, and managing the lifecycle of Kubernetes cluster extensions - bundles of arbitrary Kubernetes objects (cluster-scoped or namespace-scoped) that extend cluster functionality.

OLM v1 consists of two components:
- **operator-controller** - the core lifecycle management controller
- **catalogd** - the catalog serving component

## Goals

1. **Align with Kubernetes designs and user assumptions** - APIs and controllers follow standard Kubernetes patterns; CRDs and controllers are treated as trusted cluster extensions with global API registration.
2. **Provide secure, predictable user experiences centered around declarative GitOps concepts** - GitOps-friendly APIs with declarative, eventually-consistent behavior. Secure by default (no cluster-admin permissions; user-supplied service accounts for installs).
3. **Give cluster admins minimal necessary controls** - Fine-grained version pinning, upgrade control per extension, and optional guardrails with escape hatches. Admins have ultimate control over their cluster architecture.
4. **Support packaging, distribution, and lifecycling of cluster extensions** - Install, upgrade, and delete bundles of arbitrary Kubernetes objects. Automated upgrades, health monitoring, CRD upgrade safety checks, and constraint checking.
5. **Complement on-cluster APIs with official CLI tooling** - On-cluster APIs cover 100% of use cases; the CLI covers standard ~80% workflows. Advanced use cases interact directly with cluster APIs.

## Non-Goals

- **Multi-tenancy** - Kubernetes APIs are global; multi-tenancy promises made by OLM v0 were infeasible due to the global API system. OLM v1 does not design around multi-tenant control planes.
- **Multi-cluster extension distribution** - OLM v1 manages extensions within a single cluster.
- **Namespace-specific controller configurations** - No first-class API for configuring watched namespaces. OLM v1 assumes controllers reconcile objects cluster-wide.
- **Automatic dependency resolution and installation** - OLM v1 performs constraint checking (are dependencies met?) but does not auto-install missing dependencies. Predictability over magic.
- **Covering complex/advanced scenarios in the CLI** - The CLI handles common workflows. Complex use cases use the on-cluster APIs directly.

## Design Principles

1. **Do not fight Kubernetes** - Work with Kubernetes's global API registration, ownership model, and reconciliation patterns. API registration is cluster-scoped; OLM v1 enforces single-owner semantics for managed objects.
2. **Secure by default** - No cluster-admin permissions for OLM itself. User-supplied service accounts authorize installs/upgrades. Secure communication between all components.
3. **Simple and predictable semantics** - Two primary APIs (catalogs and install intent). Declarative, eventually-consistent behavior. Avoid the complexity that made OLM v0 difficult to reason about.
4. **Opinionated guardrails with escape hatches** - CRD upgrade safety checks, upgrade-edge enforcement, and version constraints are on by default. Admins can disable any guardrail.
5. **Extension-agnostic packaging** - Bundles can contain any Kubernetes objects, not just operator-pattern controllers. A bundle with a Deployment + Service + Ingress is as valid as one with CRDs and controllers.
6. **Constraint checking, not dependency management** - Check whether preconditions are met and report unmet constraints. Do not auto-install or auto-manage dependency trees.

## Development Practices

- All PRs must pass CI: lint (`make lint`), unit tests (`make test-unit`), and e2e tests (`make test-e2e`)
- Generated code must be up-to-date: run `make verify` before submitting
- API changes require CRD compatibility checks (`make verify-crd-compatibility`)
- Code formatting enforced via `make fmt` (yamlfmt, gofmt)
- Mock generation via mockgen; managed by bingo for version consistency
- Helm chart linting via `make lint-helm`
