# Mission

operator-controller is the central component of Operator Lifecycle Manager (OLM) v1. It extends Kubernetes with a declarative API for installing, upgrading, and managing cluster extensions. Together with catalogd, it provides a complete lifecycle management system for Kubernetes operators and, eventually, other workload types.

## Goals

1. Provide a declarative, GitOps-aligned API for installing and managing Kubernetes extensions
2. Replace OLM v0 with a simpler, more secure, and more predictable lifecycle management system
3. Give cluster admins minimal but sufficient controls to build desired cluster architectures
4. Serve operator catalog content reliably via catalogd
5. Support multi-arch container deployments (amd64, arm64, ppc64le, s390x)

## Non-Goals

- Replacing Helm or other generic package managers
- Providing a UI or dashboard

## Design Principles

- **Align with Kubernetes conventions:** Follow Kubernetes API design patterns and user assumptions
- **Declarative and GitOps-compatible:** All configuration expressed as Kubernetes resources
- **Security by default:** Non-root containers, distroless base images, least-privilege RBAC
- **Predictable upgrades:** Semantic versioning, phase-based rollouts, compatibility checks
- **Minimal surface area:** Expose only what cluster admins need; avoid unnecessary abstractions

## Development Practices

- All changes go through PRs with CI checks (unit tests, e2e tests, linting, API/CRD compatibility)
- DCO sign-off required on all commits
- Generated files (CRDs, deepcopy, manifests) must be committed alongside source changes
- Two-week cooldown on non-critical dependency updates
- Go version lags upstream to give integrators time to adopt
- Shell scripts and Makefiles must work on both macOS and Linux
