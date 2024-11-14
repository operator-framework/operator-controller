# Extension version ranges

This document explains how to specify a version range to install or update an extension with OLM 1.0.

You define a version range in a ClusterExtension's custom resource (CR) file.

### Specifying a version range in the CR

If you specify a version range in the ClusterExtension's CR, OLM 1.0 installs or updates the latest version of the extension that can be resolved within the version range.
The resolved version is the latest version of the extension that satisfies the dependencies and constraints of the extension and the environment.
Extension updates within the specified range are automatically installed if they can be resolved successfully.
Updates are not installed if they are outside of the specified range or if they cannot be resolved successfully.

### Comparisons

You define a version range by adding a comparison string to the `spec.version` field. A comparison string is composed of a list of comma or space separated values and one or more comparison operators. You can add an additional comparison string by including an OR (`||`) operator between the strings.

#### Basic comparisons

| Operator | Definition                         |
|----------|------------------------------------|
| `=`      | equal (not aliased to an operator) |
| `!=`     | not equal                          |
| `>`      | greater than                       |
| `<`      | less than                          |
| `>=`     | greater than or equal to           |
| `<=`     | less than or equal to              |

#### Range comparisons

To specify a version range, use a range comparison similar to the following example:

```yaml
version: >=3.0, <3.6
```

#### Wildcards in comparisons

You can use the `x`, `X`, and `*` characters as wildcard characters in all comparison operations.
If you use a wildcard character with the `=` operator, you define a patch level comparision.
This is equivalent to making a tilde range comparison.

*Example comparisons with wildcard characters*

| Comparison | Equivalent          |
|------------|---------------------|
| `1.2.x`    | `>= 1.2.0, < 1.3.0` |
| `>= 1.2.x` | `>= 1.2.0`          |
| `<= 2.x`   | `< 3`               |
| `*`        | `>= 0.0.0`          |


#### Patch release or tilde (`~`) range comparison

You can use the tilde (`~`) operator to make patch release comparisons.
This is useful when you want to specify a minor version up to the next major version.

*Example patch release comparisons*

| Comparison | Equivalent          |
|------------|---------------------|
| `~1.2.3`   | `>= 1.2.3, < 1.3.0` |
| `~1`       | `>= 1, <2`          |
| `~2.3`     | `>= 2.3, < 2.4`     |
| `~1.2.x`   | `>= 1.2.0, < 1.3.0` |
| `~1.x`     | `>= 1, < 2`         |


#### Major release or caret (`^`) range comparisons

You can use the caret (`^`) operator to make major release comparisons after a stable, `1.0.0`, version is published.
If you make a major release comparison before a stable version is published, minor versions define the API stability level.

*Example major release comparisons*

| Comparison | Equivalent                             |
|------------|----------------------------------------|
| `^1.2.3`   | `>= 1.2.3, < 2.0.0``>= 1.2.3, < 2.0.0` |
| `^1.2.x`   | `>= 1.2.0, < 2.0.0`                    |
| `^2.3`     | `>= 2.3, < 3`                          |
| `^2.x`     | `>= 2.0.0, < 3`                        |
| `^0.2.3`   | `>=0.2.3 <0.3.0`                       |
| `^0.2`     | `>=0.2.0 <0.3.0`                       |
| `^0.0.3`   | `>=0.0.3 <0.0.4`                       |
| `^0.0`     | `>=0.0.0 <0.1.0`                       |
| `^0`       | `>=0.0.0 <1.0.0`                       |
