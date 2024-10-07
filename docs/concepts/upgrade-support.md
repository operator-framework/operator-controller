---
hide:
  - toc
---

# Upgrade support

This document explains how OLM v1 handles upgrades.

OLM v1 introduces a simplified UX for package authors and package admins to implicitly define upgrade edges via [Semantic Versioning](https://semver.org/).

It also introduces an API to enable independently verified upgrades and downgrades.

## Upgrade constraint semantics

When determining upgrade edges, also known as upgrade paths or upgrade constraints, for an installed cluster extension, Operator Lifecycle Manager (OLM) v1 supports [legacy OLM semantics](https://olm.operatorframework.io/docs/concepts/olm-architecture/operator-catalog/creating-an-update-graph/) by default. This support follows the behavior from legacy OLM, including `replaces`, `skips`, and `skipRange` directives, with a few noted differences.

By supporting legacy OLM semantics, OLM v1 now honors the upgrade graph from catalogs accurately.

* If there are multiple possible successors, OLM v1 behavior differs in the following ways:
  * In legacy OLM, the successor closest to the channel head is chosen.
  * In OLM v1, the successor with the highest semantic version (semver) is chosen.
* Consider the following set of file-based catalog (FBC) channel entries:

  ```yaml
  # ...
  - name: example.v3.0.0
    skips: ["example.v2.0.0"]
  - name: example.v2.0.0
    skipRange: >=1.0.0 <2.0.0
  ```

If `1.0.0` is installed, OLM v1 behavior differs in the following ways:

  * Legacy OLM does not detect an upgrade edge to `v2.0.0` because `v2.0.0` is skipped and not on the `replaces` chain.
  * OLM v1 detects the upgrade edge because OLM v1 does not have a concept of a `replaces` chain. OLM v1 finds all entries that have a `replace`, `skip`, or `skipRange` value that covers the currently installed version.

You can change the default behavior of the upgrade constraints by setting the `upgradeConstraintPolicy` parameter in your cluster extension's custom resource (CR).

``` yaml hl_lines="10"
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: <extension_name>
spec:
  installNamespace: <namespace>
  packageName: <package_name>
  serviceAccount:
    name: <service_account>
  upgradeConstraintPolicy: SelfCertified
  version: "<version_or_version_range>"
```

where setting the `upgradeConstraintPolicy` to:

`SelfCertified`
:   Does not limit the next version to the set of successors, and instead allows for any downgrade, sidegrade, or upgrade.

`CatalogProvided`
:   Only allows the next version to come from the successors list. This is the default value. If the `upgradeConstraintPolicy` parameter is not defined in an extension's CR, then the policy is set to `CatalogProvided` by default.

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

As a package admin, if you must upgrade or downgrade to version that might be incompatible with the currently installed version, you can set the `.spec.upgradeConstraintPolicy` field to `SelfCertified` on the relevant `ClusterExtension` resource.

If you set the field to `SelfCertified`, no upgrade constraints are set on the package. As a result, you can change the version to any version available in the catalogs for a given package.

Example `ClusterExtension` with `.spec.upgradeConstraintPolicy` field set to `SelfCertified`:

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: extension-sample
spec:
  source:
    sourceType: Catalog
    catalog:
      packageName: argocd-operator
      version: 0.6.0
      upgradeConstraintPolicy: SelfCertified
  install:
    namespace: argocd
    serviceAccout:
      name: argocd-installer
```
