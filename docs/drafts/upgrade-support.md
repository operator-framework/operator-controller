# Upgrade support

This document explains how OLM 1.0 handles upgrades.

OLM 1.0 introduces a simplified UX for package authors and package admins to implicitly define upgrade edges via [Semantic Versioning](https://semver.org/).

It also introduces an API to enable independently verified upgrades and downgrades.

## Upgrade constraint semantics

As of operator-controller release 0.10.0, OLM 1.0 supports the following upgrade constraint semantics:

* [Semantic Versioning](https://semver.org/) (Semver)
* [Legacy OLM 0 semantics](https://olm.operatorframework.io/docs/concepts/olm-architecture/operator-catalog/creating-an-update-graph/#methods-for-specifying-updates): the `replaces`/`skips`/`skipRange` directives

The Kubernetes manifests in this repo enable legacy support by default. Cluster admins can control which semantics to use by passing one of the following arguments to the `manager` binary:
* `--feature-gates=ForceSemverUpgradeConstraints=true` - enable Semver
* `--feature-gates=ForceSemverUpgradeConstraints=false` - disable Semver, use legacy semantics

For example, to enable Semver update the `controller-manager` Deployment manifest to include the following argument:

```yaml
- command:
  - /manager
  args:
  - --feature-gates=ForceSemverUpgradeConstraints=true
  image: controller:latest
```

In a future release, it is planned to remove the `ForceSemverUpgradeConstraints` feature gate and allow package authors to specify upgrade constraint semantics at the catalog level.

## Upgrades

OLM supports Semver to provide a simplified way for package authors to define compatible upgrades. According to the Semver standard, releases within a major version (e.g. `>=1.0.0 <2.0.0`) must be compatible. As a result, package authors can publish a new package version following the Semver specification, and OLM assumes compatibility. Package authors do not have to explicitly define upgrade edges in the catalog.

> [!NOTE]
> Currently, OLM 1.0 does not support automatic upgrades to the next major version. You must manually verify and perform major version upgrades. For more information about major version upgrades, see [Manually verified upgrades and downgrades](#manually-verified-upgrades-and-downgrades).

### Upgrades within the major version zero

According to the Semver specification, a major version zero release is for initial development. It is assumed that breaking changes might be introduced at any time. As a result, the following special conditions apply to upgrades within a major version zero release:

* You cannot automatically upgrade from one patch version to another when both major and minor versions are `0`. For example, automatic upgrades within the following version range are not allowed: `>= 0.0.1 <0.1.0`.
* You cannot automatically upgrade from one minor version to another minor version within the major version zero. For example, no upgrades from `0.1.0` to `0.2.0`. However, you can upgrade from patch versions. For example, upgrades are possible in ranges `>= 0.1.0 <0.2.0`, `>= 0.2.0 <0.3.0`, `>= 0.3.0 <0.4.0`, and so on.

You must verify and perform upgrades manually in cases where automatic upgrades are blocked.

## Manually verified upgrades and downgrades

**Warning:** If you want to force an upgrade manually, you must thoroughly verify the outcome before applying any changes to production workloads. Failure to test and verify the upgrade might lead to catastrophic consequences such as data loss.

As a package admin, if you must upgrade or downgrade to version that might be incompatible with the currently installed version, you can set the `.spec.upgradeConstraintPolicy` field to `Ignore` on the relevant `ClusterExtension` resource.

If you set the field to `Ignore`, no upgrade constraints are set on the package. As a result, you can change the version to any version available in the catalogs for a given package.

Example `ClusterExtension` with `.spec.upgradeConstraintPolicy` field set to `Ignore`:

```yaml
apiVersion: olm.operatorframework.io/v1alpha1
kind: ClusterExtension
metadata:
  name: extension-sample
spec:
  packageName: argocd-operator
  version: 0.6.0
  upgradeConstraintPolicy: Ignore
```
