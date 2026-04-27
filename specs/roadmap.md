# Roadmap

This document captures the historical evolution of operator-controller in phases, followed by open next steps for the team to define.

## Phase 1: Foundation (Dec 2022 - Jun 2023)

- Kubebuilder project initialization and Operator API (cluster-scoped CRD)
- Deppy-based resolution engine: bundle entities, variable sources, solver integration
- BundleDeployment creation via server-side apply
- Status conditions, e2e test framework, CI workflows (unit-test, e2e, linting)
- Catalogd integration (v0.2.0+) for entity sourcing
- Feature gate infrastructure, bingo tooling, codecov integration
- plain+v0 bundle type support, Tilt dev environment

## Phase 2: API Stabilization & Resolution Overhaul (Jul 2023 - Jan 2024)

- Upgrade edge support and replace-based upgrades
- SemVer upgrade constraint support with `upgradeConstraintPolicy` field
- Switch from CatalogMetadata API to catalogd HTTP server for resolution
- New `catalogmetadata` package replacing entities/entity sources
- Operator API renamed to ClusterExtension API
- Channel support (`spec.channel`) and version range filtering
- Cross-component e2e tests, DCO compliance
- Deppy solver upgrades, bundle deprecation logic

## Phase 3: Extension API & Helm Applier (Feb 2024 - Oct 2024)

- Extension API introduction (namespaced, with source union discriminator)
- ClusterExtension reworked to use Helm under the hood for manifest application
- ValidatingAdmissionPolicy for package uniqueness
- CRD upgrade safety checks (skip option via API field)
- Removal of Deppy solver, resolution CLI, plain+v0 bundle support
- Go 1.22 upgrade, k8s v0.30 dependency update
- Catalogd TLS overlay, improved status conditions and error types
- API v1 promotion, status.resolution removal, PullSecret controller
- GitHub issue forms, CODEOWNERS, release process improvements

## Phase 4: Configuration, Webhooks & Security (Nov 2024 - Jun 2025)

- API audit and breaking changes (spec.install.namespace/serviceAccount moved to top-level)
- Webhook support: certificate generation, rule validation, namespace selectors
- Network policies with namespace-wide default deny for ingress/egress
- RBAC preflight checks for install permissions
- Prometheus metrics, performance alerting, API call alerts
- OCI Helm chart deployment support
- CRD generator update for experimental CRD variants (standard vs experimental manifests)
- ServiceAccount pull secrets support
- k8s 1.33 upgrade, controller-runtime updates
- Experimental manifest generation and release pipeline

## Phase 5: Boxcutter Runtime & Revision Management (Jul 2025 - Dec 2025)

- Boxcutter runtime implementation (package-operator.run integration)
- ClusterExtensionRevision API: revision lifecycle, conditions, status propagation
- Helm-based deployment configuration (replacing kustomize-based config)
- ClusterExtensionConfig API (`spec.config`)
- ClusterExtension reconciler refactored to composable step-based pipeline
- Memory optimization: caching, transforms, reduced allocations
- Prometheus alert threshold calibration via memory profiling
- E2e profiling toolchain (heap and CPU analysis)
- Label-based cache for revision lookups
- JSONSchema validation for bundle configuration
- TLS profile support (Mozilla-based)
- Revision manifest sanitization, collision protection
- e2e tests refactored for feature-gate aware skipping
- AGENTS.md for AI coding assistant guidance

## Phase 6: Maturation & Operational Hardening (Jan 2026 - Present)

- ClusterExtensionRevision renamed to ClusterObjectSet
- Phase object externalization into Secrets (large bundle support)
- DeploymentConfig feature gate: deployment customization for registry+v1 bundles
- Progression probes: namespace, PVC, and availability probes for revision health
- ApplyConfiguration types for server-side apply
- Validation framework with ServiceAccount and object support validators
- HA-ready deployments with configurable replica count
- RBACPreAuthorizer configurable collection verbs
- Least-privilege RBAC replacing cluster-admin for BoxcutterRuntime
- Boxcutter upgrades (v0.9.0 through v0.13.1) with collision detection and preflight checks
- E2e migration to Godog/Cucumber BDD framework
- Single/Own Namespace install mode (promoted to GA, reverted to alpha, iterated)
- TLS profile updates (Mozilla v5.8), DNS1123 validation fixes
- Orphaned temp dir cleanup in catalog cache and storage

## Next Steps

Future work is tracked via GitHub issues on the upstream repository rather than phases in this file. The `/sdd-plan-next-phase` command discovers the next available epic automatically.

An issue is eligible for work when it has:
- Labels: `epic` and `refined`
- No assignee
- No unresolved dependencies (all linked blocking issues have the `done` label)
