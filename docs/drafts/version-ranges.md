# Operator version ranges

This document explains how to specify a version range to install or update an Operator with OLM 1.0.

You define an Operator's target version in its custom resource (CR) file.
The following list describes how OLM resolves an Operator's target version, and the resulting actions:

Not specifying a version in the CR
: Installs or updates the latest version of the Operator.
Updates are applied automatically when they are published to the catalog.

Specifying a version in the CR
: Installs or updates the specified version.
Updates are not applied automatically.
If you want to update the Operator, you must manually edit the CR and apply the changes to the cluster.

Specifying a channel
: Installs or updates the latest version of the Operator in the channel.
Updates are applied automatically when they are published to the specified channel.

Specifying a version range in the CR
: Installs or updates the latest version of the Operator within the version range.
Updates that are within the specified range are automatically installed.
Updates that are outside of the specified range are not installed.

The `spec.version` field uses the Masterminds `semver` package to enable the version range functionality.

OLM 1.0 does not support following methods for specifying a version range:

* Using tags and labels are not supported.
You must use semantic versioning to define a version range.
* Using [hypen range comparisons](https://github.com/Masterminds/semver#hyphen-range-comparisons) are not supported.
For example, the following range option is not supported:

  ```yaml
  version: 3.0 - 3.6
  ```

  To specify a range option, use a method similar to the following example:

  ```yaml
  version: >=3.0 <3.6
  ```

You can use the `x`, `X`, and `*` characters as wildcard characters in all comparison operations.

For more information about parsing, sorting, checking, and comparing version constraints, see the [Masterminds SemVer README](https://github.com/Masterminds/semver#semver).

## Example CRs that specify a version range
